package storage

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRelativeFilePaths(t *testing.T) {
	prefix := "root/cluster/session"
	objectPaths := []string{
		"root/cluster/session/",
		"root/cluster/session/node-a/",
		"root/cluster/session/node-a/job_events/job-1/events.gz",
		"root/cluster/session/node-b/node_events/events.gz",
		"root/cluster/session-old/node-c/node_events/events.gz",
	}

	want := []string{
		"node-a/job_events/job-1/events.gz",
		"node-b/node_events/events.gz",
	}

	if diff := cmp.Diff(want, RelativeFilePaths(prefix, objectPaths)); diff != "" {
		t.Fatalf("RelativeFilePaths() returned diff (-want +got):\n%s", diff)
	}
}
