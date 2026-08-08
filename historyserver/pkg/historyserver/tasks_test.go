package historyserver

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

type taskLogStorageReader struct {
	filename string
}

func (*taskLogStorageReader) List() []utils.ClusterInfo { return nil }

func (r *taskLogStorageReader) GetContent(_ string, filename string) io.Reader {
	r.filename = filename
	return strings.NewReader("Processing 1\n")
}

func (*taskLogStorageReader) ListFiles(_, _ string) []string { return nil }

func TestTaskLogBasename(t *testing.T) {
	tests := map[string]struct {
		filename string
		want     string
	}{
		"absolute Ray path": {
			filename: "/tmp/ray/session_latest/logs/worker-abc-123.out",
			want:     "worker-abc-123.out",
		},
		"stored basename": {
			filename: "worker-abc-123.err",
			want:     "worker-abc-123.err",
		},
		"path traversal": {
			filename: "../../worker-abc-123.out",
			want:     "worker-abc-123.out",
		},
		"empty": {filename: "", want: ""},
		"directory": {
			filename: "/",
			want:     "",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := taskLogBasename(test.filename); got != test.want {
				t.Fatalf("taskLogBasename(%q) = %q, want %q", test.filename, got, test.want)
			}
		})
	}
}

func TestGetTaskLogFileUsesStoredBasename(t *testing.T) {
	const clusterSessionKey = "raycluster_default_session"
	loader := newTestSessionLoader(t, &fakeProcessor{}, 1)
	loader.putSnapshot(clusterSessionKey, &eventserver.SessionSnapshot{Tasks: []eventtypes.Task{{
		TaskID:      "task-id",
		TaskAttempt: 0,
		NodeID:      "node-id",
		WorkerID:    "worker-id",
		TaskLogInfo: &eventtypes.TaskLogInfo{
			StdoutFile: "/tmp/ray/session_latest/logs/worker-abc-123.out",
		},
	}}})

	reader := &taskLogStorageReader{}
	handler := &ServerHandler{sessionLoader: loader, reader: reader}
	content, err := handler._getNodeLogFile(
		clusterSessionKey,
		"cluster-prefix",
		"session",
		GetLogFileOptions{TaskID: "task-id", Suffix: "out", Lines: -1},
	)
	if err != nil {
		t.Fatalf("_getNodeLogFile() error = %v", err)
	}
	if got, want := string(content), "Processing 1\n"; got != want {
		t.Fatalf("_getNodeLogFile() content = %q, want %q", got, want)
	}
	if got, want := reader.filename, "session/node-id/logs/worker-abc-123.out"; got != want {
		t.Fatalf("storage filename = %q, want %q", got, want)
	}
}

func TestFormatTaskForResponseTaskLogInfo(t *testing.T) {
	task := eventtypes.Task{TaskLogInfo: &eventtypes.TaskLogInfo{
		StdoutFile:  "worker.out",
		StderrFile:  "worker.err",
		StdoutStart: 10,
		StdoutEnd:   20,
		StderrStart: 30,
		StderrEnd:   40,
	}}

	response := formatTaskForResponse(task, true)
	assert.Equal(t, map[string]interface{}{
		"stdout_file":  "worker.out",
		"stderr_file":  "worker.err",
		"stdout_start": int64(10),
		"stdout_end":   int64(20),
		"stderr_start": int64(30),
		"stderr_end":   int64(40),
	}, response["task_log_info"])

	response = formatTaskForResponse(eventtypes.Task{}, true)
	assert.Nil(t, response["task_log_info"])
}
