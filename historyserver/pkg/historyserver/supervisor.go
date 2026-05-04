package historyserver

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/singleflight"

	"github.com/ray-project/kuberay/historyserver/pkg/snapshot"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// sessionPipeline is the narrow subset of *Pipeline that the
// Supervisor depends on.
type sessionPipeline interface {
	ProcessSession(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *snapshot.SessionSnapshot, error)
}

// Supervisor serves snapshots for /enter_cluster by checking the loader's
// LRU and running the Pipeline on miss. Concurrent callers for the same
// session are coalesced via singleflight.
//
// A single Supervisor is safe to share across all HTTP handlers for one
// replica; its mutable state lives inside singleflight.Group.
type Supervisor struct {
	pipeline sessionPipeline
	loader   *SnapshotLoader
	// sf coalesces concurrent Ensure calls for the same session.
	sf        singleflight.Group
	serverCtx context.Context
}

// NewSupervisor wires a Supervisor. All arguments must be non-nil. serverCtx
// is the server-lifetime context used to cancel in-flight work on shutdown.
func NewSupervisor(p sessionPipeline, l *SnapshotLoader, serverCtx context.Context) *Supervisor {
	return &Supervisor{pipeline: p, loader: l, serverCtx: serverCtx}
}

// Ensure blocks until a snapshot is available for (info.Name, info.Namespace,
// info.SessionName) or an unrecoverable error is observed. Concurrent callers
// for the same session are coalesced via singleflight.
//
// Returns:
//   - (false, nil): snapshot available (cached or just built). Router serves
//     the historical session.
//   - (true,  nil): session belongs to a live cluster; no snapshot was built.
//     Router rewrites the session-name cookie to "live".
//   - (_, ctx.Err()): caller's ctx died while waiting. Shared work keeps running.
//   - (_, other error): real failure.
func (s *Supervisor) Ensure(ctx context.Context, info utils.ClusterInfo) (live bool, err error) {
	// Fast pre-flight: skip singleflight entirely if ctx is already dead.
	if err := ctx.Err(); err != nil {
		return false, err
	}

	clusterNameID := info.Name + "_" + info.Namespace
	key := clusterNameID + "/" + info.SessionName

	// Fast-path: session snapshot is already in the LRU.
	if _, lerr := s.loader.Load(clusterNameID, info.SessionName); lerr == nil {
		return false, nil
	} else if !errors.Is(lerr, ErrSnapshotNotFound) {
		return false, fmt.Errorf("loader.Load %s/%s: %w", clusterNameID, info.SessionName, lerr)
	}

	// TODO(jwj): Graceful drain if needed. Currently SIGTERM immediately cancels
	// in-flight work. If 500-during-shutdown becomes a customer pain point, switch
	// closure to a separate runCtx with grace timer.
	ch := s.sf.DoChan(key, func() (interface{}, error) {
		return s.runOnce(s.serverCtx, info, clusterNameID)
	})

	select {
	case <-ctx.Done():
		// Release the caller; the winner keeps running and its result is cached
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
func (s *Supervisor) runOnce(ctx context.Context, info utils.ClusterInfo, clusterNameID string) (bool, error) {
	// Layer 1: LRU. Guards against a race where Prime fired between the
	// caller's fast-path lookup and singleflight execution.
	if _, lerr := s.loader.Load(clusterNameID, info.SessionName); lerr == nil {
		return false, nil
	} else if !errors.Is(lerr, ErrSnapshotNotFound) {
		return false, fmt.Errorf("loader.Load %s/%s: %w", clusterNameID, info.SessionName, lerr)
	}

	// Layer 2: Synchronously process the session raw events.
	status, built, perr := s.pipeline.ProcessSession(ctx, info)
	if perr != nil {
		return false, perr
	}

	switch status {
	case SessionStatusProcessed:
		if built != nil {
			// Prime so the immediate follow-up handler call is an LRU hit.
			s.loader.Prime(clusterNameID, info.SessionName, built)
		}
		return false, nil

	case SessionStatusLive:
		// Live cluster: handler proxies to the Ray Dashboard; nothing to snapshot.
		return true, nil

	default:
		// Unreachable under the Pipeline contract; defensive guard so a future
		// bug surfaces as a clear 500 instead of a silent success.
		return false, fmt.Errorf("unexpected session status %v", status)
	}
}
