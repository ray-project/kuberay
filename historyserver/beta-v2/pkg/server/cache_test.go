package server

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/snapshot"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// fakeStorageReader is a minimal storage.StorageReader retained as a shared
// test helper for file-backed handlers (see handlers_test.go's
// nodeLogsFakeReader). The SnapshotLoader itself is LRU-only and does not
// touch storage; cache tests do not use this type.
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

// snapWith returns a minimal snapshot pointer for use with Prime.
func snapWith(sessionKey string) *snapshot.SessionSnapshot {
	return &snapshot.SessionSnapshot{
		SessionKey:  sessionKey,
		GeneratedAt: time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC),
	}
}

// TestLoaderPrimeThenLoad_ReturnsSameInstance verifies the canonical hot path:
// Prime, then two Loads must each return the exact pointer that was Primed
// (no copy, no fetch — there is no fetch).
func TestLoaderPrimeThenLoad_ReturnsSameInstance(t *testing.T) {
	loader, err := NewSnapshotLoader(0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	primed := snapWith("key-1")
	loader.Prime("cluster-a", "session-1", primed)

	first, err := loader.Load("cluster-a", "session-1")
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	second, err := loader.Load("cluster-a", "session-1")
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if first != primed || second != primed {
		t.Fatalf("expected same pointer on cache hit, got %p / %p (want %p)", first, second, primed)
	}
}

// TestLoaderColdMiss_ReturnsNotFound verifies that an LRU miss returns
// ErrSnapshotNotFound. The Supervisor relies on this sentinel to decide
// whether to invoke Pipeline.
func TestLoaderColdMiss_ReturnsNotFound(t *testing.T) {
	loader, err := NewSnapshotLoader(0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	if _, err := loader.Load("cluster-c", "session-3"); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("expected ErrSnapshotNotFound on cold miss, got %v", err)
	}
}

// TestLoaderMissDoesNotCacheNotFound verifies that a miss is not
// remembered: a later Prime + Load must succeed. WHY this matters: a
// negative cache would prevent the Supervisor's Prime from ever taking
// effect for a freshly-built session.
func TestLoaderMissDoesNotCacheNotFound(t *testing.T) {
	loader, err := NewSnapshotLoader(0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	if _, err := loader.Load("cluster-c", "session-3"); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("expected ErrSnapshotNotFound, got %v", err)
	}

	primed := snapWith("key-3")
	loader.Prime("cluster-c", "session-3", primed)

	got, err := loader.Load("cluster-c", "session-3")
	if err != nil {
		t.Fatalf("Load after Prime: %v", err)
	}
	if got != primed {
		t.Fatalf("Load after Prime returned a different pointer; negative cache leaked")
	}
}

// TestLoaderPrime_OverwritesStale verifies that Prime replaces any prior
// cached entry for the same key.
func TestLoaderPrime_OverwritesStale(t *testing.T) {
	loader, err := NewSnapshotLoader(0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	loader.Prime("cluster-overwrite", "session-overwrite", snapWith("stale"))
	loader.Prime("cluster-overwrite", "session-overwrite", snapWith("fresh"))

	got, err := loader.Load("cluster-overwrite", "session-overwrite")
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
	loader, err := NewSnapshotLoader(0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	loader.Prime("cluster-nil", "session-nil", nil)

	if _, err := loader.Load("cluster-nil", "session-nil"); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("expected ErrSnapshotNotFound after nil Prime, got %v", err)
	}
}

// TestLoaderLRUEviction verifies that once capacity is exceeded, the oldest
// Primed entry is evicted and a subsequent Load of that key returns
// ErrSnapshotNotFound (which the Supervisor would translate into a Pipeline
// rebuild).
func TestLoaderLRUEviction(t *testing.T) {
	loader, err := NewSnapshotLoader(2)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	loader.Prime("cluster-d", "s1", snapWith("k1"))
	loader.Prime("cluster-d", "s2", snapWith("k2"))
	// s3 evicts s1 (LRU).
	loader.Prime("cluster-d", "s3", snapWith("k3"))

	if _, err := loader.Load("cluster-d", "s1"); !errors.Is(err, ErrSnapshotNotFound) {
		t.Fatalf("expected s1 to be evicted -> ErrSnapshotNotFound, got %v", err)
	}
	if _, err := loader.Load("cluster-d", "s2"); err != nil {
		t.Fatalf("expected s2 to still be cached, got %v", err)
	}
	if _, err := loader.Load("cluster-d", "s3"); err != nil {
		t.Fatalf("expected s3 to still be cached, got %v", err)
	}
}

// TestLoaderConcurrentPrimeLoad verifies that concurrent Prime/Load calls
// are safe — golang-lru/v2 locks internally, so we only assert that the
// final state has the snapshot present and no goroutine panicked.
func TestLoaderConcurrentPrimeLoad(t *testing.T) {
	loader, err := NewSnapshotLoader(0)
	if err != nil {
		t.Fatalf("NewSnapshotLoader: %v", err)
	}

	primed := snapWith("hot")
	loader.Prime("cluster-e", "session-hot", primed)

	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Mix Prime + Load to stress the lock.
			loader.Prime("cluster-e", "session-hot", primed)
			if got, err := loader.Load("cluster-e", "session-hot"); err != nil || got != primed {
				t.Errorf("concurrent Load: got=%p err=%v want=%p", got, err, primed)
			}
		}()
	}
	wg.Wait()
}
