// Package server contains the HTTP layer of the History Server v2 beta.
//
// This file implements the LRU snapshot cache used by the HTTP handlers to
// avoid hitting object storage on every request. Because the processor writes
// snapshots with a skip-if-exists guard, a snapshot at a given path is
// immutable: once cached, it never needs invalidation. LRU handles capacity;
// there is no TTL by design (see implementation_plan.md §9 decision #12).
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/metrics"
	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/snapshot"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
)

// ErrSnapshotNotFound is returned by SnapshotLoader.Load when the requested
// session.json does not exist in storage. HTTP handlers map this to a 503
// response with Retry-After: 600.
var ErrSnapshotNotFound = errors.New("snapshot not found")

// DefaultCacheSize is the default LRU capacity. ~100 cached snapshots should
// cover steady-state browsing patterns without excessive memory.
const DefaultCacheSize = 100

// SnapshotLoader fetches SessionSnapshot JSON blobs from storage with LRU cache.
//
// No TTL: snapshots are immutable (processor guarantees via skip-if-exists),
// so a cache hit is always valid. LRU handles capacity.
type SnapshotLoader struct {
	reader storage.StorageReader
	cache  *lru.Cache[string, *snapshot.SessionSnapshot]
}

// NewSnapshotLoader constructs a loader. size <= 0 uses DefaultCacheSize.
func NewSnapshotLoader(reader storage.StorageReader, size int) (*SnapshotLoader, error) {
	if size <= 0 {
		size = DefaultCacheSize
	}
	cache, err := lru.New[string, *snapshot.SessionSnapshot](size)
	if err != nil {
		return nil, fmt.Errorf("lru cache init: %w", err)
	}
	return &SnapshotLoader{reader: reader, cache: cache}, nil
}

// Load returns the SessionSnapshot for the given cluster/session.
// Cache hit -> cached pointer. Miss -> fetch + decode + cache.
// Not found -> ErrSnapshotNotFound (not cached).
//
// Metrics: every call increments exactly one of CacheHits / CacheMisses. On
// miss, any fetch error also bumps SnapshotFetchErrors by kind — so
// "not_found" ticks up once per first-look at a dead-but-unsnapped session,
// and "other" ticks up on genuinely unexpected read / decode failures.
func (l *SnapshotLoader) Load(clusterNameID, sessionName string) (*snapshot.SessionSnapshot, error) {
	key := cacheKey(clusterNameID, sessionName)
	if snap, ok := l.cache.Get(key); ok {
		metrics.CacheHits.Inc()
		return snap, nil
	}
	metrics.CacheMisses.Inc()
	snap, err := l.fetch(clusterNameID, sessionName)
	if err != nil {
		if errors.Is(err, ErrSnapshotNotFound) {
			metrics.SnapshotFetchErrors.WithLabelValues("not_found").Inc()
		} else {
			metrics.SnapshotFetchErrors.WithLabelValues("other").Inc()
		}
		return nil, err
	}
	l.cache.Add(key, snap)
	return snap, nil
}

// Prime inserts a freshly-built SessionSnapshot into the LRU under the
// canonical (clusterNameID, sessionName) key.
//
// WHY this exists only in beta-v2: in lazy mode the Supervisor builds and
// PUTs the snapshot inline with the HTTP request. The obvious follow-on —
// the next handler call for the same session — would otherwise miss the LRU
// (because Load has never been called) and pay a full S3 GET to read back
// exactly what we just persisted. Prime short-circuits that: the Supervisor
// plants the live pointer immediately after a successful PUT, so the next
// Load is O(1) and touches no object-store API. The fast-path branch of
// Supervisor.Ensure also relies on this.
//
// Snapshots are immutable (processor guarantees via skip-if-exists), so
// overwriting any prior cached entry for the same key is safe — the two
// objects must be byte-identical at the S3 layer.
//
// Concurrent Prime + Load is safe: hashicorp/golang-lru/v2 guards Add/Get
// with its own mutex, so we do not need additional locking here.
func (l *SnapshotLoader) Prime(clusterNameID, sessionName string, snap *snapshot.SessionSnapshot) {
	if snap == nil {
		// Defensive: caller must not plant a nil snapshot or a subsequent
		// Load would return (nil, nil) and break handler assumptions. This
		// branch is an internal-contract guard, not a runtime path we
		// expect to hit.
		return
	}
	l.cache.Add(cacheKey(clusterNameID, sessionName), snap)
}

func (l *SnapshotLoader) fetch(clusterNameID, sessionName string) (*snapshot.SessionSnapshot, error) {
	reader := l.reader.GetContent(clusterNameID, snapshot.SnapshotPath(sessionName))
	if reader == nil {
		return nil, ErrSnapshotNotFound
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read snapshot body: %w", err)
	}
	var snap snapshot.SessionSnapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	return &snap, nil
}

func cacheKey(clusterNameID, sessionName string) string {
	return clusterNameID + "/" + sessionName
}
