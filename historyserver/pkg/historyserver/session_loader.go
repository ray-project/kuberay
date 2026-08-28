package historyserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const (
	// DefaultSessionProcessTimeout caps how long cold-load for a single session can run.
	DefaultSessionProcessTimeout = 2 * time.Minute
	// DefaultSessionCacheSize is the LRU capacity for dead-session snapshots.
	DefaultSessionCacheSize = 100
	// DefaultSessionCacheTTL is how long a dead-session snapshot stays cached after last access.
	// 0 disables expiry.
	DefaultSessionCacheTTL time.Duration = 0
	// DefaultSessionCacheMaxMemory bounds the memory held by cached dead-session
	// snapshots, as a Kubernetes quantity. "0" disables the bound.
	//
	// This is a soft bound on the idle resident cache, not a hard cap on process memory.
	// Real usage can exceed it in three ways:
	//   - add-then-evict: cache momentarily holds oldTotal + newEntry
	//   - one large session: a single snapshot bigger than the whole budget is kept
	//   - size proxy: entries are measured by JSON length, which undercounts the live Go object graph
	DefaultSessionCacheMaxMemory = "2Gi"
)

// ParseSessionCacheMaxMemory converts a Kubernetes quantity into a
// byte count.
func ParseSessionCacheMaxMemory(s string) (int, error) {
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a valid resource quantity: %w", s, err)
	}
	if q.Sign() < 0 {
		return 0, fmt.Errorf("%q cannot be negative", s)
	}
	if q.CmpInt64(math.MaxInt) > 0 {
		return 0, fmt.Errorf("%q exceeds the maximum of %d bytes", s, math.MaxInt)
	}
	return int(q.Value()), nil
}

// processor is an interface to enable mocking SessionProcessor in tests.
type processor interface {
	ProcessSession(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error)
}

// cacheEntry is a cached snapshot plus its JSON size for the byte budget.
type cacheEntry struct {
	snap *eventserver.SessionSnapshot
	size int
}

// SessionLoader caches dead-session snapshots in a size-bounded LRU with optional
// TTL expiry and triggers session processing on cache miss. Concurrent callers
// for the same session are coalesced via singleflight.
type SessionLoader struct {
	processor processor
	cache     *expirable.LRU[string, cacheEntry]
	maxBytes  int
	// mu guards only the byte-budget read-modify-write; expirable.LRU is
	// independently thread-safe. A lone Get/Add/Peek does not need mu.
	mu             sync.Mutex
	sf             singleflight.Group
	serverCtx      context.Context
	processTimeout time.Duration
}

// NewSessionLoader wires a SessionLoader.
func NewSessionLoader(p processor, serverCtx context.Context, processTimeout time.Duration, cacheSize, cacheMaxBytes int, cacheTTL time.Duration) *SessionLoader {
	return &SessionLoader{
		processor:      p,
		cache:          expirable.NewLRU[string, cacheEntry](cacheSize, nil, cacheTTL),
		maxBytes:       cacheMaxBytes,
		serverCtx:      serverCtx,
		processTimeout: processTimeout,
	}
}

// GetSnapshot returns the cached snapshot. It is shared by all callers and
// must be treated as read-only.
func (s *SessionLoader) GetSnapshot(clusterSessionKey string) (*eventserver.SessionSnapshot, bool) {
	entry, ok := s.cache.Get(clusterSessionKey)
	if !ok {
		return nil, false
	}
	s.renewTTL(clusterSessionKey)
	return entry.snap, true
}

// renewTTL extends ExpiresAt for a cache hit.
//
// Must not re-insert after a concurrent byte eviction.
func (s *SessionLoader) renewTTL(clusterSessionKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.cache.Peek(clusterSessionKey); ok {
		s.cache.Add(clusterSessionKey, v)
	}
}

// LoadSession blocks until a dead session is processed and cached or an
// unrecoverable error is observed.
func (s *SessionLoader) LoadSession(ctx context.Context, info utils.ClusterInfo) (live bool, err error) {
	// Fast pre-flight: skip singleflight entirely if ctx is already dead.
	if err := ctx.Err(); err != nil {
		return false, err
	}

	clusterSessionKey := utils.BuildClusterSessionKey(info.Name, info.Namespace, info.SessionName)
	if _, ok := s.cache.Get(clusterSessionKey); ok {
		return false, nil
	}

	// TODO(jiangjiawei1103): No graceful drain on shutdown. When the pod receives
	// SIGTERM, serverCtx is cancelled immediately, causing any in-flight cold-load
	// requests to return ctx.Err() and clients to receive HTTP 500.
	ch := s.sf.DoChan(clusterSessionKey, func() (interface{}, error) {
		loadCtx, cancel := context.WithTimeout(s.serverCtx, s.processTimeout)
		defer cancel()
		return s.doLoadSession(loadCtx, info, clusterSessionKey)
	})

	select {
	case <-ctx.Done():
		// Release the caller; the singleflight winner keeps running and its
		// result will be cached for the next caller for this session.
		//
		// Do NOT sf.Forget(clusterSessionKey) here: a racing new call would kick off a second
		// processor execution in parallel with the still-running one.
		return false, ctx.Err()
	case result := <-ch:
		if result.Err != nil {
			return false, result.Err
		}
		live, _ := result.Val.(bool)
		return live, nil
	}
}

// doLoadSession is the singleflight body invoked by LoadSession.
// live is true when the cluster is still alive.
func (s *SessionLoader) doLoadSession(ctx context.Context, info utils.ClusterInfo, clusterSessionKey string) (live bool, err error) {
	if _, ok := s.cache.Get(clusterSessionKey); ok {
		return false, nil
	}

	status, snap, err := s.processor.ProcessSession(ctx, info)
	if err != nil {
		return false, err
	}

	switch status {
	case SessionStatusProcessed:
		if snap == nil {
			return false, fmt.Errorf("unexpected nil snapshot for session status %v", status)
		}
		if err := s.putSnapshot(clusterSessionKey, snap); err != nil {
			return false, err
		}
		return false, nil

	case SessionStatusLive:
		return true, nil

	default:
		// The zero-value guard prevents an uninitialized status from being silently
		// treated as Live or Processed.
		return false, fmt.Errorf("unexpected session status %v", status)
	}
}

// putSnapshot caches a snapshot. It is marshaled once only to measure its size.
func (s *SessionLoader) putSnapshot(clusterSessionKey string, snap *eventserver.SessionSnapshot) error {
	encoded, err := json.Marshal(snap)
	if err != nil {
		logrus.Errorf("Failed to encode snapshot for session %q: %v", clusterSessionKey, err)
		return fmt.Errorf("encode snapshot for session %q: %w", clusterSessionKey, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache.Add(clusterSessionKey, cacheEntry{snap: snap, size: len(encoded)})
	s.evictToByteBudget()
	return nil
}

// evictToByteBudget evicts LRU entries until the total cached bytes < maxBytes.
func (s *SessionLoader) evictToByteBudget() {
	if s.maxBytes <= 0 {
		return
	}
	// Recompute after each removal: RemoveOldest may drop TTL-expired entries that totalBytes skips.
	for s.totalBytes() > s.maxBytes && s.cache.Len() > 1 {
		if _, _, ok := s.cache.RemoveOldest(); !ok {
			logrus.Errorf("byte-budget eviction stalled: RemoveOldest failed with %d entries, %d bytes (budget %d)",
				s.cache.Len(), s.totalBytes(), s.maxBytes)
			break
		}
	}
	if total := s.totalBytes(); total > s.maxBytes {
		if s.cache.Len() == 1 {
			logrus.Warnf("single cached snapshot exceeds byte budget (%d > %d bytes); keeping it", total, s.maxBytes)
		} else {
			logrus.Errorf("cache still over byte budget after eviction (%d > %d bytes, %d entries)",
				total, s.maxBytes, s.cache.Len())
		}
	}
}

// totalBytes sums the size of every cached entry.
func (s *SessionLoader) totalBytes() int {
	total := 0
	for _, entry := range s.cache.Values() {
		total += entry.size
	}
	return total
}
