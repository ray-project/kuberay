// Package processor implements the per-session pipeline that classifies a
// session as live or dead and, when dead, parses raw events into an
// in-memory SessionSnapshot.
//
// Pipeline.ProcessSession is driven on-demand by the HTTP Supervisor when
// /enter_cluster hits a dead session. The built snapshot is handed back to
// the caller for LRU priming; nothing is persisted to object storage.
package processor

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/metrics"
	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/snapshot"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// SessionStatus is ProcessSession's outcome classification. Success statuses
// (Live, Processed) return a nil error; all others pair with a non-nil error.
type SessionStatus int

const (
	// SessionStatusLive means the RayCluster CR is still present and the
	// session is intentionally skipped.
	SessionStatusLive SessionStatus = iota
	// SessionStatusProcessed means events were parsed into a SessionSnapshot.
	// The returned *SessionSnapshot is non-nil only for this status.
	SessionStatusProcessed
	// SessionStatusK8sProbeErr means the K8s Get returned a non-NotFound
	// error and the cluster state is unknown.
	SessionStatusK8sProbeErr
	// SessionStatusEventsErr means event parsing failed.
	SessionStatusEventsErr
	// SessionStatusCanceled means ctx was canceled mid-pipeline; not an *Err
	// status.
	SessionStatusCanceled
)

// Pipeline processes a single Ray session end-to-end: dead detection and
// event parsing. It is stateless across sessions and safe for concurrent
// use.
type Pipeline struct {
	reader    storage.StorageReader
	k8sClient client.Client
}

// NewPipeline constructs a Pipeline. All collaborators must be non-nil.
func NewPipeline(reader storage.StorageReader, k8sClient client.Client) *Pipeline {
	return &Pipeline{
		reader:    reader,
		k8sClient: k8sClient,
	}
}

// ProcessSession processes one session end-to-end and returns the outcome
// classification, the built snapshot (when applicable), and an error.
//
//   - (Live, nil, nil): no-op; caller moves on.
//   - (Processed, snap, nil): snapshot built; snap is the freshly-built
//     in-memory object and is never nil for this status.
//   - (K8sProbeErr | EventsErr, nil, err): real failure.
//   - (Canceled, nil, ctx.Err()): ctx was canceled between steps.
//
// ctx is polled at each step boundary; cancellation surfaces as Canceled.
// Wall time is always recorded to metrics.SessionDuration regardless of outcome.
func (p *Pipeline) ProcessSession(ctx context.Context, session utils.ClusterInfo) (SessionStatus, *snapshot.SessionSnapshot, error) {
	start := time.Now()
	defer func() { metrics.SessionDuration.Observe(time.Since(start).Seconds()) }()

	// Fast-fail if the request was canceled before we started.
	if err := ctx.Err(); err != nil {
		return SessionStatusCanceled, nil, err
	}

	// Step 1: dead detection. NotFound means dead; other errors propagate.
	// Treating unknown state as dead would snapshot a live cluster.
	dead, err := p.isDead(ctx, session)
	if err != nil {
		// Distinguish ctx cancellation from real API errors to keep alerting noise-free.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return SessionStatusCanceled, nil, ctxErr
		}
		return SessionStatusK8sProbeErr, nil, fmt.Errorf("k8s probe for %s/%s: %w", session.Namespace, session.Name, err)
	}
	if !dead {
		return SessionStatusLive, nil, nil
	}

	// Step 2: parse events with a fresh handler per call (no cross-session state).
	if err := ctx.Err(); err != nil {
		return SessionStatusCanceled, nil, err
	}
	h := eventserver.NewEventHandler(p.reader)
	if err := h.ProcessSingleSession(session); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return SessionStatusCanceled, nil, ctxErr
		}
		return SessionStatusEventsErr, nil, fmt.Errorf("process events for %s/%s: %w", session.Namespace, session.Name, err)
	}

	snap := buildSnapshotFromHandler(h, session)
	return SessionStatusProcessed, snap, nil
}

// isDead queries the Kubernetes API for the owning RayCluster CR.
//
//   - (true,  nil): CR is absent; the cluster is dead.
//   - (false, nil): CR exists; the cluster is live.
//   - (false, err): unknown state; caller should skip and retry later.
func (p *Pipeline) isDead(ctx context.Context, session utils.ClusterInfo) (bool, error) {
	rc := &rayv1.RayCluster{}
	err := p.k8sClient.Get(ctx, k8stypes.NamespacedName{
		Namespace: session.Namespace,
		Name:      session.Name,
	}, rc)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// buildSnapshotFromHandler flattens handler state into a SessionSnapshot. It
// uses the handler's getters, which lock and return deep copies, so callers
// may still hold a reference to the handler.
func buildSnapshotFromHandler(h *eventserver.EventHandler, session utils.ClusterInfo) *snapshot.SessionSnapshot {
	key := utils.BuildClusterSessionKey(session.Name, session.Namespace, session.SessionName)

	return &snapshot.SessionSnapshot{
		SessionKey:  key,
		GeneratedAt: time.Now().UTC(),
		Tasks:       groupTasksByID(h.GetTasks(key)),
		Actors:      h.GetActorsMap(key),
		Jobs:        h.GetJobsMap(key),
		Nodes:       h.GetNodeMap(key),
		LogEvents: snapshot.LogEventPayload{
			ByJobID: h.ClusterLogEventMap.GetRawEventsByJobID(key),
		},
	}
}

// groupTasksByID re-nests the flat []Task returned by EventHandler.GetTasks
// into the map[taskID][]attempt shape expected by SessionSnapshot.Tasks.
func groupTasksByID(tasks []types.Task) map[string][]types.Task {
	if len(tasks) == 0 {
		return map[string][]types.Task{}
	}
	out := make(map[string][]types.Task, len(tasks))
	for _, t := range tasks {
		out[t.TaskID] = append(out[t.TaskID], t)
	}
	return out
}
