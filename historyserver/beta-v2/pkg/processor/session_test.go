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

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/snapshot"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// fakeStorage is a tiny in-memory StorageReader + StorageWriter used by these
// tests. It is intentionally minimal: only the methods the Pipeline actually
// calls are exercised. Write routes to the same map so that a WriteFile
// followed by GetContent round-trips, mirroring real S3 semantics.
type fakeStorage struct {
	mu sync.Mutex
	// flat map whose key is the full storage path "{clusterID}/{file}"
	contents map[string][]byte
	// perKey counters so tests can assert no-writes / single-writes.
	writeCalls map[string]int
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{
		contents:   map[string][]byte{},
		writeCalls: map[string]int{},
	}
}

// seed pre-populates the storage as if the collector had already written
// these bytes. Key format matches GetContent: "{clusterID}/{file}".
func (f *fakeStorage) seed(clusterID, file string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contents[clusterID+"/"+file] = body
}

// --- StorageReader ---

func (f *fakeStorage) List() []utils.ClusterInfo { return nil }

func (f *fakeStorage) ListFiles(_ string, _ string) []string { return nil }

func (f *fakeStorage) GetContent(clusterID string, fileName string) io.Reader {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.contents[clusterID+"/"+fileName]
	if !ok {
		return nil
	}
	return bytes.NewReader(body)
}

// --- StorageWriter ---

func (f *fakeStorage) CreateDirectory(_ string) error { return nil }

func (f *fakeStorage) WriteFile(file string, reader io.ReadSeeker) error {
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contents[file] = body
	f.writeCalls[file]++
	return nil
}

func (f *fakeStorage) writeCountFor(file string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeCalls[file]
}

func (f *fakeStorage) totalWrites() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := 0
	for _, n := range f.writeCalls {
		total += n
	}
	return total
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

func clusterNameID(s utils.ClusterInfo) string {
	return s.Name + "_" + s.Namespace
}

// snapshotStorageKey returns the full storage key where writeSnapshot writes
// — "{clusterNameID}/{sessionName}/processed/session.json".
func snapshotStorageKey(s utils.ClusterInfo) string {
	return clusterNameID(s) + "/" + snapshot.SnapshotPath(s.SessionName)
}

// TestProcessSession_DeadNoSnapshot_Processes is the primary happy path: the
// RayCluster CR is absent (dead), no snapshot exists yet, so the pipeline
// must process events (possibly empty) and persist a snapshot. The second
// call exercises skip-if-exists and also verifies the returned *snapshot
// pointer is nil on the AlreadySnapped path (only Processed returns the
// freshly-built snapshot).
func TestProcessSession_DeadNoSnapshot_Processes(t *testing.T) {
	storage := newFakeStorage()
	// No CR registered with the fake client -> Get returns NotFound ->
	// isDead returns true.
	k8s := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()

	p := NewPipeline(storage, storage, k8s, "")
	session := testSession()

	status, snap, err := p.ProcessSession(context.Background(), session)
	if err != nil {
		t.Fatalf("ProcessSession: unexpected error: %v", err)
	}
	if status != SessionStatusProcessed {
		t.Fatalf("expected status=Processed on first call, got %v", status)
	}
	// WHY assert non-nil snapshot: Supervisor relies on this pointer to
	// Prime the LRU cache, avoiding a redundant S3 GET right after PUT.
	if snap == nil {
		t.Fatalf("expected non-nil snapshot pointer on Processed, got nil")
	}
	// Sanity: the returned snapshot matches what was persisted — its
	// SessionKey is what buildSnapshotFromHandler computed.
	if snap.SessionKey == "" {
		t.Fatalf("expected SessionKey populated, got empty string")
	}

	dst := snapshotStorageKey(session)
	if got := storage.writeCountFor(dst); got != 1 {
		t.Fatalf("expected 1 snapshot write to %q, got %d", dst, got)
	}
	// Sanity: subsequent call should be a no-op (skip-if-exists), and the
	// returned snapshot MUST be nil — AlreadySnapped is a "caller moves on"
	// signal, not a snapshot handoff.
	status, snap, err = p.ProcessSession(context.Background(), session)
	if err != nil {
		t.Fatalf("ProcessSession (second call): unexpected error: %v", err)
	}
	if status != SessionStatusAlreadySnapped {
		t.Fatalf("expected status=AlreadySnapped on second call, got %v", status)
	}
	if snap != nil {
		t.Fatalf("expected nil snapshot on AlreadySnapped; got %+v", snap)
	}
	if got := storage.writeCountFor(dst); got != 1 {
		t.Fatalf("skip-if-exists broken: expected 1 total write, got %d", got)
	}
}

// TestProcessSession_DeadSnapshotExists_Skips verifies that a dead session
// with an existing snapshot is a no-op: no events are re-read, no writes
// happen, and the returned snapshot pointer is nil.
func TestProcessSession_DeadSnapshotExists_Skips(t *testing.T) {
	storage := newFakeStorage()
	session := testSession()
	storage.seed(clusterNameID(session), snapshot.SnapshotPath(session.SessionName),
		[]byte(`{"sessionKey":"pre-existing"}`))

	// No CR -> dead.
	k8s := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()

	p := NewPipeline(storage, storage, k8s, "")

	status, snap, err := p.ProcessSession(context.Background(), session)
	if err != nil {
		t.Fatalf("ProcessSession: unexpected error: %v", err)
	}
	if status != SessionStatusAlreadySnapped {
		t.Fatalf("expected status=AlreadySnapped, got %v", status)
	}
	if snap != nil {
		t.Fatalf("expected nil snapshot on AlreadySnapped; got %+v", snap)
	}
	if got := storage.totalWrites(); got != 0 {
		t.Fatalf("expected 0 writes when snapshot already exists, got %d", got)
	}
}

// TestProcessSession_Live_Skips verifies that a live RayCluster (CR present)
// is skipped cleanly: no events read, no snapshot written, nil snapshot
// returned.
func TestProcessSession_Live_Skips(t *testing.T) {
	storage := newFakeStorage()
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

	p := NewPipeline(storage, storage, k8s, "")

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
	if got := storage.totalWrites(); got != 0 {
		t.Fatalf("expected 0 writes for live cluster, got %d", got)
	}
}

// TestProcessSession_K8sOtherError_Propagates verifies that non-NotFound
// errors from the K8s API are propagated (not swallowed and mis-interpreted
// as "dead"). The caller logs the error and retries on a subsequent request.
func TestProcessSession_K8sOtherError_Propagates(t *testing.T) {
	storage := newFakeStorage()
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

	p := NewPipeline(storage, storage, k8s, "")

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
	if got := storage.totalWrites(); got != 0 {
		t.Fatalf("expected 0 writes when K8s probe fails, got %d", got)
	}
}

// TestProcessSession_ContextCanceled_ReturnsCanceled verifies that a
// pre-canceled ctx short-circuits the pipeline before hitting K8s — the
// Supervisor follower-cancel path relies on this. A distinct Canceled status
// keeps client hang-ups out of the operator-alerting error buckets.
func TestProcessSession_ContextCanceled_ReturnsCanceled(t *testing.T) {
	storage := newFakeStorage()
	session := testSession()

	k8s := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	p := NewPipeline(storage, storage, k8s, "")

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
	if got := storage.totalWrites(); got != 0 {
		t.Fatalf("expected 0 writes when ctx canceled pre-flight, got %d", got)
	}
}
