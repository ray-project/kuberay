package eventserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
)

func makeTaskEventMap(taskName, nodeId, taskID, cluster string, attempt int) map[string]any {
	return map[string]any{
		"eventType":   string(types.TASK_DEFINITION_EVENT),
		"clusterName": cluster,
		"taskDefinitionEvent": map[string]any{
			"taskId":      taskID,
			"taskName":    taskName,
			"nodeId":      nodeId,
			"taskAttempt": attempt,
		},
	}
}

func TestEventProcessor(t *testing.T) {
	tests := []struct {
		name string
		// Setup
		eventsToSend []map[string]any
		cancelAfter  time.Duration // Time after which to cancel context (0 for no cancel)
		closeChan    bool          // Whether to close the channel after sending events

		// Expectations
		wantErr          bool
		expectedErrType  error // Specific error type to check (e.g., context.Canceled)
		wantStoredEvents map[string][]types.Task
	}{
		{
			name: "process multiple events then close channel",
			eventsToSend: []map[string]any{
				{
					"clusterName": "cluster1",
					"eventType":   "TASK_DEFINITION_EVENT",
					"taskDefinitionEvent": map[string]any{
						"taskId":      "ID_12345",
						"taskName":    "Name_12345",
						"nodeId":      "Nodeid_12345",
						"taskAttempt": 2,
					},
				},
				{
					"clusterName": "cluster1",
					"eventType":   "TASK_DEFINITION_EVENT",
					"taskDefinitionEvent": map[string]any{
						"taskId":      "ID_54321",
						"taskName":    "Name_54321",
						"nodeId":      "Nodeid_54321",
						"taskAttempt": 1,
					},
				},
			},
			closeChan: true,
			wantStoredEvents: map[string][]types.Task{
				"ID_12345": {
					{
						TaskID:        "ID_12345",
						Name:          "Name_12345",
						NodeID:        "Nodeid_12345",
						AttemptNumber: 2,
					},
				},
				"ID_54321": {
					{
						TaskID:        "ID_54321",
						Name:          "Name_54321",
						NodeID:        "Nodeid_54321",
						AttemptNumber: 1,
					},
				},
			},
		},
		{
			name:      "channel closed immediately",
			closeChan: true,
			wantErr:   false,
		},
		{
			name: "context canceled",
			eventsToSend: []map[string]any{
				{
					"clusterName": "cluster1",
					"eventType":   "TASK_DEFINITION_EVENT",
					"taskDefinitionEvent": map[string]any{
						"taskId":      "ID_12345",
						"taskName":    "Name_12345",
						"nodeId":      "Nodeid_12345",
						"taskAttempt": 2,
					},
				},
			},
			cancelAfter:     50 * time.Millisecond,
			wantErr:         true,
			expectedErrType: context.Canceled,
			// Event might be processed before cancellation is detected
			wantStoredEvents: map[string][]types.Task{
				"ID_12345": {
					{
						TaskID:        "ID_12345",
						Name:          "Name_12345",
						NodeID:        "Nodeid_12345",
						AttemptNumber: 2,
					},
				},
			},
		},
		{
			name:            "no events, context canceled",
			cancelAfter:     10 * time.Millisecond,
			wantErr:         true,
			expectedErrType: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Sending nil for reader since it won't be used anyways
			h := NewEventHandler(nil)

			// Channel buffer size a bit larger than events to avoid blocking sender in test setup
			ch := make(chan map[string]any, len(tt.eventsToSend)+2)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Send events into the channel
			go func() {
				for _, event := range tt.eventsToSend {
					select {
					case ch <- event:
					case <-ctx.Done(): // Stop sending if context is cancelled
						return
					}
				}
				if tt.closeChan {
					close(ch)
				}
			}()

			// Handle context cancellation if specified
			if tt.cancelAfter > 0 {
				go func() {
					time.Sleep(tt.cancelAfter)
					cancel()
				}()
			}

			// Run the ProcessEvent
			err := h.ProcessEvents(ctx, ch)

			// Check error expectations
			if (err != nil) != tt.wantErr {
				t.Errorf("ProcessEvents() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.expectedErrType != nil {
				if !errors.Is(err, tt.expectedErrType) {
					t.Errorf("ProcessEvents() error type = %T, want type %T (err: %v)", err, tt.expectedErrType, err)
				}
			}

			// Check stored events
			if tt.wantStoredEvents != nil {
				if diff := cmp.Diff(tt.wantStoredEvents, h.ClusterTaskMap.ClusterTaskMap["cluster1"].TaskMap); diff != "" {
					t.Errorf("storeEventCalls diff (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestStoreEvent(t *testing.T) {
	initialTask := types.Task{
		TaskID:        "taskid1",
		Name:          "taskName123",
		NodeID:        "nodeid123",
		AttemptNumber: 0,
	}
	tests := []struct {
		name              string
		initialState      *types.ClusterTaskMap
		eventMap          map[string]any
		wantErr           bool
		wantClusterCount  int
		wantTaskInCluster string       // Cluster to check for the task
		wantTaskID        string       // TaskID to check
		wantTasks         []types.Task // Expected tasks (all attempts), nil if not applicable
	}{
		{
			name: "unsupported event type",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: make(map[string]*types.TaskMap),
			},
			eventMap: map[string]any{
				"eventType":   "UNKNOWN_TYPE",
				"clusterName": "c1",
			},
			wantErr:          false,
			wantClusterCount: 0,
		},
		{
			name: "task event - new cluster and new task",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: make(map[string]*types.TaskMap),
			},
			eventMap:          makeTaskEventMap("taskName123", "nodeid1234", "taskid1", "cluster1", 0),
			wantErr:           false,
			wantClusterCount:  1,
			wantTaskInCluster: "cluster1",
			wantTaskID:        "taskid1",
			wantTasks: []types.Task{
				{
					TaskID:        "taskid1",
					Name:          "taskName123",
					NodeID:        "nodeid1234",
					AttemptNumber: 0,
				},
			},
		},
		{
			name: "task event - existing cluster, new task",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: map[string]*types.TaskMap{
					"cluster1": types.NewTaskMap(),
				},
			},
			eventMap:          makeTaskEventMap("taskName123", "nodeid1234", "taskid2", "cluster1", 1),
			wantErr:           false,
			wantClusterCount:  1,
			wantTaskInCluster: "cluster1",
			wantTaskID:        "taskid2",
			wantTasks: []types.Task{
				{
					TaskID:        "taskid2",
					Name:          "taskName123",
					NodeID:        "nodeid1234",
					AttemptNumber: 1,
				},
			},
		},
		{
			name: "task event - existing cluster and existing task with new attempt",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: map[string]*types.TaskMap{
					"cluster1": {
						TaskMap: map[string][]types.Task{
							"taskid1": {initialTask},
						},
					},
				},
			},
			eventMap:          makeTaskEventMap("taskName123", "nodeid123", "taskid1", "cluster1", 2),
			wantErr:           false,
			wantClusterCount:  1,
			wantTaskInCluster: "cluster1",
			wantTaskID:        "taskid1",
			// Now expects BOTH attempts to be stored
			wantTasks: []types.Task{
				{
					TaskID:        "taskid1",
					Name:          "taskName123",
					NodeID:        "nodeid123",
					AttemptNumber: 0,
				},
				{
					TaskID:        "taskid1",
					Name:          "taskName123",
					NodeID:        "nodeid123",
					AttemptNumber: 2,
				},
			},
		},
		{
			name: "task event - missing taskDefinitionEvent",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: make(map[string]*types.TaskMap),
			},
			eventMap: map[string]any{
				"eventType":   string(types.TASK_DEFINITION_EVENT),
				"clusterName": "c1",
			},
			wantErr: true,
		},
		{
			name: "task event - taskDefinitionEvent wrong type",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: make(map[string]*types.TaskMap),
			},
			eventMap: map[string]any{
				"eventType":           string(types.TASK_DEFINITION_EVENT),
				"clusterName":         "c1",
				"taskDefinitionEvent": "not a map",
			},
			wantErr: true, // Marshal will fail
		},
		{
			name: "task event - invalid task structure",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: make(map[string]*types.TaskMap),
			},
			eventMap: map[string]any{
				"eventType":   string(types.TASK_DEFINITION_EVENT),
				"clusterName": "c1",
				"taskDefinitionEvent": map[string]any{
					"taskId":      123, // Should be string
					"taskAttempt": 0,
				},
			},
			wantErr: true, // Unmarshal will fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &EventHandler{
				ClusterTaskMap: tt.initialState,
			}
			if h.ClusterTaskMap == nil {
				h.ClusterTaskMap = &types.ClusterTaskMap{
					ClusterTaskMap: make(map[string]*types.TaskMap),
				}
			}

			err := h.storeEvent(tt.eventMap)

			if (err != nil) != tt.wantErr {
				t.Fatalf("storeEvent() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			gotClusterCount := len(h.ClusterTaskMap.ClusterTaskMap)

			if gotClusterCount != tt.wantClusterCount {
				t.Errorf("storeEvent() resulted in %d clusters, want %d", gotClusterCount, tt.wantClusterCount)
			}

			if tt.wantTasks != nil {
				clusterObj, clusterExists := h.ClusterTaskMap.ClusterTaskMap[tt.wantTaskInCluster]

				if !clusterExists {
					t.Fatalf("storeEvent() cluster %s not found", tt.wantTaskInCluster)
				}

				clusterObj.Lock()
				defer clusterObj.Unlock()
				gotTasks, taskExists := clusterObj.TaskMap[tt.wantTaskID]
				if !taskExists {
					t.Fatalf("storeEvent() task %s not found in cluster %s", tt.wantTaskID, tt.wantTaskInCluster)
				}

				if diff := cmp.Diff(tt.wantTasks, gotTasks); diff != "" {
					t.Errorf("storeEvent() tasks mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

// TestTaskLifecycleEventDeduplication verifies that duplicate events are correctly filtered
// and out-of-order events are properly sorted
func TestTaskLifecycleEventDeduplication(t *testing.T) {
	// Helper to create a StateEvent
	makeStateEvent := func(state types.TaskStatus, timestampNano int64) types.StateEvent {
		return types.StateEvent{
			State:     state,
			Timestamp: time.Unix(0, timestampNano),
		}
	}

	// Helper to create a TASK_LIFECYCLE_EVENT map
	makeLifecycleEvent := func(taskID string, attempt int, transitions []map[string]any) map[string]any {
		// Convert []map[string]any to []any for proper type assertion in storeEvent
		transitionsAny := make([]any, len(transitions))
		for i, t := range transitions {
			transitionsAny[i] = t
		}
		return map[string]any{
			"eventType":   string(types.TASK_LIFECYCLE_EVENT),
			"clusterName": "test-cluster",
			"taskLifecycleEvent": map[string]any{
				"taskId":           taskID,
				"taskAttempt":      float64(attempt),
				"stateTransitions": transitionsAny,
				"nodeId":           "node-1",
				"workerId":         "worker-1",
			},
		}
	}

	// Helper to create state transition
	makeTransition := func(state string, timestampNano int64) map[string]any {
		return map[string]any{
			"state":     state,
			"timestamp": time.Unix(0, timestampNano).Format(time.RFC3339Nano),
		}
	}

	tests := []struct {
		name           string
		existingEvents []types.StateEvent // Events already in the task
		newTransitions []map[string]any   // New transitions to process
		wantEvents     []types.StateEvent // Expected final events (sorted by timestamp)
		wantState      types.TaskStatus   // Expected final state
	}{
		{
			name: "Scenario 1: Normal deduplication - same events processed twice",
			existingEvents: []types.StateEvent{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000),
				makeStateEvent(types.RUNNING, 2000),
			},
			newTransitions: []map[string]any{
				makeTransition("PENDING_NODE_ASSIGNMENT", 1000), // Duplicate
				makeTransition("RUNNING", 2000),                 // Duplicate
			},
			wantEvents: []types.StateEvent{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000),
				makeStateEvent(types.RUNNING, 2000),
			},
			wantState: types.RUNNING,
		},
		{
			name: "Scenario 2: Out-of-order events - B(t=2) arrives after A(t=1), C(t=3)",
			existingEvents: []types.StateEvent{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000), // A
				makeStateEvent(types.FINISHED, 3000),                // C
			},
			newTransitions: []map[string]any{
				makeTransition("RUNNING", 2000), // B - should be inserted in the middle
			},
			wantEvents: []types.StateEvent{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000), // A
				makeStateEvent(types.RUNNING, 2000),                 // B - now in correct position
				makeStateEvent(types.FINISHED, 3000),                // C
			},
			wantState: types.FINISHED,
		},
		{
			name: "Scenario 3: Same timestamp, different states - both should be kept",
			existingEvents: []types.StateEvent{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000),
			},
			newTransitions: []map[string]any{
				makeTransition("RUNNING", 1000), // Same timestamp, different state
			},
			wantEvents: []types.StateEvent{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000),
				makeStateEvent(types.RUNNING, 1000),
			},
			wantState: types.RUNNING, // Last after sort (order of same timestamp is stable)
		},
		{
			name: "Scenario 4: Exact duplicate event - only one should remain",
			existingEvents: []types.StateEvent{
				makeStateEvent(types.RUNNING, 1000),
			},
			newTransitions: []map[string]any{
				makeTransition("RUNNING", 1000), // Exact duplicate
				makeTransition("RUNNING", 1000), // Another duplicate
			},
			wantEvents: []types.StateEvent{
				makeStateEvent(types.RUNNING, 1000), // Only one
			},
			wantState: types.RUNNING,
		},
		{
			name: "Scenario 5: Partial overlap - existing [A,B], new [B,C] -> result [A,B,C]",
			existingEvents: []types.StateEvent{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000), // A
				makeStateEvent(types.RUNNING, 2000),                 // B
			},
			newTransitions: []map[string]any{
				makeTransition("RUNNING", 2000),  // B - duplicate
				makeTransition("FINISHED", 3000), // C - new
			},
			wantEvents: []types.StateEvent{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000), // A
				makeStateEvent(types.RUNNING, 2000),                 // B
				makeStateEvent(types.FINISHED, 3000),                // C
			},
			wantState: types.FINISHED,
		},
		{
			name:           "Scenario 6: Empty initial events - add new events",
			existingEvents: []types.StateEvent{},
			newTransitions: []map[string]any{
				makeTransition("PENDING_NODE_ASSIGNMENT", 1000),
				makeTransition("RUNNING", 2000),
			},
			wantEvents: []types.StateEvent{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000),
				makeStateEvent(types.RUNNING, 2000),
			},
			wantState: types.RUNNING,
		},
		{
			name: "Scenario 7: Multiple reprocessing cycles - events should not grow",
			existingEvents: []types.StateEvent{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000),
				makeStateEvent(types.RUNNING, 2000),
				makeStateEvent(types.FINISHED, 3000),
			},
			newTransitions: []map[string]any{
				// Simulating reprocess of same file
				makeTransition("PENDING_NODE_ASSIGNMENT", 1000),
				makeTransition("RUNNING", 2000),
				makeTransition("FINISHED", 3000),
			},
			wantEvents: []types.StateEvent{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000),
				makeStateEvent(types.RUNNING, 2000),
				makeStateEvent(types.FINISHED, 3000),
			},
			wantState: types.FINISHED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewEventHandler(nil)

			// Pre-populate existing events if any
			if len(tt.existingEvents) > 0 {
				taskMap := h.ClusterTaskMap.GetOrCreateTaskMap("test-cluster")
				taskMap.CreateOrMergeAttempt("task-1", 0, func(task *types.Task) {
					task.TaskID = "task-1"
					task.Events = tt.existingEvents
					if len(tt.existingEvents) > 0 {
						task.State = tt.existingEvents[len(tt.existingEvents)-1].State
					}
				})
			}

			// Process the lifecycle event
			eventMap := makeLifecycleEvent("task-1", 0, tt.newTransitions)
			err := h.storeEvent(eventMap)
			if err != nil {
				t.Fatalf("storeEvent() unexpected error: %v", err)
			}

			// Get the task and verify
			taskMap := h.ClusterTaskMap.GetOrCreateTaskMap("test-cluster")
			taskMap.Lock()
			defer taskMap.Unlock()

			tasks, exists := taskMap.TaskMap["task-1"]
			if !exists || len(tasks) == 0 {
				t.Fatal("Task not found after processing")
			}

			task := tasks[0]

			// Verify events
			if diff := cmp.Diff(tt.wantEvents, task.Events); diff != "" {
				t.Errorf("Events mismatch (-want +got):\n%s", diff)
			}

			// Verify final state
			if task.State != tt.wantState {
				t.Errorf("State = %v, want %v", task.State, tt.wantState)
			}

			// Verify event count (important for deduplication)
			if len(task.Events) != len(tt.wantEvents) {
				t.Errorf("Event count = %d, want %d", len(task.Events), len(tt.wantEvents))
			}
		})
	}
}

// TestActorLifecycleEventDeduplication verifies that duplicate actor events are correctly filtered
func TestActorLifecycleEventDeduplication(t *testing.T) {
	// Helper to create an ActorStateEvent
	makeActorStateEvent := func(state types.StateType, timestampNano int64) types.ActorStateEvent {
		return types.ActorStateEvent{
			State:     state,
			Timestamp: time.Unix(0, timestampNano),
		}
	}

	// Helper to create an ACTOR_LIFECYCLE_EVENT map
	makeActorLifecycleEvent := func(actorID string, transitions []map[string]any) map[string]any {
		// Convert []map[string]any to []any for proper type assertion in storeEvent
		transitionsAny := make([]any, len(transitions))
		for i, t := range transitions {
			transitionsAny[i] = t
		}
		return map[string]any{
			"eventType":   string(types.ACTOR_LIFECYCLE_EVENT),
			"clusterName": "test-cluster",
			"actorLifecycleEvent": map[string]any{
				"actorId":          actorID,
				"stateTransitions": transitionsAny,
			},
		}
	}

	// Helper to create state transition
	makeTransition := func(state string, timestampNano int64) map[string]any {
		return map[string]any{
			"state":     state,
			"timestamp": time.Unix(0, timestampNano).Format(time.RFC3339Nano),
		}
	}

	tests := []struct {
		name           string
		existingEvents []types.ActorStateEvent
		newTransitions []map[string]any
		wantEvents     []types.ActorStateEvent
		wantState      types.StateType
	}{
		{
			name: "Actor: Normal deduplication",
			existingEvents: []types.ActorStateEvent{
				makeActorStateEvent(types.PENDING_CREATION, 1000),
				makeActorStateEvent(types.ALIVE, 2000),
			},
			newTransitions: []map[string]any{
				makeTransition("PENDING_CREATION", 1000),
				makeTransition("ALIVE", 2000),
			},
			wantEvents: []types.ActorStateEvent{
				makeActorStateEvent(types.PENDING_CREATION, 1000),
				makeActorStateEvent(types.ALIVE, 2000),
			},
			wantState: types.ALIVE,
		},
		{
			name: "Actor: Out-of-order with sort",
			existingEvents: []types.ActorStateEvent{
				makeActorStateEvent(types.PENDING_CREATION, 1000),
				makeActorStateEvent(types.DEAD, 3000),
			},
			newTransitions: []map[string]any{
				makeTransition("ALIVE", 2000), // Should be inserted between
			},
			wantEvents: []types.ActorStateEvent{
				makeActorStateEvent(types.PENDING_CREATION, 1000),
				makeActorStateEvent(types.ALIVE, 2000),
				makeActorStateEvent(types.DEAD, 3000),
			},
			wantState: types.DEAD,
		},
		{
			name: "Actor: Exact duplicate should not increase count",
			existingEvents: []types.ActorStateEvent{
				makeActorStateEvent(types.ALIVE, 1000),
			},
			newTransitions: []map[string]any{
				makeTransition("ALIVE", 1000),
				makeTransition("ALIVE", 1000),
				makeTransition("ALIVE", 1000),
			},
			wantEvents: []types.ActorStateEvent{
				makeActorStateEvent(types.ALIVE, 1000),
			},
			wantState: types.ALIVE,
		},
		{
			name: "Actor: Same timestamp different states should both be kept",
			existingEvents: []types.ActorStateEvent{
				makeActorStateEvent(types.PENDING_CREATION, 1000),
			},
			newTransitions: []map[string]any{
				makeTransition("ALIVE", 1000), // Same timestamp, different state
			},
			wantEvents: []types.ActorStateEvent{
				makeActorStateEvent(types.PENDING_CREATION, 1000),
				makeActorStateEvent(types.ALIVE, 1000),
			},
			wantState: types.ALIVE,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewEventHandler(nil)

			// Pre-populate existing events
			if len(tt.existingEvents) > 0 {
				actorMap := h.ClusterActorMap.GetOrCreateActorMap("test-cluster")
				actorMap.CreateOrMergeActor("actor-1", func(a *types.Actor) {
					a.ActorID = "actor-1"
					a.Events = tt.existingEvents
					if len(tt.existingEvents) > 0 {
						a.State = tt.existingEvents[len(tt.existingEvents)-1].State
					}
				})
			}

			// Process the lifecycle event
			eventMap := makeActorLifecycleEvent("actor-1", tt.newTransitions)
			err := h.storeEvent(eventMap)
			if err != nil {
				t.Fatalf("storeEvent() unexpected error: %v", err)
			}

			// Get the actor and verify
			actor, found := h.GetActorByID("test-cluster", "actor-1")
			if !found {
				t.Fatal("Actor not found after processing")
			}

			// Verify events count (most important for dedup testing)
			if len(actor.Events) != len(tt.wantEvents) {
				t.Errorf("Event count = %d, want %d", len(actor.Events), len(tt.wantEvents))
			}

			// Verify final state
			if actor.State != tt.wantState {
				t.Errorf("State = %v, want %v", actor.State, tt.wantState)
			}
		})
	}
}

// TestDriverJobLifeCycleEventDuplication tests that duplicate events are properly filtered and sorted
// TODO(chiayi): Update once more fields are added to driver job event
func TestDriverJobLifecycleEventDuplication(t *testing.T) {
	makeDriverJobStateTransitionEvent := func(state types.JobState, timestampNano int64) types.JobStateTransition {
		return types.JobStateTransition{
			State:     state,
			Timestamp: time.Unix(0, timestampNano),
		}
	}

	makeDriverJobLifecycleEvent := func(jobID string, transitions []map[string]any) map[string]any {
		transitionsAny := make([]any, len(transitions))
		for i, t := range transitions {
			transitionsAny[i] = t
		}
		return map[string]any{
			"eventType":   string(types.DRIVER_JOB_LIFECYCLE_EVENT),
			"clusterName": "test-cluster",
			"driverJobLifecycleEvent": map[string]any{
				"jobId":            jobID,
				"stateTransitions": transitionsAny,
			},
		}
	}

	makeTransition := func(state string, timestampNano int64) map[string]any {
		return map[string]any{
			"state":     state,
			"timestamp": time.Unix(0, timestampNano).Format(time.RFC3339Nano),
		}
	}

	tests := []struct {
		name string
		// Available Job States are UNSPECIFIED, CREATED, JOBFINISHED
		existingTransitions []types.JobStateTransition
		newTransitions      []map[string]any
		wantTransitions     []types.JobStateTransition
		wantState           types.JobState
	}{
		{
			name: "Driver Job: Same event is processed twice",
			existingTransitions: []types.JobStateTransition{
				makeDriverJobStateTransitionEvent(types.UNSPECIFIED, 1000),
				makeDriverJobStateTransitionEvent(types.CREATED, 2000),
			},
			newTransitions: []map[string]any{
				makeTransition("UNSPECIFIED", 1000),
				makeTransition("CREATED", 2000),
			},
			wantTransitions: []types.JobStateTransition{
				makeDriverJobStateTransitionEvent(types.UNSPECIFIED, 1000),
				makeDriverJobStateTransitionEvent(types.CREATED, 2000),
			},
			wantState: types.CREATED,
		},
		{
			name:                "Driver Job: Empty existing transitions",
			existingTransitions: []types.JobStateTransition{},
			newTransitions: []map[string]any{
				makeTransition("UNSPECIFIED", 1000),
				makeTransition("CREATED", 2000),
				makeTransition("FINISHED", 3000),
			},
			wantTransitions: []types.JobStateTransition{
				makeDriverJobStateTransitionEvent(types.UNSPECIFIED, 1000),
				makeDriverJobStateTransitionEvent(types.CREATED, 2000),
				makeDriverJobStateTransitionEvent(types.JOBFINISHED, 3000),
			},
			wantState: types.JOBFINISHED,
		},
		{
			name: "Driver Job: Out of order transition event",
			existingTransitions: []types.JobStateTransition{
				makeDriverJobStateTransitionEvent(types.UNSPECIFIED, 1000),
				makeDriverJobStateTransitionEvent(types.JOBFINISHED, 3000),
			},
			newTransitions: []map[string]any{
				makeTransition("CREATED", 2000),
			},
			wantTransitions: []types.JobStateTransition{
				makeDriverJobStateTransitionEvent(types.UNSPECIFIED, 1000),
				makeDriverJobStateTransitionEvent(types.CREATED, 2000),
				makeDriverJobStateTransitionEvent(types.JOBFINISHED, 3000),
			},
			wantState: types.JOBFINISHED,
		},
		{
			name: "Driver Job: Multiple duplicate of same transition event",
			existingTransitions: []types.JobStateTransition{
				makeDriverJobStateTransitionEvent(types.CREATED, 2000),
			},
			newTransitions: []map[string]any{
				makeTransition("CREATED", 2000),
				makeTransition("CREATED", 2000),
				makeTransition("CREATED", 2000),
				makeTransition("CREATED", 2000),
			},
			wantTransitions: []types.JobStateTransition{
				makeDriverJobStateTransitionEvent(types.CREATED, 2000),
			},
			wantState: types.CREATED,
		},
		{
			name: "Driver Job: Different transition events with same time stamp, all be kept",
			existingTransitions: []types.JobStateTransition{
				makeDriverJobStateTransitionEvent(types.UNSPECIFIED, 1000),
			},
			newTransitions: []map[string]any{
				makeTransition("CREATED", 1000),
				makeTransition("CREATED", 2000),
				makeTransition("FINISHED", 2000),
			},
			wantTransitions: []types.JobStateTransition{
				makeDriverJobStateTransitionEvent(types.UNSPECIFIED, 1000),
				makeDriverJobStateTransitionEvent(types.CREATED, 1000),
				makeDriverJobStateTransitionEvent(types.CREATED, 2000),
				makeDriverJobStateTransitionEvent(types.JOBFINISHED, 2000),
			},
			wantState: types.JOBFINISHED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewEventHandler(nil)

			if len(tt.existingTransitions) > 0 {
				jobMap := h.ClusterJobMap.GetOrCreateJobMap("test-cluster")
				jobMap.CreateOrMergeJob("job-1", func(job *types.Job) {
					job.JobID = "job-1"
					job.StateTransitions = tt.existingTransitions
					if len(tt.existingTransitions) > 0 {
						job.State = tt.existingTransitions[len(tt.existingTransitions)-1].State
					}
				})
			}

			eventMap := makeDriverJobLifecycleEvent("job-1", tt.newTransitions)
			err := h.storeEvent(eventMap)
			if err != nil {
				t.Fatalf("storeEvent() unexpected error: %v", err)
			}

			job, exists := h.GetJobByJobID("test-cluster", "job-1")
			if !exists {
				t.Fatal("Job not found after processing")
			}

			if len(job.StateTransitions) != len(tt.wantTransitions) {
				t.Errorf("Actual transition count %d but expected %d", len(job.StateTransitions), len(tt.wantTransitions))
			}

			if job.State != tt.wantState {
				t.Errorf("Actual State %v but expected %v", job.State, tt.wantState)
			}
		})
	}
}

// TestMultipleReprocessingCycles simulates hourly reprocessing and verifies no memory growth
func TestMultipleReprocessingCycles(t *testing.T) {
	h := NewEventHandler(nil)

	// The same events that would be in an event file
	// Use []any to match what storeEvent expects from JSON parsing
	transitions := []any{
		map[string]any{"state": "PENDING_NODE_ASSIGNMENT", "timestamp": time.Unix(0, 1000).Format(time.RFC3339Nano)},
		map[string]any{"state": "RUNNING", "timestamp": time.Unix(0, 2000).Format(time.RFC3339Nano)},
		map[string]any{"state": "FINISHED", "timestamp": time.Unix(0, 3000).Format(time.RFC3339Nano)},
	}

	eventMap := map[string]any{
		"eventType":   string(types.TASK_LIFECYCLE_EVENT),
		"clusterName": "test-cluster",
		"taskLifecycleEvent": map[string]any{
			"taskId":           "task-1",
			"taskAttempt":      float64(0),
			"stateTransitions": transitions,
			"nodeId":           "node-1",
			"workerId":         "worker-1",
		},
	}

	// Simulate 10 hourly reprocessing cycles
	for cycle := 0; cycle < 10; cycle++ {
		err := h.storeEvent(eventMap)
		if err != nil {
			t.Fatalf("Cycle %d: storeEvent() error = %v", cycle, err)
		}

		// Check event count after each cycle
		taskMap := h.ClusterTaskMap.GetOrCreateTaskMap("test-cluster")
		taskMap.Lock()
		tasks := taskMap.TaskMap["task-1"]
		eventCount := len(tasks[0].Events)
		taskMap.Unlock()

		// Should always be exactly 3 events, never growing
		if eventCount != 3 {
			t.Errorf("Cycle %d: Event count = %d, want 3 (events are duplicating!)", cycle, eventCount)
		}
	}
}

func TestTransformToEventTimestamp(t *testing.T) {
	// Test that transformToEvent correctly converts timestamp format
	eventMap := map[string]any{
		"eventId":    "test-event-id",
		"eventType":  "TASK_DEFINITION_EVENT",
		"sourceType": "CORE_WORKER",
		"timestamp":  "2026-01-16T19:22:49.414579427Z",
		"severity":   "INFO",
		"taskDefinitionEvent": map[string]any{
			"taskId": "task-123",
			"jobId":  "AQAAAA==",
		},
	}

	event := transformToEvent(eventMap)

	// Verify timestamp is converted to Unix milliseconds
	expectedTimestamp := "1768591369414"
	if event.Timestamp != expectedTimestamp {
		t.Errorf("transformToEvent timestamp = %q, want %q", event.Timestamp, expectedTimestamp)
	}

	// Verify other fields are preserved
	if event.EventID != "test-event-id" {
		t.Errorf("EventID = %q, want %q", event.EventID, "test-event-id")
	}
	if event.SourceType != "CORE_WORKER" {
		t.Errorf("SourceType = %q, want %q", event.SourceType, "CORE_WORKER")
	}
}
