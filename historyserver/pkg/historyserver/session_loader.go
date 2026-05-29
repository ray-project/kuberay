package historyserver

import (
	"context"
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const (
	// DefaultSessionProcessTimeout caps how long cold-load for a single session can run.
	DefaultSessionProcessTimeout = 2 * time.Minute
	// DefaultSessionCacheSize is the LRU capacity for dead-session snapshots.
	DefaultSessionCacheSize = 100
)

// processor is an interface to enable mocking SessionProcessor in tests.
type processor interface {
	ProcessSession(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error)
}

// SessionLoader caches dead-session snapshots in an LRU and triggers session
// processing on cache miss. Concurrent callers for the same session are
// coalesced via singleflight.
type SessionLoader struct {
	processor      processor
	cache          *lru.Cache[string, *eventserver.SessionSnapshot]
	sf             singleflight.Group
	serverCtx      context.Context
	processTimeout time.Duration
}

// NewSessionLoader wires a SessionLoader.
func NewSessionLoader(p processor, serverCtx context.Context, processTimeout time.Duration, cacheSize int) (*SessionLoader, error) {
	c, err := lru.New[string, *eventserver.SessionSnapshot](cacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}
	return &SessionLoader{
		processor:      p,
		cache:          c,
		serverCtx:      serverCtx,
		processTimeout: processTimeout,
	}, nil
}

// GetSnapshot returns the cached snapshot for a dead session.
func (s *SessionLoader) GetSnapshot(clusterSessionKey string) (*eventserver.SessionSnapshot, bool) {
	return s.cache.Get(clusterSessionKey)
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
		s.putSnapshot(clusterSessionKey, snap)
		return false, nil

	case SessionStatusLive:
		return true, nil

	default:
		// The zero-value guard prevents an uninitialized status from being silently
		// treated as Live or Processed.
		return false, fmt.Errorf("unexpected session status %v", status)
	}
}

// putSnapshot stores a dead-session snapshot in the LRU cache.
func (s *SessionLoader) putSnapshot(clusterSessionKey string, snap *eventserver.SessionSnapshot) {
	s.cache.Add(clusterSessionKey, snap)
}
