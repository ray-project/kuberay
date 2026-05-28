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
	fn    func(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *SessionSnapshot, error)
}

func (f *fakeProcessor) ProcessSession(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *SessionSnapshot, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.fn == nil {
		return SessionStatusProcessed, &SessionSnapshot{}, nil
	}
	return f.fn(ctx, info)
}

func (f *fakeProcessor) callCount() int32 { return atomic.LoadInt32(&f.calls) }

func (f *fakeProcessor) setFn(fn func(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *SessionSnapshot, error)) {
	f.fn = fn
}

func newTestSessionLoader(t *testing.T, p processor, cacheSize int) *SessionLoader {
	t.Helper()
	if cacheSize <= 0 {
		cacheSize = DefaultSessionCacheSize
	}
	return NewSessionLoader(p, context.Background(), DefaultSessionProcessTimeout, cacheSize)
}

func testEnterClusterInfo() utils.ClusterInfo {
	return utils.ClusterInfo{
		Name:        "raycluster-test",
		Namespace:   "default",
		SessionName: "session_2026-04-22_10-00-00_000000_1",
	}
}

// snapWith returns a minimal snapshot pointer for cache seeding.
func snapWith(sessionKey string) *SessionSnapshot {
	return &SessionSnapshot{
		SessionKey:  sessionKey,
		GeneratedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
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
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *SessionSnapshot, error) {
			<-release
			return SessionStatusEventsErr, nil, processorErr
		},
	}
	sessionLoader := newTestSessionLoader(t, fp, 0)

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
	fp.setFn(func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *SessionSnapshot, error) {
		return SessionStatusProcessed, &SessionSnapshot{}, nil
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
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *SessionSnapshot, error) {
			return SessionStatusEventsErr, nil, processorErr
		},
	}
	sessionLoader := newTestSessionLoader(t, fp, 0)

	_, err := sessionLoader.LoadSession(context.Background(), info)
	if !errors.Is(err, processorErr) {
		t.Fatalf("expected LoadSession to return processorErr, got %v", err)
	}
	if got := fp.callCount(); got != 1 {
		t.Fatalf("expected processor called exactly 1x (no internal retry), got %d", got)
	}

	fp.setFn(func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *SessionSnapshot, error) {
		return SessionStatusProcessed, &SessionSnapshot{}, nil
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
				fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *SessionSnapshot, error) {
					var built *SessionSnapshot
					if tc.status == SessionStatusProcessed {
						built = &SessionSnapshot{}
					}
					return tc.status, built, nil
				},
			}
			sessionLoader := newTestSessionLoader(t, fp, 0)

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

// TestLoadSession_ZeroValueStatus_DoesNotSilentlyMatchLive verifies the zero-value
// SessionStatus (SessionStatusUnknown) surfaces an error.
func TestLoadSession_ZeroValueStatus_DoesNotSilentlyMatchLive(t *testing.T) {
	fp := &fakeProcessor{
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *SessionSnapshot, error) {
			var zero SessionStatus // SessionStatusUnknown
			return zero, nil, nil
		},
	}
	sessionLoader := newTestSessionLoader(t, fp, 0)

	live, err := sessionLoader.LoadSession(context.Background(), testEnterClusterInfo())
	if err == nil {
		t.Fatalf("expected error from zero-value status, got nil (live=%v)", live)
	}
	if live {
		t.Fatalf("zero-value status must not produce live=true (got live=%v, err=%v)", live, err)
	}
}

// TestLoadSession_FastPath_SkipsSingleflight verifies the fast-path:
// once a session is loaded, repeat LoadSession calls must not invoke processor again.
func TestLoadSession_FastPath_SkipsSingleflight(t *testing.T) {
	fp := &fakeProcessor{
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *SessionSnapshot, error) {
			return SessionStatusProcessed, &SessionSnapshot{}, nil
		},
	}
	sessionLoader := newTestSessionLoader(t, fp, 0)
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

// TestGetSnapshot_PrimeThenGet verifies the canonical hot path:
// prime, then two Gets each return the exact pointer that was Primed.
func TestGetSnapshot_PrimeThenGet(t *testing.T) {
	sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
	key := utils.BuildClusterSessionKey("cluster-a", "default", "session-1")

	primed := snapWith("key-1")
	sl.prime(key, primed)

	first, ok := sl.GetSnapshot(key)
	if !ok {
		t.Fatal("first GetSnapshot: ok=false")
	}
	second, ok := sl.GetSnapshot(key)
	if !ok {
		t.Fatal("second GetSnapshot: ok=false")
	}
	if first != primed || second != primed {
		t.Fatalf("expected same pointer on cache hit, got %p / %p (want %p)", first, second, primed)
	}
}

// TestGetSnapshot_ColdMiss verifies that a miss returns (nil, false), mirroring
// the original `_, ok := loaded[key]` semantics.
func TestGetSnapshot_ColdMiss(t *testing.T) {
	sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
	key := utils.BuildClusterSessionKey("cluster-c", "default", "session-3")

	snap, ok := sl.GetSnapshot(key)
	if ok || snap != nil {
		t.Fatalf("expected (nil, false) on cold miss, got (%v, %v)", snap, ok)
	}
}

// TestGetSnapshot_MissDoesNotPersist verifies that a miss is not remembered:
// a later prime + Get must succeed. WHY: a negative cache would prevent
// fresh-build Primes from taking effect.
func TestGetSnapshot_MissDoesNotPersist(t *testing.T) {
	sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
	key := utils.BuildClusterSessionKey("cluster-c", "default", "session-3")

	if _, ok := sl.GetSnapshot(key); ok {
		t.Fatal("expected miss before prime")
	}

	primed := snapWith("key-3")
	sl.prime(key, primed)

	got, ok := sl.GetSnapshot(key)
	if !ok || got != primed {
		t.Fatalf("Get after prime: got=(%p, %v); want=(%p, true) — negative cache leaked", got, ok, primed)
	}
}

// TestGetSnapshot_PrimeOverwrites verifies that prime replaces any prior entry.
func TestGetSnapshot_PrimeOverwrites(t *testing.T) {
	sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
	key := utils.BuildClusterSessionKey("cluster-overwrite", "default", "session-overwrite")

	sl.prime(key, snapWith("stale"))
	sl.prime(key, snapWith("fresh"))

	got, ok := sl.GetSnapshot(key)
	if !ok {
		t.Fatal("Get: ok=false")
	}
	if got.SessionKey != "fresh" {
		t.Fatalf("prime did not overwrite; got SessionKey=%q, want %q", got.SessionKey, "fresh")
	}
}

// TestGetSnapshot_LRUEviction verifies that once capacity is exceeded, the
// oldest Primed entry is evicted and GetSnapshot for that key returns ok=false.
func TestGetSnapshot_LRUEviction(t *testing.T) {
	sl := newTestSessionLoader(t, &fakeProcessor{}, 2)
	k1 := utils.BuildClusterSessionKey("cluster-d", "default", "s1")
	k2 := utils.BuildClusterSessionKey("cluster-d", "default", "s2")
	k3 := utils.BuildClusterSessionKey("cluster-d", "default", "s3")

	sl.prime(k1, snapWith("k1"))
	sl.prime(k2, snapWith("k2"))
	// s3 evicts s1 (LRU).
	sl.prime(k3, snapWith("k3"))

	if _, ok := sl.GetSnapshot(k1); ok {
		t.Fatal("expected s1 to be evicted")
	}
	if _, ok := sl.GetSnapshot(k2); !ok {
		t.Fatal("expected s2 to still be cached")
	}
	if _, ok := sl.GetSnapshot(k3); !ok {
		t.Fatal("expected s3 to still be cached")
	}
}

// TestGetSnapshot_ConcurrentPrimeGet asserts that concurrent prime/Get calls
// are safe — golang-lru/v2 locks internally, so we only assert that the
// final state has the snapshot present and no goroutine panicked.
func TestGetSnapshot_ConcurrentPrimeGet(t *testing.T) {
	sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
	key := utils.BuildClusterSessionKey("cluster-e", "default", "session-hot")

	primed := snapWith("hot")
	sl.prime(key, primed)

	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sl.prime(key, primed)
			got, ok := sl.GetSnapshot(key)
			if !ok || got != primed {
				t.Errorf("concurrent Get: got=(%p, %v) want=(%p, true)", got, ok, primed)
			}
		}()
	}
	wg.Wait()
}
