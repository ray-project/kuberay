package types

import (
	"sync"
	"time"
)

type TaskStatus string

const (
	NIL                                        TaskStatus = "NIL"
	PENDING_ARGS_AVAIL                         TaskStatus = "PENDING_ARGS_AVAIL"
	PENDING_NODE_ASSIGNMENT                    TaskStatus = "PENDING_NODE_ASSIGNMENT"
	PENDING_OBJ_STORE_MEM_AVAIL                TaskStatus = "PENDING_OBJ_STORE_MEM_AVAIL"
	PENDING_ARGS_FETCH                         TaskStatus = "PENDING_ARGS_FETCH"
	SUBMITTED_TO_WORKER                        TaskStatus = "SUBMITTED_TO_WORKER"
	PENDING_ACTOR_TASK_ARGS_FETCH              TaskStatus = "PENDING_ACTOR_TASK_ARGS_FETCH"
	PENDING_ACTOR_TASK_ORDERING_OR_CONCURRENCY TaskStatus = "PENDING_ACTOR_TASK_ORDERING_OR_CONCURRENCY"
	RUNNING                                    TaskStatus = "RUNNING"
	RUNNING_IN_RAY_GET                         TaskStatus = "RUNNING_IN_RAY_GET"
	RUNNING_IN_RAY_WAIT                        TaskStatus = "RUNNING_IN_RAY_WAIT"
	FINISHED                                   TaskStatus = "FINISHED"
	FAILED                                     TaskStatus = "FAILED"
)

type TaskType string

const (
	NORMAL_TASK         TaskType = "NORMAL_TASK"
	ACTOR_CREATION_TASK TaskType = "ACTOR_CREATION_TASK"
	ACTOR_TASK          TaskType = "ACTOR_TASK"
	DRIVER_TASK         TaskType = "DRIVER_TASK"
)

type Task struct {
	TaskID            string `json:"taskId"`
	Name              string `json:"taskName"`
	AttemptNumber     int    `json:"taskAttempt"`
	State             TaskStatus
	JobID             string `json:"jobId"`
	NodeID            string `json:"nodeId"`
	ActorID           string
	PlacementGroupID  string         `json:"placementGroupId"`
	Type              TaskType       `json:"taskType"`
	FuncOrClassName   string         `json:"functionName"`
	Language          string         `json:"language"`
	RequiredResources map[string]int `json:"requiredResources"`
	StartTime         time.Time
	EndTime           time.Time
	// ProfilingData ProfilingData
	WorkerID      string `json:"workerId"`
	ErrorType     string `json:"errorType"`
	ErrorMessage  string `json:"errorMessage"`
	TaskLogInfo   map[string]string
	CallSite      string
	LabelSelector map[string]string
}

// TaskMap is a struct that uses TaskID as key and the Task struct as value
type TaskMap struct {
	TaskMap map[string]Task
	Mu      sync.Mutex
}

func (t *TaskMap) Lock() {
	t.Mu.Lock()
}

func (t *TaskMap) Unlock() {
	t.Mu.Unlock()
}

func NewTaskMap() *TaskMap {
	return &TaskMap{
		TaskMap: make(map[string]Task),
	}
}

// ClusterTaskMap uses the cluster name as the key
type ClusterTaskMap struct {
	ClusterTaskMap map[string]*TaskMap
	Mu             sync.RWMutex
}

func (c *ClusterTaskMap) RLock() {
	c.Mu.RLock()
}

func (c *ClusterTaskMap) RUnlock() {
	c.Mu.RUnlock()
}

func (c *ClusterTaskMap) Lock() {
	c.Mu.Lock()
}

func (c *ClusterTaskMap) Unlock() {
	c.Mu.Unlock()
}
