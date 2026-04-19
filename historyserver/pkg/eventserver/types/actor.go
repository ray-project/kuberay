package types

import (
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
	Port      int
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
	actorMap map[string]Actor
	mu       sync.Mutex
}

func NewActorMap() *ActorMap {
	return &ActorMap{
		actorMap: make(map[string]Actor),
	}
}

// ClusterActorMap uses the cluster name as the key
type ClusterActorMap struct {
	clusterActorMap map[string]*ActorMap
	mu              sync.RWMutex
}

func NewClusterActorMap() *ClusterActorMap {
	return &ClusterActorMap{
		clusterActorMap: make(map[string]*ActorMap),
	}
}

// GetOrCreateActorMap returns the ActorMap for the given cluster, creating it if it doesn't exist.
// This is the actor equivalent of ClusterTaskMap.GetOrCreateTaskMap
func (c *ClusterActorMap) GetOrCreateActorMap(clusterName string) *ActorMap {
	c.mu.Lock()
	defer c.mu.Unlock()

	actorMap, exists := c.clusterActorMap[clusterName]
	if !exists {
		actorMap = NewActorMap()
		c.clusterActorMap[clusterName] = actorMap
	}
	return actorMap
}

// GetActors returns all actors in a cluster session as deep copies.
func (c *ClusterActorMap) GetActors(clusterName string) []Actor {
	c.mu.RLock()
	actorMap, ok := c.clusterActorMap[clusterName]
	c.mu.RUnlock()
	if !ok {
		return []Actor{}
	}

	return actorMap.GetActors()
}

// GetActorByID returns an actor by ID in a cluster session as a deep copy.
func (c *ClusterActorMap) GetActorByID(clusterName, actorID string) (Actor, bool) {
	c.mu.RLock()
	actorMap, ok := c.clusterActorMap[clusterName]
	c.mu.RUnlock()
	if !ok {
		return Actor{}, false
	}

	return actorMap.GetActorByID(actorID)
}

// GetActorsMap returns all actors in a cluster session as deep copies keyed by actor ID.
func (c *ClusterActorMap) GetActorsMap(clusterName string) map[string]Actor {
	c.mu.RLock()
	actorMap, ok := c.clusterActorMap[clusterName]
	c.mu.RUnlock()
	if !ok {
		return map[string]Actor{}
	}

	return actorMap.GetActorsMap()
}

// CreateOrMergeActor finds or creates an actor and applies the merge function.
// Unlike Task which has AttemptNumber requiring binary search,
// Actor uses simple map lookup since ActorID is unique.
// This handles the case where LIFECYCLE events arrive before DEFINITION events.
func (a *ActorMap) CreateOrMergeActor(actorId string, mergeFn func(*Actor)) {
	a.mu.Lock()
	defer a.mu.Unlock()

	actor, exists := a.actorMap[actorId]
	if !exists {
		// Actor doesn't exist, create new with ActorID initialized
		newActor := Actor{ActorID: actorId}
		mergeFn(&newActor)
		a.actorMap[actorId] = newActor
		return
	}

	// Actor exists: apply merge function and write back to map
	// NOTE: Must write back because Go map returns a copy, not a reference
	mergeFn(&actor)
	a.actorMap[actorId] = actor
}

// GetActors returns all actors as deep copies.
func (a *ActorMap) GetActors() []Actor {
	a.mu.Lock()
	defer a.mu.Unlock()

	actors := make([]Actor, 0, len(a.actorMap))
	for _, actor := range a.actorMap {
		actors = append(actors, actor.DeepCopy())
	}

	return actors
}

// GetActorByID returns an actor by ID as a deep copy.
func (a *ActorMap) GetActorByID(actorID string) (Actor, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	actor, ok := a.actorMap[actorID]
	if !ok {
		return Actor{}, false
	}

	return actor.DeepCopy(), true
}

// GetActorsMap returns all actors as deep copies keyed by actor ID.
func (a *ActorMap) GetActorsMap() map[string]Actor {
	a.mu.Lock()
	defer a.mu.Unlock()

	actors := make(map[string]Actor, len(a.actorMap))
	for id, actor := range a.actorMap {
		actors[id] = actor.DeepCopy()
	}

	return actors
}

// DeepCopy returns a deep copy of the Actor, including slices and maps.
// This prevents race conditions when the returned Actor is used after locks are released.
func (a Actor) DeepCopy() Actor {
	cp := a
	if len(a.Events) > 0 {
		cp.Events = make([]ActorStateEvent, len(a.Events))
		copy(cp.Events, a.Events)
	}
	if len(a.RequiredResources) > 0 {
		cp.RequiredResources = make(map[string]float64, len(a.RequiredResources))
		for k, v := range a.RequiredResources {
			cp.RequiredResources[k] = v
		}
	}
	if len(a.LabelSelector) > 0 {
		cp.LabelSelector = make(map[string]string, len(a.LabelSelector))
		for k, v := range a.LabelSelector {
			cp.LabelSelector[k] = v
		}
	}
	return cp
}
