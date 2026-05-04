package historyserver

import (
	"testing"
	"time"

	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
)

func TestFormatNodeForStateAPI(t *testing.T) {
	// Case 1: Node with no transitions
	node1 := eventtypes.Node{
		NodeID:         "node1",
		NodeIPAddress:  "192.168.1.1",
		StartTimestamp: time.UnixMilli(1714512000000),
		Labels:         map[string]string{"ray.io/node-type": "worker"},
	}

	result1 := formatNodeForStateAPI(node1)

	if result1["state"] != "DEAD" {
		t.Errorf("Expected state to be DEAD, got %v", result1["state"])
	}
	if result1["node_id"] != "node1" {
		t.Errorf("Expected node_id to be node1, got %v", result1["node_id"])
	}
	if result1["is_head_node"] != false {
		t.Errorf("Expected is_head_node to be false, got %v", result1["is_head_node"])
	}

	// Case 2: Node with ALIVE transition and resources
	node2 := eventtypes.Node{
		NodeID:         "node2",
		NodeIPAddress:  "192.168.1.2",
		StartTimestamp: time.UnixMilli(1714512000000),
		Labels:         map[string]string{"ray.io/node-type": "head"},
		StateTransitions: []eventtypes.NodeStateTransition{
			{
				State:     eventtypes.NODE_ALIVE,
				Timestamp: time.UnixMilli(1714512000000),
				Resources: map[string]float64{"CPU": 4.0},
			},
		},
	}

	result2 := formatNodeForStateAPI(node2)

	if result2["state"] != "ALIVE" {
		t.Errorf("Expected state to be ALIVE, got %v", result2["state"])
	}
	if result2["is_head_node"] != true {
		t.Errorf("Expected is_head_node to be true, got %v", result2["is_head_node"])
	}
	resources, ok := result2["resources_total"].(map[string]float64)
	if !ok || resources["CPU"] != 4.0 {
		t.Errorf("Expected CPU resource to be 4.0, got %v", resources)
	}

	// Case 3: Node with DEAD transition (should keep resources from previous ALIVE transition)
	node3 := eventtypes.Node{
		NodeID:         "node3",
		NodeIPAddress:  "192.168.1.3",
		StartTimestamp: time.UnixMilli(1714512000000),
		StateTransitions: []eventtypes.NodeStateTransition{
			{
				State:     eventtypes.NODE_ALIVE,
				Timestamp: time.UnixMilli(1714512000000),
				Resources: map[string]float64{"CPU": 2.0},
			},
			{
				State:     eventtypes.NODE_DEAD,
				Timestamp: time.UnixMilli(1714512001000),
				Resources: map[string]float64{}, // Dead transition usually has empty resources
			},
		},
	}

	result3 := formatNodeForStateAPI(node3)

	if result3["state"] != "DEAD" {
		t.Errorf("Expected state to be DEAD, got %v", result3["state"])
	}
	resources3, ok := result3["resources_total"].(map[string]float64)
	if !ok || resources3["CPU"] != 2.0 {
		t.Errorf("Expected CPU resource to be 2.0 (preserved from ALIVE state), got %v", resources3)
	}
}
