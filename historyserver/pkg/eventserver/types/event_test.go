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
		assert.Empty(t, em.GetAll())
	})

	t.Run("AddEvent stores events by jobID", func(t *testing.T) {
		em := NewEventMap()
		em.AddEvent("job1", Event{EventID: "1", Timestamp: "1768591375000"})
		em.AddEvent("job2", Event{EventID: "2", Timestamp: "1768591376000"})

		all := em.GetAll()
		assert.Len(t, all, 2)
		assert.Len(t, all["job1"], 1)
		assert.Len(t, all["job2"], 1)
	})

	t.Run("AddEvent with empty jobID stores under global", func(t *testing.T) {
		em := NewEventMap()
		em.AddEvent("", Event{EventID: "1"})
		em.AddEvent("", Event{EventID: "2"})

		all := em.GetAll()
		assert.Len(t, all["global"], 2)
		_, hasEmpty := all[""]
		assert.False(t, hasEmpty)
	})

	t.Run("AddEvent enforces MaxEventsPerJob limit", func(t *testing.T) {
		em := NewEventMap()
		for i := 0; i < MaxEventsPerJob+100; i++ {
			em.AddEvent("job1", Event{EventID: fmt.Sprintf("%d", i)})
		}

		events := em.GetByJobID("job1")
		assert.Len(t, events, MaxEventsPerJob)
		// Verify oldest events are dropped (FIFO)
		assert.Equal(t, "100", events[0].EventID)
	})

	t.Run("GetByJobID returns sorted events", func(t *testing.T) {
		em := NewEventMap()
		em.AddEvent("job1", Event{EventID: "1", Timestamp: "1768591390000"}) // 30s
		em.AddEvent("job1", Event{EventID: "2", Timestamp: "1768591370000"}) // 10s
		em.AddEvent("job1", Event{EventID: "3", Timestamp: "1768591380000"}) // 20s

		events := em.GetByJobID("job1")
		assert.Equal(t, "1768591370000", events[0].Timestamp)
		assert.Equal(t, "1768591380000", events[1].Timestamp)
		assert.Equal(t, "1768591390000", events[2].Timestamp)
	})

	t.Run("GetByJobID returns empty slice for nonexistent job", func(t *testing.T) {
		em := NewEventMap()
		events := em.GetByJobID("nonexistent")
		require.NotNil(t, events)
		assert.Empty(t, events)
	})

	t.Run("GetAll returns sorted events for each job", func(t *testing.T) {
		em := NewEventMap()
		em.AddEvent("job1", Event{EventID: "1", Timestamp: "1768591380000"}) // 20s
		em.AddEvent("job1", Event{EventID: "2", Timestamp: "1768591370000"}) // 10s

		all := em.GetAll()
		assert.Equal(t, "2", all["job1"][0].EventID)
		assert.Equal(t, "1", all["job1"][1].EventID)
	})
}

func TestClusterEventMap(t *testing.T) {
	t.Run("NewClusterEventMap creates empty map", func(t *testing.T) {
		cm := NewClusterEventMap()
		require.NotNil(t, cm)
		assert.Empty(t, cm.GetAll("any"))
	})

	t.Run("GetOrCreateEventMap creates and reuses EventMap", func(t *testing.T) {
		cm := NewClusterEventMap()

		em1 := cm.GetOrCreateEventMap("cluster1")
		require.NotNil(t, em1)

		em2 := cm.GetOrCreateEventMap("cluster1")
		assert.Same(t, em1, em2, "should return same instance")

		em3 := cm.GetOrCreateEventMap("cluster2")
		assert.NotSame(t, em1, em3, "different cluster should get different instance")
	})

	t.Run("GetAll returns events for cluster", func(t *testing.T) {
		cm := NewClusterEventMap()
		em := cm.GetOrCreateEventMap("cluster1")
		em.AddEvent("job1", Event{EventID: "1", Timestamp: "1768591375000"})
		em.AddEvent("", Event{EventID: "2", Timestamp: "1768591370000"})

		all := cm.GetAll("cluster1")
		assert.Len(t, all, 2)
		assert.Contains(t, all, "job1")
		assert.Contains(t, all, "global")
	})

	t.Run("GetAll returns empty map for nonexistent cluster", func(t *testing.T) {
		cm := NewClusterEventMap()
		all := cm.GetAll("nonexistent")
		require.NotNil(t, all)
		assert.Empty(t, all)
	})

	t.Run("GetByJobID returns events for cluster and job", func(t *testing.T) {
		cm := NewClusterEventMap()
		em := cm.GetOrCreateEventMap("cluster1")
		em.AddEvent("job1", Event{EventID: "1", Timestamp: "1768591375000"}) // 15s
		em.AddEvent("job1", Event{EventID: "2", Timestamp: "1768591370000"}) // 10s

		events := cm.GetByJobID("cluster1", "job1")
		assert.Len(t, events, 2)
		assert.Equal(t, "2", events[0].EventID)
		assert.Equal(t, "1", events[1].EventID)
	})

	t.Run("GetByJobID returns empty for nonexistent cluster or job", func(t *testing.T) {
		cm := NewClusterEventMap()
		cm.GetOrCreateEventMap("cluster1")

		events := cm.GetByJobID("nonexistent", "job1")
		assert.Empty(t, events)

		events = cm.GetByJobID("cluster1", "nonexistent")
		assert.Empty(t, events)
	})
}

func TestEvent(t *testing.T) {
	event := Event{
		EventID:        "LrBbQwLLTK2+SSsBX+AG4EDK02CAVnvtVMD2MA==",
		EventType:      "ACTOR_DEFINITION_EVENT",
		SourceType:     "GCS",
		Timestamp:      "2026-01-16T19:16:15.210327633Z",
		Severity:       "INFO",
		Label:          "ACTOR_DEFINITION_EVENT",
		NodeID:         "531134a446a4f1b4d07301c0ee09b0ca32593dbb",
		SourceHostname: "ray-head-node-0",
		SourcePid:      12345,
		CustomFields: map[string]any{
			"sessionName": "session_2026-01-16_11-06-54_467309_1",
		},
	}

	assert.Equal(t, "LrBbQwLLTK2+SSsBX+AG4EDK02CAVnvtVMD2MA==", event.EventID)
	assert.Equal(t, "ACTOR_DEFINITION_EVENT", event.EventType)
	assert.Equal(t, "GCS", event.SourceType)
	assert.Equal(t, "INFO", event.Severity)
	assert.Equal(t, "ray-head-node-0", event.SourceHostname)
	assert.Equal(t, 12345, event.SourcePid)
	assert.NotNil(t, event.CustomFields["sessionName"])
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
