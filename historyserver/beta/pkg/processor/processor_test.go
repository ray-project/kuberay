package processor

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// fakePipeline is a minimal sessionProcessor used by processor tests.
// It atomically counts calls and, per-call, consults an optional error
// function so individual sessions can be made to fail deterministically.
//
// Why atomic counters instead of a mutex-protected int: Run() serializes
// calls into processSession anyway (runOnce is a plain for-loop), but the
// test goroutine must observe counts concurrently — atomics keep that
// read-path lock-free and race-detector clean.
type fakePipeline struct {
	calls atomic.Int64

	mu        sync.Mutex
	errBySess map[string]error        // keyed by SessionName; non-nil pairs with K8sProbeErr status
	seenOrder []string                // SessionNames in call order (per test tick)
	onCall    func(utils.ClusterInfo) // optional hook invoked on every call
}

func newFakePipeline() *fakePipeline {
	return &fakePipeline{errBySess: map[string]error{}}
}

func (f *fakePipeline) setErr(sessionName string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errBySess[sessionName] = err
}

// processSession here returns (Processed, nil) by default, or
// (K8sProbeErr, setErr'd-err) when the test configured an error for this
// session. This keeps the existing tests (which only care about call count /
// ordering / "did an error abort the loop?") working without widening their
// surface; tests that exercise specific statuses can assert via seen() and
// add new helpers as needed.
func (f *fakePipeline) processSession(_ context.Context, s utils.ClusterInfo) (SessionStatus, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.seenOrder = append(f.seenOrder, s.SessionName)
	err := f.errBySess[s.SessionName]
	hook := f.onCall
	f.mu.Unlock()
	if hook != nil {
		hook(s)
	}
	if err != nil {
		return SessionStatusK8sProbeErr, err
	}
	return SessionStatusProcessed, nil
}

func (f *fakePipeline) callCount() int64 {
	return f.calls.Load()
}

func (f *fakePipeline) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.seenOrder))
	copy(out, f.seenOrder)
	return out
}

// fakeReader is a minimal storage.StorageReader that returns a canned
// session list. Only List() is exercised by Processor.
type fakeReader struct {
	mu       sync.Mutex
	sessions []utils.ClusterInfo
}

func newFakeReader(sessions []utils.ClusterInfo) *fakeReader {
	return &fakeReader{sessions: sessions}
}

func (r *fakeReader) List() []utils.ClusterInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Return a copy so callers iterating cannot race with seed mutation.
	out := make([]utils.ClusterInfo, len(r.sessions))
	copy(out, r.sessions)
	return out
}

func (r *fakeReader) GetContent(_ string, _ string) io.Reader { return nil }

func (r *fakeReader) ListFiles(_ string, _ string) []string { return nil }

// sess is a tiny ClusterInfo factory to keep table-driven tests readable.
func sess(name string) utils.ClusterInfo {
	return utils.ClusterInfo{
		Name:        "rc-" + name,
		Namespace:   "default",
		SessionName: name,
	}
}

// waitForAtLeast polls f.callCount() until it reaches target or timeout
// elapses. Uses short backoff; no real per-tick sleeping beyond a handful
// of milliseconds so the full suite stays under a few seconds.
func waitForAtLeast(t *testing.T, f *fakePipeline, target int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f.callCount() >= target {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for >=%d calls, got %d", target, f.callCount())
}

// TestProcessor_ImmediateFirstPassAndTicker verifies that Run performs one
// iteration immediately (so fresh deployments don't wait 10 minutes) and
// then ticks periodically. With a 50ms interval and 2 sessions, after
// ~120ms we expect at least 2 ticks worth of calls = >=4.
func TestProcessor_ImmediateFirstPassAndTicker(t *testing.T) {
	pipe := newFakePipeline()
	reader := newFakeReader([]utils.ClusterInfo{sess("a"), sess("b")})

	pr := NewProcessor(pipe, reader, 50*time.Millisecond)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		pr.Run(stop)
		close(done)
	}()

	// Wait for at least 4 calls (immediate pass of 2 + one tick of 2).
	waitForAtLeast(t, pipe, 4, 2*time.Second)

	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Run did not return within 1s of stop")
	}

	if got := pipe.callCount(); got < 4 {
		t.Fatalf("expected >=4 calls, got %d", got)
	}
}

// TestProcessor_OneBadSessionDoesNotAbortPass verifies that an error from
// one session does not short-circuit the remaining sessions in the same
// tick. This is the contract referenced in plan §7 open risk #1.
func TestProcessor_OneBadSessionDoesNotAbortPass(t *testing.T) {
	pipe := newFakePipeline()
	pipe.setErr("b", errors.New("boom"))

	reader := newFakeReader([]utils.ClusterInfo{sess("a"), sess("b"), sess("c")})

	// Use a huge interval so the only pass that happens is the immediate one.
	pr := NewProcessor(pipe, reader, time.Hour)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		pr.Run(stop)
		close(done)
	}()

	// Immediate pass should process all 3 sessions regardless of b's error.
	waitForAtLeast(t, pipe, 3, 2*time.Second)

	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Run did not return within 1s of stop")
	}

	seen := pipe.seen()
	want := []string{"a", "b", "c"}
	if len(seen) < len(want) {
		t.Fatalf("expected at least %v, got %v", want, seen)
	}
	for i, w := range want {
		if seen[i] != w {
			t.Fatalf("session %d: want %q, got %q (full: %v)", i, w, seen[i], seen)
		}
	}
}

// TestProcessor_EmptyListIsSafe verifies no panic and zero processSession
// calls when the reader returns an empty session list.
func TestProcessor_EmptyListIsSafe(t *testing.T) {
	pipe := newFakePipeline()
	reader := newFakeReader(nil)

	pr := NewProcessor(pipe, reader, 20*time.Millisecond)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		pr.Run(stop)
		close(done)
	}()

	// Give enough time for the immediate pass + a tick or two.
	time.Sleep(80 * time.Millisecond)

	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Run did not return within 1s of stop")
	}

	if got := pipe.callCount(); got != 0 {
		t.Fatalf("expected 0 processSession calls with empty list, got %d", got)
	}
}

// TestProcessor_StopStopsLoopPromptly verifies that Run returns quickly
// after stop is closed, even if the configured interval is effectively
// infinite. This is the contract that lets the binary respond to SIGTERM
// without waiting 10 minutes for the next tick.
func TestProcessor_StopStopsLoopPromptly(t *testing.T) {
	pipe := newFakePipeline()
	reader := newFakeReader([]utils.ClusterInfo{sess("only")})

	// One-hour interval: the loop will never tick on its own during the test.
	pr := NewProcessor(pipe, reader, time.Hour)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		pr.Run(stop)
		close(done)
	}()

	// Wait for the immediate pass to land so we know Run is in select-wait.
	waitForAtLeast(t, pipe, 1, time.Second)

	start := time.Now()
	close(stop)
	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("Run took %s to return after stop; want <1s", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatalf("Run did not return within 1s of stop")
	}
}

// TestProcessor_DefaultIntervalWhenZero verifies the zero-interval guard
// in NewProcessor. We don't actually run the loop — just assert the field
// was set, because running at 10min would slow the suite to a crawl.
func TestProcessor_DefaultIntervalWhenZero(t *testing.T) {
	pipe := newFakePipeline()
	reader := newFakeReader(nil)

	pr := NewProcessor(pipe, reader, 0)
	if pr.interval != DefaultInterval {
		t.Fatalf("interval=0 should be promoted to DefaultInterval (%s), got %s", DefaultInterval, pr.interval)
	}

	pr2 := NewProcessor(pipe, reader, -5*time.Second)
	if pr2.interval != DefaultInterval {
		t.Fatalf("negative interval should be promoted to DefaultInterval (%s), got %s", DefaultInterval, pr2.interval)
	}
}
