package historyserver

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// sessionPipeline is the narrow subset of *Pipeline that the
// Supervisor depends on.
type sessionPipeline interface {
	ProcessSession(ctx context.Context, info utils.ClusterInfo) (SessionStatus, error)
}

// Supervisor blocks /enter_cluster until the session's events are loaded
// for this replica, deduping concurrent callers via singleflight.
//
// A single Supervisor is safe to share across all HTTP handlers for one
// replica; its mutable state lives inside singleflight.Group and the
// loaded set.
type Supervisor struct {
	pipeline sessionPipeline
	// sf coalesces concurrent Ensure calls for the same session.
	sf singleflight.Group
	// loaded records sessions already ingested for this replica so repeat
	// /enter_cluster calls return without re-parsing.
	loadedMu  sync.RWMutex
	loaded    map[string]struct{}
	serverCtx context.Context
}

// NewSupervisor wires a Supervisor. All arguments must be non-nil. serverCtx
// is the server-lifetime context used to cancel in-flight work on shutdown.
func NewSupervisor(p sessionPipeline, serverCtx context.Context) *Supervisor {
	return &Supervisor{
		pipeline:  p,
		loaded:    make(map[string]struct{}),
		serverCtx: serverCtx,
	}
}

// Ensure blocks until the session's events have been loaded for this
// replica or an unrecoverable error is observed. Concurrent callers
// for the same session are coalesced via singleflight.
//
// Returns:
//   - (false, nil): events were loaded. Router serves the historical session.
//   - (true,  nil): session belongs to a live cluster. Router rewrites the
//     session-name cookie to "live".
//   - (_, ctx.Err()): caller's ctx died while waiting. Shared work keeps running.
//   - (_, other error): other error.
func (s *Supervisor) Ensure(ctx context.Context, info utils.ClusterInfo) (live bool, err error) {
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
		return s.runOnce(s.serverCtx, info, key)
	})

	select {
	case <-ctx.Done():
		// Release the caller; the winner keeps running and its result is shared
		// for the next caller of this session.
		//
		// Do NOT sf.Forget(key) here: a racing new call would kick off a second
		// Pipeline execution in parallel with the still-running winner.
		return false, ctx.Err()
	case result := <-ch:
		if result.Err != nil {
			return false, result.Err
		}
		live, _ := result.Val.(bool)
		return live, nil
	}
}

// runOnce is the singleflight winner's body. On success it returns a bool
// indicating whether the session belongs to a live cluster.
func (s *Supervisor) runOnce(ctx context.Context, info utils.ClusterInfo, key string) (bool, error) {
	// Keep the same fast-path for guarding against a race where loaded
	// gets marked between caller's lookup and singleflight execution.
	s.loadedMu.RLock()
	_, loaded := s.loaded[key]
	s.loadedMu.RUnlock()
	if loaded {
		return false, nil
	}
	// Synchronously process the session raw events.
	status, perr := s.pipeline.ProcessSession(ctx, info)
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
		// Unreachable under the Pipeline contract; defensive guard so a future
		// bug surfaces as a clear 500 instead of a silent success.
		return false, fmt.Errorf("unexpected session status %v", status)
	}
}
