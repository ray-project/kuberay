package historyserver

import (
	"errors"
	"strings"
	"testing"

	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/snapshot"
)

// TestResolveTaskLogFilename_SnapshotLookup verifies the snapshot lookup path:
// resolveTaskLogFilename reads task data from SnapshotLoader.
func TestResolveTaskLogFilename_SnapshotLookup(t *testing.T) {
	const clusterNameID = "raycluster-test_default"
	const sessionID = "session_2026-04-22_10-00-00_000000_1"

	t.Run("snapshot missing wraps ErrSnapshotNotFound", func(t *testing.T) {
		loader := newTestLoader(t)
		s := &ServerHandler{loader: loader}

		_, _, err := s.resolveTaskLogFilename(clusterNameID, sessionID, "task1", 0, "out")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, ErrSnapshotNotFound) {
			t.Fatalf("expected wrapped ErrSnapshotNotFound, got %v", err)
		}
	})

	t.Run("task not in snapshot returns task-not-found", func(t *testing.T) {
		loader := newTestLoader(t)
		loader.Prime(clusterNameID, sessionID, &snapshot.SessionSnapshot{
			SessionKey: "raycluster-test_default_" + sessionID,
			Tasks:      map[string][]eventtypes.Task{},
		})
		s := &ServerHandler{loader: loader}

		_, _, err := s.resolveTaskLogFilename(clusterNameID, sessionID, "missing-task", 0, "out")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "task not found") {
			t.Fatalf("expected 'task not found' error, got %v", err)
		}
	})

	t.Run("attempt mismatch returns attempt-not-found", func(t *testing.T) {
		loader := newTestLoader(t)
		loader.Prime(clusterNameID, sessionID, &snapshot.SessionSnapshot{
			SessionKey: "raycluster-test_default_" + sessionID,
			Tasks: map[string][]eventtypes.Task{
				"task1": {{TaskID: "task1", TaskAttempt: 0}},
			},
		})
		s := &ServerHandler{loader: loader}

		_, _, err := s.resolveTaskLogFilename(clusterNameID, sessionID, "task1", 99, "out")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "attempt not found") {
			t.Fatalf("expected 'attempt not found' error, got %v", err)
		}
	})
}

// TestResolveActorLogFilename_SnapshotLookup verifies the snapshot lookup path:
// resolveActorLogFilename reads actor data from SnapshotLoader.
func TestResolveActorLogFilename_SnapshotLookup(t *testing.T) {
	const clusterNameID = "raycluster-test_default"
	const sessionID = "session_2026-04-22_10-00-00_000000_1"

	t.Run("snapshot missing wraps ErrSnapshotNotFound", func(t *testing.T) {
		loader := newTestLoader(t)
		s := &ServerHandler{loader: loader}

		_, _, err := s.resolveActorLogFilename(clusterNameID, sessionID, "actor1", "out")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !errors.Is(err, ErrSnapshotNotFound) {
			t.Fatalf("expected wrapped ErrSnapshotNotFound, got %v", err)
		}
	})

	t.Run("actor not in snapshot returns actor-not-found", func(t *testing.T) {
		loader := newTestLoader(t)
		loader.Prime(clusterNameID, sessionID, &snapshot.SessionSnapshot{
			SessionKey: "raycluster-test_default_" + sessionID,
			Actors:     map[string]eventtypes.Actor{},
		})
		s := &ServerHandler{loader: loader}

		_, _, err := s.resolveActorLogFilename(clusterNameID, sessionID, "missing-actor", "out")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "actor not found") {
			t.Fatalf("expected 'actor not found' error, got %v", err)
		}
	})
}
