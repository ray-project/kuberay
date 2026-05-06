package historyserver

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// processor is the narrow subset of *SessionProcessor that the
// SessionLoader depends on.
type processor interface {
	ProcessSession(ctx context.Context, info utils.ClusterInfo) (SessionStatus, error)
}

// SessionLoader ensures a session is ready to serve before the router
// dispatches handler logic. There are three outcomes:
//
//  1. Live cluster: skip loading; caller redirects to the Ray Dashboard.
//  2. Dead, already loaded: fast-path; events are already in in-memory maps.
//  3. Dead, not yet loaded: drive SessionProcessor to ingest events into the maps.
//
// Concurrent callers for the same session are coalesced via singleflight.
//
// A single SessionLoader is safe to share across all HTTP handlers for one
// replica; its mutable state lives inside singleflight.Group and the
// loaded set.
type SessionLoader struct {
	processor processor
	sf        singleflight.Group
	loadedMu  sync.RWMutex
	loaded    map[string]struct{}
	serverCtx context.Context
}

// NewSessionLoader wires a SessionLoader. All arguments must be non-nil.
// serverCtx is the server-lifetime context used to cancel in-flight work
// on shutdown.
func NewSessionLoader(p processor, serverCtx context.Context) *SessionLoader {
	return &SessionLoader{
		processor: p,
		loaded:    make(map[string]struct{}),
		serverCtx: serverCtx,
	}
}

// LoadSession blocks until the session is ready to serve for this replica
// or an unrecoverable error is observed. Concurrent callers for the same
// session are coalesced via singleflight.
//
// Returns:
//   - (false, nil): events loaded; router serves the historical session.
//   - (true,  nil): live cluster; router rewrites the session-name cookie to "live".
//   - (_, ctx.Err()): caller's ctx died while waiting. Shared work keeps running.
//   - (_, other error): unrecoverable error.
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
		// Release the caller; the winner keeps running and its result is shared
		// for the next caller of this session.
		//
		// Do NOT sf.Forget(key) here: a racing new call would kick off a second
		// processor execution in parallel with the still-running winner.
		return false, ctx.Err()
	case result := <-ch:
		if result.Err != nil {
			return false, result.Err
		}
		live, _ := result.Val.(bool)
		return live, nil
	}
}

// doLoadSession checks if the session belongs to a live cluster and drives
// the processor to ingest events into the maps for dead sessions.
//
// Returns:
//   - (false, nil): events loaded, either a fast-path hit or events ingested.
//   - (true, nil): session belongs to a live cluster; not added to the loaded set.
//   - (_, err): processor failed.
func (s *SessionLoader) doLoadSession(ctx context.Context, info utils.ClusterInfo, key string) (bool, error) {
	// Guard against a race where loaded gets marked between the caller's
	// lookup and singleflight execution.
	s.loadedMu.RLock()
	_, loaded := s.loaded[key]
	s.loadedMu.RUnlock()
	if loaded {
		return false, nil
	}
	// Synchronously process the session raw events.
	status, perr := s.processor.ProcessSession(ctx, info)
	if perr != nil {
		return false, perr
	}

	switch status {
	case SessionStatusProcessed:
		// Mark the session as loaded so subsequent /enter_cluster calls
		// are no-ops on this replica.
		s.loadedMu.Lock()
		s.loaded[key] = struct{}{}
		s.loadedMu.Unlock()
		return false, nil

	case SessionStatusLive:
		// Session belongs to a live cluster. Handler proxies to the Ray Dashboard.
		return true, nil

	default:
		// Unreachable under the SessionProcessor contract; defensive guard so a future
		// bug surfaces as a clear 500 instead of a silent success.
		return false, fmt.Errorf("unexpected session status %v", status)
	}
}
