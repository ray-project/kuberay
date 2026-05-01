package eventserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

func makeTaskEventMap(taskName, nodeID, taskID, cluster string, attempt int) map[string]any {
	return map[string]any{
		"eventType":   string(types.TASK_DEFINITION_EVENT),
		"clusterName": cluster,
		"taskDefinitionEvent": map[string]any{
			"taskId":      taskID,
			"taskName":    taskName,
			"nodeId":      nodeID,
			"taskAttempt": attempt,
		},
	}
}

func TestEventProcessor(t *testing.T) {
	// IDs follow Ray's ID specification:
	// ref: https://github.com/ray-project/ray/blob/f229d5376eb87b09a3fa0b991323450de84df890/src/ray/design_docs/id_specification.md
	// Sizes: JobID=4B, ActorID=12B unique+JobID (16B), TaskID=8B unique+ActorID (24B), NodeID/WorkerID=28B
	const (
		// Pure-hex IDs to verify normalizeIDToHex is identity.
		// Use lowercase to match production hex output and avoid map-key collision.
		testJobID   = "aaaabbbb"                                                 // 4B
		testActorID = "aaaabbbb1234aaaabbbb1234" + testJobID                     // 12B unique + JobID
		testTaskID1 = "ccccdddd5678cccc" + testActorID                           // 8B unique + ActorID
		testTaskID2 = "ccccdddd9012dddd" + testActorID                           // 8B unique + ActorID
		testNodeID1 = "eeeeffff1234eeeeffff1234eeeeffff1234eeeeffff1234eeeeffff" // 28B
		testNodeID2 = "eeeeffff9012eeeeffff9012eeeeffff9012eeeeffff9012eeeeffff" // 28B

		testClusterName = "cluster1"
		testTaskName1   = "Name_12345"
		testTaskName2   = "Name_54321"
	)
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
					"clusterName": testClusterName,
					"eventType":   "TASK_DEFINITION_EVENT",
					"taskDefinitionEvent": map[string]any{
						"taskId":      testTaskID1,
						"taskName":    testTaskName1,
						"nodeId":      testNodeID1,
						"taskAttempt": 2,
					},
				},
				{
					"clusterName": testClusterName,
					"eventType":   "TASK_DEFINITION_EVENT",
					"taskDefinitionEvent": map[string]any{
						"taskId":      testTaskID2,
						"taskName":    testTaskName2,
						"nodeId":      testNodeID2,
						"taskAttempt": 1,
					},
				},
			},
			closeChan: true,
			wantStoredEvents: map[string][]types.Task{
				testTaskID1: {
					{
						TaskID:      testTaskID1,
						TaskName:    testTaskName1,
						NodeID:      testNodeID1,
						TaskAttempt: 2,
					},
				},
				testTaskID2: {
					{
						TaskID:      testTaskID2,
						TaskName:    testTaskName2,
						NodeID:      testNodeID2,
						TaskAttempt: 1,
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
					"clusterName": testClusterName,
					"eventType":   "TASK_DEFINITION_EVENT",
					"taskDefinitionEvent": map[string]any{
						"taskId":      testTaskID1,
						"taskName":    testTaskName1,
						"nodeId":      testNodeID1,
						"taskAttempt": 2,
					},
				},
			},
			cancelAfter:     50 * time.Millisecond,
			wantErr:         true,
			expectedErrType: context.Canceled,
			// Event might be processed before cancellation is detected
			wantStoredEvents: map[string][]types.Task{
				testTaskID1: {
					{
						TaskID:      testTaskID1,
						TaskName:    testTaskName1,
						NodeID:      testNodeID1,
						TaskAttempt: 2,
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
				if diff := cmp.Diff(tt.wantStoredEvents, h.ClusterTaskMap.ClusterTaskMap[testClusterName].TaskMap); diff != "" {
					t.Errorf("storeEventCalls diff (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestStoreEvent(t *testing.T) {
	// IDs follow Ray's ID spec; see TestEventProcessor for rationale.
	const (
		testJobID        = "aaaabbbb"                                                 // 4B
		testActorID      = "aaaabbbb1234aaaabbbb1234" + testJobID                     // 12B unique + JobID
		testTaskID1      = "ccccdddd5678cccc" + testActorID                           // 8B unique + ActorID
		testTaskID2      = "ccccdddd9012dddd" + testActorID                           // 8B unique + ActorID
		testNodeID1      = "eeeeffff1234eeeeffff1234eeeeffff1234eeeeffff1234eeeeffff" // 28B
		testNodeID2      = "eeeeffff9012eeeeffff9012eeeeffff9012eeeeffff9012eeeeffff" // 28B
		testUpperNodeID1 = "AAAABBBB1234AAAABBBB1234AAAABBBB1234AAAABBBB1234AAAABBBB" // 28B uppercase
		testLowerNodeID1 = "aaaabbbb1234aaaabbbb1234aaaabbbb1234aaaabbbb1234aaaabbbb" // 28B lowercase
		testUpperTaskID1 = "AAAABBBB5678AAAABBBB5678AAAABBBB5678AAAABBBB5678"         // 24B uppercase
		testLowerTaskID1 = "aaaabbbb5678aaaabbbb5678aaaabbbb5678aaaabbbb5678"         // 24B lowercase

		// Base64 and Hex pairs to verify normalization of base64 to hex
		testBase64ID1 = "AgAAAA=="
		testBase64ID2 = "AwAAAA=="
		testHexID1    = "02000000" // hex for base64 "AgAAAA=="
		testHexID2    = "03000000" // hex for base64 "AwAAAA=="

		testClusterName = "cluster1"
		testTaskName1   = "Name_12345"
		testTaskName2   = "Name_54321"
	)

	initialTask := types.Task{
		TaskID:      testTaskID1,
		TaskName:    testTaskName1,
		NodeID:      testNodeID1,
		TaskAttempt: 0,
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
				"clusterName": testClusterName,
			},
			wantErr:          false,
			wantClusterCount: 0,
		},
		{
			name: "task event - new cluster and new task",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: make(map[string]*types.TaskMap),
			},
			eventMap:          makeTaskEventMap(testTaskName1, testNodeID1, testTaskID1, testClusterName, 0),
			wantErr:           false,
			wantClusterCount:  1,
			wantTaskInCluster: testClusterName,
			wantTaskID:        testTaskID1,
			wantTasks: []types.Task{
				{
					TaskID:      testTaskID1,
					TaskName:    testTaskName1,
					NodeID:      testNodeID1,
					TaskAttempt: 0,
				},
			},
		},
		{
			name: "task event - existing cluster, new task",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: map[string]*types.TaskMap{
					testClusterName: types.NewTaskMap(),
				},
			},
			eventMap:          makeTaskEventMap(testTaskName2, testNodeID2, testTaskID2, testClusterName, 1),
			wantErr:           false,
			wantClusterCount:  1,
			wantTaskInCluster: testClusterName,
			wantTaskID:        testTaskID2,
			wantTasks: []types.Task{
				{
					TaskID:      testTaskID2,
					TaskName:    testTaskName2,
					NodeID:      testNodeID2,
					TaskAttempt: 1,
				},
			},
		},
		{
			name: "task event - existing cluster and existing task with new attempt",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: map[string]*types.TaskMap{
					testClusterName: {
						TaskMap: map[string][]types.Task{
							testTaskID1: {initialTask},
						},
					},
				},
			},
			eventMap:          makeTaskEventMap(testTaskName1, testNodeID1, testTaskID1, testClusterName, 2),
			wantErr:           false,
			wantClusterCount:  1,
			wantTaskInCluster: testClusterName,
			wantTaskID:        testTaskID1,
			// Now expects BOTH attempts to be stored
			wantTasks: []types.Task{
				{
					TaskID:      testTaskID1,
					TaskName:    testTaskName1,
					NodeID:      testNodeID1,
					TaskAttempt: 0,
				},
				{
					TaskID:      testTaskID1,
					TaskName:    testTaskName1,
					NodeID:      testNodeID1,
					TaskAttempt: 2,
				},
			},
		},
		{
			name: "task event - base64 ID is normalized to hex",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: make(map[string]*types.TaskMap),
			},
			eventMap:          makeTaskEventMap(testTaskName1, testBase64ID1, testBase64ID2, testClusterName, 0),
			wantErr:           false,
			wantClusterCount:  1,
			wantTaskInCluster: testClusterName,
			wantTaskID:        testHexID2,
			wantTasks: []types.Task{
				{
					TaskID:      testHexID2,
					TaskName:    testTaskName1,
					NodeID:      testHexID1,
					TaskAttempt: 0,
				},
			},
		},
		{
			name: "task event - uppercase hex IDs normalized to lowercase",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: make(map[string]*types.TaskMap),
			},
			eventMap:          makeTaskEventMap(testTaskName1, testUpperNodeID1, testUpperTaskID1, testClusterName, 0),
			wantErr:           false,
			wantClusterCount:  1,
			wantTaskInCluster: testClusterName,
			wantTaskID:        testLowerTaskID1,
			wantTasks: []types.Task{
				{
					TaskID:      testLowerTaskID1,
					TaskName:    testTaskName1,
					NodeID:      testLowerNodeID1,
					TaskAttempt: 0,
				},
			},
		},
		{
			name: "task event - lowercase hex IDs are returned as-is",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: make(map[string]*types.TaskMap),
			},
			eventMap:          makeTaskEventMap(testTaskName1, testLowerNodeID1, testLowerTaskID1, testClusterName, 0),
			wantErr:           false,
			wantClusterCount:  1,
			wantTaskInCluster: testClusterName,
			wantTaskID:        testLowerTaskID1,
			wantTasks: []types.Task{
				{
					TaskID:      testLowerTaskID1,
					TaskName:    testTaskName1,
					NodeID:      testLowerNodeID1,
					TaskAttempt: 0,
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
				"clusterName": testClusterName,
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
				"clusterName":         testClusterName,
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
				"clusterName": testClusterName,
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
	// IDs follow Ray's ID spec; see TestEventProcessor for rationale.
	const (
		testJobID       = "aaaabbbb"                                                 // 4B
		testActorID     = "aaaabbbb1234aaaabbbb1234" + testJobID                     // 12B unique + JobID
		testTaskID      = "ccccdddd5678cccc" + testActorID                           // 8B unique + ActorID
		testNodeID      = "eeeeffff1234eeeeffff1234eeeeffff1234eeeeffff1234eeeeffff" // 28B
		testWorkerID    = "eeeeffff0000eeeeffff0000eeeeffff0000eeeeffff0000eeeeffff" // 28B
		testClusterName = "cluster1"
	)

	// Helper to create a StateEvent
	makeStateEvent := func(state types.TaskStatus, timestampNano int64) types.TaskStateTransition {
		return types.TaskStateTransition{
			State:     state,
			Timestamp: time.Unix(0, timestampNano),
		}
	}

	// Helper to create a TASK_LIFECYCLE_EVENT map
	makeLifecycleEvent := func(transitions []map[string]any) map[string]any {
		// Convert []map[string]any to []any for proper type assertion in storeEvent
		transitionsAny := make([]any, len(transitions))
		for i, t := range transitions {
			transitionsAny[i] = t
		}
		return map[string]any{
			"eventType":   string(types.TASK_LIFECYCLE_EVENT),
			"clusterName": testClusterName,
			"taskLifecycleEvent": map[string]any{
				"taskId":           testTaskID,
				"taskAttempt":      float64(0),
				"stateTransitions": transitionsAny,
				"nodeId":           testNodeID,
				"workerId":         testWorkerID,
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
		existingEvents []types.TaskStateTransition // Events already in the task
		newTransitions []map[string]any            // New transitions to process
		wantEvents     []types.TaskStateTransition // Expected final events (sorted by timestamp)
		wantState      types.TaskStatus            // Expected final state
	}{
		{
			name: "Scenario 1: Normal deduplication - same events processed twice",
			existingEvents: []types.TaskStateTransition{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000),
				makeStateEvent(types.RUNNING, 2000),
			},
			newTransitions: []map[string]any{
				makeTransition("PENDING_NODE_ASSIGNMENT", 1000), // Duplicate
				makeTransition("RUNNING", 2000),                 // Duplicate
			},
			wantEvents: []types.TaskStateTransition{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000),
				makeStateEvent(types.RUNNING, 2000),
			},
			wantState: types.RUNNING,
		},
		{
			name: "Scenario 2: Out-of-order events - B(t=2) arrives after A(t=1), C(t=3)",
			existingEvents: []types.TaskStateTransition{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000), // A
				makeStateEvent(types.FINISHED, 3000),                // C
			},
			newTransitions: []map[string]any{
				makeTransition("RUNNING", 2000), // B - should be inserted in the middle
			},
			wantEvents: []types.TaskStateTransition{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000), // A
				makeStateEvent(types.RUNNING, 2000),                 // B - now in correct position
				makeStateEvent(types.FINISHED, 3000),                // C
			},
			wantState: types.FINISHED,
		},
		{
			name: "Scenario 3: Same timestamp, different states - both should be kept",
			existingEvents: []types.TaskStateTransition{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000),
			},
			newTransitions: []map[string]any{
				makeTransition("RUNNING", 1000), // Same timestamp, different state
			},
			wantEvents: []types.TaskStateTransition{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000),
				makeStateEvent(types.RUNNING, 1000),
			},
			wantState: types.RUNNING, // Last after sort (order of same timestamp is stable)
		},
		{
			name: "Scenario 4: Exact duplicate event - only one should remain",
			existingEvents: []types.TaskStateTransition{
				makeStateEvent(types.RUNNING, 1000),
			},
			newTransitions: []map[string]any{
				makeTransition("RUNNING", 1000), // Exact duplicate
				makeTransition("RUNNING", 1000), // Another duplicate
			},
			wantEvents: []types.TaskStateTransition{
				makeStateEvent(types.RUNNING, 1000), // Only one
			},
			wantState: types.RUNNING,
		},
		{
			name: "Scenario 5: Partial overlap - existing [A,B], new [B,C] -> result [A,B,C]",
			existingEvents: []types.TaskStateTransition{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000), // A
				makeStateEvent(types.RUNNING, 2000),                 // B
			},
			newTransitions: []map[string]any{
				makeTransition("RUNNING", 2000),  // B - duplicate
				makeTransition("FINISHED", 3000), // C - new
			},
			wantEvents: []types.TaskStateTransition{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000), // A
				makeStateEvent(types.RUNNING, 2000),                 // B
				makeStateEvent(types.FINISHED, 3000),                // C
			},
			wantState: types.FINISHED,
		},
		{
			name:           "Scenario 6: Empty initial events - add new events",
			existingEvents: []types.TaskStateTransition{},
			newTransitions: []map[string]any{
				makeTransition("PENDING_NODE_ASSIGNMENT", 1000),
				makeTransition("RUNNING", 2000),
			},
			wantEvents: []types.TaskStateTransition{
				makeStateEvent(types.PENDING_NODE_ASSIGNMENT, 1000),
				makeStateEvent(types.RUNNING, 2000),
			},
			wantState: types.RUNNING,
		},
		{
			name: "Scenario 7: Multiple reprocessing cycles - events should not grow",
			existingEvents: []types.TaskStateTransition{
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
			wantEvents: []types.TaskStateTransition{
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
				taskMap := h.ClusterTaskMap.GetOrCreateTaskMap(testClusterName)
				taskMap.CreateOrMergeAttempt(testTaskID, 0, func(task *types.Task) {
					task.TaskID = testTaskID
					task.StateTransitions = tt.existingEvents
					if len(tt.existingEvents) > 0 {
						task.State = tt.existingEvents[len(tt.existingEvents)-1].State
					}
				})
			}

			// Process the lifecycle event
			eventMap := makeLifecycleEvent(tt.newTransitions)
			err := h.storeEvent(eventMap)
			if err != nil {
				t.Fatalf("storeEvent() unexpected error: %v", err)
			}

			// Get the task and verify
			taskMap := h.ClusterTaskMap.GetOrCreateTaskMap(testClusterName)
			taskMap.Lock()
			defer taskMap.Unlock()

			tasks, exists := taskMap.TaskMap[testTaskID]
			if !exists || len(tasks) == 0 {
				t.Fatal("Task not found after processing")
			}

			task := tasks[0]

			// Verify events
			if diff := cmp.Diff(tt.wantEvents, task.StateTransitions); diff != "" {
				t.Errorf("Events mismatch (-want +got):\n%s", diff)
			}

			// Verify final state
			if task.State != tt.wantState {
				t.Errorf("State = %v, want %v", task.State, tt.wantState)
			}

			// Verify event count (important for deduplication)
			if len(task.StateTransitions) != len(tt.wantEvents) {
				t.Errorf("Event count = %d, want %d", len(task.StateTransitions), len(tt.wantEvents))
			}
		})
	}
}

// TestActorLifecycleEventDeduplication verifies that duplicate actor events are correctly filtered
func TestActorLifecycleEventDeduplication(t *testing.T) {
	// IDs follow Ray's ID spec; see TestEventProcessor for rationale.
	const (
		testJobID       = "aaaabbbb"                             // 4B
		testActorID     = "aaaabbbb1234aaaabbbb1234" + testJobID // 12B unique + JobID
		testClusterName = "cluster1"
	)

	// Helper to create an ActorStateEvent
	makeActorStateEvent := func(state types.StateType, timestampNano int64) types.ActorStateEvent {
		return types.ActorStateEvent{
			State:     state,
			Timestamp: time.Unix(0, timestampNano),
		}
	}

	// Helper to create an ACTOR_LIFECYCLE_EVENT map
	makeActorLifecycleEvent := func(transitions []map[string]any) map[string]any {
		// Convert []map[string]any to []any for proper type assertion in storeEvent
		transitionsAny := make([]any, len(transitions))
		for i, t := range transitions {
			transitionsAny[i] = t
		}
		return map[string]any{
			"eventType":   string(types.ACTOR_LIFECYCLE_EVENT),
			"clusterName": testClusterName,
			"actorLifecycleEvent": map[string]any{
				"actorId":          testActorID,
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
				actorMap := h.ClusterActorMap.GetOrCreateActorMap(testClusterName)
				actorMap.CreateOrMergeActor(testActorID, func(a *types.Actor) {
					a.ActorID = testActorID
					a.Events = tt.existingEvents
					if len(tt.existingEvents) > 0 {
						a.State = tt.existingEvents[len(tt.existingEvents)-1].State
					}
				})
			}

			// Process the lifecycle event
			eventMap := makeActorLifecycleEvent(tt.newTransitions)
			err := h.storeEvent(eventMap)
			if err != nil {
				t.Fatalf("storeEvent() unexpected error: %v", err)
			}

			// Get the actor and verify
			actor, found := h.GetActorByID(testClusterName, testActorID)
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
	// IDs follow Ray's ID spec; see TestEventProcessor for rationale.
	const (
		testJobID       = "aaaabbbb" // 4B
		testClusterName = "cluster1"
	)

	makeDriverJobStateTransitionEvent := func(state types.JobState, timestampNano int64) types.JobStateTransition {
		return types.JobStateTransition{
			State:     state,
			Timestamp: time.Unix(0, timestampNano),
		}
	}

	makeDriverJobLifecycleEvent := func(transitions []map[string]any) map[string]any {
		transitionsAny := make([]any, len(transitions))
		for i, t := range transitions {
			transitionsAny[i] = t
		}
		return map[string]any{
			"eventType":   string(types.DRIVER_JOB_LIFECYCLE_EVENT),
			"clusterName": testClusterName,
			"driverJobLifecycleEvent": map[string]any{
				"jobId":            testJobID,
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
				jobMap := h.ClusterJobMap.GetOrCreateJobMap(testClusterName)
				jobMap.CreateOrMergeJob(testJobID, func(job *types.Job) {
					job.JobID = testJobID
					job.StateTransitions = tt.existingTransitions
					if len(tt.existingTransitions) > 0 {
						job.State = tt.existingTransitions[len(tt.existingTransitions)-1].State
					}
				})
			}

			eventMap := makeDriverJobLifecycleEvent(tt.newTransitions)
			err := h.storeEvent(eventMap)
			if err != nil {
				t.Fatalf("storeEvent() unexpected error: %v", err)
			}

			job, exists := h.GetJobByJobID(testClusterName, testJobID)
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

	// IDs follow Ray's ID spec; see TestEventProcessor for rationale.
	const (
		testJobID       = "aaaabbbb"                                                 // 4B
		testActorID     = "aaaabbbb1234aaaabbbb1234" + testJobID                     // 12B unique + JobID
		testTaskID      = "ccccdddd5678cccc" + testActorID                           // 8B unique + ActorID
		testNodeID      = "eeeeffff1234eeeeffff1234eeeeffff1234eeeeffff1234eeeeffff" // 28B
		testWorkerID    = "eeeeffff0000eeeeffff0000eeeeffff0000eeeeffff0000eeeeffff" // 28B
		testClusterName = "cluster1"
	)

	// The same events that would be in an event file
	// Use []any to match what storeEvent expects from JSON parsing
	transitions := []any{
		map[string]any{"state": "PENDING_NODE_ASSIGNMENT", "timestamp": time.Unix(0, 1000).Format(time.RFC3339Nano)},
		map[string]any{"state": "RUNNING", "timestamp": time.Unix(0, 2000).Format(time.RFC3339Nano)},
		map[string]any{"state": "FINISHED", "timestamp": time.Unix(0, 3000).Format(time.RFC3339Nano)},
	}

	eventMap := map[string]any{
		"eventType":   string(types.TASK_LIFECYCLE_EVENT),
		"clusterName": testClusterName,
		"taskLifecycleEvent": map[string]any{
			"taskId":           testTaskID,
			"taskAttempt":      float64(0),
			"stateTransitions": transitions,
			"nodeId":           testNodeID,
			"workerId":         testWorkerID,
		},
	}

	// Simulate 10 hourly reprocessing cycles
	for cycle := 0; cycle < 10; cycle++ {
		err := h.storeEvent(eventMap)
		if err != nil {
			t.Fatalf("Cycle %d: storeEvent() error = %v", cycle, err)
		}

		// Check event count after each cycle
		taskMap := h.ClusterTaskMap.GetOrCreateTaskMap(testClusterName)
		taskMap.Lock()
		tasks := taskMap.TaskMap[testTaskID]
		eventCount := len(tasks[0].StateTransitions)
		taskMap.Unlock()

		// Should always be exactly 3 events, never growing
		if eventCount != 3 {
			t.Errorf("Cycle %d: Event count = %d, want 3 (events are duplicating!)", cycle, eventCount)
		}
	}
}

func TestNormalizeIDToHex(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string returns empty",
			input:    "",
			expected: "",
		},
		{
			name:     "valid base64 converts to hex",
			input:    "AgAAAA==",
			expected: "02000000",
		},
		{
			name:     "invalid base64 returns original",
			input:    "not_valid_base64!!!",
			expected: "not_valid_base64!!!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeIDToHex(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeIDToHex(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeActorIDsToHex(t *testing.T) {
	base64Val := "AgAAAA=="
	expectedHex, _ := utils.ConvertBase64ToHex(base64Val)

	actor := types.Actor{
		ActorID:          base64Val,
		JobID:            base64Val,
		PlacementGroupID: base64Val,
		Address: types.Address{
			NodeID:   base64Val,
			WorkerID: base64Val,
		},
	}

	normalizeActorIDsToHex(&actor)

	if actor.ActorID != expectedHex {
		t.Errorf("ActorID = %q, want %q", actor.ActorID, expectedHex)
	}
	if actor.JobID != expectedHex {
		t.Errorf("JobID = %q, want %q", actor.JobID, expectedHex)
	}
	if actor.PlacementGroupID != expectedHex {
		t.Errorf("PlacementGroupID = %q, want %q", actor.PlacementGroupID, expectedHex)
	}
	if actor.Address.NodeID != expectedHex {
		t.Errorf("Address.NodeID = %q, want %q", actor.Address.NodeID, expectedHex)
	}
	if actor.Address.WorkerID != expectedHex {
		t.Errorf("Address.WorkerID = %q, want %q", actor.Address.WorkerID, expectedHex)
	}
}
