package types

import (
	"strconv"
	"sync"
	"time"
)

type StateType string

const (
	DEPENDENCIES_UNREADY StateType = "DEPENDENCIES_UNREADY"
	PENDING_CREATION     StateType = "PENDING_CREATION"
	ALIVE                StateType = "ALIVE"
	RESTARTING           StateType = "RESTARTING"
	DEAD                 StateType = "DEAD"
)

type Address struct {
	NodeID    string
	IPAddress string
	Port      string
	WorkerID  string
}

// ActorStateEvent represents a single state transition event with its timestamp.
// This mirrors the stateTransitions format from Ray's actor lifecycle event.
// Fields:
// - State: the actor state (DEPENDENCIES_UNREADY, PENDING_CREATION, ALIVE, RESTARTING, DEAD)
// - Timestamp: when the state transition occurred
// - NodeID: node where actor runs (only populated in ALIVE state)
// - WorkerID: worker running the actor (only populated in ALIVE state)
// - ReprName: actor repr name (may change during lifecycle)
// - DeathCause: JSON string containing death details (only in DEAD state)
type ActorStateEvent struct {
	State      StateType `json:"state"`
	Timestamp  time.Time `json:"timestamp"`
	NodeID     string    `json:"nodeId,omitempty"`
	WorkerID   string    `json:"workerId,omitempty"`
	ReprName   string    `json:"reprName,omitempty"`
	DeathCause string    `json:"deathCause,omitempty"`
}

type Actor struct {
	ActorID          string `json:"actorId"`
	JobID            string `json:"jobId"`
	PlacementGroupID string `json:"placementGroupId,omitempty"`
	State            StateType

	// PID is extracted from deathCause.actorDiedErrorContext.pid (not from definition event)
	PID int `json:"pid,omitempty"`

	// Address contains node/worker info, populated from ALIVE state transitions
	Address Address `json:"address"`

	Name       string `json:"name,omitempty"`
	ActorClass string `json:"className"`

	// NumRestarts is calculated by counting RESTARTING states in Events
	NumRestarts int `json:"numRestarts"`

	// RequiredResources type changed from int to float64 to match Ray protobuf
	RequiredResources map[string]float64 `json:"requiredResources,omitempty"`

	// ExitDetails is extracted from deathCause.actorDiedErrorContext.errorMessage
	ExitDetails string `json:"exitDetails,omitempty"`

	ReprName      string            `json:"reprName,omitempty"`
	CallSite      string            `json:"callSite,omitempty"`
	LabelSelector map[string]string `json:"labelSelector,omitempty"`

	// IsDetached indicates if actor is detached (survives driver exit)
	IsDetached bool `json:"isDetached"`

	// RayNamespace is the Ray namespace this actor belongs to
	RayNamespace string `json:"rayNamespace,omitempty"`

	// SerializedRuntimeEnv contains the serialized runtime environment
	SerializedRuntimeEnv string `json:"serializedRuntimeEnv,omitempty"`

	// --- LIFECYCLE FIELDS ---

	// Events stores the complete state transition history
	// Deduplication is applied based on timestamp to avoid duplicate events
	Events []ActorStateEvent `json:"events,omitempty"`

	// StartTime is the timestamp of first ALIVE state (computed from Events)
	StartTime time.Time `json:"startTime,omitempty"`

	// EndTime is the timestamp of DEAD state (computed from Events)
	EndTime time.Time `json:"endTime,omitempty"`
}

// ActorMap is a struct that uses ActorID as key and the Actor struct as value
type ActorMap struct {
	ActorMap map[string]Actor
	Mu       sync.Mutex
}

func (a *ActorMap) Lock() {
	a.Mu.Lock()
}

func (a *ActorMap) Unlock() {
	a.Mu.Unlock()
}

func NewActorMap() *ActorMap {
	return &ActorMap{
		ActorMap: make(map[string]Actor),
	}
}

// ClusterActorMap uses the cluster name as the key
type ClusterActorMap struct {
	ClusterActorMap map[string]*ActorMap
	Mu              sync.RWMutex
}

func (c *ClusterActorMap) RLock() {
	c.Mu.RLock()
}

func (c *ClusterActorMap) RUnlock() {
	c.Mu.RUnlock()
}

func (c *ClusterActorMap) Lock() {
	c.Mu.Lock()
}

func (c *ClusterActorMap) Unlock() {
	c.Mu.Unlock()
}

// GetOrCreateActorMap returns the ActorMap for the given cluster, creating it if it doesn't exist.
// This is the actor equivalent of ClusterTaskMap.GetOrCreateTaskMap
func (c *ClusterActorMap) GetOrCreateActorMap(clusterName string) *ActorMap {
	c.Lock()
	defer c.Unlock()

	actorMap, exists := c.ClusterActorMap[clusterName]
	if !exists {
		actorMap = NewActorMap()
		c.ClusterActorMap[clusterName] = actorMap
	}
	return actorMap
}

// CreateOrMergeActor finds or creates an actor and applies the merge function.
// Unlike Task which has AttemptNumber requiring binary search,
// Actor uses simple map lookup since ActorID is unique.
// This handles the case where LIFECYCLE events arrive before DEFINITION events.
func (a *ActorMap) CreateOrMergeActor(actorId string, mergeFn func(*Actor)) {
	a.Lock()
	defer a.Unlock()

	actor, exists := a.ActorMap[actorId]
	if !exists {
		// Actor doesn't exist, create new with ActorID initialized
		newActor := Actor{ActorID: actorId}
		mergeFn(&newActor)
		a.ActorMap[actorId] = newActor
		return
	}

	// Actor exists: apply merge function and write back to map
	// NOTE: Must write back because Go map returns a copy, not a reference
	mergeFn(&actor)
	a.ActorMap[actorId] = actor
}

func GetActorFieldValue(actor Actor, filterKey string) string {
	switch filterKey {
	case "actor_id":
		return actor.ActorID
	case "job_id":
		return actor.JobID
	case "state":
		return string(actor.State)
	case "name", "actor_name":
		return actor.Name
	case "class_name", "actor_class":
		return actor.ActorClass
	case "node_id":
		return actor.Address.NodeID
	case "pid":
		return strconv.Itoa(actor.PID)
	case "placement_group_id":
		return actor.PlacementGroupID
	default:
		return ""
	}
}
