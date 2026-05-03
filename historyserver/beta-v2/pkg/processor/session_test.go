package processor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// fakeReader is a tiny in-memory StorageReader used by these tests. Only the
// methods the Pipeline actually calls are exercised.
type fakeReader struct {
	mu       sync.Mutex
	contents map[string][]byte // key = "{clusterID}/{file}"
}

func newFakeReader() *fakeReader {
	return &fakeReader{contents: map[string][]byte{}}
}

func (f *fakeReader) List() []utils.ClusterInfo { return nil }

func (f *fakeReader) ListFiles(_ string, _ string) []string { return nil }

func (f *fakeReader) GetContent(clusterID string, fileName string) io.Reader {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.contents[clusterID+"/"+fileName]
	if !ok {
		return nil
	}
	return bytes.NewReader(body)
}

// newScheme returns a runtime.Scheme with RayCluster registered; every test
// needs this because the fake K8s client requires typed GVK awareness.
func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := rayv1.AddToScheme(s); err != nil {
		t.Fatalf("rayv1.AddToScheme: %v", err)
	}
	return s
}

// testSession returns a canonical ClusterInfo used across tests.
func testSession() utils.ClusterInfo {
	return utils.ClusterInfo{
		Name:        "raycluster-sample",
		Namespace:   "default",
		SessionName: "session_2026-04-22_10-00-00",
	}
}

// TestProcessSession_DeadSession_Processes is the primary happy path: the
// RayCluster CR is absent (dead), so the pipeline parses events and returns
// a freshly-built SessionSnapshot. Without the legacy write-back step,
// every call against a dead session re-parses — Pipeline alone is stateless;
// the Supervisor's LRU + Prime is what avoids the redundancy in production.
func TestProcessSession_DeadSession_Processes(t *testing.T) {
	reader := newFakeReader()
	// No CR registered with the fake client -> Get returns NotFound ->
	// isDead returns true.
	k8s := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()

	p := NewPipeline(reader, k8s)
	session := testSession()

	status, snap, err := p.ProcessSession(context.Background(), session)
	if err != nil {
		t.Fatalf("ProcessSession: unexpected error: %v", err)
	}
	if status != SessionStatusProcessed {
		t.Fatalf("expected status=Processed on first call, got %v", status)
	}
	// WHY assert non-nil snapshot: the Supervisor relies on this pointer to
	// Prime the LRU cache after a successful parse.
	if snap == nil {
		t.Fatalf("expected non-nil snapshot pointer on Processed, got nil")
	}
	if snap.SessionKey == "" {
		t.Fatalf("expected SessionKey populated, got empty string")
	}

	// Second call: same dead session; without write-back there is no
	// skip-if-exists fast path — Pipeline re-parses and returns Processed
	// again. Avoiding this redundancy in production is the Supervisor's
	// Prime side effect, not Pipeline's responsibility.
	status, snap, err = p.ProcessSession(context.Background(), session)
	if err != nil {
		t.Fatalf("ProcessSession (second call): unexpected error: %v", err)
	}
	if status != SessionStatusProcessed {
		t.Fatalf("expected status=Processed on second call, got %v", status)
	}
	if snap == nil {
		t.Fatalf("expected non-nil snapshot on second Processed call, got nil")
	}
}

// TestProcessSession_Live_Skips verifies that a live RayCluster (CR present)
// is skipped cleanly: no events read, nil snapshot returned.
func TestProcessSession_Live_Skips(t *testing.T) {
	reader := newFakeReader()
	session := testSession()

	// CR exists -> isDead returns (false, nil) -> ProcessSession returns early.
	rc := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      session.Name,
			Namespace: session.Namespace,
		},
	}
	k8s := fake.NewClientBuilder().
		WithScheme(newScheme(t)).
		WithObjects(rc).
		Build()

	p := NewPipeline(reader, k8s)

	status, snap, err := p.ProcessSession(context.Background(), session)
	if err != nil {
		t.Fatalf("ProcessSession: unexpected error: %v", err)
	}
	if status != SessionStatusLive {
		t.Fatalf("expected status=Live, got %v", status)
	}
	if snap != nil {
		t.Fatalf("expected nil snapshot on Live; got %+v", snap)
	}
}

// TestProcessSession_K8sOtherError_Propagates verifies that non-NotFound
// errors from the K8s API are propagated (not swallowed and mis-interpreted
// as "dead"). The caller logs the error and retries on a subsequent request.
func TestProcessSession_K8sOtherError_Propagates(t *testing.T) {
	reader := newFakeReader()
	session := testSession()

	injected := errors.New("simulated API server outage")
	k8s := fake.NewClientBuilder().
		WithScheme(newScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				return injected
			},
		}).
		Build()

	p := NewPipeline(reader, k8s)

	status, snap, err := p.ProcessSession(context.Background(), session)
	if err == nil {
		t.Fatalf("expected non-nil error, got nil")
	}
	if !errors.Is(err, injected) {
		t.Fatalf("expected error to wrap %v, got %v", injected, err)
	}
	if status != SessionStatusK8sProbeErr {
		t.Fatalf("expected status=K8sProbeErr, got %v", status)
	}
	if snap != nil {
		t.Fatalf("expected nil snapshot on error; got %+v", snap)
	}
}

// TestProcessSession_ContextCanceled_ReturnsCanceled verifies that a
// pre-canceled ctx short-circuits the pipeline before hitting K8s — the
// Supervisor follower-cancel path relies on this. A distinct Canceled status
// keeps client hang-ups out of the operator-alerting error buckets.
func TestProcessSession_ContextCanceled_ReturnsCanceled(t *testing.T) {
	reader := newFakeReader()
	session := testSession()

	k8s := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	p := NewPipeline(reader, k8s)

	// Pre-cancel the context — first-step ctx.Err() gate must fire.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	status, snap, err := p.ProcessSession(ctx, session)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if status != SessionStatusCanceled {
		t.Fatalf("expected status=Canceled, got %v", status)
	}
	if snap != nil {
		t.Fatalf("expected nil snapshot on Canceled; got %+v", snap)
	}
}
