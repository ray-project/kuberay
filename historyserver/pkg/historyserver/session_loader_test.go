package historyserver

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// fakeProcessor is a configurable test double for processor.
type fakeProcessor struct {
	calls int32
	fn    func(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error)
}

func (f *fakeProcessor) ProcessSession(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.fn == nil {
		return SessionStatusProcessed, &eventserver.SessionSnapshot{}, nil
	}
	return f.fn(ctx, info)
}

func (f *fakeProcessor) callCount() int32 { return atomic.LoadInt32(&f.calls) }

func (f *fakeProcessor) setFn(fn func(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error)) {
	f.fn = fn
}

func newTestSessionLoader(t *testing.T, p processor, cacheSize int) *SessionLoader {
	t.Helper()
	if cacheSize <= 0 {
		cacheSize = DefaultSessionCacheSize
	}
	return NewSessionLoader(p, context.Background(), DefaultSessionProcessTimeout, cacheSize, DefaultSessionCacheTTL)
}

func testEnterClusterInfo() utils.ClusterInfo {
	return utils.ClusterInfo{
		Name:        "raycluster-test",
		Namespace:   "default",
		SessionName: "session_2026-04-22_10-00-00_000000_1",
	}
}

// testSnapshot builds a minimal snapshot.
func testSnapshot(clusterSessionKey string) *eventserver.SessionSnapshot {
	return &eventserver.SessionSnapshot{
		SessionKey:  clusterSessionKey,
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
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
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
	fp.setFn(func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
		return SessionStatusProcessed, &eventserver.SessionSnapshot{}, nil
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
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
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

	fp.setFn(func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
		return SessionStatusProcessed, &eventserver.SessionSnapshot{}, nil
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
				fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
					var built *eventserver.SessionSnapshot
					if tc.status == SessionStatusProcessed {
						built = &eventserver.SessionSnapshot{}
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
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
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
		fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
			return SessionStatusProcessed, &eventserver.SessionSnapshot{}, nil
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

// TestGetSnapshot_PutThenGet verifies the canonical hot path:
// putSnapshot, then GetSnapshot returns a snapshot with matching content.
func TestGetSnapshot_PutThenGet(t *testing.T) {
	info := testEnterClusterInfo()
	clusterSessionKey := utils.BuildClusterSessionKey(info.Name, info.Namespace, info.SessionName)

	sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
	stored := testSnapshot(clusterSessionKey)
	sl.putSnapshot(clusterSessionKey, stored)

	got, ok := sl.GetSnapshot(clusterSessionKey)
	if !ok {
		t.Fatal("GetSnapshot: ok=false")
	}
	if got.SessionKey != stored.SessionKey {
		t.Fatalf("SessionKey: got %q, want %q", got.SessionKey, stored.SessionKey)
	}
}

// TestGetSnapshot_ColdMiss verifies that a miss returns (nil, false).
func TestGetSnapshot_ColdMiss(t *testing.T) {
	info := testEnterClusterInfo()
	clusterSessionKey := utils.BuildClusterSessionKey(info.Name, info.Namespace, info.SessionName)

	sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
	snap, ok := sl.GetSnapshot(clusterSessionKey)
	if ok || snap != nil {
		t.Fatalf("expected (nil, false) on cold miss, got (%v, %v)", snap, ok)
	}
}

// TestGetSnapshot_PutOverwrites verifies that putSnapshot replaces any prior entry.
func TestGetSnapshot_PutOverwrites(t *testing.T) {
	info := testEnterClusterInfo()
	clusterSessionKey := utils.BuildClusterSessionKey(info.Name, info.Namespace, info.SessionName)

	stale := testSnapshot(clusterSessionKey)
	stale.GeneratedAt = time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)
	fresh := testSnapshot(clusterSessionKey)

	sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
	sl.putSnapshot(clusterSessionKey, stale)
	sl.putSnapshot(clusterSessionKey, fresh)

	got, ok := sl.GetSnapshot(clusterSessionKey)
	if !ok {
		t.Fatal("Get: ok=false")
	}
	if !got.GeneratedAt.Equal(fresh.GeneratedAt) {
		t.Fatalf("putSnapshot did not overwrite; GeneratedAt = %v, want %v", got.GeneratedAt, fresh.GeneratedAt)
	}
}

// TestGetSnapshot_TasksIsPerRequest verifies that mutating the slice returned
// by one GetSnapshot call must not affect another concurrent reader of the same
// cached entry.
func TestGetSnapshot_TasksIsPerRequest(t *testing.T) {
	info := testEnterClusterInfo()
	key := utils.BuildClusterSessionKey(info.Name, info.Namespace, info.SessionName)

	sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
	sl.putSnapshot(key, &eventserver.SessionSnapshot{
		SessionKey: key,
		Tasks:      []eventtypes.Task{{TaskID: "c"}, {TaskID: "a"}, {TaskID: "b"}},
	})

	first, _ := sl.GetSnapshot(key)
	second, _ := sl.GetSnapshot(key)

	sort.Slice(first.Tasks, func(i, j int) bool {
		return first.Tasks[i].TaskID < first.Tasks[j].TaskID
	})

	wantOriginal := []string{"c", "a", "b"}
	for i, task := range second.Tasks {
		if task.TaskID != wantOriginal[i] {
			t.Fatalf("second.Tasks[%d].TaskID = %q, want %q (mutation leaked)", i, task.TaskID, wantOriginal[i])
		}
	}
}

// TestGetSnapshot_LRUEviction verifies that once capacity is exceeded, the
// oldest cached entry is evicted and GetSnapshot for that key returns ok=false.
func TestGetSnapshot_LRUEviction(t *testing.T) {
	info := testEnterClusterInfo()
	k1 := utils.BuildClusterSessionKey(info.Name, info.Namespace, "session_2026-04-22_10-00-00_000000_1")
	k2 := utils.BuildClusterSessionKey(info.Name, info.Namespace, "session_2026-04-22_11-00-00_000000_1")
	k3 := utils.BuildClusterSessionKey(info.Name, info.Namespace, "session_2026-04-22_12-00-00_000000_1")

	sl := newTestSessionLoader(t, &fakeProcessor{}, 2)
	sl.putSnapshot(k1, testSnapshot(k1))
	sl.putSnapshot(k2, testSnapshot(k2))
	// Third session evicts the first (LRU).
	sl.putSnapshot(k3, testSnapshot(k3))

	if _, ok := sl.GetSnapshot(k1); ok {
		t.Fatal("expected first session to be evicted")
	}
	if _, ok := sl.GetSnapshot(k2); !ok {
		t.Fatal("expected second session to still be cached")
	}
	if _, ok := sl.GetSnapshot(k3); !ok {
		t.Fatal("expected third session to still be cached")
	}
}

// TestGetSnapshot_TTLExpiry verifies that when a TTL is set, a cached snapshot
// expires after the TTL elapses and GetSnapshot returns ok=false.
func TestGetSnapshot_TTLExpiry(t *testing.T) {
	info := testEnterClusterInfo()
	key := utils.BuildClusterSessionKey(info.Name, info.Namespace, info.SessionName)

	const ttl = 30 * time.Millisecond
	sl := NewSessionLoader(&fakeProcessor{}, context.Background(), DefaultSessionProcessTimeout, DefaultSessionCacheSize, ttl)
	sl.putSnapshot(key, testSnapshot(key))

	if _, ok := sl.GetSnapshot(key); !ok {
		t.Fatal("expected snapshot to be cached right after put")
	}

	// Gone once the TTL elapses.
	time.Sleep(80 * time.Millisecond)
	if _, ok := sl.GetSnapshot(key); ok {
		t.Fatal("expected snapshot to be evicted after TTL expiry")
	}
}
