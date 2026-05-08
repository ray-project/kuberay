package historyserver

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// processor is an interface to enable mocking SessionProcessor in tests.
type processor interface {
	ProcessSession(ctx context.Context, info utils.ClusterInfo) (SessionStatus, error)
}

// SessionLoader ensures a dead session is loaded into in-memory maps.
// Concurrent callers for the same session are coalesced via singleflight.
type SessionLoader struct {
	processor processor
	sf        singleflight.Group
	loadedMu  sync.RWMutex
	loaded    map[string]struct{}
	serverCtx context.Context
}

// NewSessionLoader wires a SessionLoader.
func NewSessionLoader(p processor, serverCtx context.Context) *SessionLoader {
	return &SessionLoader{
		processor: p,
		loaded:    make(map[string]struct{}),
		serverCtx: serverCtx,
	}
}

// LoadSession blocks until the session is ready to serve for this replica.
// live is true when the cluster is still alive.
func (s *SessionLoader) LoadSession(ctx context.Context, info utils.ClusterInfo) (live bool, err error) {
	// Fast pre-flight: skip singleflight entirely if ctx is already dead.
	if err := ctx.Err(); err != nil {
		return false, err
	}

	clusterNameID := info.Name + "_" + info.Namespace
	key := clusterNameID + "/" + info.SessionName

	// Fast-path: session is already loaded.
	s.loadedMu.RLock()
	_, loaded := s.loaded[key]
	s.loadedMu.RUnlock()
	if loaded {
		return false, nil
	}

	// TODO(jwj): Graceful drain if needed. Currently SIGTERM immediately cancels
	// in-flight work. If 500-during-shutdown becomes a customer pain point, switch
	// closure to a separate runCtx with grace timer.
	ch := s.sf.DoChan(key, func() (interface{}, error) {
		return s.doLoadSession(s.serverCtx, info, key)
	})

	select {
	case <-ctx.Done():
		// Release the caller; the shared singleflight call keeps running and
		// its result will be available to the next caller for this session.
		//
		// Do NOT sf.Forget(key) here: a racing new call would kick off a second
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
func (s *SessionLoader) doLoadSession(ctx context.Context, info utils.ClusterInfo, key string) (live bool, err error) {
	s.loadedMu.RLock()
	_, loaded := s.loaded[key]
	s.loadedMu.RUnlock()
	if loaded {
		return false, nil
	}

	status, err := s.processor.ProcessSession(ctx, info)
	if err != nil {
		return false, err
	}

	switch status {
	case SessionStatusProcessed:
		s.loadedMu.Lock()
		s.loaded[key] = struct{}{}
		s.loadedMu.Unlock()
		return false, nil

	case SessionStatusLive:
		return true, nil

	default:
		// Unreachable under the SessionProcessor contract; defensive guard against
		// future SessionStatus additions.
		return false, fmt.Errorf("unexpected session status %v", status)
	}
}
