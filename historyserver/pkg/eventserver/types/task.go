package types

import (
	"sort"
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
	GETTING_AND_PINNING_ARGS                   TaskStatus = "GETTING_AND_PINNING_ARGS"
)

type TaskType string

const (
	NORMAL_TASK         TaskType = "NORMAL_TASK"
	ACTOR_CREATION_TASK TaskType = "ACTOR_CREATION_TASK"
	ACTOR_TASK          TaskType = "ACTOR_TASK"
	DRIVER_TASK         TaskType = "DRIVER_TASK"
)

// StateEvent represents a single state transition event with its timestamp.
// This mirrors the stateTransitions format from Ray's event export API.
type StateEvent struct {
	State     TaskStatus `json:"state"`
	Timestamp time.Time  `json:"timestamp"`
}

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
	// Events stores the complete state transition history.
	// Each element represents a state change with its timestamp.
	Events []StateEvent `json:"events,omitempty"`
	// ProfilingData ProfilingData
	WorkerID      string `json:"workerId"`
	ErrorType     string `json:"errorType"`
	ErrorMessage  string `json:"errorMessage"`
	TaskLogInfo   map[string]string
	CallSite      string
	LabelSelector map[string]string
}

// TaskMap is a struct that uses TaskID as key and stores a list of Task attempts.
// Each TaskID maps to a slice of Tasks, where each element represents a different attempt.
type TaskMap struct {
	TaskMap map[string][]Task
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
		TaskMap: make(map[string][]Task),
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

// GetOrCreateTaskMap returns the TaskMap for the given cluster, creating it if it doesn't exist.
func (c *ClusterTaskMap) GetOrCreateTaskMap(clusterName string) *TaskMap {
	c.Lock()
	defer c.Unlock()

	taskMap, exists := c.ClusterTaskMap[clusterName]
	if !exists {
		taskMap = NewTaskMap()
		c.ClusterTaskMap[clusterName] = taskMap
	}
	return taskMap
}

// CreateOrMergeAttempt finds or creates a task attempt and applies the merge function.
// Uses binary search for O(log n) lookup. Maintains sorted order by AttemptNumber.
// This handles the case where LIFECYCLE events arrive before DEFINITION events.
// - If the attempt doesn't exist, creates a new one at the correct position
// - If the attempt exists, applies mergeFn to merge new data into existing
func (t *TaskMap) CreateOrMergeAttempt(taskId string, attemptNum int, mergeFn func(*Task)) {
	t.Lock()
	defer t.Unlock()

	attempts, exists := t.TaskMap[taskId]
	if !exists {
		// Task doesn't exist, create new
		newTask := Task{TaskID: taskId, AttemptNumber: attemptNum}
		mergeFn(&newTask)
		t.TaskMap[taskId] = []Task{newTask}
		return
	}

	// Binary search: find the first index where AttemptNumber >= attemptNum
	idx := sort.Search(len(attempts), func(i int) bool {
		return attempts[i].AttemptNumber >= attemptNum
	})

	// Check if attempt already exists at this position
	if idx < len(attempts) && attempts[idx].AttemptNumber == attemptNum {
		// Exists: merge into existing
		mergeFn(&attempts[idx])
		return
	}

	// Doesn't exist: insert at correct position to maintain sorted order
	newTask := Task{TaskID: taskId, AttemptNumber: attemptNum}
	mergeFn(&newTask)

	// Insert at idx position
	attempts = append(attempts, Task{})    // Extend slice by 1
	copy(attempts[idx+1:], attempts[idx:]) // Shift elements right
	attempts[idx] = newTask                // Insert at correct position
	t.TaskMap[taskId] = attempts
}

func GetTaskFieldValue(task Task, filterKey string) string {
	switch filterKey {
	case "task_id":
		return task.TaskID
	case "job_id":
		return task.JobID
	case "state":
		return string(task.State)
	case "name", "task_name":
		return task.Name
	case "func_name", "function_name":
		return task.FuncOrClassName
	case "node_id":
		return task.NodeID
	case "actor_id":
		return task.ActorID
	case "type", "task_type":
		return string(task.Type)
	case "worker_id":
		return task.WorkerID
	case "language":
		return task.Language
	case "error_type":
		return task.ErrorType
	default:
		return ""
	}
}
