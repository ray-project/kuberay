package historyserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/snapshot"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// SessionStatus is ProcessSession's outcome classification. Success statuses
// (Live, Processed) return a nil error; all others pair with a non-nil error.
type SessionStatus int

const (
	// SessionStatusUnknown is the zero value, reserved as a defensive guard.
	SessionStatusUnknown SessionStatus = iota
	// SessionStatusLive means the RayCluster CR is still present and the
	// session is intentionally skipped.
	SessionStatusLive
	// SessionStatusProcessed means events were ingested into EventHandler's
	// in-memory state.
	SessionStatusProcessed
	// SessionStatusClusterStateUnknown means the cluster state could not be
	// determined (e.g., transient API failure).
	SessionStatusClusterStateUnknown
	// SessionStatusEventsErr means event parsing failed.
	SessionStatusEventsErr
	// SessionStatusCanceled means ctx was canceled mid-processing.
	SessionStatusCanceled
)

// SessionProcessor processes a single session end-to-end: dead detection and
// event parsing. It is stateless across sessions and safe for concurrent use.
type SessionProcessor struct {
	handler   *eventserver.EventHandler
	k8sClient client.Client
}

// NewSessionProcessor constructs a SessionProcessor.
func NewSessionProcessor(handler *eventserver.EventHandler, k8sClient client.Client) *SessionProcessor {
	return &SessionProcessor{
		handler:   handler,
		k8sClient: k8sClient,
	}
}

// ProcessSession processes one session end-to-end.
func (p *SessionProcessor) ProcessSession(ctx context.Context, session utils.ClusterInfo) (SessionStatus, *snapshot.SessionSnapshot, error) {
	dead, err := p.isDead(ctx, session)
	if err != nil {
		if isCtxCanceled(err) {
			return SessionStatusCanceled, nil, err
		}
		return SessionStatusClusterStateUnknown, nil, fmt.Errorf("check cluster state for %s/%s: %w", session.Namespace, session.Name, err)
	}
	if !dead {
		return SessionStatusLive, nil, nil
	}

	if err := p.handler.ProcessSingleSession(ctx, session); err != nil {
		if isCtxCanceled(err) {
			return SessionStatusCanceled, nil, err
		}
		return SessionStatusEventsErr, nil, fmt.Errorf("process events for %s/%s: %w", session.Namespace, session.Name, err)
	}

	snap := buildSnapshotFromHandler(p.handler, session)
	return SessionStatusProcessed, snap, nil
}

// buildSnapshotFromHandler builds a SessionSnapshot from the handler's per-session state.
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
func groupTasksByID(tasks []eventtypes.Task) map[string][]eventtypes.Task {
	if len(tasks) == 0 {
		return map[string][]eventtypes.Task{}
	}
	out := make(map[string][]eventtypes.Task, len(tasks))
	for _, t := range tasks {
		out[t.TaskID] = append(out[t.TaskID], t)
	}
	return out
}

// isDead determines if the RayCluster CR is absent.
//
// Known limit: An old session of a still-running RayCluster will be misclassified as live.
func (p *SessionProcessor) isDead(ctx context.Context, session utils.ClusterInfo) (bool, error) {
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

// isCtxCanceled checks if the error is caused by context cancellation
// or deadline exceeded.
func isCtxCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
