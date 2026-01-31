package types

import (
	"sort"
	"sync"
	"time"
)

// For common proto definitions, please refer to:
// https://github.com/ray-project/ray/blob/master/src/ray/protobuf/common.proto.

// Type of a task.
type TaskType string

const (
	NORMAL_TASK         TaskType = "NORMAL_TASK"
	ACTOR_CREATION_TASK TaskType = "ACTOR_CREATION_TASK"
	ACTOR_TASK          TaskType = "ACTOR_TASK"
	DRIVER_TASK         TaskType = "DRIVER_TASK"
)

// Language represents the language of a task or worker.
type Language string

const (
	PYTHON Language = "PYTHON"
	JAVA   Language = "JAVA"
	CPP    Language = "CPP"
)

// JavaFunctionDescriptor is a FunctionDescriptor for Java.
type JavaFunctionDescriptor struct {
	ClassName    string `json:"className"`
	FunctionName string `json:"functionName"`
	Signature    string `json:"signature"`
}

// CppFunctionDescriptor is a FunctionDescriptor for C/C++.
type CppFunctionDescriptor struct {
	FunctionName string `json:"functionName"`
	Caller       string `json:"caller"`
	ClassName    string `json:"className"`
}

// PythonFunctionDescriptor is a FunctionDescriptor for Python.
type PythonFunctionDescriptor struct {
	ModuleName   string `json:"moduleName"`
	ClassName    string `json:"className"`
	FunctionName string `json:"functionName"`
	FunctionHash string `json:"functionHash"`
}

// FunctionDescriptor is a union wrapper for various function descriptor types.
//
// TODO(jwj): Add validation for the "oneof" logic in eventserver.go if necessary.
type FunctionDescriptor struct {
	JavaFunctionDescriptor   *JavaFunctionDescriptor   `json:"javaFunctionDescriptor,omitempty"`
	PythonFunctionDescriptor *PythonFunctionDescriptor `json:"pythonFunctionDescriptor,omitempty"`
	CppFunctionDescriptor    *CppFunctionDescriptor    `json:"cppFunctionDescriptor,omitempty"`
}

// TaskStatus represents the current state of a task.
type TaskStatus string

// The following statuses follow a rough chronological order of transition.
// For typical order of states, please refer to:
// https://github.com/ray-project/ray/blob/d0b1d151d8ea964a711e451d0ae736f8bf95b629/src/ray/protobuf/common.proto#L884-L899.
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

// TaskStateTransition represents a change in a task's state at a specific timestamp.
type TaskStateTransition struct {
	State     TaskStatus `json:"state"`
	Timestamp time.Time  `json:"timestamp"`
}

// TODO(jwj): Add the task log info and ray error info.
type TaskLogInfo struct{}
type RayErrorInfo struct{}

// Task's fields are populated from the TASK_DEFINITION_EVENT, ACTOR_TASK_DEFINITION_EVENT, and TASK_LIFECYCLE_EVENT.
// A TASK_DEFINITION_EVENT or an ACTOR_TASK_DEFINITION_EVENT is expected to be emitted once per task attempt,
type Task struct {
	// TaskID and TaskAttempt form the unique identifier for a task.
	TaskID      string `json:"taskId"`
	TaskAttempt int    `json:"taskAttempt"`

	// The task definition information.
	// TaskType is the type of a task, only available for TASK_DEFINITION_EVENT.
	// For ACTOR_TASK_DEFINITION_EVENT, the TaskType is populated as ACTOR_TASK manually.
	// TODO(jwj): Can ACTOR_TASK be populated automatically?
	TaskType TaskType `json:"taskType,omitempty"`
	Language string   `json:"language,omitempty"`
	// For TASK_DEFINITION_EVENT, only TaskFunc and TaskName are populated.
	// For ACTOR_TASK_DEFINITION_EVENT, only ActorFunc and ActorTaskName are populated.
	// TODO(jwj): Need a validation function to make the "oneof" relationship more explicit.
	// It might be better to define separate structs or TaskDefinition interface with custom JSON marshal/unmarshal logic.
	TaskFunc          *FunctionDescriptor `json:"taskFunc,omitempty"`
	ActorFunc         *FunctionDescriptor `json:"actorFunc,omitempty"`
	TaskName          string              `json:"taskName,omitempty"`
	ActorTaskName     string              `json:"actorTaskName,omitempty"`
	RequiredResources map[string]float64  `json:"requiredResources,omitempty"`

	// The correlation ids of the task that can be used to correlate the task with other events.
	// ActorID is only available for ACTOR_TASK_DEFINITION_EVENT.
	ActorID              string            `json:"actorId,omitempty"`
	JobID                string            `json:"jobId"` // Present in both DEFINITION and LIFECYCLE events.
	ParentTaskID         string            `json:"parentTaskId,omitempty"`
	PlacementGroupID     string            `json:"placementGroupId,omitempty"`
	RefIDs               map[string]string `json:"refIds,omitempty"`
	SerializedRuntimeEnv string            `json:"serializedRuntimeEnv,omitempty"`
	// CallSite is the human readable stacktrace of the actor task invocation.
	CallSite string `json:"callSite,omitempty"`
	// LabelSelector is the key-value label constraints of the node to schedule this actor task on.
	LabelSelector map[string]string `json:"labelSelector,omitempty"`

	// The task execution information, populated from TASK_LIFECYCLE_EVENT.
	StateTransitions []TaskStateTransition `json:"stateTransitions,omitempty"`
	// RayErrorInfo     RayErrorInfo         `json:"rayErrorInfo,omitempty"`

	NodeID    string `json:"nodeId,omitempty"`
	WorkerID  string `json:"workerId,omitempty"`
	WorkerPID int    `json:"workerPid,omitempty"`
	// Whether the task is paused by the debugger.
	IsDebuggerPaused bool `json:"isDebuggerPaused,omitempty"`
	// Actor task repr name, if applicable.
	ActorReprName string `json:"actorReprName,omitempty"`

	// TaskLogInfo  TaskLogInfo `json:"taskLogInfo,omitempty"`

	State     TaskStatus
	StartTime time.Time
	EndTime   time.Time
}

// TaskMap is a struct that uses TaskID as the key and stores a list of Task attempts.
// Each TaskID maps to a slice of Tasks, where each element represents a different task attempt.
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

type ClusterTaskMap struct {
	// ClusterTaskMap is a map of cluster session ID to TaskMap.
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

// GetOrCreateTaskMap retrieves the TaskMap for the given cluster session, creating it if it doesn't exist.
func (c *ClusterTaskMap) GetOrCreateTaskMap(clusterSessionKey string) *TaskMap {
	c.Lock()
	defer c.Unlock()

	taskMap, exists := c.ClusterTaskMap[clusterSessionKey]
	if !exists {
		taskMap = NewTaskMap()
		c.ClusterTaskMap[clusterSessionKey] = taskMap
	}
	return taskMap
}

// CreateOrMergeTaskAttempt creates a new slice of Task attempts or insert the current attempt at the correct position for the given taskId.
// Uses binary search for O(log n) lookup. Maintains sorted order by AttemptNumber.
// This handles the case where LIFECYCLE events arrive before DEFINITION events.
//   - If the attempt doesn't exist, creates a new one at the correct position
//   - If the attempt exists, applies mergeFn to merge new data into existing
func (t *TaskMap) CreateOrMergeAttempt(taskId string, taskAttempt int, mergeFn func(*Task)) {
	t.Lock()
	defer t.Unlock()

	// Case 1: tasks doesn't exist.
	// Create a new slice of Task attempts with the current attempt.
	tasks, exists := t.TaskMap[taskId]
	if !exists {
		newTask := Task{TaskID: taskId, TaskAttempt: taskAttempt}
		mergeFn(&newTask)
		t.TaskMap[taskId] = []Task{newTask}
		return
	}

	// Case 2: tasks exists and the current attempt already exists.
	// Run binary search to find the first index where TaskAttempt >= taskAttempt.
	idx := sort.Search(len(tasks), func(i int) bool {
		return tasks[i].TaskAttempt >= taskAttempt
	})

	// Apply the merge function to the existing attempt.
	if idx < len(tasks) && tasks[idx].TaskAttempt == taskAttempt {
		mergeFn(&tasks[idx])
		return
	}

	// Case 3: tasks exists and the current attempt doesn't exist.
	// Create a new ActorTask attempt and apply the merge function.
	newTask := Task{TaskID: taskId, TaskAttempt: taskAttempt}
	mergeFn(&newTask)

	// Insert the current attempt at the correct position.
	tasks = append(tasks, Task{})    // Extend slice by 1
	copy(tasks[idx+1:], tasks[idx:]) // Shift elements right
	tasks[idx] = newTask             // Insert at correct position
	t.TaskMap[taskId] = tasks
}

// GetLastState returns the last state of the task.
func (t *Task) GetLastState() TaskStatus {
	if len(t.StateTransitions) == 0 {
		return NIL
	}
	return t.StateTransitions[len(t.StateTransitions)-1].State
}

func GetTaskFieldValue(task Task, filterKey string) string {
	switch filterKey {
	case "task_id":
		return task.TaskID
	case "parent_task_id":
		return task.ParentTaskID
	case "job_id":
		return task.JobID
	case "state":
		return string(task.State)
	// case "name", "task_name":
	// 	return task.Name
	// case "func_name", "function_name":
	// 	return task.FuncOrClassName
	case "node_id":
		return task.NodeID
	case "actor_id":
		return task.ActorID
	case "type", "task_type":
		return string(task.TaskType)
	case "worker_id":
		return task.WorkerID
	case "language":
		return task.Language
	// case "error_type":
	// 	return task.ErrorType
	default:
		return ""
	}
}

// DeepCopy returns a deep copy of the Task, including slices and maps.
// This prevents race conditions when the returned Task is used after locks are released.
func (t Task) DeepCopy() Task {
	cp := t
	// if len(t.Events) > 0 {
	// 	cp.Events = make([]StateEvent, len(t.Events))
	// 	copy(cp.Events, t.Events)
	// }
	// if len(t.RequiredResources) > 0 {
	// 	cp.RequiredResources = make(map[string]float64, len(t.RequiredResources))
	// 	for k, v := range t.RequiredResources {
	// 		cp.RequiredResources[k] = v
	// 	}
	// }
	// if len(t.TaskLogInfo) > 0 {
	// 	cp.TaskLogInfo = make(map[string]string, len(t.TaskLogInfo))
	// 	for k, v := range t.TaskLogInfo {
	// 		cp.TaskLogInfo[k] = v
	// 	}
	// }
	if len(t.LabelSelector) > 0 {
		cp.LabelSelector = make(map[string]string, len(t.LabelSelector))
		for k, v := range t.LabelSelector {
			cp.LabelSelector[k] = v
		}
	}
	return cp
}
