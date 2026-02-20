package historyserver

import (
	"context"
	"testing"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage/localtest"
	"github.com/stretchr/testify/require"
)

// TestGetNodeLogFileTimeout tests that context.WithTimeout correctly cancels slow storage operations.
// This test verifies that when a context timeout is set, the operation respects it and returns
// context.DeadlineExceeded error instead of waiting for the full delay.
func TestGetNodeLogFileTimeout(t *testing.T) {
	// Create a delayed mock reader that takes 10 seconds to read
	delayedReader := localtest.NewDelayedMockReader(10 * time.Second)

	// Create minimal EventHandler with test node data
	nodeMap := eventtypes.NewNodeMap()
	nodeMap.NodeMap["node-123"] = eventtypes.Node{
		NodeID:        "node-123",
		NodeIPAddress: "192.168.1.1",
	}
	eventHandler := &eventserver.EventHandler{
		ClusterNodeMap: &eventtypes.ClusterNodeMap{
			ClusterNodeMap: map[string]*eventtypes.NodeMap{
				"cluster-1_default_session-1": nodeMap,
			},
		},
	}

	// Create ServerHandler with delayed reader
	handler := &ServerHandler{
		reader:       delayedReader,
		eventHandler: eventHandler,
	}

	// Create context with 100ms timeout (much less than 10s delay)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	options := GetLogFileOptions{
		NodeID:   "node-123",
		Filename: "raylet.out",
		Lines:    100,
	}

	start := time.Now()
	_, err := handler._getNodeLogFile(ctx, "cluster-1_default", "session-1", options)
	elapsed := time.Since(start)

	require.Error(t, err, "Expected an error due to context timeout, got nil")
	require.ErrorIsf(t, err, context.DeadlineExceeded, "Expected context.DeadlineExceeded error, got: %v", err)
	require.InDelta(t, 100*time.Millisecond, elapsed, float64(50*time.Millisecond), "Expected timeout around 100ms")
}
