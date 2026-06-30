package historyserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/singleflight"

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
	// DefaultSessionCacheMaxBytes bounds the total bytes of cached dead-session snapshots.
	// 0 disables the byte bound.
	//
	// This is a soft bound on the idle resident cache, not a hard cap on process memory.
	// Real usage can exceed it in three ways:
	//   - add-then-evict: cache momentarily holds oldTotal + newEntry
	//   - one large session: a single snapshot bigger than the whole budget is kept
	//   - per-request decode: every GET unmarshals cached bytes into a full *SessionSnapshot
	DefaultSessionCacheMaxBytes = 256 << 20
)

// processor is an interface to enable mocking SessionProcessor in tests.
type processor interface {
	ProcessSession(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error)
}

// SessionLoader caches dead-session snapshots in a size-bounded LRU with optional
// TTL expiry and triggers session processing on cache miss. Concurrent callers
// for the same session are coalesced via singleflight.
type SessionLoader struct {
	processor      processor
	cache          *expirable.LRU[string, []byte]
	maxBytes       int
	mu             sync.Mutex
	sf             singleflight.Group
	serverCtx      context.Context
	processTimeout time.Duration
}

// NewSessionLoader wires a SessionLoader.
func NewSessionLoader(p processor, serverCtx context.Context, processTimeout time.Duration, cacheSize, cacheMaxBytes int, cacheTTL time.Duration) *SessionLoader {
	return &SessionLoader{
		processor:      p,
		cache:          expirable.NewLRU[string, []byte](cacheSize, nil, cacheTTL),
		maxBytes:       cacheMaxBytes,
		serverCtx:      serverCtx,
		processTimeout: processTimeout,
	}
}

// GetSnapshot returns a per-request view of the cached snapshot, which is
// a freshly decoded copy, safe for concurrent use.
func (s *SessionLoader) GetSnapshot(clusterSessionKey string) (*eventserver.SessionSnapshot, bool) {
	encoded, ok := s.cache.Get(clusterSessionKey)
	if !ok {
		return nil, false
	}
	s.renewTTL(clusterSessionKey)

	snap, err := decodeSnapshot(encoded)
	if err != nil {
		// A corrupt entry should be impossible since we encoded it ourselves.
		// If it ever happens, report a miss so it can be re-processed.
		logrus.Errorf("Dropping corrupt cache entry for session %q: %v", clusterSessionKey, err)
		s.mu.Lock()
		s.cache.Remove(clusterSessionKey)
		s.mu.Unlock()
		return nil, false
	}
	return snap, true
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

// putSnapshot stores encoded session snapshot bytes in the LRU cache.
func (s *SessionLoader) putSnapshot(clusterSessionKey string, snap *eventserver.SessionSnapshot) error {
	encoded, err := encodeSnapshot(snap)
	if err != nil {
		logrus.Errorf("Failed to encode snapshot for session %q: %v", clusterSessionKey, err)
		return fmt.Errorf("encode snapshot for session %q: %w", clusterSessionKey, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache.Add(clusterSessionKey, encoded)
	s.evictToByteBudget()
	return nil
}

// evictToByteBudget evicts LRU entries until the total cached bytes < maxBytes.
func (s *SessionLoader) evictToByteBudget() {
	if s.maxBytes <= 0 {
		return
	}
	total := s.totalBytes()
	for total > s.maxBytes && s.cache.Len() > 1 {
		_, evicted, ok := s.cache.RemoveOldest()
		if !ok {
			logrus.Errorf("byte-budget eviction stalled: RemoveOldest failed with %d entries, %d bytes (budget %d)",
				s.cache.Len(), total, s.maxBytes)
			break
		}
		total -= len(evicted)
	}
	if total > s.maxBytes {
		if s.cache.Len() == 1 {
			logrus.Warnf("single cached snapshot exceeds byte budget (%d > %d bytes); keeping it", total, s.maxBytes)
		} else {
			logrus.Errorf("cache still over byte budget after eviction (%d > %d bytes, %d entries)",
				total, s.maxBytes, s.cache.Len())
		}
	}
}

// totalBytes sums the length of every cached entry.
func (s *SessionLoader) totalBytes() int {
	total := 0
	for _, encoded := range s.cache.Values() {
		total += len(encoded)
	}
	return total
}

// encodeSnapshot serializes a snapshot to its cached byte form.
//
// Use JSON, not gob since snapshots have map[string]any CustomFields and gob requires
// registering concrete types and fails to round-trip the arbitrary nested values reliably.
func encodeSnapshot(snap *eventserver.SessionSnapshot) ([]byte, error) {
	return json.Marshal(snap)
}

// decodeSnapshot reconstructs a snapshot from its cached byte form.
func decodeSnapshot(encoded []byte) (*eventserver.SessionSnapshot, error) {
	var snap eventserver.SessionSnapshot
	if err := json.Unmarshal(encoded, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}
