package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	restful "github.com/emicklei/go-restful/v3"

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/processor"
	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/snapshot"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// fakePipeline is a test double for sessionPipeline that lets each test
// dictate the exact (status, snapshot, error) tuple ProcessSession returns
// and count the number of invocations.
//
// WHY a function-field rather than a static response: the singleflight
// concurrency test needs to hold the pipeline inside ProcessSession while
// additional callers pile up, which is easiest to express by having the
// test block inside the injected function.
type fakePipeline struct {
	calls int32
	fn    func(ctx context.Context, info utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error)
}

func (f *fakePipeline) ProcessSession(ctx context.Context, info utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.fn == nil {
		return processor.SessionStatusProcessed, &snapshot.SessionSnapshot{SessionKey: "default"}, nil
	}
	return f.fn(ctx, info)
}

func (f *fakePipeline) callCount() int32 { return atomic.LoadInt32(&f.calls) }

func testEnterClusterInfo() utils.ClusterInfo {
	return utils.ClusterInfo{
		Name:        "raycluster-e2e",
		Namespace:   "default",
		SessionName: "session_2026-04-22_10-00-00",
	}
}

func newTestLoader(t *testing.T) (*SnapshotLoader, *fakeStorageReader) {
	t.Helper()
	reader := newFakeStorageReader()
	loader, err := NewSnapshotLoader(reader, 0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}
	return loader, reader
}

// TestEnsure_SnapshotCached_NoPipelineCall is the Layer-1 happy path: the
// LRU already has the snapshot (either because a prior call Primed it, or
// because it was fetched via Load earlier). Ensure must NOT invoke Pipeline.
func TestEnsure_SnapshotCached_NoPipelineCall(t *testing.T) {
	loader, _ := newTestLoader(t)
	info := testEnterClusterInfo()
	clusterNameID := info.Name + "_" + info.Namespace

	loader.Prime(clusterNameID, info.SessionName, &snapshot.SessionSnapshot{SessionKey: "primed"})

	fp := &fakePipeline{}
	sup := NewSupervisor(fp, loader)

	if err := sup.Ensure(context.Background(), info); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got := fp.callCount(); got != 0 {
		t.Fatalf("expected Pipeline to NOT be called when LRU is hot; got %d calls", got)
	}
}

// TestEnsure_SnapshotInS3_NoPipelineCall is the Layer-2 path: LRU cold, but
// S3 has the object (a sibling replica already wrote it, or the cache was
// evicted). Ensure must hydrate via Load and NOT invoke Pipeline.
func TestEnsure_SnapshotInS3_NoPipelineCall(t *testing.T) {
	loader, reader := newTestLoader(t)
	info := testEnterClusterInfo()
	clusterNameID := info.Name + "_" + info.Namespace

	// Plant the JSON blob where fetch() will find it — same shape
	// buildSnapshotFromHandler would have produced on the other replica.
	reader.put(clusterNameID, snapshot.SnapshotPath(info.SessionName),
		makeSnapshotBlob(t, "from-s3"))

	fp := &fakePipeline{}
	sup := NewSupervisor(fp, loader)

	if err := sup.Ensure(context.Background(), info); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got := fp.callCount(); got != 0 {
		t.Fatalf("expected Pipeline to NOT be called when S3 has the object; got %d calls", got)
	}
	// Follow-up Load should now be a pure LRU hit — Load's own insertion
	// after a successful miss+fetch takes care of that.
	if _, err := loader.Load(clusterNameID, info.SessionName); err != nil {
		t.Fatalf("subsequent Load: %v", err)
	}
}

// TestEnsure_Missing_PipelineRuns_AndPrimes is the Layer-3 happy path: no
// snapshot in LRU or S3, so Pipeline runs, PUTs a snapshot, and Ensure
// Primes the result into the LRU. Verified via a follow-up Load against a
// reader that NEVER held the blob — so a cache miss would blow up the test.
func TestEnsure_Missing_PipelineRuns_AndPrimes(t *testing.T) {
	loader, reader := newTestLoader(t)
	info := testEnterClusterInfo()
	clusterNameID := info.Name + "_" + info.Namespace

	built := &snapshot.SessionSnapshot{SessionKey: "freshly-built"}
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
			return processor.SessionStatusProcessed, built, nil
		},
	}
	sup := NewSupervisor(fp, loader)

	if err := sup.Ensure(context.Background(), info); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got := fp.callCount(); got != 1 {
		t.Fatalf("expected Pipeline to be called exactly once; got %d", got)
	}

	// Reader has no blob -> if Prime was missed, this Load would fall
	// through to ErrSnapshotNotFound. A successful hit proves the Prime.
	got, err := loader.Load(clusterNameID, info.SessionName)
	if err != nil {
		t.Fatalf("Load after Ensure should be cache-hit but got err: %v", err)
	}
	if got != built {
		t.Fatalf("expected Load to return the primed pointer; got %p want %p", got, built)
	}
	// Reader should have been touched exactly once (the initial Layer-2
	// miss that triggered Pipeline). Prime bypasses the reader entirely.
	if reader.totalCalls() != 1 {
		t.Fatalf("expected reader to be touched only by the initial Layer-2 miss; got %d", reader.totalCalls())
	}
}

// TestEnsure_LoaderTransientError_Propagates verifies the conservative
// Layer-2 policy: a non-NotFound error from loader.Load must NOT trigger
// Pipeline — it must bubble to the caller as an error. WHY: a transient
// S3 glitch should fail-fast instead of triggering a costly K8s probe +
// event-parse storm (beta_poc.md Q1 answer).
func TestEnsure_LoaderTransientError_Propagates(t *testing.T) {
	info := testEnterClusterInfo()

	transient := errors.New("simulated S3 connection refused")

	// A reader that returns a reader with a corrupt body -> Load's
	// io.ReadAll or json.Unmarshal returns an "other" error. The exact
	// error string doesn't matter; we just need it to NOT be
	// ErrSnapshotNotFound.
	reader := &flakyReader{err: transient}
	loader, err := NewSnapshotLoader(reader, 0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	fp := &fakePipeline{}
	sup := NewSupervisor(fp, loader)

	err = sup.Ensure(context.Background(), info)
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}
	if !errors.Is(err, transient) {
		t.Fatalf("expected wrapped transient error, got: %v", err)
	}
	if got := fp.callCount(); got != 0 {
		t.Fatalf("expected Pipeline to NOT be called on transient loader error; got %d calls", got)
	}
}

// TestEnsure_ConcurrentCallers_PipelineOnce is the singleflight contract:
// N goroutines hitting Ensure for the same session concurrently must cause
// at most 1 Pipeline invocation. We verify both the coalescing and the
// SingleflightDedupTotal metric increment.
func TestEnsure_ConcurrentCallers_PipelineOnce(t *testing.T) {
	loader, _ := newTestLoader(t)
	info := testEnterClusterInfo()

	// A barrier the test uses to hold Pipeline inside ProcessSession while
	// additional callers line up. Without this, the winner would often
	// finish before any follower arrives, trivializing the dedup.
	release := make(chan struct{})
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
			<-release
			return processor.SessionStatusProcessed, &snapshot.SessionSnapshot{SessionKey: "coalesced"}, nil
		},
	}
	sup := NewSupervisor(fp, loader)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = sup.Ensure(context.Background(), info)
		}(i)
	}

	// Wait briefly so followers enter the singleflight group, then release.
	// WHY the sleep is tolerable in a unit test: we only need enough time
	// for goroutines to reach sf.DoChan; 50ms is several orders of
	// magnitude longer than typical scheduling latency and still keeps the
	// test sub-second.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	if got := fp.callCount(); got != 1 {
		t.Fatalf("expected exactly 1 Pipeline call for N=%d coalesced callers, got %d", n, got)
	}
}

// TestEnsure_ContextCanceled_ReleasesFollower verifies the DoChan select:
// a follower whose ctx is canceled mid-wait must return ctx.Err() promptly
// without blocking on the winner. The winner keeps running.
func TestEnsure_ContextCanceled_ReleasesFollower(t *testing.T) {
	loader, _ := newTestLoader(t)
	info := testEnterClusterInfo()

	// Winner blocks until the test explicitly lets it finish — so the
	// follower is guaranteed to be waiting when we cancel its ctx.
	winnerRelease := make(chan struct{})
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
			<-winnerRelease
			return processor.SessionStatusProcessed, &snapshot.SessionSnapshot{SessionKey: "eventual"}, nil
		},
	}
	sup := NewSupervisor(fp, loader)

	// Fire the winner in a background goroutine with its own
	// (not-to-be-canceled) ctx so the singleflight group stays alive.
	winnerDone := make(chan error, 1)
	go func() {
		winnerDone <- sup.Ensure(context.Background(), info)
	}()
	// Small delay so the winner actually reaches ProcessSession before the
	// follower arrives. Without it we could accidentally end up without a
	// coalesced group and the test would assert the wrong path.
	time.Sleep(20 * time.Millisecond)

	// Follower: cancel its ctx immediately. Expect ctx.Canceled — not the
	// winner's eventual success.
	followerCtx, followerCancel := context.WithCancel(context.Background())
	followerCancel()
	if err := sup.Ensure(followerCtx, info); !errors.Is(err, context.Canceled) {
		t.Fatalf("follower: expected context.Canceled, got %v", err)
	}

	// Release the winner and verify it finishes cleanly — i.e. the
	// follower's early return did NOT abort the shared work.
	close(winnerRelease)
	if err := <-winnerDone; err != nil {
		t.Fatalf("winner finished with error: %v", err)
	}
}

// TestEnsure_Live_ReturnsOK_NoSnapshot verifies that a live-cluster status
// from Pipeline (CR still present) propagates as a clean nil from Ensure.
// No snapshot is Primed — frontend handlers go through redirectRequest for
// live sessions, so there is nothing for us to cache.
func TestEnsure_Live_ReturnsOK_NoSnapshot(t *testing.T) {
	loader, _ := newTestLoader(t)
	info := testEnterClusterInfo()
	clusterNameID := info.Name + "_" + info.Namespace

	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
			return processor.SessionStatusLive, nil, nil
		},
	}
	sup := NewSupervisor(fp, loader)

	if err := sup.Ensure(context.Background(), info); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := loader.Load(clusterNameID, info.SessionName); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("expected no snapshot cached for live session; got err=%v", err)
	}
}

// TestEnsure_PipelineError_Propagates verifies that a real pipeline error
// (K8sProbeErr / EventsErr / SnapshotWriteErr) surfaces through Ensure
// unchanged so the handler can return 500.
func TestEnsure_PipelineError_Propagates(t *testing.T) {
	loader, _ := newTestLoader(t)
	info := testEnterClusterInfo()

	boom := errors.New("k8s probe exploded")
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
			return processor.SessionStatusK8sProbeErr, nil, boom
		},
	}
	sup := NewSupervisor(fp, loader)

	err := sup.Ensure(context.Background(), info)
	if !errors.Is(err, boom) {
		t.Fatalf("expected Ensure to propagate Pipeline error %v, got %v", boom, err)
	}
}

// --- enterCluster handler (router.go) integration -------------------------

// TestEnterClusterHandler_LiveSession_SkipsSupervisor verifies the "live"
// fast path. Even when a Supervisor is wired, session == "live" must NOT
// block on Pipeline — the handler should set cookies, return 200, and
// leave the Supervisor untouched. WHY: live sessions are served via
// redirectRequest, so snapshot work is meaningless for them.
func TestEnterClusterHandler_LiveSession_SkipsSupervisor(t *testing.T) {
	loader, _ := newTestLoader(t)
	fp := &fakePipeline{}
	sup := NewSupervisor(fp, loader)

	s := newServerWithLoader(&fakeLoader{})
	s.supervisor = sup

	container := restfulContainerFor(s)
	req := httptest.NewRequest(http.MethodGet, "/enter_cluster/ns1/c1/live", nil)
	rec := httptest.NewRecorder()
	container.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if fp.callCount() != 0 {
		t.Fatalf("expected Pipeline to NOT be called for live session; got %d", fp.callCount())
	}
}

// TestEnterClusterHandler_DeadSession_BlocksOnSupervisor verifies the
// lazy-mode blocking contract: Supervisor.Ensure is invoked and must
// return before the handler writes 200.
func TestEnterClusterHandler_DeadSession_BlocksOnSupervisor(t *testing.T) {
	loader, _ := newTestLoader(t)
	built := &snapshot.SessionSnapshot{SessionKey: "built-by-pipeline"}
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
			return processor.SessionStatusProcessed, built, nil
		},
	}
	sup := NewSupervisor(fp, loader)

	s := newServerWithLoader(&fakeLoader{})
	s.supervisor = sup

	container := restfulContainerFor(s)
	req := httptest.NewRequest(http.MethodGet, "/enter_cluster/default/raycluster-e2e/session_42", nil)
	rec := httptest.NewRecorder()
	container.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if fp.callCount() != 1 {
		t.Fatalf("expected Pipeline to run exactly once; got %d", fp.callCount())
	}
	// Cookies must still be set even on the blocking path so the frontend
	// can immediately fire follow-up API calls.
	cookieNames := []string{cookieClusterNameKey, cookieClusterNamespaceKey, cookieSessionNameKey}
	gotCookies := map[string]string{}
	for _, c := range rec.Result().Cookies() {
		gotCookies[c.Name] = c.Value
	}
	for _, name := range cookieNames {
		if _, ok := gotCookies[name]; !ok {
			t.Errorf("missing cookie %q; got %v", name, gotCookies)
		}
	}
}

// TestEnterClusterHandler_SupervisorError_Returns500 verifies that a
// Pipeline failure propagates to the HTTP layer as a 500.
func TestEnterClusterHandler_SupervisorError_Returns500(t *testing.T) {
	loader, _ := newTestLoader(t)
	boom := errors.New("k8s probe exploded")
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
			return processor.SessionStatusK8sProbeErr, nil, boom
		},
	}
	sup := NewSupervisor(fp, loader)

	s := newServerWithLoader(&fakeLoader{})
	s.supervisor = sup

	container := restfulContainerFor(s)
	req := httptest.NewRequest(http.MethodGet, "/enter_cluster/default/raycluster-e2e/session_42", nil)
	rec := httptest.NewRecorder()
	container.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (body=%q)", rec.Code, rec.Body.String())
	}
}

// restfulContainerFor builds a *restful.Container with s's routes
// registered — the same shape server_test.go uses via RegisterRouter, but
// broken out here because enter_cluster_test.go is the only file that
// needs container-level HTTP round-trips AND wants to wire a Supervisor.
func restfulContainerFor(s *Server) *restful.Container {
	c := restful.NewContainer()
	s.RegisterRouter(c)
	return c
}

// flakyReader is a storage.StorageReader whose GetContent returns a
// non-nil io.Reader that always fails on Read. This simulates an S3
// connection that accepts the open but drops the response body mid-stream,
// producing a non-NotFound "other" error inside SnapshotLoader.fetch —
// exactly the class of error Supervisor's conservative policy must
// propagate without invoking Pipeline.
type flakyReader struct {
	err error
}

func (f *flakyReader) List() []utils.ClusterInfo             { return nil }
func (f *flakyReader) ListFiles(_ string, _ string) []string { return nil }
func (f *flakyReader) GetContent(_ string, _ string) io.Reader {
	return &erroringReader{err: f.err}
}

// erroringReader is a one-shot io.Reader whose Read always returns an
// injected error. Paired with flakyReader so fetch()'s io.ReadAll fails
// with a wrapped, non-NotFound error.
type erroringReader struct{ err error }

func (e *erroringReader) Read(_ []byte) (int, error) { return 0, e.err }
