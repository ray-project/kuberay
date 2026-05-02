package historyserver

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// SessionStatus is ProcessSession's outcome classification. Success statuses
// (Live, Processed) return a nil error; all others pair with a non-nil error.
type SessionStatus int

const (
	// SessionStatusLive means the RayCluster CR is still present and the
	// session is intentionally skipped.
	SessionStatusLive SessionStatus = iota
	// SessionStatusProcessed means events were ingested into EventHandler's
	// in-memory state.
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
	handler   *eventserver.EventHandler
	k8sClient client.Client
}

// NewPipeline constructs a Pipeline. All collaborators must be non-nil.
func NewPipeline(handler *eventserver.EventHandler, k8sClient client.Client) *Pipeline {
	return &Pipeline{
		handler:   handler,
		k8sClient: k8sClient,
	}
}

// ProcessSession processes one session end-to-end and returns the outcome
// classification and an error.
//
//   - (Live, nil): no-op; caller moves on.
//   - (Processed, nil): events were ingested into EventHandler's in-memory state.
//   - (K8sProbeErr | EventsErr, err): real failure.
//   - (Canceled, ctx.Err()): ctx was canceled between steps.
//
// ctx is polled at each step boundary; cancellation surfaces as Canceled.
func (p *Pipeline) ProcessSession(ctx context.Context, session utils.ClusterInfo) (SessionStatus, error) {
	// Fast-fail if the request was canceled before we started.
	if err := ctx.Err(); err != nil {
		return SessionStatusCanceled, err
	}

	// Step 1: dead detection. NotFound means dead; other errors propagate.
	// Treating unknown state as dead would snapshot a live cluster.
	dead, err := p.isDead(ctx, session)
	if err != nil {
		// Distinguish ctx cancellation from real API errors to keep alerting noise-free.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return SessionStatusCanceled, ctxErr
		}
		return SessionStatusK8sProbeErr, fmt.Errorf("k8s probe for %s/%s: %w", session.Namespace, session.Name, err)
	}
	if !dead {
		return SessionStatusLive, nil
	}

	// Step 2: drive parsing through the shared EventHandler. Events ingest
	// into the EventHandler's per-session in-memory maps; handlers read
	// directly from those maps.
	if err := ctx.Err(); err != nil {
		return SessionStatusCanceled, err
	}
	if err := p.handler.ProcessSingleSession(session); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return SessionStatusCanceled, ctxErr
		}
		return SessionStatusEventsErr, fmt.Errorf("process events for %s/%s: %w", session.Namespace, session.Name, err)
	}
	return SessionStatusProcessed, nil
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
