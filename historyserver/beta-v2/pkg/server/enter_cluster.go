// Package server — Supervisor for lazy-mode /enter_cluster.
//
// In beta-v2 we retired the ticker-driven eventprocessor: snapshots are
// built on demand by the HTTP layer the first time a user visits a dead
// session. The Supervisor is the glue: it blocks the request until either a
// cached snapshot is found, a snapshot is fetched from S3, or the Pipeline
// synchronously builds-and-PUTs a fresh one. Concurrent callers for the
// same session are coalesced via golang.org/x/sync/singleflight so at most
// one Pipeline execution runs per HS replica.
//
// Design references:
//   - historyserver/beta_poc.md §1 idempotency layers (LRU -> S3 -> Pipeline)
//   - historyserver/beta_poc.md §6 singleflight wrapper
//   - historyserver/beta_poc.md §8 risk #1 (ctx.Err between steps)
//   - historyserver/beta_poc.md §8 risk #2 (resolved: closure uses serverCtx
//     to decouple shared work from any single caller's lifecycle)
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
// Supervisor depends on. Declaring it in the consumer package (rather than
// importing the concrete type) mirrors the beta pattern used for
// processor.sessionProcessor and, crucially, lets tests inject a fake
// without spinning up a fake K8s client for every Supervisor-level case.
//
// Keep the interface tiny on purpose: "widen as needed" > "predict the
// future" — if Supervisor ever needs another method, add it here.
type sessionPipeline interface {
	ProcessSession(ctx context.Context, info utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error)
}

// Supervisor serializes per-session work triggered by /enter_cluster. It
// orchestrates the three-layer idempotency defense laid out in
// beta_poc.md §1: cached pointer in the LRU, then S3 GET via loader,
// then Pipeline.ProcessSession as a last resort.
//
// A single Supervisor instance is safe to share across all HTTP handlers
// for one HS replica; its only mutable state lives inside singleflight.Group,
// which is internally synchronized.
//
// Multi-replica note: each replica owns an independent Supervisor (and
// therefore its own singleflight). If two replicas race to build the same
// session, both run Pipeline; the second PUT is a no-op at the S3 layer
// because skip-if-exists guarantees snapshot immutability. Accepted per
// beta_poc.md §9 ("分散式 singleflight can be added later").
type Supervisor struct {
	pipeline sessionPipeline
	loader   *SnapshotLoader
	// sf coalesces concurrent Ensure calls for the same session. The key
	// uses the same "{name}_{namespace}/{session}" shape the LRU uses for
	// cache lookups, so reasoning about dedup and cache keys stays in
	// lockstep.
	sf        singleflight.Group
	serverCtx context.Context
}

// NewSupervisor wires a Supervisor. All three arguments must be non-nil; passing
// nil for any argument is treated as a programmer error and will panic on first Ensure.
// serverCtx is the server-lifetime context, used to cancel in-flight work during graceful shutdown.
func NewSupervisor(p sessionPipeline, l *SnapshotLoader, serverCtx context.Context) *Supervisor {
	return &Supervisor{pipeline: p, loader: l, serverCtx: serverCtx}
}

// Ensure blocks until a snapshot is available for (info.Name, info.Namespace,
// info.SessionName) or an unrecoverable error is observed. Concurrent callers
// for the same session are coalesced via singleflight.
//
// Return contract:
//   - nil                   -> caller may proceed; the snapshot is either
//     cached or was just written (live sessions
//     also return nil — no snapshot is needed).
//   - ctx.Err() (canceled)  -> the CALLER's ctx was canceled while waiting.
//     The winner goroutine keeps running; a future
//     request will benefit from its result.
//   - other error           -> bubble to HTTP 500.
//
// Three-layer resolution inside the singleflight closure:
//
//	Layer 1 (LRU hit)    — loader.Load returns the cached pointer in O(1).
//	Layer 2 (S3 GET)     — loader.Load falls through to reader.GetContent;
//	                       on ErrSnapshotNotFound we proceed to Layer 3. On
//	                       any OTHER error we CONSERVATIVELY return the
//	                       error without running Pipeline — a transient S3
//	                       outage should not trigger a needless K8s probe
//	                       + event-parse storm (see beta_poc.md Q1 answer).
//	Layer 3 (Pipeline)   — dead-detection + events + PUT + Prime.
//
// WHY singleflight.DoChan and not Do: Do is synchronous and ignores the
// follower's ctx. With DoChan each caller's wait is wrapped in a select,
// so any caller (winner OR follower) whose HTTP client hangs up can
// return promptly. The shared closure runs under serverCtx so it is
// not affected by ANY single caller leaving. See beta_poc.md §8 risk #2.
func (s *Supervisor) Ensure(ctx context.Context, info utils.ClusterInfo) error {
	// Fast pre-flight: if the caller's ctx is already dead, don't even
	// enter singleflight. This keeps the dedup group clean in the unusual
	// case where the client disconnected before the handler ran.
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
		// Release the caller immediately. The singleflight winner continues
		// running in the background; when it finishes, its result is cached
		// (LRU + S3) so the next caller for this session gets a fast path.
		//
		// We DO NOT call sf.Forget(key) here — if we did, a racing new call
		// for the same session would kick off a second Pipeline execution
		// in parallel with the still-running winner. Leaving the group key
		// in place means the next caller joins the existing singleflight
		// and reaps the winner's result.
		return ctx.Err()
	case result := <-ch:
		if result.Shared {
			// NOTE on semantics: singleflight.Result.Shared is true for
			// EVERY caller of a coalesced group (including the winner).
			// So this counter reports "participants in a coalesced group",
			// not "followers only" — documented in the metric Help string.
			metrics.SingleflightDedupTotal.Inc()
		}
		return result.Err
	}
}

// runOnce is the body the singleflight winner executes. Returning a non-nil
// interface{} is pointless (callers discard it) so we always return
// (nil, err) — the Prime side-effect is what matters.
//
// This is split out from Ensure mainly for readability: the Do closure would
// otherwise be ~40 lines of nested logic inside a select block.
func (s *Supervisor) runOnce(ctx context.Context, info utils.ClusterInfo, clusterNameID string) (interface{}, error) {
	// Layer 1 + 2: LRU then S3 GET (both hidden inside loader.Load).
	snap, err := s.loader.Load(clusterNameID, info.SessionName)
	if err == nil {
		// Snapshot already persisted (either this process cached it, or a
		// sibling replica PUT it earlier). Ensure the loader cache holds
		// it for subsequent same-process calls.
		//
		// NOTE: Load already inserts into the LRU on miss+success, so this
		// is already correct — we deliberately do NOT re-Prime here to
		// keep the fast path metric-free (a CacheHit is just as cheap as
		// a Prime+Load pair).
		_ = snap
		return nil, nil
	}
	// Conservative policy for non-NotFound errors: bubble up to the client
	// as a 500 rather than triggering a costly Pipeline execution. A
	// transient S3 outage will clear within seconds; the client can retry.
	// See beta_poc.md Q1 for the why.
	if !errors.Is(err, ErrSnapshotNotFound) {
		return nil, fmt.Errorf("loader.Load %s/%s: %w", clusterNameID, info.SessionName, err)
	}

	// Layer 3: synchronously build + PUT. Uses the server-lifetime ctx (not the
	// winner's request ctx) so that:
	//   - winner client disconnect does NOT propagate context.Canceled to
	//     followers whose own connections remain healthy;
	//   - HS graceful shutdown (SIGTERM) DOES still cancel in-flight work via
	//     serverCtx, so we don't keep spinning during pod termination.
	status, built, perr := s.pipeline.ProcessSession(ctx, info)
	if perr != nil {
		// Pipeline already labeled the failure into metrics.SessionErrors
		// (or reported it as Canceled). We just propagate.
		return nil, perr
	}

	switch status {
	case processor.SessionStatusProcessed:
		metrics.SessionsProcessed.Inc()
		if built != nil {
			// WHY Prime: we just persisted this object; planting it into
			// the LRU saves the immediate follow-up handler call (e.g.
			// frontend calling /tasks right after /enter_cluster returns)
			// a redundant S3 GET.
			s.loader.Prime(clusterNameID, info.SessionName, built)
		}
		return nil, nil

	case processor.SessionStatusAlreadySnapped:
		// A sibling process snapped it between our Layer-2 check and our
		// Pipeline call. Not an error — nothing to Prime here; the next
		// handler Load will fall through to S3 and cache the result.
		metrics.SessionsSkipped.WithLabelValues("already_snapped").Inc()
		return nil, nil

	case processor.SessionStatusLive:
		// The RayCluster CR is still there. Frontend handlers go through
		// redirectRequest for "live" sessions, so there is nothing for us
		// to snapshot. Return OK so /enter_cluster sets cookies and
		// proceeds.
		metrics.SessionsSkipped.WithLabelValues("live").Inc()
		return nil, nil

	default:
		// Unreachable under the Pipeline contract (error statuses always
		// come with a non-nil err, handled above). Guard so a future
		// Pipeline bug becomes a clear 500 instead of a silent success.
		return nil, fmt.Errorf("unexpected session status %v", status)
	}
}
