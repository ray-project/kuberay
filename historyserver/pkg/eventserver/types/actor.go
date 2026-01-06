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
	ActorID           string
	JobID             string
	PlacementGroupID  string
	State             StateType
	PID               string
	Address           Address
	Name              string
	NumRestarts       string
	ActorClass        string
	StartTime         time.Time
	EndTime           time.Time
	RequiredResources map[string]int
	ExitDetails       string
	ReprName          string
	CallSite          string
	LabelSelector     map[string]string
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
