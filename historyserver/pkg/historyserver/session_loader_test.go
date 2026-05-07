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

// fakeProcessor is a configurable test double for processor.
type fakeProcessor struct {
	calls int32
	fn    func(ctx context.Context, info utils.ClusterInfo) (SessionStatus, error)
}

func (f *fakeProcessor) ProcessSession(ctx context.Context, info utils.ClusterInfo) (SessionStatus, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.fn == nil {
		return SessionStatusProcessed, nil
	}
	return f.fn(ctx, info)
}

func (f *fakeProcessor) callCount() int32 { return atomic.LoadInt32(&f.calls) }

func (f *fakeProcessor) setFn(fn func(ctx context.Context, info utils.ClusterInfo) (SessionStatus, error)) {
	f.fn = fn
}

func testEnterClusterInfo() utils.ClusterInfo {
	return utils.ClusterInfo{
		Name:        "raycluster-test",
		Namespace:   "default",
		SessionName: "session_2026-04-22_10-00-00_000000_1",
	}
}

// TestLoadSession_ProcessorError_PropagatesAndCleans verifies that on the error
// path, all coalesced callers see the same error and the dedup group is cleared.
func TestLoadSession_ProcessorError_PropagatesAndCleans(t *testing.T) {
	info := testEnterClusterInfo()
	processorErr := errors.New("simulated parse failure")

	// Phase 1: error path
	release := make(chan struct{})
	fp := &fakeProcessor{
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, error) {
			<-release
			return SessionStatusEventsErr, processorErr
		},
	}
	sessionLoader := NewSessionLoader(fp, context.Background())

	const n = 5
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = sessionLoader.LoadSession(context.Background(), info)
		}(i)
	}

	// 50ms >> scheduling latency, well under a second.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := fp.callCount(); got != 1 {
		t.Fatalf("expected exactly 1 processor call (dedup applies on error path), got %d", got)
	}
	for i, err := range errs {
		if !errors.Is(err, processorErr) {
			t.Errorf("caller %d: expected error wrapping processorErr, got %v", i, err)
		}
	}

	// Phase 2: dedup group cleared
	fp.setFn(func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, error) {
		return SessionStatusProcessed, nil
	})
	if _, err := sessionLoader.LoadSession(context.Background(), info); err != nil {
		t.Fatalf("post-error retry: expected nil, got %v", err)
	}
	if got := fp.callCount(); got != 2 {
		t.Fatalf("expected processor called 2x total (1 error + 1 retry), got %d", got)
	}
}

// TestLoadSession_ProcessorError_NoInternalRetry verifies a processor error
// is returned to the caller exactly once; SessionLoader does not re-invoke
// processor within the same LoadSession call.
func TestLoadSession_ProcessorError_NoInternalRetry(t *testing.T) {
	info := testEnterClusterInfo()
	processorErr := errors.New("simulated parse failure")
	fp := &fakeProcessor{
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, error) {
			return SessionStatusEventsErr, processorErr
		},
	}
	sessionLoader := NewSessionLoader(fp, context.Background())

	_, err := sessionLoader.LoadSession(context.Background(), info)
	if !errors.Is(err, processorErr) {
		t.Fatalf("expected LoadSession to return processorErr, got %v", err)
	}
	if got := fp.callCount(); got != 1 {
		t.Fatalf("expected processor called exactly 1x (no internal retry), got %d", got)
	}

	fp.setFn(func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, error) {
		return SessionStatusProcessed, nil
	})
	if _, err := sessionLoader.LoadSession(context.Background(), info); err != nil {
		t.Fatalf("client-driven retry: expected nil, got %v", err)
	}
	if got := fp.callCount(); got != 2 {
		t.Fatalf("expected processor called 2x total (1 error + 1 client retry), got %d", got)
	}
}

// TestLoadSession_LiveAndProcessed verifies LoadSession surfaces the live/processed distinction.
func TestLoadSession_LiveAndProcessed(t *testing.T) {
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
			fp := &fakeProcessor{
				fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, error) {
					return tc.status, nil
				},
			}
			sessionLoader := NewSessionLoader(fp, context.Background())

			live, err := sessionLoader.LoadSession(context.Background(), testEnterClusterInfo())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if live != tc.wantLive {
				t.Fatalf("live = %v, want %v", live, tc.wantLive)
			}
		})
	}
}

// TestLoadSession_FastPath_SkipsSingleflight verifies the fast-path:
// once a session is loaded, repeat LoadSession calls must not invoke processor again.
func TestLoadSession_FastPath_SkipsSingleflight(t *testing.T) {
	fp := &fakeProcessor{
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, error) {
			return SessionStatusProcessed, nil
		},
	}
	sessionLoader := NewSessionLoader(fp, context.Background())
	info := testEnterClusterInfo()

	// First call: cold path, processor runs, session marked loaded.
	if _, err := sessionLoader.LoadSession(context.Background(), info); err != nil {
		t.Fatalf("cold-path LoadSession: %v", err)
	}
	if got := fp.callCount(); got != 1 {
		t.Fatalf("after cold path: callCount = %d, want 1", got)
	}

	// Subsequent calls: fast path, processor must NOT be invoked again.
	for i := 0; i < 5; i++ {
		if _, err := sessionLoader.LoadSession(context.Background(), info); err != nil {
			t.Fatalf("fast path LoadSession #%d: %v", i, err)
		}
	}
	if got := fp.callCount(); got != 1 {
		t.Fatalf("after fast path: callCount = %d, want 1 (processor must not be invoked again)", got)
	}
}
