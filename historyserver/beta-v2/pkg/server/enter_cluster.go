package server

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/singleflight"

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/metrics"
	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/processor"
	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/snapshot"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// sessionPipeline is the narrow subset of *processor.Pipeline that the
// Supervisor depends on.
type sessionPipeline interface {
	ProcessSession(ctx context.Context, info utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error)
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
//   - nil: caller may proceed (cached, just-written, or live).
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
		return s.runOnce(s.serverCtx, info, clusterNameID)
	})

	select {
	case <-ctx.Done():
		// Release the caller; the winner keeps running and its result is cached
		// for the next caller of this session.
		//
		// Do NOT sf.Forget(key) here: a racing new call would kick off a second
		// Pipeline execution in parallel with the still-running winner.
		return ctx.Err()
	case result := <-ch:
		if result.Shared {
			// singleflight.Result.Shared is true for every member of a coalesced
			// group, including the winner — so this counts participants, not just followers.
			metrics.SingleflightDedupTotal.Inc()
		}
		return result.Err
	}
}

// runOnce is the singleflight winner's body. It always returns (nil, err);
// callers care about the Prime side effect on success.
func (s *Supervisor) runOnce(ctx context.Context, info utils.ClusterInfo, clusterNameID string) (interface{}, error) {
	// Layer 1: LRU (inside loader.Load).
	if _, err := s.loader.Load(clusterNameID, info.SessionName); err == nil {
		return nil, nil
	} else if !errors.Is(err, ErrSnapshotNotFound) {
		return nil, fmt.Errorf("loader.Load %s/%s: %w", clusterNameID, info.SessionName, err)
	}

	// Layer 2: synchronously build under serverCtx (not the winner's request
	// ctx) so a winner disconnect does not cancel work that followers still
	// depend on, while SIGTERM still cancels via serverCtx.
	status, built, perr := s.pipeline.ProcessSession(ctx, info)
	if perr != nil {
		// Pipeline already labeled the failure into metrics; we just propagate.
		return nil, perr
	}

	switch status {
	case processor.SessionStatusProcessed:
		metrics.SessionsProcessed.Inc()
		if built != nil {
			// Prime so the immediate follow-up handler call is an LRU hit.
			s.loader.Prime(clusterNameID, info.SessionName, built)
		}
		return nil, nil

	case processor.SessionStatusLive:
		// Live cluster: handler redirects to the live source; nothing to snapshot.
		metrics.SessionsSkipped.WithLabelValues("live").Inc()
		return nil, nil

	default:
		// Unreachable under the Pipeline contract; defensive guard so a future
		// bug surfaces as a clear 500 instead of a silent success.
		return nil, fmt.Errorf("unexpected session status %v", status)
	}
}
