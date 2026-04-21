package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
)

// ErrSnapshotNotFound indicates no processed snapshot exists for the session,
// or the snapshot exists but lacks a _COMPLETED marker (partial write).
// Callers (e.g., SessionResolver) should treat this as a signal to trigger
// on-demand EventProcessor.Process().
var ErrSnapshotNotFound = errors.New("session snapshot not found")

// Layout filenames under processed/<sessionID>/.
const (
	ProcessedDir           = "processed"
	SnapshotFullFile       = "session.json"
	SnapshotNodeFile       = "node.json"
	SnapshotTaskFile       = "task.json"
	SnapshotActorFile      = "actor.json"
	SnapshotJobFile        = "job.json"
	SnapshotCompleteMarker = "_COMPLETED"
)

// SnapshotStore persists SessionSnapshots under the following object storage layout:
//
// TOOD(jwj): Make layout more precise after discussion.
//
//	processed/<sessionID>/session.json   authoritative full snapshot (read by Get)
//	processed/<sessionID>/node.json      Nodes field only  (reserved for future read-path optimization, avoiding in-memory filtering)
//	processed/<sessionID>/task.json      Tasks field only
//	processed/<sessionID>/actor.json     Actors field only
//	processed/<sessionID>/job.json       Jobs field only
//	processed/<sessionID>/_COMPLETED     zero-byte marker (written last; absence means partial/invalid)
type SnapshotStore interface {
	// Get returns:
	//   - the full session snapshot when a _COMPLETED marker is present (cache hit)
	//   - ErrSnapshotNotFound when no _COMPLETED marker is present (treat cache miss as "trigger reprocess")
	//
	// Note: Currently reads only session.json. The split files exist so a future
	// optimization can let endpoints that need a single sub-map skip deserializing
	// the full snapshot.
	Get(ctx context.Context, sessionID string) (*types.SessionSnapshot, error)

	// Put writes all snapshot files and the _COMPLETED marker (written last).
	// Note: A failure mid-sequence leaves a partial directory that IsCompleted
	// will correctly report as false on the next call.
	Put(ctx context.Context, sessionID string, snap *types.SessionSnapshot) error

	// IsCompleted reports whether the _COMPLETED marker exists for a session.
	// This is the sole signal used by SessionResolver to decide hit vs miss.
	IsCompleted(ctx context.Context, sessionID string) (bool, error)
}

// snapshotStore is the default SnapshotStore implementation wrapping a
// StorageReader and StorageWriter.
//
// KNOWN LEAKY ABSTRACTION: rootDir parameter
// rootDir MUST equal the root path under which the provided reader/writer
// were configured. This is required because the existing storage interface
// is asymmetric:
//
//   - StorageReader.GetContent / ListFiles prepend their configured root
//     internally, so callers pass paths relative to that root.
//   - StorageWriter.WriteFile / CreateDirectory accept absolute paths and
//     expect callers to compose the root prefix themselves (see
//     pkg/collector/eventcollector/eventcollector.go for the existing convention).
//
// TODO(jwj): Unify StorageReader/StorageWriter path handling so rootDir can be removed.
type snapshotStore struct {
	reader  StorageReader
	writer  StorageWriter
	rootDir string
}

// NewSnapshotStore constructs the default SnapshotStore.
// rootDir is the prefix under which the storage provider was configured (e.g., RayLogsHandler.S3RootDir).
func NewSnapshotStore(reader StorageReader, writer StorageWriter, rootDir string) SnapshotStore {
	return &snapshotStore{
		reader:  reader,
		writer:  writer,
		rootDir: rootDir,
	}
}

// TODO(jwj): Unify StorageReader/StorageWriter path handling so this can be removed.
// relPath returns the path relative to the reader's configured root.
//
// Used with:
//   - StorageReader.GetContent
//   - StorageReader.ListFiles
func (s *snapshotStore) relPath(sessionID, file string) string {
	return path.Join(ProcessedDir, sessionID, file)
}

// TODO(jwj): Unify StorageReader/StorageWriter path handling so this can be removed.
// absPath returns the absolute path including rootDir.
//
// Used with:
//   - StorageWriter.WriteFile
//   - StorageWriter.CreateDirectory
//
// which do NOT prepend root.
func (s *snapshotStore) absPath(sessionID, file string) string {
	return path.Join(s.rootDir, ProcessedDir, sessionID, file)
}

func (s *snapshotStore) IsCompleted(ctx context.Context, sessionID string) (bool, error) {
	// TODO(jwj): StorageReader does not accept a context now; propagate when interface gains it.
	_ = ctx

	// TODO(jwj): slices.Contains?
	fileNames := s.reader.ListFiles("", path.Join(ProcessedDir, sessionID))
	for _, fileName := range fileNames {
		if fileName == SnapshotCompleteMarker {
			return true, nil
		}
	}
	return false, nil
}

func (s *snapshotStore) Get(ctx context.Context, sessionID string) (*types.SessionSnapshot, error) {
	completed, err := s.IsCompleted(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to check completion for %q: %w", sessionID, err)
	}
	if !completed {
		return nil, ErrSnapshotNotFound
	}

	fileName := s.relPath(sessionID, SnapshotFullFile)
	r := s.reader.GetContent("", fileName)
	if r == nil {
		// Complete marker is present but full snapshot is missing, which should not happen under
		// correct Put ordering.
		// Treat as miss so SessionResolver reprocesses.
		return nil, ErrSnapshotNotFound
	}

	var snap types.SessionSnapshot
	if err := json.NewDecoder(r).Decode(&snap); err != nil {
		return nil, fmt.Errorf("failed to decode full snapshot for %q: %w", sessionID, err)
	}
	return &snap, nil
}

func (s *snapshotStore) Put(ctx context.Context, sessionID string, snap *types.SessionSnapshot) error {
	// TODO(jwj): StorageWriter does not accept a context now; propagate when interface gains it.
	_ = ctx

	if snap == nil {
		return fmt.Errorf("put nil snapshot for %q", sessionID)
	}

	dirPath := path.Join(s.rootDir, ProcessedDir, sessionID)
	if err := s.writer.CreateDirectory(dirPath); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", dirPath, err)
	}

	writes := []struct {
		file string
		data any
	}{
		{SnapshotFullFile, snap},
		{SnapshotNodeFile, snap.Nodes},
		{SnapshotTaskFile, snap.Tasks},
		{SnapshotActorFile, snap.Actors},
		{SnapshotJobFile, snap.Jobs},
	}
	for _, w := range writes {
		body, err := json.Marshal(w.data)
		if err != nil {
			return fmt.Errorf("failed to marshal %s for %q: %w", w.file, sessionID, err)
		}

		filePath := s.absPath(sessionID, w.file)
		if err := s.writer.WriteFile(filePath, bytes.NewReader(body)); err != nil {
			return fmt.Errorf("failed to write %s for %q: %w", filePath, sessionID, err)
		}
	}

	// Write the completion marker. Any failure above leaves a partial directory
	// that IsCompleted rejects, triggering reprocess on the next Resolve call.
	completeMarkerPath := s.absPath(sessionID, SnapshotCompleteMarker)
	if err := s.writer.WriteFile(completeMarkerPath, bytes.NewReader(nil)); err != nil {
		return fmt.Errorf("failed to write completion marker for %q: %w", sessionID, err)
	}
	return nil
}

// Compile-time interface check.
var _ SnapshotStore = (*snapshotStore)(nil)
