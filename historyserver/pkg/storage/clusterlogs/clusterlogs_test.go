package clusterlogs

import (
	"testing"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

func TestClusterLogsPaths(t *testing.T) {
	rootDir := ""
	ownerKind := "rayjob"
	ownerName := "job-1"
	ns := "default"
	cluster := "cluster-1"
	session := "session-1"
	node := "node-1"
	jobID := "01000000"

	wantPrefix := "cluster-history/rayjob/default/job-1/cluster-1"
	if got := Prefix(rootDir, &utils.ClusterInfo{
		OwnerKind: ownerKind,
		OwnerName: ownerName,
		Namespace: ns,
		Name:      cluster,
	}); got != wantPrefix {
		t.Errorf("Prefix() = %q, want %q", got, wantPrefix)
	}

	wantSession := wantPrefix + "/session-1"
	if got := SessionDir(rootDir, &utils.ClusterInfo{
		OwnerKind: ownerKind,
		OwnerName: ownerName,
		Namespace: ns,
		Name:      cluster,
		SessionName: session,
	}); got != wantSession {
		t.Errorf("SessionDir() = %q, want %q", got, wantSession)
	}

	wantFetchedEndpoints := wantSession + "/fetched_endpoints"
	if got := FetchedEndpointsDir(wantPrefix, session); got != wantFetchedEndpoints {
		t.Errorf("FetchedEndpointsDir() = %q, want %q", got, wantFetchedEndpoints)
	}

	wantNode := wantSession + "/node-1"
	if got := NodeDir(rootDir, &utils.ClusterInfo{
		OwnerKind: ownerKind,
		OwnerName: ownerName,
		Namespace: ns,
		Name:      cluster,
		SessionName: session,
	}, node); got != wantNode {
		t.Errorf("NodeDir() = %q, want %q", got, wantNode)
	}

	wantLogs := wantNode + "/logs"
	if got := LogsDir(rootDir, &utils.ClusterInfo{
		OwnerKind: ownerKind,
		OwnerName: ownerName,
		Namespace: ns,
		Name:      cluster,
		SessionName: session,
	}, node); got != wantLogs {
		t.Errorf("LogsDir() = %q, want %q", got, wantLogs)
	}

	wantNodeEvents := wantNode + "/node_events"
	if got := NodeEventsDir(rootDir, &utils.ClusterInfo{
		OwnerKind: ownerKind,
		OwnerName: ownerName,
		Namespace: ns,
		Name:      cluster,
		SessionName: session,
	}, node); got != wantNodeEvents {
		t.Errorf("NodeEventsDir() = %q, want %q", got, wantNodeEvents)
	}

	wantJobEvents := wantNode + "/job_events/01000000"
	if got := JobEventsDir(rootDir, &utils.ClusterInfo{
		OwnerKind: ownerKind,
		OwnerName: ownerName,
		Namespace: ns,
		Name:      cluster,
		SessionName: session,
	}, node, jobID); got != wantJobEvents {
		t.Errorf("JobEventsDir() = %q, want %q", got, wantJobEvents)
	}

	wantJobEventsNoID := wantNode + "/job_events"
	if got := JobEventsDir(rootDir, &utils.ClusterInfo{
		OwnerKind: ownerKind,
		OwnerName: ownerName,
		Namespace: ns,
		Name:      cluster,
		SessionName: session,
	}, node, ""); got != wantJobEventsNoID {
		t.Errorf("JobEventsDir(no jobID) = %q, want %q", got, wantJobEventsNoID)
	}

	if got := RelLogsDir(session, node); got != "session-1/node-1/logs" {
		t.Errorf("RelLogsDir() = %q", got)
	}
	if got := RelNodeEventsDir(session, node); got != "session-1/node-1/node_events" {
		t.Errorf("RelNodeEventsDir() = %q", got)
	}
	if got := RelJobEventsDir(session, node, jobID); got != "session-1/node-1/job_events/01000000" {
		t.Errorf("RelJobEventsDir() = %q", got)
	}
}
