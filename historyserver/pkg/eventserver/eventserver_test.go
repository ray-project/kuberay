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
		wantStoredEvents map[string]types.Task
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
			wantStoredEvents: map[string]types.Task{
				"ID_12345": {
					TaskID:        "ID_12345",
					Name:          "Name_12345",
					NodeID:        "Nodeid_12345",
					AttemptNumber: 2,
				},
				"ID_54321": {
					TaskID:        "ID_54321",
					Name:          "Name_54321",
					NodeID:        "Nodeid_54321",
					AttemptNumber: 1,
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
			wantStoredEvents: map[string]types.Task{
				"ID_12345": {
					TaskID:        "ID_12345",
					Name:          "Name_12345",
					NodeID:        "Nodeid_12345",
					AttemptNumber: 2,
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
		wantTaskInCluster string      // Cluster to check for the task
		wantTaskID        string      // TaskID to check
		wantTask          *types.Task // Expected task, nil if not applicable
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
			wantTask: &types.Task{
				TaskID:        "taskid1",
				Name:          "taskName123",
				NodeID:        "nodeid1234",
				AttemptNumber: 0,
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
			wantTask: &types.Task{
				TaskID:        "taskid2",
				Name:          "taskName123",
				NodeID:        "nodeid1234",
				AttemptNumber: 1,
			},
		},
		{
			name: "task event - existing cluster and existing task",
			initialState: &types.ClusterTaskMap{
				ClusterTaskMap: map[string]*types.TaskMap{
					"cluster1": {
						TaskMap: map[string]types.Task{
							"taskid1": initialTask,
						},
					},
				},
			},
			eventMap:          makeTaskEventMap("taskName123", "nodeid123", "taskid1", "cluster1", 0),
			wantErr:           false,
			wantClusterCount:  1,
			wantTaskInCluster: "cluster1",
			wantTaskID:        "taskid1",
			// TODO: currently, task wont be changed.
			wantTask: &initialTask,
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

			if tt.wantTask != nil {
				clusterObj, clusterExists := h.ClusterTaskMap.ClusterTaskMap[tt.wantTaskInCluster]

				if !clusterExists {
					t.Fatalf("storeEvent() cluster %s not found", tt.wantTaskInCluster)
				}

				clusterObj.Lock()
				defer clusterObj.Unlock()
				gotTask, taskExists := clusterObj.TaskMap[tt.wantTaskID]
				if !taskExists {
					t.Fatalf("storeEvent() task %s not found in cluster %s", tt.wantTaskID, tt.wantTaskInCluster)
				}

				if diff := cmp.Diff(*tt.wantTask, gotTask); diff != "" {
					t.Errorf("storeEvent() task mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}
