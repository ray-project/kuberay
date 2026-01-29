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
	PlacementGroupID  string             `json:"placementGroupId"`
	Type              TaskType           `json:"taskType"`
	FuncOrClassName   string             `json:"functionName"`
	Language          string             `json:"language"`
	RequiredResources map[string]float64 `json:"requiredResources"` // float64 to match Ray protobuf (e.g., {"CPU": 0.5})
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

// DeepCopy returns a deep copy of the Task, including slices and maps.
// This prevents race conditions when the returned Task is used after locks are released.
func (t Task) DeepCopy() Task {
	cp := t
	if len(t.Events) > 0 {
		cp.Events = make([]StateEvent, len(t.Events))
		copy(cp.Events, t.Events)
	}
	if len(t.RequiredResources) > 0 {
		cp.RequiredResources = make(map[string]float64, len(t.RequiredResources))
		for k, v := range t.RequiredResources {
			cp.RequiredResources[k] = v
		}
	}
	if len(t.TaskLogInfo) > 0 {
		cp.TaskLogInfo = make(map[string]string, len(t.TaskLogInfo))
		for k, v := range t.TaskLogInfo {
			cp.TaskLogInfo[k] = v
		}
	}
	if len(t.LabelSelector) > 0 {
		cp.LabelSelector = make(map[string]string, len(t.LabelSelector))
		for k, v := range t.LabelSelector {
			cp.LabelSelector[k] = v
		}
	}
	return cp
}

// SessionTaskMap uses sessionName as the key to store tasks for each session.
type SessionTaskMap struct {
	SessionTaskMap map[string]*TaskMap
	Mu             sync.RWMutex
}

func NewSessionTaskMap() *SessionTaskMap {
	return &SessionTaskMap{
		SessionTaskMap: make(map[string]*TaskMap),
	}
}

func (s *SessionTaskMap) RLock() {
	s.Mu.RLock()
}

func (s *SessionTaskMap) RUnlock() {
	s.Mu.RUnlock()
}

func (s *SessionTaskMap) Lock() {
	s.Mu.Lock()
}

func (s *SessionTaskMap) Unlock() {
	s.Mu.Unlock()
}

// GetOrCreateTaskMap returns the TaskMap for the given session, creating it if it doesn't exist.
func (s *SessionTaskMap) GetOrCreateTaskMap(sessionName string) *TaskMap {
	s.Lock()
	defer s.Unlock()

	taskMap, exists := s.SessionTaskMap[sessionName]
	if !exists {
		taskMap = NewTaskMap()
		s.SessionTaskMap[sessionName] = taskMap
	}
	return taskMap
}

// ClusterSessionTaskMap provides session-scoped task storage.
// Uses secondary index to get the task map.
type ClusterSessionTaskMap struct {
	ClusterSessionTaskMap map[string]*SessionTaskMap
	Mu                    sync.RWMutex
}

func (c *ClusterSessionTaskMap) RLock() {
	c.Mu.RLock()
}

func (c *ClusterSessionTaskMap) RUnlock() {
	c.Mu.RUnlock()
}

func (c *ClusterSessionTaskMap) Lock() {
	c.Mu.Lock()
}

func (c *ClusterSessionTaskMap) Unlock() {
	c.Mu.Unlock()
}

// GetOrCreateSessionTaskMap returns the SessionTaskMap for the given cluster, creating it if it doesn't exist.
func (c *ClusterSessionTaskMap) GetOrCreateSessionTaskMap(clusterName string) *SessionTaskMap {
	c.Lock()
	defer c.Unlock()

	sessionTaskMap, exists := c.ClusterSessionTaskMap[clusterName]
	if !exists {
		sessionTaskMap = NewSessionTaskMap()
		c.ClusterSessionTaskMap[clusterName] = sessionTaskMap
	}
	return sessionTaskMap
}
