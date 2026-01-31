package types

import (
	"sort"
	"sync"
)

// For common proto definitions, please refer to:
// https://github.com/ray-project/ray/blob/master/src/ray/protobuf/common.proto.

// Language represents the language of a task or worker.
type Language int32

const (
	PYTHON Language = 0
	JAVA   Language = 1
	CPP    Language = 2
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
//	can validate "oneof" when handling  actor task events in eventserver.go.
type FunctionDescriptor struct {
	JavaFunctionDescriptor   *JavaFunctionDescriptor   `json:"javaFunctionDescriptor,omitempty"`
	PythonFunctionDescriptor *PythonFunctionDescriptor `json:"pythonFunctionDescriptor,omitempty"`
	CppFunctionDescriptor    *CppFunctionDescriptor    `json:"cppFunctionDescriptor,omitempty"`
}

// ActorTask is the definition of an actor task. The fields are populated from the ACTOR_TASK_DEFINITION_EVENT.
// An ACTOR_TASK_DEFINITION_EVENT is expected to be emitted once per task attempt.
type ActorTask struct {
	// TaskID and TaskAttempt form the unique identifier for an actor task.
	TaskID      string `json:"taskId"`
	TaskAttempt int    `json:"taskAttempt"`

	// The actor task definition information.
	Language          string             `json:"language"`
	ActorFunc         FunctionDescriptor `json:"actorFunc"`
	ActorTaskName     string             `json:"actorTaskName"`
	RequiredResources map[string]float64 `json:"requiredResources"`

	// The correlation ids of the task that can be used to correlate the task with other events.
	JobID                string            `json:"jobId"`
	ActorID              string            `json:"actorId"`
	ParentTaskID         string            `json:"parentTaskId"`
	PlacementGroupID     string            `json:"placementGroupId"`
	RefIDs               map[string]string `json:"refIds"`
	SerializedRuntimeEnv string            `json:"serializedRuntimeEnv"`

	// CallSite is the human readable stacktrace of the actor task invocation.
	CallSite string `json:"callSite,omitempty"`

	// LabelSelector is the key-value label constraints of the node to schedule this actor task on.
	// TODO(jwj): Determine whether to set omitempty.
	LabelSelector map[string]string `json:"labelSelector"`
}

// ActorTaskMap is a struct that uses TaskID as the key and stores a list of ActorTask attempts.
// Each TaskID maps to a slice of ActorTasks, where each element represents a different attempt.
type ActorTaskMap struct {
	ActorTaskMap map[string][]ActorTask
	Mu           sync.Mutex
}

func (a *ActorTaskMap) Lock() {
	a.Mu.Lock()
}

func (a *ActorTaskMap) Unlock() {
	a.Mu.Unlock()
}

func NewActorTaskMap() *ActorTaskMap {
	return &ActorTaskMap{
		ActorTaskMap: make(map[string][]ActorTask),
	}
}

type ClusterActorTaskMap struct {
	// ClusterActorTaskMap is a map of cluster session ID to ActorTaskMap.
	ClusterActorTaskMap map[string]*ActorTaskMap
	Mu                  sync.RWMutex
}

func (c *ClusterActorTaskMap) RLock() {
	c.Mu.RLock()
}

func (c *ClusterActorTaskMap) RUnlock() {
	c.Mu.RUnlock()
}

func (c *ClusterActorTaskMap) Lock() {
	c.Mu.Lock()
}

func (c *ClusterActorTaskMap) Unlock() {
	c.Mu.Unlock()
}

// GetOrCreateActorTaskMap retrieves the ActorTaskMap for the given cluster session, creating it if it doesn't exist.
func (c *ClusterActorTaskMap) GetOrCreateActorTaskMap(clusterSessionID string) *ActorTaskMap {
	c.Lock()
	defer c.Unlock()

	actorTaskMap, exists := c.ClusterActorTaskMap[clusterSessionID]
	if !exists {
		actorTaskMap = NewActorTaskMap()
		c.ClusterActorTaskMap[clusterSessionID] = actorTaskMap
	}
	return actorTaskMap
}

// CreateOrMergeActorTask creates a new slice of ActorTask attempts or insert the current attempt at the correct position for the given taskId.
// For now, only ACTOR_TASK_DEFINITION_EVENT is handled, so the merge function is a dummy function.
func (a *ActorTaskMap) CreateOrMergeActorTask(taskId string, taskAttempt int, mergeFn func(*ActorTask)) {
	a.Lock()
	defer a.Unlock()

	// Case 1: actorTasks doesn't exist.
	// Create a new slice of ActorTask attempts with the current attempt.
	actorTasks, exists := a.ActorTaskMap[taskId]
	if !exists {
		newActorTask := ActorTask{TaskID: taskId, TaskAttempt: taskAttempt}
		mergeFn(&newActorTask) // For now, this is a dummy function.
		a.ActorTaskMap[taskId] = []ActorTask{newActorTask}
		return
	}

	// Case 2: actorTasks exists and the current attempt already exists.
	// Run binary search to find the first index where TaskAttempt >= taskAttempt.
	idx := sort.Search(len(actorTasks), func(i int) bool {
		return actorTasks[i].TaskAttempt >= taskAttempt
	})

	// Apply the merge function to the existing attempt.
	if idx < len(actorTasks) && actorTasks[idx].TaskAttempt == taskAttempt {
		mergeFn(&actorTasks[idx]) // For now, this is a dummy function.
		return
	}

	// Case 3: actorTasks exists and the current attempt doesn't exist.
	// Create a new ActorTask attempt and apply the merge function.
	newActorTask := ActorTask{TaskID: taskId, TaskAttempt: taskAttempt}
	mergeFn(&newActorTask)

	// Insert the current attempt at the correct position.
	actorTasks = append(actorTasks, ActorTask{})
	copy(actorTasks[idx+1:], actorTasks[idx:])
	actorTasks[idx] = newActorTask
	a.ActorTaskMap[taskId] = actorTasks
}
