package historyserver

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/sirupsen/logrus"

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
	// SessionStatusCanceled means ctx was canceled mid-processing; not an *Err
	// status.
	SessionStatusCanceled
)

// SessionProcessor processes a single session end-to-end: dead detection and
// event parsing. It is stateless across sessions and safe for concurrent use.
type SessionProcessor struct {
	handler   *eventserver.EventHandler
	k8sClient client.Client
}

// NewSessionProcessor constructs a SessionProcessor. All collaborators must be non-nil.
func NewSessionProcessor(handler *eventserver.EventHandler, k8sClient client.Client) *SessionProcessor {
	return &SessionProcessor{
		handler:   handler,
		k8sClient: k8sClient,
	}
}

// ProcessSession processes one session end-to-end and returns the session status.
func (p *SessionProcessor) ProcessSession(ctx context.Context, session utils.ClusterInfo) (SessionStatus, error) {
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

// isDead determines whether a session is dead by checking the RayCluster CR.
// A session is dead if either:
//   - the RayCluster CR is absent, or
//   - the CR exists but was created after the queried session.
//
// TODO(jwj): Use collector-written UID or a storage-side probe for handling
// cases in which multiple sessions exist in the same CR.
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

	sessionTimestamp := ParseSessionTimestamp(session.SessionName)
	if sessionTimestamp.IsZero() {
		logrus.Errorf("failed to parse session timestamp for %s/%s/%s; treating as live",
			session.Namespace, session.Name, session.SessionName)
		return false, nil
	}
	if sessionTimestamp.Before(rc.CreationTimestamp.Time) {
		return true, nil
	}
	return false, nil
}
