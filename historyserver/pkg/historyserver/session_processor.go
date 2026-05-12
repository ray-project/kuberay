package historyserver

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// resolver is an interface to enable mocking HTTPLiveSessionResolver in tests.
type resolver interface {
	FetchSessionName(ctx context.Context, namespace, headSvcName string) (string, error)
}

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
	resolver  resolver
}

// NewSessionProcessor constructs a SessionProcessor.
func NewSessionProcessor(handler *eventserver.EventHandler, k8sClient client.Client, r resolver) *SessionProcessor {
	return &SessionProcessor{
		handler:   handler,
		k8sClient: k8sClient,
		resolver:  r,
	}
}

// ProcessSession processes one session end-to-end.
func (p *SessionProcessor) ProcessSession(ctx context.Context, session utils.ClusterInfo) (SessionStatus, error) {
	dead, err := p.isDead(ctx, session)
	if err != nil {
		if isCtxCanceled(err) {
			return SessionStatusCanceled, err
		}
		return SessionStatusClusterStateUnknown, fmt.Errorf("check cluster state for %s/%s: %w", session.Namespace, session.Name, err)
	}
	if !dead {
		return SessionStatusLive, nil
	}

	if err := p.handler.ProcessSingleSession(ctx, session); err != nil {
		if isCtxCanceled(err) {
			return SessionStatusCanceled, err
		}
		return SessionStatusEventsErr, fmt.Errorf("process events for %s/%s: %w", session.Namespace, session.Name, err)
	}
	return SessionStatusProcessed, nil
}

// isDead returns true when the queried session is not the one currently
// running on the RayCluster.
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

	headSvcName := rc.Status.Head.ServiceName
	if headSvcName == "" {
		return false, errors.New("RayCluster head service not ready")
	}

	liveSessionName, err := p.resolver.FetchSessionName(ctx, session.Namespace, headSvcName)
	if err != nil {
		return false, fmt.Errorf("resolve live session: %w", err)
	}
	return session.SessionName != liveSessionName, nil
}

// isCtxCanceled checks if the error is caused by context cancellation
// or deadline exceeded.
func isCtxCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
