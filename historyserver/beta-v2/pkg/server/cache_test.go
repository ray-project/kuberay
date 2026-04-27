package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/snapshot"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// fakeStorageReader is a minimal storage.StorageReader used only by these
// tests. Only GetContent is exercised; List / ListFiles return empty values
// to satisfy the interface.
type fakeStorageReader struct {
	mu          sync.Mutex
	contents    map[string][]byte // key = clusterID + "|" + fileName
	calls       int32             // total GetContent invocations (atomic)
	perKeyCalls map[string]int    // GetContent invocations per key
}

func newFakeStorageReader() *fakeStorageReader {
	return &fakeStorageReader{
		contents:    map[string][]byte{},
		perKeyCalls: map[string]int{},
	}
}

func (f *fakeStorageReader) put(clusterID, fileName string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contents[clusterID+"|"+fileName] = body
}

func (f *fakeStorageReader) List() []utils.ClusterInfo {
	return nil
}

func (f *fakeStorageReader) ListFiles(_ string, _ string) []string {
	return nil
}

func (f *fakeStorageReader) GetContent(clusterID string, fileName string) io.Reader {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	key := clusterID + "|" + fileName
	f.perKeyCalls[key]++
	body, ok := f.contents[key]
	if !ok {
		return nil
	}
	return bytes.NewReader(body)
}

func (f *fakeStorageReader) totalCalls() int32 {
	return atomic.LoadInt32(&f.calls)
}

func (f *fakeStorageReader) callsFor(clusterID, fileName string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.perKeyCalls[clusterID+"|"+fileName]
}

// makeSnapshotBlob creates a minimal SessionSnapshot encoded as JSON.
func makeSnapshotBlob(t *testing.T, sessionKey string) []byte {
	t.Helper()
	snap := snapshot.SessionSnapshot{
		SessionKey:  sessionKey,
		GeneratedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	return b
}

// TestLoaderCacheHitReturnsSameInstance verifies that the second Load of the
// same key returns the exact cached pointer without invoking the reader again.
func TestLoaderCacheHitReturnsSameInstance(t *testing.T) {
	reader := newFakeStorageReader()
	clusterID := "cluster-a"
	sessionName := "session-1"
	reader.put(clusterID, snapshot.SnapshotPath(sessionName),
		makeSnapshotBlob(t, "key-1"))

	loader, err := NewSnapshotLoader(reader, 0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	first, err := loader.Load(clusterID, sessionName)
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	second, err := loader.Load(clusterID, sessionName)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if first != second {
		t.Fatalf("expected same pointer on cache hit, got %p != %p", first, second)
	}
	if got := reader.callsFor(clusterID, snapshot.SnapshotPath(sessionName)); got != 1 {
		t.Fatalf("expected exactly 1 GetContent call, got %d", got)
	}
}

// TestLoaderCacheMissFetchesAndCaches verifies that a miss goes to storage,
// and a subsequent hit does not.
func TestLoaderCacheMissFetchesAndCaches(t *testing.T) {
	reader := newFakeStorageReader()
	clusterID := "cluster-b"
	sessionName := "session-2"
	reader.put(clusterID, snapshot.SnapshotPath(sessionName),
		makeSnapshotBlob(t, "key-2"))

	loader, err := NewSnapshotLoader(reader, 0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	snap, err := loader.Load(clusterID, sessionName)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.SessionKey != "key-2" {
		t.Fatalf("unexpected SessionKey: got %q want %q", snap.SessionKey, "key-2")
	}
	if got := reader.totalCalls(); got != 1 {
		t.Fatalf("expected 1 reader call after miss, got %d", got)
	}

	if _, err := loader.Load(clusterID, sessionName); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if got := reader.totalCalls(); got != 1 {
		t.Fatalf("expected reader calls to stay at 1 after cache hit, got %d", got)
	}
}

// TestLoaderNotFoundReturnsErrAndDoesNotCache verifies the not-found contract:
// a missing snapshot returns ErrSnapshotNotFound, and failures are not cached
// (re-loading after the object appears must succeed).
func TestLoaderNotFoundReturnsErrAndDoesNotCache(t *testing.T) {
	reader := newFakeStorageReader()
	clusterID := "cluster-c"
	sessionName := "session-3"

	loader, err := NewSnapshotLoader(reader, 0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	if _, err := loader.Load(clusterID, sessionName); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("expected ErrSnapshotNotFound, got %v", err)
	}

	// Simulate snapshot appearing later; subsequent Load must re-fetch and
	// succeed (failures must not have been cached).
	reader.put(clusterID, snapshot.SnapshotPath(sessionName),
		makeSnapshotBlob(t, "key-3"))

	snap, err := loader.Load(clusterID, sessionName)
	if err != nil {
		t.Fatalf("Load after put: %v", err)
	}
	if snap.SessionKey != "key-3" {
		t.Fatalf("unexpected SessionKey: got %q want %q", snap.SessionKey, "key-3")
	}
	// Two GetContent calls: one returned nil, one returned the bytes.
	if got := reader.callsFor(clusterID, snapshot.SnapshotPath(sessionName)); got != 2 {
		t.Fatalf("expected 2 GetContent calls, got %d", got)
	}
}

// TestLoaderPrime_BypassesFetch verifies that after Prime plants a snapshot,
// a subsequent Load is a pure cache hit — zero reader calls. WHY this is the
// headline property: the Supervisor PUTs + Primes in one shot, and the next
// handler call for the same session must not pay a redundant S3 GET.
func TestLoaderPrime_BypassesFetch(t *testing.T) {
	reader := newFakeStorageReader()
	clusterID := "cluster-prime"
	sessionName := "session-prime"
	// Deliberately do NOT put the snapshot blob into the reader — if Prime
	// is working, Load must never reach the reader. Any reader call would
	// show up in totalCalls().

	loader, err := NewSnapshotLoader(reader, 0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	primed := &snapshot.SessionSnapshot{
		SessionKey:  "primed-key",
		GeneratedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
	}
	loader.Prime(clusterID, sessionName, primed)

	got, err := loader.Load(clusterID, sessionName)
	if err != nil {
		t.Fatalf("Load after Prime: %v", err)
	}
	if got != primed {
		t.Fatalf("Load after Prime returned a different pointer; Prime handoff broken")
	}
	if calls := reader.totalCalls(); calls != 0 {
		t.Fatalf("expected zero reader calls after Prime, got %d", calls)
	}
}

// TestLoaderPrime_OverwritesStale verifies that Prime replaces any prior
// cached entry for the same key. Snapshots are byte-immutable in production
// (skip-if-exists guarantees it), but the test simulates a stale-then-fresh
// transition to prove overwrite semantics explicitly.
func TestLoaderPrime_OverwritesStale(t *testing.T) {
	reader := newFakeStorageReader()
	clusterID := "cluster-overwrite"
	sessionName := "session-overwrite"

	loader, err := NewSnapshotLoader(reader, 0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	stale := &snapshot.SessionSnapshot{SessionKey: "stale"}
	fresh := &snapshot.SessionSnapshot{SessionKey: "fresh"}

	loader.Prime(clusterID, sessionName, stale)
	loader.Prime(clusterID, sessionName, fresh)

	got, err := loader.Load(clusterID, sessionName)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.SessionKey != "fresh" {
		t.Fatalf("Prime did not overwrite; got SessionKey=%q, want %q", got.SessionKey, "fresh")
	}
}

// TestLoaderPrime_NilIsNoOp verifies the defensive nil guard: Prime with a
// nil snapshot must not pollute the cache. WHY we test this: a bug in the
// Supervisor that passes a nil pointer must not cause future Loads to
// return (nil, nil) — which would break handler assumptions. The guard
// ensures Load still falls through to ErrSnapshotNotFound instead.
func TestLoaderPrime_NilIsNoOp(t *testing.T) {
	reader := newFakeStorageReader()
	clusterID := "cluster-nil"
	sessionName := "session-nil"

	loader, err := NewSnapshotLoader(reader, 0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	loader.Prime(clusterID, sessionName, nil)

	if _, err := loader.Load(clusterID, sessionName); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("expected ErrSnapshotNotFound after nil Prime, got %v", err)
	}
}

// TestLoaderLRUEviction verifies that once capacity is exceeded, the oldest
// entry is evicted and a subsequent Load of that key re-fetches from storage.
func TestLoaderLRUEviction(t *testing.T) {
	reader := newFakeStorageReader()
	clusterID := "cluster-d"
	sessions := []string{"s1", "s2", "s3"}
	for i, s := range sessions {
		reader.put(clusterID, snapshot.SnapshotPath(s),
			makeSnapshotBlob(t, "k"+sessions[i]))
	}

	loader, err := NewSnapshotLoader(reader, 2)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	// Fill cache with s1, s2 (cache now holds both).
	for _, s := range sessions[:2] {
		if _, err := loader.Load(clusterID, s); err != nil {
			t.Fatalf("Load %q: %v", s, err)
		}
	}
	// Insert s3 -> s1 is the LRU and should be evicted.
	if _, err := loader.Load(clusterID, sessions[2]); err != nil {
		t.Fatalf("Load s3: %v", err)
	}

	// At this point each session has exactly 1 GetContent call.
	for _, s := range sessions {
		if got := reader.callsFor(clusterID, snapshot.SnapshotPath(s)); got != 1 {
			t.Fatalf("after initial fill, expected 1 call for %q, got %d", s, got)
		}
	}

	// Re-load s1: must re-fetch (evicted).
	if _, err := loader.Load(clusterID, sessions[0]); err != nil {
		t.Fatalf("re-load s1: %v", err)
	}
	if got := reader.callsFor(clusterID, snapshot.SnapshotPath(sessions[0])); got != 2 {
		t.Fatalf("expected 2 calls for evicted s1, got %d", got)
	}

	// Re-load s3: still in cache, no additional fetch.
	if _, err := loader.Load(clusterID, sessions[2]); err != nil {
		t.Fatalf("re-load s3: %v", err)
	}
	if got := reader.callsFor(clusterID, snapshot.SnapshotPath(sessions[2])); got != 1 {
		t.Fatalf("expected s3 to still be cached (1 call), got %d", got)
	}
}

// TestLoaderConcurrentLoadSameKey stresses concurrent Loads of the same key
// on an empty cache. End state: every goroutine gets a non-nil snapshot with
// the expected SessionKey. We do NOT assert an exact number of fetches,
// because the underlying Get/Add sequence is not atomic (LRU allows a thundering
// herd on a cold key: one or more fetches is acceptable).
func TestLoaderConcurrentLoadSameKey(t *testing.T) {
	reader := newFakeStorageReader()
	clusterID := "cluster-e"
	sessionName := "session-hot"
	reader.put(clusterID, snapshot.SnapshotPath(sessionName),
		makeSnapshotBlob(t, "key-hot"))

	loader, err := NewSnapshotLoader(reader, 0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	const n = 10
	var wg sync.WaitGroup
	results := make([]*snapshot.SessionSnapshot, n)
	errs := make([]error, n)
	start := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			snap, err := loader.Load(clusterID, sessionName)
			results[idx] = snap
			errs[idx] = err
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d error: %v", i, errs[i])
		}
		if results[i] == nil {
			t.Fatalf("goroutine %d got nil snapshot", i)
		}
		if results[i].SessionKey != "key-hot" {
			t.Fatalf("goroutine %d unexpected SessionKey: %q", i, results[i].SessionKey)
		}
	}

	// At least one fetch must have happened; upper bound is n (thundering herd
	// on cold entry). Asserting >= 1 and <= n is the contract we can rely on.
	calls := reader.callsFor(clusterID, snapshot.SnapshotPath(sessionName))
	if calls < 1 || calls > n {
		t.Fatalf("fetch count out of expected range [1, %d]: %d", n, calls)
	}
}
