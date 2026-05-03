package historyserver

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// fakePipeline is a test double for sessionPipeline that lets each test
// dictate the exact (status, error) tuple ProcessSession returns and
// count the number of invocations.
type fakePipeline struct {
	calls int32
	fn    func(ctx context.Context, info utils.ClusterInfo) (SessionStatus, error)
}

func (f *fakePipeline) ProcessSession(ctx context.Context, info utils.ClusterInfo) (SessionStatus, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.fn == nil {
		return SessionStatusProcessed, nil
	}
	return f.fn(ctx, info)
}

func (f *fakePipeline) callCount() int32 { return atomic.LoadInt32(&f.calls) }

func (f *fakePipeline) setFn(fn func(ctx context.Context, info utils.ClusterInfo) (SessionStatus, error)) {
	f.fn = fn
}

func testEnterClusterInfo() utils.ClusterInfo {
	return utils.ClusterInfo{
		Name:        "raycluster-test",
		Namespace:   "default",
		SessionName: "session_2026-04-22_10-00-00_000000_1",
	}
}

// TestEnsure_PipelineError_PropagatesAndCleans verifies the dedup
// behavior on the error path: when Pipeline returns an error, all
// coalesced callers see the same error AND the dedup group is cleared
// so a subsequent call starts a fresh singleflight group.
func TestEnsure_PipelineError_PropagatesAndCleans(t *testing.T) {
	info := testEnterClusterInfo()
	pipelineErr := errors.New("simulated parse failure")

	// Phase 1 — N concurrent callers; Pipeline errors once and all
	// callers must receive the same error.
	release := make(chan struct{})
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, error) {
			<-release
			return SessionStatusEventsErr, pipelineErr
		},
	}
	sup := NewSupervisor(fp, context.Background())

	const n = 5
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = sup.Ensure(context.Background(), info)
		}(i)
	}

	// Barrier: let all N goroutines park inside Ensure before we let
	// the winner finish. 50 ms is several orders of magnitude longer
	// than scheduling latency yet keeps the test well under a second.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := fp.callCount(); got != 1 {
		t.Fatalf("expected exactly 1 Pipeline call (dedup applies on error path), got %d", got)
	}
	for i, err := range errs {
		if !errors.Is(err, pipelineErr) {
			t.Errorf("caller %d: expected error wrapping pipelineErr, got %v", i, err)
		}
	}

	// Phase 2 — the dedup group must be cleared after the failed call;
	// a fresh Pipeline invocation should run for the next /enter_cluster.
	fp.setFn(func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, error) {
		return SessionStatusProcessed, nil
	})
	if _, err := sup.Ensure(context.Background(), info); err != nil {
		t.Fatalf("post-error retry: expected nil, got %v", err)
	}
	if got := fp.callCount(); got != 2 {
		t.Fatalf("expected Pipeline called 2x total (1 error + 1 retry), got %d", got)
	}
}

// TestEnsure_PipelineError_NoInternalRetry verifies a Pipeline error
// is returned to the caller exactly once; Supervisor does not re-invoke
// Pipeline within the same Ensure call.
func TestEnsure_PipelineError_NoInternalRetry(t *testing.T) {
	info := testEnterClusterInfo()
	pipelineErr := errors.New("simulated parse failure")
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, error) {
			return SessionStatusEventsErr, pipelineErr
		},
	}
	sup := NewSupervisor(fp, context.Background())

	_, err := sup.Ensure(context.Background(), info)
	if !errors.Is(err, pipelineErr) {
		t.Fatalf("expected Ensure to return pipelineErr, got %v", err)
	}
	if got := fp.callCount(); got != 1 {
		t.Fatalf("expected Pipeline called exactly 1x (no internal retry), got %d", got)
	}

	// Loaded set must be empty, which can be verified by triggering a second Ensure
	// and confirming that Pipeline runs again.
	fp.setFn(func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, error) {
		return SessionStatusProcessed, nil
	})
	if _, err := sup.Ensure(context.Background(), info); err != nil {
		t.Fatalf("client-driven retry: expected nil, got %v", err)
	}
	if got := fp.callCount(); got != 2 {
		t.Fatalf("expected Pipeline called 2x total (1 error + 1 client retry), got %d", got)
	}
}

// TestEnsure_LiveAndProcessed verifies Ensure surfaces the live/processed
// distinction so the router can rewrite the session-name cookie when a live
// cluster is reached via its real session name.
func TestEnsure_LiveAndProcessed(t *testing.T) {
	tests := []struct {
		name     string
		status   SessionStatus
		wantLive bool
	}{
		{"processed -> live=false", SessionStatusProcessed, false},
		{"live -> live=true", SessionStatusLive, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fp := &fakePipeline{
				fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, error) {
					return tc.status, nil
				},
			}
			sup := NewSupervisor(fp, context.Background())

			live, err := sup.Ensure(context.Background(), testEnterClusterInfo())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if live != tc.wantLive {
				t.Fatalf("live = %v, want %v", live, tc.wantLive)
			}
		})
	}
}

// TestEnsure_FastPath_SkipsSingleflight verifies the fast-path:
// once a session is loaded, repeat Ensure calls must not invoke Pipeline again.
func TestEnsure_FastPath_SkipsSingleflight(t *testing.T) {
	fp := &fakePipeline{
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, error) {
			return SessionStatusProcessed, nil
		},
	}
	sup := NewSupervisor(fp, context.Background())
	info := testEnterClusterInfo()

	// First call: cold path, Pipeline runs, session marked loaded.
	if _, err := sup.Ensure(context.Background(), info); err != nil {
		t.Fatalf("cold-path Ensure: %v", err)
	}
	if got := fp.callCount(); got != 1 {
		t.Fatalf("after cold path: callCount = %d, want 1", got)
	}

	// Subsequent calls: fast path, Pipeline must NOT be invoked again.
	for i := 0; i < 5; i++ {
		if _, err := sup.Ensure(context.Background(), info); err != nil {
			t.Fatalf("fast path Ensure #%d: %v", i, err)
		}
	}
	if got := fp.callCount(); got != 1 {
		t.Fatalf("after fast path: callCount = %d, want 1 (Pipeline must not be invoked again)", got)
	}
}
