package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventMap(t *testing.T) {
	t.Run("NewEventMap creates empty map", func(t *testing.T) {
		em := NewEventMap()
		require.NotNil(t, em)
		assert.Empty(t, em.GetAllEvents())
	})

	t.Run("AddEvent stores events by jobID", func(t *testing.T) {
		em := NewEventMap()
		em.AddEvent("job1", Event{EventID: "1", Timestamp: "1768591375000"})
		em.AddEvent("job2", Event{EventID: "2", Timestamp: "1768591376000"})

		all := em.GetAllEvents()
		assert.Len(t, all, 2)
		assert.Len(t, all["job1"], 1)
		assert.Len(t, all["job2"], 1)
	})

	t.Run("AddEvent with empty jobID stores under global", func(t *testing.T) {
		em := NewEventMap()
		em.AddEvent("", Event{EventID: "1"})
		em.AddEvent("", Event{EventID: "2"})

		all := em.GetAllEvents()
		assert.Len(t, all["global"], 2)
		_, hasEmpty := all[""]
		assert.False(t, hasEmpty)
	})

	t.Run("AddEvent enforces MaxEventsPerJob limit", func(t *testing.T) {
		em := NewEventMap()
		for i := 0; i < MaxEventsPerJob+100; i++ {
			em.AddEvent("job1", Event{EventID: fmt.Sprintf("%d", i)})
		}

		events := em.GetEventsByJobID("job1")
		assert.Len(t, events, MaxEventsPerJob)
		// Verify oldest events are dropped (FIFO)
		assert.Equal(t, "100", events[0].EventID)
	})

	t.Run("GetByJobID returns sorted events", func(t *testing.T) {
		em := NewEventMap()
		em.AddEvent("job1", Event{EventID: "1", Timestamp: "1768591390000"}) // 30s
		em.AddEvent("job1", Event{EventID: "2", Timestamp: "1768591370000"}) // 10s
		em.AddEvent("job1", Event{EventID: "3", Timestamp: "1768591380000"}) // 20s

		events := em.GetEventsByJobID("job1")
		assert.Equal(t, "1768591370000", events[0].Timestamp)
		assert.Equal(t, "1768591380000", events[1].Timestamp)
		assert.Equal(t, "1768591390000", events[2].Timestamp)
	})

	t.Run("GetEventsByJobID returns empty slice for nonexistent job", func(t *testing.T) {
		em := NewEventMap()
		events := em.GetEventsByJobID("nonexistent")
		require.NotNil(t, events)
		assert.Empty(t, events)
	})

	t.Run("GetAllEvents returns sorted events for each job", func(t *testing.T) {
		em := NewEventMap()
		em.AddEvent("job1", Event{EventID: "1", Timestamp: "1768591380000"}) // 20s
		em.AddEvent("job1", Event{EventID: "2", Timestamp: "1768591370000"}) // 10s

		all := em.GetAllEvents()
		assert.Equal(t, "2", all["job1"][0].EventID)
		assert.Equal(t, "1", all["job1"][1].EventID)
	})
}

func TestClusterEventMap(t *testing.T) {
	t.Run("NewClusterEventMap creates empty map", func(t *testing.T) {
		cm := NewClusterEventMap()
		require.NotNil(t, cm)
		assert.Empty(t, cm.GetAllEvents("any"))
	})

	t.Run("GetOrCreateEventMap creates and reuses EventMap", func(t *testing.T) {
		cm := NewClusterEventMap()

		// clusterSessionKey format: "{clusterName}_{namespace}_{sessionName}"
		clusterSessionKey1 := "my-cluster_default_session_2026-01-16"
		clusterSessionKey2 := "my-cluster_default_session_2026-01-17"

		em1 := cm.GetOrCreateEventMap(clusterSessionKey1)
		require.NotNil(t, em1)

		em2 := cm.GetOrCreateEventMap(clusterSessionKey1)
		assert.Same(t, em1, em2, "should return same instance")

		em3 := cm.GetOrCreateEventMap(clusterSessionKey2)
		assert.NotSame(t, em1, em3, "different cluster session should get different instance")
	})

	t.Run("GetAllEvents returns events for cluster session", func(t *testing.T) {
		cm := NewClusterEventMap()
		clusterSessionKey := "my-cluster_default_session_2026-01-16"
		em := cm.GetOrCreateEventMap(clusterSessionKey)
		em.AddEvent("job1", Event{EventID: "1", Timestamp: "1768591375000"})
		em.AddEvent("", Event{EventID: "2", Timestamp: "1768591370000"})

		all := cm.GetAllEvents(clusterSessionKey)
		assert.Len(t, all, 2)
		assert.Contains(t, all, "job1")
		assert.Contains(t, all, "global")
	})

	t.Run("GetAllEvents returns empty map for nonexistent cluster", func(t *testing.T) {
		cm := NewClusterEventMap()
		all := cm.GetAllEvents("nonexistent")
		require.NotNil(t, all)
		assert.Empty(t, all)
	})

	t.Run("GetEventsByJobID returns events for cluster session and job", func(t *testing.T) {
		cm := NewClusterEventMap()
		clusterSessionKey := "my-cluster_default_session_2026-01-16"
		em := cm.GetOrCreateEventMap(clusterSessionKey)
		em.AddEvent("job1", Event{EventID: "1", Timestamp: "1768591375000"}) // 15s
		em.AddEvent("job1", Event{EventID: "2", Timestamp: "1768591370000"}) // 10s

		events := cm.GetEventsByJobID(clusterSessionKey, "job1")
		assert.Len(t, events, 2)
		assert.Equal(t, "2", events[0].EventID)
		assert.Equal(t, "1", events[1].EventID)
	})

	t.Run("GetEventsByJobID returns empty for nonexistent cluster session or job", func(t *testing.T) {
		cm := NewClusterEventMap()
		clusterSessionKey := "my-cluster_default_session_2026-01-16"
		cm.GetOrCreateEventMap(clusterSessionKey)

		events := cm.GetEventsByJobID("nonexistent_ns_session", "job1")
		assert.Empty(t, events)

		events = cm.GetEventsByJobID(clusterSessionKey, "nonexistent")
		assert.Empty(t, events)
	})

	t.Run("GetEventsByJobID with empty string returns empty slice", func(t *testing.T) {
		// This test verifies the behavior when job_id parameter is provided but empty
		// (e.g., /events?job_id=), which should return empty results.
		cm := NewClusterEventMap()
		clusterSessionKey := "my-cluster_default_session_2026-01-16"
		em := cm.GetOrCreateEventMap(clusterSessionKey)
		em.AddEvent("job1", Event{EventID: "1", Timestamp: "1768591375000"})
		em.AddEvent("", Event{EventID: "2", Timestamp: "1768591370000"}) // stored under "global"

		// Query with empty string should NOT match "global" - it should return empty
		// because the API distinguishes between "no parameter" and "empty parameter"
		events := cm.GetEventsByJobID(clusterSessionKey, "")
		assert.Empty(t, events, "empty string jobID should return empty slice, not global events")
	})
}

func TestEvent(t *testing.T) {
	event := Event{
		EventID:        "LrBbQwLLTK2+SSsBX+AG4EDK02CAVnvtVMD2MA==",
		SourceType:     GCS,
		SourceHostname: "ray-head-node-0",
		SourcePid:      12345,
		Label:          "ACTOR_DEFINITION_EVENT",
		Message:        "Test",
		Timestamp:      "1737055775210",
		Severity:       INFO,
		CustomFields: map[string]any{
			"actorId": "abc123",
			"nodeId":  "531134a446a4f1b4d07301c0ee09b0ca32593dbb",
			"jobId":   "01000000",
		},
		// History Server specific fields
		EventType:   ACTOR_DEFINITION_EVENT,
		SessionName: "session_2026-01-16_11-06-54_467309_1",
	}

	// Ray Dashboard compatible fields
	assert.Equal(t, "LrBbQwLLTK2+SSsBX+AG4EDK02CAVnvtVMD2MA==", event.EventID)
	assert.Equal(t, GCS, event.SourceType)
	assert.Equal(t, "ray-head-node-0", event.SourceHostname)
	assert.Equal(t, 12345, event.SourcePid)
	assert.Equal(t, "ACTOR_DEFINITION_EVENT", event.Label)
	assert.Equal(t, INFO, event.Severity)
	assert.NotNil(t, event.CustomFields["actorId"])
	assert.Equal(t, "531134a446a4f1b4d07301c0ee09b0ca32593dbb", event.CustomFields["nodeId"])
	assert.Equal(t, "1737055775210", event.Timestamp)
	assert.Equal(t, "Test", event.Message)

	// History Server specific fields
	assert.Equal(t, ACTOR_DEFINITION_EVENT, event.EventType)
	assert.Equal(t, "session_2026-01-16_11-06-54_467309_1", event.SessionName)
}

func TestSortEventsByTimestamp(t *testing.T) {
	events := []Event{
		{EventID: "3", Timestamp: "1768591390000"}, // 30s
		{EventID: "1", Timestamp: "1768591370000"}, // 10s
		{EventID: "2", Timestamp: "1768591380000"}, // 20s
	}

	sortEventsByTimestamp(events)

	assert.Equal(t, "1", events[0].EventID)
	assert.Equal(t, "2", events[1].EventID)
	assert.Equal(t, "3", events[2].EventID)
}
