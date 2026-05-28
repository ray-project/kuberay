package historyserver

import (
	"strings"
	"testing"

	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// TestResolveTaskLogFilename_SnapshotLookup verifies the snapshot lookup path:
// resolveTaskLogFilename reads task data from SessionLoader's cache.
func TestResolveTaskLogFilename_SnapshotLookup(t *testing.T) {
	const (
		clusterName      = "raycluster-test"
		clusterNamespace = "default"
		sessionID        = "session_2026-04-22_10-00-00_000000_1"
		clusterNameID    = clusterName + "_" + clusterNamespace
	)
	snapshotKey := utils.BuildClusterSessionKey(clusterName, clusterNamespace, sessionID)

	t.Run("snapshot missing returns not-found error", func(t *testing.T) {
		sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
		s := &ServerHandler{sessionLoader: sl}

		_, _, err := s.resolveTaskLogFilename(snapshotKey, clusterNameID, sessionID, "task1", 0, "out")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "snapshot not found") {
			t.Fatalf("expected 'snapshot not found' error, got %v", err)
		}
	})

	t.Run("task not in snapshot returns task-not-found", func(t *testing.T) {
		sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
		sl.prime(snapshotKey, &SessionSnapshot{
			SessionKey: snapshotKey,
			Tasks:      map[string][]eventtypes.Task{},
		})
		s := &ServerHandler{sessionLoader: sl}

		_, _, err := s.resolveTaskLogFilename(snapshotKey, clusterNameID, sessionID, "missing-task", 0, "out")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "task not found") {
			t.Fatalf("expected 'task not found' error, got %v", err)
		}
	})

	t.Run("attempt mismatch returns attempt-not-found", func(t *testing.T) {
		sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
		sl.prime(snapshotKey, &SessionSnapshot{
			SessionKey: snapshotKey,
			Tasks: map[string][]eventtypes.Task{
				"task1": {{TaskID: "task1", TaskAttempt: 0}},
			},
		})
		s := &ServerHandler{sessionLoader: sl}

		_, _, err := s.resolveTaskLogFilename(snapshotKey, clusterNameID, sessionID, "task1", 99, "out")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "attempt not found") {
			t.Fatalf("expected 'attempt not found' error, got %v", err)
		}
	})
}

// TestResolveActorLogFilename_SnapshotLookup verifies the snapshot lookup path:
// resolveActorLogFilename reads actor data from SessionLoader's cache.
func TestResolveActorLogFilename_SnapshotLookup(t *testing.T) {
	const (
		clusterName      = "raycluster-test"
		clusterNamespace = "default"
		sessionID        = "session_2026-04-22_10-00-00_000000_1"
		clusterNameID    = clusterName + "_" + clusterNamespace
	)
	snapshotKey := utils.BuildClusterSessionKey(clusterName, clusterNamespace, sessionID)

	t.Run("snapshot missing returns not-found error", func(t *testing.T) {
		sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
		s := &ServerHandler{sessionLoader: sl}

		_, _, err := s.resolveActorLogFilename(snapshotKey, clusterNameID, sessionID, "actor1", "out")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "snapshot not found") {
			t.Fatalf("expected 'snapshot not found' error, got %v", err)
		}
	})

	t.Run("actor not in snapshot returns actor-not-found", func(t *testing.T) {
		sl := newTestSessionLoader(t, &fakeProcessor{}, 0)
		sl.prime(snapshotKey, &SessionSnapshot{
			SessionKey: snapshotKey,
			Actors:     map[string]eventtypes.Actor{},
		})
		s := &ServerHandler{sessionLoader: sl}

		_, _, err := s.resolveActorLogFilename(snapshotKey, clusterNameID, sessionID, "missing-actor", "out")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "actor not found") {
			t.Fatalf("expected 'actor not found' error, got %v", err)
		}
	})
}
