package clusterlogs

import (
	"testing"
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
	if got := Prefix(rootDir, ownerKind, ownerName, ns, cluster); got != wantPrefix {
		t.Errorf("Prefix() = %q, want %q", got, wantPrefix)
	}

	wantSession := wantPrefix + "/session-1"
	if got := SessionDir(rootDir, ownerKind, ownerName, ns, cluster, session); got != wantSession {
		t.Errorf("SessionDir() = %q, want %q", got, wantSession)
	}

	wantNode := wantSession + "/node-1"
	if got := NodeDir(rootDir, ownerKind, ownerName, ns, cluster, session, node); got != wantNode {
		t.Errorf("NodeDir() = %q, want %q", got, wantNode)
	}

	wantLogs := wantNode + "/logs"
	if got := LogsDir(rootDir, ownerKind, ownerName, ns, cluster, session, node); got != wantLogs {
		t.Errorf("LogsDir() = %q, want %q", got, wantLogs)
	}

	wantNodeEvents := wantNode + "/node_events"
	if got := NodeEventsDir(rootDir, ownerKind, ownerName, ns, cluster, session, node); got != wantNodeEvents {
		t.Errorf("NodeEventsDir() = %q, want %q", got, wantNodeEvents)
	}

	wantJobEvents := wantNode + "/job_events/01000000"
	if got := JobEventsDir(rootDir, ownerKind, ownerName, ns, cluster, session, node, jobID); got != wantJobEvents {
		t.Errorf("JobEventsDir() = %q, want %q", got, wantJobEvents)
	}

	wantJobEventsNoID := wantNode + "/job_events"
	if got := JobEventsDir(rootDir, ownerKind, ownerName, ns, cluster, session, node, ""); got != wantJobEventsNoID {
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
