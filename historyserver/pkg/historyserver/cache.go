package historyserver

import (
	"errors"
	"fmt"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/ray-project/kuberay/historyserver/pkg/snapshot"
)

// ErrSnapshotNotFound is returned by SnapshotLoader.Load when the requested
// snapshot is not in the LRU. Handlers translate this into 503 +
// Retry-After: 600 so the Dashboard frontend re-fires /enter_cluster, which
// drives the Pipeline to rebuild and Prime the snapshot.
var ErrSnapshotNotFound = errors.New("snapshot not found")

// DefaultCacheSize is the default LRU capacity.
const DefaultCacheSize = 100

// SnapshotLoader is an in-memory LRU of SessionSnapshot pointers. It has no
// fallback: a miss is reported as ErrSnapshotNotFound and the Supervisor
// rebuilds via Pipeline.ProcessSession + Prime.
type SnapshotLoader struct {
	cache *lru.Cache[string, *snapshot.SessionSnapshot]
}

// NewSnapshotLoader constructs a loader. size <= 0 uses DefaultCacheSize.
func NewSnapshotLoader(size int) (*SnapshotLoader, error) {
	if size <= 0 {
		size = DefaultCacheSize
	}
	cache, err := lru.New[string, *snapshot.SessionSnapshot](size)
	if err != nil {
		return nil, fmt.Errorf("lru cache init: %w", err)
	}
	return &SnapshotLoader{cache: cache}, nil
}

// Load returns the SessionSnapshot for the given cluster/session.
//
//   - Cache hit: returns the cached pointer.
//   - Miss: returns ErrSnapshotNotFound (the miss itself is not cached).
func (l *SnapshotLoader) Load(clusterNameID, sessionName string) (*snapshot.SessionSnapshot, error) {
	key := cacheKey(clusterNameID, sessionName)
	if snap, ok := l.cache.Get(key); ok {
		return snap, nil
	}
	return nil, ErrSnapshotNotFound
}

// Prime inserts a freshly-built SessionSnapshot into the LRU under the
// canonical (clusterNameID, sessionName) key. The Supervisor calls this
// after Pipeline.ProcessSession returns Processed so subsequent handler
// reads land on a cache hit.
//
// Concurrent Prime/Load is safe (hashicorp/golang-lru/v2 locks internally).
func (l *SnapshotLoader) Prime(clusterNameID, sessionName string, snap *snapshot.SessionSnapshot) {
	if snap == nil {
		// Defensive: nil would later return (nil, nil) and break handler invariants.
		return
	}
	l.cache.Add(cacheKey(clusterNameID, sessionName), snap)
}

func cacheKey(clusterNameID, sessionName string) string {
	return clusterNameID + "/" + sessionName
}
