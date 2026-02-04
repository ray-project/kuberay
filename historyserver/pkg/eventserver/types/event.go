package types

import (
	"sort"
	"sync"
)

// EventType is the Ray event type.
type EventType string

// There are 11 potential Ray event types:
// https://github.com/ray-project/ray/blob/3b41c97fa90c58b0b72c0026f57005b92310160d/src/ray/protobuf/public/events_base_event.proto#L49-L61
const (
	EVENT_TYPE_UNSPECIFIED      EventType = "EVENT_TYPE_UNSPECIFIED"
	TASK_DEFINITION_EVENT       EventType = "TASK_DEFINITION_EVENT"
	TASK_LIFECYCLE_EVENT        EventType = "TASK_LIFECYCLE_EVENT"
	ACTOR_TASK_DEFINITION_EVENT EventType = "ACTOR_TASK_DEFINITION_EVENT"
	TASK_PROFILE_EVENT          EventType = "TASK_PROFILE_EVENT"
	DRIVER_JOB_DEFINITION_EVENT EventType = "DRIVER_JOB_DEFINITION_EVENT"
	DRIVER_JOB_LIFECYCLE_EVENT  EventType = "DRIVER_JOB_LIFECYCLE_EVENT"
	NODE_DEFINITION_EVENT       EventType = "NODE_DEFINITION_EVENT"
	NODE_LIFECYCLE_EVENT        EventType = "NODE_LIFECYCLE_EVENT"
	ACTOR_DEFINITION_EVENT      EventType = "ACTOR_DEFINITION_EVENT"
	ACTOR_LIFECYCLE_EVENT       EventType = "ACTOR_LIFECYCLE_EVENT"
)

// AllEventTypes includes all potential event types defined in Ray.
var AllEventTypes = []EventType{
	EVENT_TYPE_UNSPECIFIED,
	TASK_DEFINITION_EVENT,
	TASK_LIFECYCLE_EVENT,
	ACTOR_TASK_DEFINITION_EVENT,
	TASK_PROFILE_EVENT,
	DRIVER_JOB_DEFINITION_EVENT,
	DRIVER_JOB_LIFECYCLE_EVENT,
	NODE_DEFINITION_EVENT,
	NODE_LIFECYCLE_EVENT,
	ACTOR_DEFINITION_EVENT,
	ACTOR_LIFECYCLE_EVENT,
}

// MaxEventsPerJob is the maximum number of events to cache per job.
// This matches Ray Dashboard's MAX_EVENTS_TO_CACHE constant.
const MaxEventsPerJob = 10000

// Event represents an event returned by the /events API endpoint.
// Fields use camelCase JSON tags to match Ray Dashboard's format.
// This struct is derived from RayEvents stored in object storage.
type Event struct {
	EventID        string         `json:"eventId"`
	EventType      string         `json:"eventType"`                // e.g., TASK_DEFINITION_EVENT
	SourceType     string         `json:"sourceType"`               // e.g., GCS, CORE_WORKER
	Timestamp      string         `json:"timestamp"`                // ISO 8601 format
	Severity       string         `json:"severity"`                 // INFO, WARNING, ERROR
	Message        string         `json:"message,omitempty"`        // Usually empty in RayEvents
	Label          string         `json:"label,omitempty"`          // Same as EventType for filtering
	NodeID         string         `json:"nodeId,omitempty"`         // Node where event originated
	SourceHostname string         `json:"sourceHostname,omitempty"` // Extracted from NodeDefinitionEvent
	SourcePid      int            `json:"sourcePid,omitempty"`      // Extracted from lifecycle events
	CustomFields   map[string]any `json:"customFields,omitempty"`   // Event-specific nested data
}

// EventMap stores events grouped by jobId with FIFO eviction.
// Key is jobId (base64 encoded) or "global" for cluster-wide events.
// This follows the same pattern as TaskMap and ActorMap.
type EventMap struct {
	events map[string][]Event
	mu     sync.RWMutex
}

// NewEventMap creates a new EventMap instance.
func NewEventMap() *EventMap {
	return &EventMap{
		events: make(map[string][]Event),
	}
}

// AddEvent adds an event to the map and enforces the per-job limit.
// Empty jobID is stored under "global" key.
func (e *EventMap) AddEvent(jobID string, event Event) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if jobID == "" {
		jobID = "global"
	}

	e.events[jobID] = append(e.events[jobID], event)

	// Enforce limit: keep newest events (FIFO - drop oldest)
	if len(e.events[jobID]) > MaxEventsPerJob {
		e.events[jobID] = e.events[jobID][len(e.events[jobID])-MaxEventsPerJob:]
	}
}

// GetAll returns all events grouped by jobId, sorted by timestamp.
func (e *EventMap) GetAll() map[string][]Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make(map[string][]Event, len(e.events))
	for jobID, events := range e.events {
		sorted := make([]Event, len(events))
		copy(sorted, events)
		sortEventsByTimestamp(sorted)
		result[jobID] = sorted
	}
	return result
}

// GetByJobID returns events for a specific job, sorted by timestamp.
func (e *EventMap) GetByJobID(jobID string) []Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	events, ok := e.events[jobID]
	if !ok {
		return []Event{}
	}

	sorted := make([]Event, len(events))
	copy(sorted, events)
	sortEventsByTimestamp(sorted)
	return sorted
}

// sortEventsByTimestamp sorts events in ascending order by timestamp (ISO 8601 string comparison).
func sortEventsByTimestamp(events []Event) {
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp < events[j].Timestamp
	})
}

// ClusterEventMap stores EventMaps per cluster.
// This follows the same pattern as ClusterTaskMap and ClusterActorMap.
type ClusterEventMap struct {
	clusterEvents map[string]*EventMap
	mu            sync.RWMutex
}

// NewClusterEventMap creates a new ClusterEventMap instance.
func NewClusterEventMap() *ClusterEventMap {
	return &ClusterEventMap{
		clusterEvents: make(map[string]*EventMap),
	}
}

// GetOrCreateEventMap returns the EventMap for the given cluster, creating it if needed.
func (c *ClusterEventMap) GetOrCreateEventMap(clusterName string) *EventMap {
	c.mu.Lock()
	defer c.mu.Unlock()

	if eventMap, ok := c.clusterEvents[clusterName]; ok {
		return eventMap
	}
	eventMap := NewEventMap()
	c.clusterEvents[clusterName] = eventMap
	return eventMap
}

// GetAll returns all events for a cluster, grouped by jobId.
func (c *ClusterEventMap) GetAll(clusterName string) map[string][]Event {
	c.mu.RLock()
	eventMap, ok := c.clusterEvents[clusterName]
	c.mu.RUnlock()

	if !ok {
		return map[string][]Event{}
	}
	return eventMap.GetAll()
}

// GetByJobID returns events for a specific job in a cluster.
func (c *ClusterEventMap) GetByJobID(clusterName, jobID string) []Event {
	c.mu.RLock()
	eventMap, ok := c.clusterEvents[clusterName]
	c.mu.RUnlock()

	if !ok {
		return []Event{}
	}
	return eventMap.GetByJobID(jobID)
}
