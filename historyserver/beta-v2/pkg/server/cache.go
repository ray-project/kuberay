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
// snapshot does not exist in storage.
var ErrSnapshotNotFound = errors.New("snapshot not found")

// DefaultCacheSize is the default LRU capacity.
const DefaultCacheSize = 100

// SnapshotLoader fetches SessionSnapshot JSON blobs from storage with an
// LRU cache in front.
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
//
//   - Cache hit: returns the cached pointer.
//   - Miss: fetches, decodes, and caches.
//   - Not found: returns ErrSnapshotNotFound (not cached).
//
// Increments exactly one of CacheHits or CacheMisses; on miss, also increments
// SnapshotFetchErrors with kind "not_found" or "other".
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
// canonical (clusterNameID, sessionName) key, letting callers populate the
// cache without an extra fetch.
//
// Concurrent Prime/Load is safe (hashicorp/golang-lru/v2 locks internally).
func (l *SnapshotLoader) Prime(clusterNameID, sessionName string, snap *snapshot.SessionSnapshot) {
	if snap == nil {
		// Defensive: nil would later return (nil, nil) and break handler invariants.
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
