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
	Port      string
	WorkerID  string
}

type Actor struct {
	ActorID           string `json:"actorId"`
	JobID             string `json:"jobId"`
	PlacementGroupID  string `json:"placementGroupId"`
	State             StateType
	PID               string            `json:"pid"`
	Address           Address           `json:"address"`
	Name              string            `json:"name"`
	NumRestarts       int               `json:"numRestarts"`
	ActorClass        string            `json:"className"`
	StartTime         time.Time         `json:"startTime"`
	EndTime           time.Time         `json:"endTime"`
	RequiredResources map[string]int    `json:"requiredResources"`
	ExitDetails       string            `json:"exitDetails"`
	ReprName          string            `json:"reprName"`
	CallSite          string            `json:"callSite"`
	LabelSelector     map[string]string `json:"labelSelector"`
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
		return actor.PID
	case "placement_group_id":
		return actor.PlacementGroupID
	default:
		return ""
	}
}
