package historyserver

import (
	"bytes"
	"context"
	"errors"
	"reflect"
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

type loaderTestConfig struct {
	cacheSize int
	maxBytes  int
	cacheTTL  time.Duration
}

func newTestLoader(t *testing.T, p processor, cfg loaderTestConfig) *SessionLoader {
	t.Helper()
	cacheSize := cfg.cacheSize
	if cacheSize <= 0 {
		cacheSize = DefaultSessionCacheSize
	}
	return NewSessionLoader(p, context.Background(), DefaultSessionProcessTimeout, cacheSize, cfg.maxBytes, cfg.cacheTTL)
}

func testClusterInfo() utils.ClusterInfo {
	return utils.ClusterInfo{
		Name:        "raycluster-test",
		Namespace:   "default",
		SessionName: "session_2026-04-22_10-00-00_000000_1",
	}
}

func testClusterSessionKey() string {
	info := testClusterInfo()
	return utils.BuildClusterSessionKey(info.Name, info.Namespace, info.SessionName)
}

func testClusterSessionKeyFor(sessionName string) string {
	info := testClusterInfo()
	return utils.BuildClusterSessionKey(info.Name, info.Namespace, sessionName)
}

// testSnapshot builds a minimal snapshot.
func testSnapshot(clusterSessionKey string) *eventserver.SessionSnapshot {
	return &eventserver.SessionSnapshot{
		SessionKey: clusterSessionKey,
	}
}

func requireSnapshotCached(t *testing.T, sl *SessionLoader, key string, wantOK bool) {
	t.Helper()
	_, ok := sl.GetSnapshot(key)
	if ok != wantOK {
		t.Fatalf("GetSnapshot(%q): ok=%v, want %v", key, ok, wantOK)
	}
}

// A failed cold load must not block the next LoadSession from re-running the processor.
func assertProcessorRetryAfterError(t *testing.T, sl *SessionLoader, fp *fakeProcessor, info utils.ClusterInfo) {
	t.Helper()
	fp.setFn(func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
		return SessionStatusProcessed, &eventserver.SessionSnapshot{}, nil
	})
	if _, err := sl.LoadSession(context.Background(), info); err != nil {
		t.Fatalf("post-error retry: expected nil, got %v", err)
	}
	if got := fp.callCount(); got != 2 {
		t.Fatalf("expected processor called 2x total (1 error + 1 retry), got %d", got)
	}
}

// TestLoadSession_ProcessorError verifies processor errors propagate without
// internal retry and that singleflight dedup applies on the error path.
func TestLoadSession_ProcessorError(t *testing.T) {
	info := testClusterInfo()
	processorErr := errors.New("simulated parse failure")

	t.Run("no_internal_retry", func(t *testing.T) {
		fp := &fakeProcessor{
			fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
				return SessionStatusEventsErr, nil, processorErr
			},
		}
		sl := newTestLoader(t, fp, loaderTestConfig{})

		_, err := sl.LoadSession(context.Background(), info)
		if !errors.Is(err, processorErr) {
			t.Fatalf("expected LoadSession to return processorErr, got %v", err)
		}
		if got := fp.callCount(); got != 1 {
			t.Fatalf("expected processor called exactly 1x (no internal retry), got %d", got)
		}

		assertProcessorRetryAfterError(t, sl, fp, info)
	})

	t.Run("concurrent_dedup", func(t *testing.T) {
		release := make(chan struct{})
		fp := &fakeProcessor{
			fn: func(_ context.Context, _ utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
				<-release
				return SessionStatusEventsErr, nil, processorErr
			},
		}
		sl := newTestLoader(t, fp, loaderTestConfig{})

		const n = 5
		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				_, errs[idx] = sl.LoadSession(context.Background(), info)
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

		assertProcessorRetryAfterError(t, sl, fp, info)
	})
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
			sl := newTestLoader(t, fp, loaderTestConfig{})

			live, err := sl.LoadSession(context.Background(), testClusterInfo())
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
	sl := newTestLoader(t, fp, loaderTestConfig{})

	live, err := sl.LoadSession(context.Background(), testClusterInfo())
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
	sl := newTestLoader(t, fp, loaderTestConfig{})
	info := testClusterInfo()

	// First call: cold path, processor runs, session marked loaded.
	if _, err := sl.LoadSession(context.Background(), info); err != nil {
		t.Fatalf("cold-path LoadSession: %v", err)
	}
	if got := fp.callCount(); got != 1 {
		t.Fatalf("after cold path: callCount = %d, want 1", got)
	}

	// Subsequent calls: fast path, processor must NOT be invoked again.
	for i := 0; i < 5; i++ {
		if _, err := sl.LoadSession(context.Background(), info); err != nil {
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
	key := testClusterSessionKey()

	sl := newTestLoader(t, &fakeProcessor{}, loaderTestConfig{})
	stored := testSnapshot(key)
	sl.putSnapshot(key, stored)

	got, ok := sl.GetSnapshot(key)
	if !ok {
		t.Fatal("GetSnapshot: ok=false")
	}
	if got.SessionKey != stored.SessionKey {
		t.Fatalf("SessionKey: got %q, want %q", got.SessionKey, stored.SessionKey)
	}
}

// TestGetSnapshot_ColdMiss verifies that a miss returns (nil, false).
func TestGetSnapshot_ColdMiss(t *testing.T) {
	sl := newTestLoader(t, &fakeProcessor{}, loaderTestConfig{})
	requireSnapshotCached(t, sl, testClusterSessionKey(), false)
}

// TestGetSnapshot_PutOverwrites verifies that putSnapshot replaces any prior entry.
func TestGetSnapshot_PutOverwrites(t *testing.T) {
	key := testClusterSessionKey()

	stale := testSnapshot(key)
	stale.Tasks = []eventtypes.Task{{TaskID: "stale-task"}}
	fresh := testSnapshot(key)
	fresh.Tasks = []eventtypes.Task{{TaskID: "fresh-task"}}

	sl := newTestLoader(t, &fakeProcessor{}, loaderTestConfig{})
	sl.putSnapshot(key, stale)
	sl.putSnapshot(key, fresh)

	got, ok := sl.GetSnapshot(key)
	if !ok {
		t.Fatal("Get: ok=false")
	}
	if len(got.Tasks) != 1 || got.Tasks[0].TaskID != "fresh-task" {
		t.Fatalf("putSnapshot did not overwrite; got tasks = %#v, want fresh snapshot", got.Tasks)
	}
}

// TestGetSnapshot_ConcurrentReadsAreThreadSafe verifies the thread-safety
// of the byte cache under data race detection (-race).
func TestGetSnapshot_ConcurrentReadsAreThreadSafe(t *testing.T) {
	key := testClusterSessionKey()

	sl := newTestLoader(t, &fakeProcessor{}, loaderTestConfig{})
	sl.putSnapshot(key, richSnapshot(key))

	const goroutines = 50
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sl.putSnapshot(key, richSnapshot(key)); err != nil {
				t.Errorf("putSnapshot: %v", err)
			}
		}()
	}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mine, ok := sl.GetSnapshot(key)
			if !ok {
				t.Errorf("GetSnapshot: unexpected miss")
				return
			}
			mine.Actors["actor-e2e-cafe"] = eventtypes.Actor{ActorID: "MUTATED"}
			delete(mine.Nodes, "node-e2e-dead")
			mine.Jobs["injected"] = eventtypes.Job{}

			fresh, ok := sl.GetSnapshot(key)
			if !ok {
				t.Errorf("GetSnapshot: unexpected miss on re-read")
				return
			}
			if fresh.Actors["actor-e2e-cafe"].ActorID != "actor-e2e-cafe" {
				t.Errorf("Actors map leaked across requests")
			}
			if _, ok := fresh.Nodes["node-e2e-dead"]; !ok {
				t.Errorf("Nodes map leaked across requests")
			}
			if _, ok := fresh.Jobs["injected"]; ok {
				t.Errorf("Jobs map leaked across requests")
			}
		}()
	}

	wg.Wait()
}

// TestGetSnapshot_CorruptEntry_TreatedAsMiss verifies that a non-decodable cache
// entry is dropped and reported as a miss instead of panicking.
func TestGetSnapshot_CorruptEntry_TreatedAsMiss(t *testing.T) {
	sl := newTestLoader(t, &fakeProcessor{}, loaderTestConfig{})
	sl.cache.Add("corrupt", []byte("{not valid json"))

	snap, ok := sl.GetSnapshot("corrupt")
	if ok || snap != nil {
		t.Fatalf("corrupt entry: got (%v, %v), want (nil, false)", snap, ok)
	}
	if _, stillCached := sl.cache.Get("corrupt"); stillCached {
		t.Fatal("corrupt entry should have been dropped")
	}
}

// TestGetSnapshot_LRUEviction verifies that once capacity is exceeded, the
// oldest cached entry is evicted and GetSnapshot for that key returns ok=false.
func TestGetSnapshot_LRUEviction(t *testing.T) {
	k1 := testClusterSessionKey()
	k2 := testClusterSessionKeyFor("session_2026-04-22_11-00-00_000000_1")
	k3 := testClusterSessionKeyFor("session_2026-04-22_12-00-00_000000_1")

	sl := newTestLoader(t, &fakeProcessor{}, loaderTestConfig{cacheSize: 2})
	sl.putSnapshot(k1, testSnapshot(k1))
	sl.putSnapshot(k2, testSnapshot(k2))
	// Third session evicts the first (LRU).
	sl.putSnapshot(k3, testSnapshot(k3))

	requireSnapshotCached(t, sl, k1, false)
	requireSnapshotCached(t, sl, k2, true)
	requireSnapshotCached(t, sl, k3, true)
}

// TestGetSnapshot_TTLExpiry verifies that when a TTL is set, a cached snapshot
// expires after the TTL elapses and GetSnapshot returns ok=false.
func TestGetSnapshot_TTLExpiry(t *testing.T) {
	key := testClusterSessionKey()

	const ttl = 30 * time.Millisecond
	sl := newTestLoader(t, &fakeProcessor{}, loaderTestConfig{cacheTTL: ttl})
	sl.putSnapshot(key, testSnapshot(key))

	requireSnapshotCached(t, sl, key, true)

	time.Sleep(80 * time.Millisecond)
	requireSnapshotCached(t, sl, key, false)
}

// TestGetSnapshot_SlidingTTLRenewal verifies that repeated GetSnapshot calls
// keep a cached entry alive until idle TTL expiry.
func TestGetSnapshot_SlidingTTLRenewal(t *testing.T) {
	key := testClusterSessionKey()

	const ttl = 30 * time.Millisecond
	sl := newTestLoader(t, &fakeProcessor{}, loaderTestConfig{cacheTTL: ttl})
	sl.putSnapshot(key, testSnapshot(key))

	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		requireSnapshotCached(t, sl, key, true)
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(80 * time.Millisecond)
	requireSnapshotCached(t, sl, key, false)
}

// TestCache_ByteBudgetEviction verifies that exceeding maxBytes evicts the
// LRU entries until the cache is back under budget.
func TestCache_ByteBudgetEviction(t *testing.T) {
	olderKey := testClusterSessionKey()
	newerKey := testClusterSessionKeyFor("session_2026-04-22_11-00-00_000000_1")

	s1 := richSnapshot(olderKey)

	// Budget large enough for one richSnapshot but not two.
	enc1, _ := encodeSnapshot(s1)
	maxBytes := len(enc1) + 1
	sl := newTestLoader(t, &fakeProcessor{}, loaderTestConfig{cacheSize: 1000, maxBytes: maxBytes})

	sl.putSnapshot(olderKey, s1)
	sl.putSnapshot(newerKey, richSnapshot(newerKey))

	requireSnapshotCached(t, sl, olderKey, false)
	requireSnapshotCached(t, sl, newerKey, true)
	if entries, total := sl.cache.Len(), sl.totalBytes(); entries != 1 || total > maxBytes {
		t.Fatalf("after byte-budget eviction: got (entries=%d, bytes=%d), want (1, <=%d)", entries, total, maxBytes)
	}
}

// TestCache_ByteBudgetKeepsOversizedSoleEntry verifies a single snapshot larger
// than the whole budget is kept.
func TestCache_ByteBudgetKeepsOversizedSoleEntry(t *testing.T) {
	key := testClusterSessionKey()
	sl := newTestLoader(t, &fakeProcessor{}, loaderTestConfig{cacheSize: 1000, maxBytes: 1})
	sl.putSnapshot(key, richSnapshot(key))

	requireSnapshotCached(t, sl, key, true)
}

// TestSnapshotCache_RoundTripPreservesData verifies the encode/decode round-trip
// is lossless.
func TestSnapshotCache_RoundTripPreservesData(t *testing.T) {
	snap := richSnapshot(testClusterSessionKey())

	b1, err := encodeSnapshot(snap)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := decodeSnapshot(b1)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b2, err := encodeSnapshot(got)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("round-trip changed the snapshot:\n before=%s\n  after=%s", b1, b2)
	}

	// byte equality can hide int to float64 drift inside map[string]any after JSON unmarshal.
	wantCF := snap.LogEventsByJobID["job-e2e-aaaa"][0].CustomFields
	gotCF := got.LogEventsByJobID["job-e2e-aaaa"][0].CustomFields
	if !reflect.DeepEqual(wantCF, gotCF) {
		t.Fatalf("CustomFields round-trip mismatch:\n want=%#v\n  got=%#v", wantCF, gotCF)
	}

	wantTask, gotTask := snap.Tasks[0], got.Tasks[0]
	if gotTask.State != wantTask.State ||
		!gotTask.CreationTime.Equal(wantTask.CreationTime) ||
		!gotTask.StartTime.Equal(wantTask.StartTime) ||
		!gotTask.EndTime.Equal(wantTask.EndTime) {
		t.Fatalf("Task derived fields lost in round-trip:\n want=%+v\n  got=%+v", wantTask, gotTask)
	}
	if got.Actors["actor-e2e-cafe"].State != snap.Actors["actor-e2e-cafe"].State {
		t.Fatalf("Actor.State lost in round-trip: want=%q got=%q",
			snap.Actors["actor-e2e-cafe"].State, got.Actors["actor-e2e-cafe"].State)
	}
}

// richSnapshot builds a snapshot exercising all five session states.
//
// Sentinel strings (…-e2e-…) are asserted by the HTTP e2e test.
// TODO(jiangjiawei1103): Move to test data.
func richSnapshot(clusterSessionKey string) *eventserver.SessionSnapshot {
	return &eventserver.SessionSnapshot{
		SessionKey: clusterSessionKey,
		Tasks: []eventtypes.Task{
			{
				TaskID: "task-e2e-1111", TaskAttempt: 0, JobID: "job-e2e-aaaa",
				TaskName: "task-e2e-name", RequiredResources: map[string]float64{"CPU": 2},
				// Set derived lifecycle fields so the round-trip test proves they survive encoding/decoding.
				State:        eventtypes.RUNNING,
				CreationTime: time.Unix(1770635700, 0).UTC(),
				StartTime:    time.Unix(1770635705, 0).UTC(),
				EndTime:      time.Unix(1770635710, 0).UTC(),
			},
			{TaskID: "task-e2e-2222", TaskAttempt: 1, JobID: "job-e2e-aaaa"},
		},
		Actors: map[string]eventtypes.Actor{
			"actor-e2e-cafe": {ActorID: "actor-e2e-cafe", JobID: "job-e2e-aaaa", ActorClass: "MyActor", Name: "actorname-e2e", NumRestarts: 3, State: eventtypes.ALIVE},
		},
		Jobs: map[string]eventtypes.Job{
			"job-e2e-aaaa": {JobID: "job-e2e-aaaa", SubmissionID: "sub-e2e-bbbb", JobType: "SUBMISSION", EntryPoint: "python main.py", RuntimeEnv: map[string]string{"pip": "ray"}},
		},
		Nodes: map[string]eventtypes.Node{
			"node-e2e-dead": {
				NodeID:        "node-e2e-dead",
				NodeIPAddress: "10.0.0.7",
				Hostname:      "host-e2e",
				Labels:        map[string]string{},
				StateTransitions: []eventtypes.NodeStateTransition{
					{State: eventtypes.NODE_ALIVE, Timestamp: time.Unix(1770635705, 0).UTC(), Resources: map[string]float64{"CPU": 8}},
				},
			},
		},
		LogEventsByJobID: map[string][]eventtypes.LogEvent{
			"job-e2e-aaaa": {
				{
					EventID:    "evt-e2e-1",
					SourceType: "GCS",
					Severity:   "INFO",
					Message:    "e2e-log-message-sentinel",
					Timestamp:  "1770635705",
					// JSON-safe values only (numbers as float64) so the decoded
					// map[string]any compares equal to the original.
					CustomFields: map[string]any{
						"job_id":     "job-e2e-aaaa",
						"custom_str": "e2e-custom-value",
						"nested":     map[string]any{"inner": "e2e-nested-value"},
						"list":       []any{"e2e-list-a", "e2e-list-b"},
						"num":        float64(42),
						"flag":       true,
					},
				},
			},
		},
	}
}
