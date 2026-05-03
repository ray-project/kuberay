package server

import (
	"context"
	"errors"
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

func newTestLoader(t *testing.T) *SnapshotLoader {
	t.Helper()
	loader, err := NewSnapshotLoader(0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}
	return loader
}

// TestEnsure_SnapshotCached_NoPipelineCall is the LRU happy path: the
// snapshot was Primed by a prior call. Ensure must NOT invoke Pipeline.
func TestEnsure_SnapshotCached_NoPipelineCall(t *testing.T) {
	loader := newTestLoader(t)
	info := testEnterClusterInfo()
	clusterNameID := info.Name + "_" + info.Namespace

	loader.Prime(clusterNameID, info.SessionName, &snapshot.SessionSnapshot{SessionKey: "primed"})

	fp := &fakePipeline{}
	sup := NewSupervisor(fp, loader, context.Background())

	if err := sup.Ensure(context.Background(), info); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got := fp.callCount(); got != 0 {
		t.Fatalf("expected Pipeline to NOT be called when LRU is hot; got %d calls", got)
	}
}

// TestEnsure_Missing_PipelineRuns_AndPrimes is the cold path: no snapshot
// in the LRU, so Pipeline runs and Ensure Primes the result. Verified via a
// follow-up Load — a cache miss would blow up the test.
func TestEnsure_Missing_PipelineRuns_AndPrimes(t *testing.T) {
	loader := newTestLoader(t)
	info := testEnterClusterInfo()
	clusterNameID := info.Name + "_" + info.Namespace

	built := &snapshot.SessionSnapshot{SessionKey: "freshly-built"}
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
			return processor.SessionStatusProcessed, built, nil
		},
	}
	sup := NewSupervisor(fp, loader, context.Background())

	if err := sup.Ensure(context.Background(), info); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got := fp.callCount(); got != 1 {
		t.Fatalf("expected Pipeline to be called exactly once; got %d", got)
	}

	got, err := loader.Load(clusterNameID, info.SessionName)
	if err != nil {
		t.Fatalf("Load after Ensure should be cache-hit but got err: %v", err)
	}
	if got != built {
		t.Fatalf("expected Load to return the primed pointer; got %p want %p", got, built)
	}
}

// TestEnsure_ConcurrentCallers_PipelineOnce is the singleflight contract:
// N goroutines hitting Ensure for the same session concurrently must cause
// at most 1 Pipeline invocation. We verify both the coalescing and the
// SingleflightDedupTotal metric increment.
func TestEnsure_ConcurrentCallers_PipelineOnce(t *testing.T) {
	loader := newTestLoader(t)
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
	sup := NewSupervisor(fp, loader, context.Background())

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
	loader := newTestLoader(t)
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
	sup := NewSupervisor(fp, loader, context.Background())

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
	loader := newTestLoader(t)
	info := testEnterClusterInfo()
	clusterNameID := info.Name + "_" + info.Namespace

	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
			return processor.SessionStatusLive, nil, nil
		},
	}
	sup := NewSupervisor(fp, loader, context.Background())

	if err := sup.Ensure(context.Background(), info); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := loader.Load(clusterNameID, info.SessionName); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("expected no snapshot cached for live session; got err=%v", err)
	}
}

// TestEnsure_PipelineError_Propagates verifies that a real pipeline error
// (K8sProbeErr / EventsErr) surfaces through Ensure unchanged so the
// handler can return 500.
func TestEnsure_PipelineError_Propagates(t *testing.T) {
	loader := newTestLoader(t)
	info := testEnterClusterInfo()

	boom := errors.New("k8s probe exploded")
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
			return processor.SessionStatusK8sProbeErr, nil, boom
		},
	}
	sup := NewSupervisor(fp, loader, context.Background())

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
	loader := newTestLoader(t)
	fp := &fakePipeline{}
	sup := NewSupervisor(fp, loader, context.Background())

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
	loader := newTestLoader(t)
	built := &snapshot.SessionSnapshot{SessionKey: "built-by-pipeline"}
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
			return processor.SessionStatusProcessed, built, nil
		},
	}
	sup := NewSupervisor(fp, loader, context.Background())

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
	loader := newTestLoader(t)
	boom := errors.New("k8s probe exploded")
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (processor.SessionStatus, *snapshot.SessionSnapshot, error) {
			return processor.SessionStatusK8sProbeErr, nil, boom
		},
	}
	sup := NewSupervisor(fp, loader, context.Background())

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
