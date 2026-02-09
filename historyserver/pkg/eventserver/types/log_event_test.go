package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobEventMap(t *testing.T) {
	t.Run("NewJobEventMap creates empty map", func(t *testing.T) {
		em := NewJobEventMap()
		require.NotNil(t, em)
		assert.Empty(t, em.GetAllEvents())
	})

	t.Run("AddEvent stores events by jobID", func(t *testing.T) {
		em := NewJobEventMap()
		em.AddEvent(&LogEvent{EventID: "1", Timestamp: "1768591375", CustomFields: map[string]any{"job_id": "job1"}})
		em.AddEvent(&LogEvent{EventID: "2", Timestamp: "1768591376", CustomFields: map[string]any{"job_id": "job2"}})

		all := em.GetAllEvents()
		assert.Len(t, all, 2)
		assert.Len(t, all["job1"], 1)
		assert.Len(t, all["job2"], 1)
	})

	t.Run("AddEvent with no job_id stores under global", func(t *testing.T) {
		em := NewJobEventMap()
		em.AddEvent(&LogEvent{EventID: "1"})
		em.AddEvent(&LogEvent{EventID: "2"})

		all := em.GetAllEvents()
		assert.Len(t, all["global"], 2)
		_, hasEmpty := all[""]
		assert.False(t, hasEmpty)
	})

	t.Run("AddEvent enforces MaxEventsPerJob limit with 110% threshold", func(t *testing.T) {
		em := NewJobEventMap()
		// Add more than 110% of MaxEventsPerJob to trigger trimming.
		// Ray Dashboard's eviction logic (like in event_head.py _update_events):
		// - Only trims when len exceeds 110% of max
		// - After trimming, keeps newest MaxEventsPerJob events
		// So if we add exactly 110%+1 events, we get MaxEventsPerJob after one trim.
		numEvents := int(float64(MaxEventsPerJob)*1.1) + 1
		for i := 0; i < numEvents; i++ {
			em.AddEvent(&LogEvent{
				EventID:      fmt.Sprintf("%d", i),
				Timestamp:    fmt.Sprintf("%d", 1768591000+i),
				CustomFields: map[string]any{"job_id": "job1"},
			})
		}

		events := em.GetEventsByJobID("job1")
		assert.Equal(t, MaxEventsPerJob, len(events), "should be exactly MaxEventsPerJob after one trim")

		// Verify we kept the newest events (highest timestamps)
		// The oldest event should have been evicted
		firstEventTimestamp := events[0]["timestamp"].(string)
		// The first kept event should be the (numEvents - MaxEventsPerJob + 1)th event
		// which has timestamp 1768591000 + 1001 = 1768592001 (for numEvents=11001, MaxEventsPerJob=10000)
		expectedFirstTimestamp := fmt.Sprintf("%d", 1768591000+numEvents-MaxEventsPerJob)
		assert.Equal(t, expectedFirstTimestamp, firstEventTimestamp, "should have evicted oldest events")
	})

	t.Run("GetByJobID returns sorted events", func(t *testing.T) {
		em := NewJobEventMap()
		em.AddEvent(&LogEvent{EventID: "1", Timestamp: "1768591390", CustomFields: map[string]any{"job_id": "job1"}})
		em.AddEvent(&LogEvent{EventID: "2", Timestamp: "1768591370", CustomFields: map[string]any{"job_id": "job1"}})
		em.AddEvent(&LogEvent{EventID: "3", Timestamp: "1768591380", CustomFields: map[string]any{"job_id": "job1"}})

		events := em.GetEventsByJobID("job1")
		assert.Equal(t, "1768591370", events[0]["timestamp"])
		assert.Equal(t, "1768591380", events[1]["timestamp"])
		assert.Equal(t, "1768591390", events[2]["timestamp"])
	})

	t.Run("GetEventsByJobID returns empty slice for nonexistent job", func(t *testing.T) {
		em := NewJobEventMap()
		events := em.GetEventsByJobID("nonexistent")
		require.NotNil(t, events)
		assert.Empty(t, events)
	})

	t.Run("GetAllEvents returns sorted events for each job", func(t *testing.T) {
		em := NewJobEventMap()
		em.AddEvent(&LogEvent{EventID: "1", Timestamp: "1768591380", CustomFields: map[string]any{"job_id": "job1"}})
		em.AddEvent(&LogEvent{EventID: "2", Timestamp: "1768591370", CustomFields: map[string]any{"job_id": "job1"}})

		all := em.GetAllEvents()
		assert.Equal(t, "2", all["job1"][0]["eventId"])
		assert.Equal(t, "1", all["job1"][1]["eventId"])
	})
}

func TestLogEventStore(t *testing.T) {
	t.Run("NewLogEventStore creates empty store", func(t *testing.T) {
		cs := NewLogEventStore()
		require.NotNil(t, cs)
		assert.Empty(t, cs.GetAllEvents("any"))
	})

	t.Run("GetOrCreateJobEventMap creates and reuses JobEventMap", func(t *testing.T) {
		cs := NewLogEventStore()

		// clusterSessionKey format: "{clusterName}_{namespace}_{sessionName}"
		clusterSessionKey1 := "my-cluster_default_session_2026-01-16"
		clusterSessionKey2 := "my-cluster_default_session_2026-01-17"

		em1 := cs.GetOrCreateJobEventMap(clusterSessionKey1)
		require.NotNil(t, em1)

		em2 := cs.GetOrCreateJobEventMap(clusterSessionKey1)
		assert.Same(t, em1, em2, "should return same instance")

		em3 := cs.GetOrCreateJobEventMap(clusterSessionKey2)
		assert.NotSame(t, em1, em3, "different cluster session should get different instance")
	})

	t.Run("GetAllEvents returns events for cluster session", func(t *testing.T) {
		cs := NewLogEventStore()
		clusterSessionKey := "my-cluster_default_session_2026-01-16"
		em := cs.GetOrCreateJobEventMap(clusterSessionKey)
		em.AddEvent(&LogEvent{EventID: "1", Timestamp: "1768591375", CustomFields: map[string]any{"job_id": "job1"}})
		em.AddEvent(&LogEvent{EventID: "2", Timestamp: "1768591370"})

		all := cs.GetAllEvents(clusterSessionKey)
		assert.Len(t, all, 2)
		assert.Contains(t, all, "job1")
		assert.Contains(t, all, "global")
	})

	t.Run("GetAllEvents returns empty map for nonexistent cluster", func(t *testing.T) {
		cs := NewLogEventStore()
		all := cs.GetAllEvents("nonexistent")
		require.NotNil(t, all)
		assert.Empty(t, all)
	})

	t.Run("GetEventsByJobID returns events for cluster session and job", func(t *testing.T) {
		cs := NewLogEventStore()
		clusterSessionKey := "my-cluster_default_session_2026-01-16"
		em := cs.GetOrCreateJobEventMap(clusterSessionKey)
		em.AddEvent(&LogEvent{EventID: "1", Timestamp: "1768591375", CustomFields: map[string]any{"job_id": "job1"}})
		em.AddEvent(&LogEvent{EventID: "2", Timestamp: "1768591370", CustomFields: map[string]any{"job_id": "job1"}})

		events := cs.GetEventsByJobID(clusterSessionKey, "job1")
		assert.Len(t, events, 2)
		assert.Equal(t, "2", events[0]["eventId"])
		assert.Equal(t, "1", events[1]["eventId"])
	})

	t.Run("GetEventsByJobID returns empty for nonexistent cluster session or job", func(t *testing.T) {
		cs := NewLogEventStore()
		clusterSessionKey := "my-cluster_default_session_2026-01-16"
		cs.GetOrCreateJobEventMap(clusterSessionKey)

		events := cs.GetEventsByJobID("nonexistent_ns_session", "job1")
		assert.Empty(t, events)

		events = cs.GetEventsByJobID(clusterSessionKey, "nonexistent")
		assert.Empty(t, events)
	})

	t.Run("GetEventsByJobID with empty string returns empty slice", func(t *testing.T) {
		cs := NewLogEventStore()
		clusterSessionKey := "my-cluster_default_session_2026-01-16"
		em := cs.GetOrCreateJobEventMap(clusterSessionKey)
		em.AddEvent(&LogEvent{EventID: "1", Timestamp: "1768591375", CustomFields: map[string]any{"job_id": "job1"}})
		em.AddEvent(&LogEvent{EventID: "2", Timestamp: "1768591370"}) // stored under "global"

		// Query with empty string should NOT match "global" - it should return empty
		events := cs.GetEventsByJobID(clusterSessionKey, "")
		assert.Empty(t, events, "empty string jobID should return empty slice, not global events")
	})
}

func TestLogEvent(t *testing.T) {
	event := LogEvent{
		EventID:        "abc123def456",
		SourceType:     "GCS",
		SourceHostname: "ray-head-node-0",
		SourcePid:      12345,
		Label:          "cluster_lifecycle",
		Message:        "Test message",
		Timestamp:      "1737055775",
		Severity:       "INFO",
		CustomFields: map[string]any{
			"job_id":        "01000000",
			"submission_id": "raysubmit_123",
		},
	}

	// Verify GetJobID extracts from custom_fields
	assert.Equal(t, "01000000", event.GetJobID())

	// Verify ToAPIResponse converts to camelCase
	response := event.ToAPIResponse()
	assert.Equal(t, "abc123def456", response["eventId"])
	assert.Equal(t, "GCS", response["sourceType"])
	assert.Equal(t, "ray-head-node-0", response["sourceHostname"])
	assert.Equal(t, 12345, response["sourcePid"])
	assert.Equal(t, "cluster_lifecycle", response["label"])
	assert.Equal(t, "INFO", response["severity"])
	assert.Equal(t, "1737055775", response["timestamp"])
	assert.Equal(t, "Test message", response["message"])

	// Verify customFields are converted to camelCase
	cf := response["customFields"].(map[string]any)
	assert.Equal(t, "01000000", cf["jobId"])
	assert.Equal(t, "raysubmit_123", cf["submissionId"])
}

func TestLogEventGetJobID(t *testing.T) {
	t.Run("returns job_id from custom_fields", func(t *testing.T) {
		event := LogEvent{
			EventID:      "1",
			CustomFields: map[string]any{"job_id": "test_job"},
		}
		assert.Equal(t, "test_job", event.GetJobID())
	})

	t.Run("returns global when no job_id", func(t *testing.T) {
		event := LogEvent{
			EventID:      "1",
			CustomFields: map[string]any{"other_field": "value"},
		}
		assert.Equal(t, "global", event.GetJobID())
	})

	t.Run("returns global when custom_fields is nil", func(t *testing.T) {
		event := LogEvent{EventID: "1"}
		assert.Equal(t, "global", event.GetJobID())
	})

	t.Run("returns global when job_id is empty string", func(t *testing.T) {
		event := LogEvent{
			EventID:      "1",
			CustomFields: map[string]any{"job_id": ""},
		}
		assert.Equal(t, "global", event.GetJobID())
	})
}

func TestLogEventRestoreNewline(t *testing.T) {
	t.Run("restores escaped newlines", func(t *testing.T) {
		event := LogEvent{
			EventID: "1",
			Message: "line1\\nline2\\nline3",
		}
		event.RestoreNewline()
		assert.Equal(t, "line1\nline2\nline3", event.Message)
	})

	t.Run("restores escaped carriage returns", func(t *testing.T) {
		event := LogEvent{
			EventID: "1",
			Message: "line1\\rline2",
		}
		event.RestoreNewline()
		assert.Equal(t, "line1\nline2", event.Message)
	})
}

func TestToCamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"source_hostname", "sourceHostname"},
		{"source_pid", "sourcePid"},
		{"job_id", "jobId"},
		{"custom_fields", "customFields"},
		{"single", "single"},
		{"", ""},
		{"a_b_c", "aBC"},
	}

	for _, tc := range tests {
		result := toCamelCase(tc.input)
		assert.Equal(t, tc.expected, result, "toCamelCase(%q)", tc.input)
	}
}

func TestToGoogleStyle(t *testing.T) {
	input := map[string]any{
		"job_id":        "test",
		"submission_id": "raysubmit_123",
		"nested_field": map[string]any{
			"inner_key": "value",
		},
	}

	result := toGoogleStyle(input)

	assert.Equal(t, "test", result["jobId"])
	assert.Equal(t, "raysubmit_123", result["submissionId"])

	nested := result["nestedField"].(map[string]any)
	assert.Equal(t, "value", nested["innerKey"])
}
