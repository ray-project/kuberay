package historyserver

import (
	"testing"

	"github.com/ray-project/kuberay/historyserver/pkg/storage/localtest"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestListClustersCrashDetection(t *testing.T) {
	mockReader := localtest.NewMockReader()
	if mockReader == nil {
		t.Fatal("NewMockReader returned nil")
	}

	// ClientManager with no clients simulates an environment with no live
	// RayClusters — every in_progress session should be inferred as terminated.
	cm := &ClientManager{
		configs: []*rest.Config{},
		clients: []client.Client{},
	}

	handler := &ServerHandler{
		maxClusters:   100,
		reader:        mockReader,
		clientManager: cm,
		clustersMap:   make(map[utils.ClusterKey][]utils.ClusterInfo),
	}

	clusters := handler.listClusters(0)

	// Localtest mock defines:
	//   cluster-1 → completed → must stay completed
	//   cluster-2 → in_progress, no live cluster → must become terminated
	found := 0
	for _, c := range clusters {
		if c.Name == "cluster-1" {
			found++
			if c.Status != utils.SessionStatusCompleted {
				t.Errorf("cluster-1: expected completed, got %q", c.Status)
			}
		}
		if c.Name == "cluster-2" {
			found++
			if c.Status != utils.SessionStatusTerminated {
				t.Errorf("cluster-2: expected terminated (crash detected), got %q", c.Status)
			}
		}
	}
	if found < 2 {
		t.Errorf("expected both cluster-1 and cluster-2 in result, found %d matches", found)
	}
}
