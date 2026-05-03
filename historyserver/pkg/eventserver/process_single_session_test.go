package eventserver

import (
	"testing"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcessSingleSession exercises the lazy-mode error contract that
// determines whether the Supervisor will mark a session as loaded.
//
// Contract:
//   - Storage I/O failures (GetContent nil, io.ReadAll error) are transient;
//     if the file list was non-empty but every file failed at I/O, surface an
//     error so the next /enter_cluster retries.
//   - Corrupt-data failures (Unmarshal, storeEvent) do NOT count as transient;
//     retrying won't help.
//   - Empty file lists are treated as legitimately empty.
//     NOTE: This is currently indistinguishable from a transient storage outage.
//   - ReadLogEvents errors propagate from ProcessSingleSession.
func TestProcessSingleSession(t *testing.T) {
	clusterInfo := utils.ClusterInfo{Name: "cluster", Namespace: "ns", SessionName: "session1"}

	t.Run("returns error when every listed file fails I/O", func(t *testing.T) {
		mock := newLogEventMockReader()
		mock.addDir("cluster_ns", "session1/job_events/", []string{"job-01000000/"})
		mock.addDir("cluster_ns", "session1/job_events/job-01000000/",
			[]string{"01000000-2024-01-01-00"})
		mock.addDir("cluster_ns", "session1/node_events/",
			[]string{"node1-2024-01-01-00"})
		mock.addDir("cluster_ns", "session1/logs", []string{})

		h := NewEventHandler(mock)
		err := h.ProcessSingleSession(clusterInfo)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ingested 0 of 2")
	})

	t.Run("empty file list returns nil (legit empty session)", func(t *testing.T) {
		mock := newLogEventMockReader()
		mock.addDir("cluster_ns", "session1/job_events/", []string{})
		mock.addDir("cluster_ns", "session1/node_events/", []string{})
		mock.addDir("cluster_ns", "session1/logs", []string{})

		h := NewEventHandler(mock)
		err := h.ProcessSingleSession(clusterInfo)
		require.NoError(t, err)
	})

	t.Run("partial success does not return error", func(t *testing.T) {
		mock := newLogEventMockReader()
		mock.addDir("cluster_ns", "session1/node_events/",
			[]string{"node1-2024-01-01-00", "node2-2024-01-01-00"})
		mock.addFile("cluster_ns", "session1/node_events/node1-2024-01-01-00",
			`[{"eventType":"NODE_DEFINITION_EVENT","nodeDefinitionEvent":{"nodeId":"YWJjZA=="}}]`)
		mock.addDir("cluster_ns", "session1/job_events/", []string{})
		mock.addDir("cluster_ns", "session1/logs", []string{})

		h := NewEventHandler(mock)
		err := h.ProcessSingleSession(clusterInfo)
		require.NoError(t, err)
	})

	t.Run("all corrupt JSON does not return error", func(t *testing.T) {
		mock := newLogEventMockReader()
		mock.addDir("cluster_ns", "session1/node_events/",
			[]string{"node1-2024-01-01-00", "node2-2024-01-01-00"})
		mock.addFile("cluster_ns", "session1/node_events/node1-2024-01-01-00", "this is not json")
		mock.addFile("cluster_ns", "session1/node_events/node2-2024-01-01-00", "neither is this")
		mock.addDir("cluster_ns", "session1/job_events/", []string{})
		mock.addDir("cluster_ns", "session1/logs", []string{})

		h := NewEventHandler(mock)
		err := h.ProcessSingleSession(clusterInfo)
		require.NoError(t, err)
	})

	t.Run("ReadLogEvents error propagates even when RayEvents path is empty", func(t *testing.T) {
		mock := newLogEventMockReader()
		mock.addDir("cluster_ns", "session1/job_events/", []string{})
		mock.addDir("cluster_ns", "session1/node_events/", []string{})
		mock.addDir("cluster_ns", "session1/logs", []string{"node1/"})
		mock.addDir("cluster_ns", "session1/logs/node1/events", []string{"event_GCS.log"})

		h := NewEventHandler(mock)
		err := h.ProcessSingleSession(clusterInfo)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read log events")
	})
}
