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
//   - nil: caller may proceed (loaded, just-loaded, or live).
//   - ctx.Err() (canceled): caller's ctx died while waiting; the shared
//     work keeps running.
//   - other error: real failure.
func (s *Supervisor) Ensure(ctx context.Context, info utils.ClusterInfo) error {
	// Fast pre-flight: skip singleflight entirely if ctx is already dead.
	if err := ctx.Err(); err != nil {
		return err
	}

	clusterNameID := info.Name + "_" + info.Namespace
	key := clusterNameID + "/" + info.SessionName

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
		return ctx.Err()
	case result := <-ch:
		return result.Err
	}
}

// runOnce is the singleflight winner's body. It always returns (nil, err);
// callers care about the loaded-set side effect on success.
func (s *Supervisor) runOnce(ctx context.Context, info utils.ClusterInfo, key string) (interface{}, error) {
	// Fast-path: session is already loaded on this replica.
	s.loadedMu.RLock()
	_, ok := s.loaded[key]
	s.loadedMu.RUnlock()
	if ok {
		return nil, nil
	}

	// Synchronously process under serverCtx (not the winner's request
	// ctx) so a winner disconnect does not cancel work that followers
	// still depend on, while SIGTERM still cancels via serverCtx.
	status, perr := s.pipeline.ProcessSession(ctx, info)
	if perr != nil {
		return nil, perr
	}

	switch status {
	case SessionStatusProcessed:
		// Mark the session as loaded so subsequent /enter_cluster calls
		// are no-ops on this replica.
		s.loadedMu.Lock()
		s.loaded[key] = struct{}{}
		s.loadedMu.Unlock()
		return nil, nil

	case SessionStatusLive:
		// Live cluster: handler redirects to the live source; nothing to load.
		// Do not mark loaded — when the cluster eventually dies, the next
		// /enter_cluster should run the parse path.
		return nil, nil

	default:
		// Unreachable under the Pipeline contract; defensive guard so a future
		// bug surfaces as a clear 500 instead of a silent success.
		return nil, fmt.Errorf("unexpected session status %v", status)
	}
}
