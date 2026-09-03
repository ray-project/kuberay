package logcollector

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const (
	testSessionID = "session_2026-01-11_19-38-40_123456"
	testNodeID    = "node1"
	testLogPrefix = "root/cluster-history/raycluster/default/rc/" + testSessionID + "/" + testNodeID + "/logs/"
)

func newRotatedTestHandler(writer *MockStorageWriter) *RayLogHandler {
	return &RayLogHandler{
		Writer:              writer,
		RootDir:             "root",
		RayClusterName:      "rc",
		RayClusterNamespace: "default",
		RayNodeName:         testNodeID,
	}
}

// writeLogFile creates path with content and returns its inode.
func writeLogFile(t *testing.T, path, content string) uint64 {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) = %v", path, err)
	}
	return inodeOf(t, path)
}

func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) = %v", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("Stat(%s): inode unavailable on this platform", path)
	}
	return stat.Ino
}

func TestRotationBaseName(t *testing.T) {
	tests := map[string]struct {
		name     string
		wantBase string
		wantOK   bool
	}{
		"first backup":       {name: "raylet.out.1", wantBase: "raylet.out", wantOK: true},
		"second backup":      {name: "raylet.out.2", wantBase: "raylet.out", wantOK: true},
		"multi digit backup": {name: "raylet.out.12", wantBase: "raylet.out", wantOK: true},
		"worker backup":      {name: "worker-abc123-01000000-123.err.3", wantBase: "worker-abc123-01000000-123.err", wantOK: true},
		"no extension base":  {name: "raylet.4", wantBase: "raylet", wantOK: true},
		"active out":         {name: "raylet.out"},
		"active err":         {name: "worker-abc123-01000000-123.err"},
		"no dot":             {name: "raylet"},
		"zero index":         {name: "raylet.out.0"},
		"leading zero index": {name: "raylet.out.01"},
		"trailing dot":       {name: "raylet.out."},
		"non numeric suffix": {name: "raylet.out.gz"},
		"leading dot only":   {name: ".1"},
		"already rotated":    {name: "raylet.rotated.42-2048.out"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			base, ok := rotationBaseName(test.name)
			if ok != test.wantOK || base != test.wantBase {
				t.Fatalf("rotationBaseName(%q) = (%q, %v), want (%q, %v)", test.name, base, ok, test.wantBase, test.wantOK)
			}
		})
	}
}

func TestRotatedLogName(t *testing.T) {
	tests := map[string]struct {
		name  string
		inode uint64
		size  int64
		want  string
	}{
		"worker stdout": {
			name: "worker-abc123-01000000-123.out.1", inode: 4390125, size: 1048576,
			want: "worker-abc123-01000000-123.rotated.4390125-1048576.out",
		},
		"worker stderr": {
			name: "worker-abc123-01000000-123.err.5", inode: 4390125, size: 64,
			want: "worker-abc123-01000000-123.rotated.4390125-64.err",
		},
		"component stdout": {
			name: "raylet.out.2", inode: 4390126, size: 2048,
			want: "raylet.rotated.4390126-2048.out",
		},
		"dot log": {
			name: "python-core-worker-abc_123.log.3", inode: 7, size: 9,
			want: "python-core-worker-abc_123.rotated.7-9.log",
		},
		"no extension": {
			name: "raylet.4", inode: 11, size: 12,
			want: "raylet.rotated.11-12",
		},
		"dotfile base": {
			name: ".out.1", inode: 13, size: 14,
			want: ".out.rotated.13-14",
		},
		"not a rotation backup": {name: "raylet.out", inode: 1, size: 1},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := rotatedLogName(test.name, test.inode, test.size)
			if got != test.want || ok != (test.want != "") {
				t.Fatalf("rotatedLogName(%q, %d, %d) = (%q, %v), want %q", test.name, test.inode, test.size, got, ok, test.want)
			}
		})
	}
}

func TestCollectRotatedLogUploadsDeterministicObject(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	backup := filepath.Join(logsDir, "raylet.out.1")
	inode := writeLogFile(t, backup, "rotated raylet")
	nested := filepath.Join(logsDir, "old", "worker-abc123-01000000-123.err.2")
	nestedInode := writeLogFile(t, nested, "rotated worker")
	active := filepath.Join(logsDir, "raylet.out")
	writeLogFile(t, active, "active raylet")

	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	want := map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", inode, len("rotated raylet")):                                    "rotated raylet",
		testLogPrefix + "old/" + mustRotatedName(t, "worker-abc123-01000000-123.err.2", nestedInode, len("rotated worker")): "rotated worker",
	}
	assertWritten(t, writer, want)
}

func TestCollectRotatedLogSkipsActiveFiles(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	for _, name := range []string{"raylet.out", "raylet.err", "monitor.log", "worker-abc-01000000-1.out"} {
		writeLogFile(t, filepath.Join(logsDir, name), "active")
	}

	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)
	assertWritten(t, writer, map[string]string{})
}

// A generation keeps its inode as Ray shifts it down the rotation ring, so it
// must not be uploaded a second time under a different index.
func TestCollectRotatedLogIgnoresRotationIndexChange(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	first := filepath.Join(logsDir, "raylet.out.1")
	inode := writeLogFile(t, first, "generation one")
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	second := filepath.Join(logsDir, "raylet.out.2")
	if err := os.Rename(first, second); err != nil {
		t.Fatalf("Rename() = %v", err)
	}
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", inode, len("generation one")): "generation one",
	})
}

func TestCollectRotatedLogUploadsNewGenerationReusingIndex(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	backup := filepath.Join(logsDir, "raylet.out.1")
	firstInode := writeLogFile(t, backup, "generation one")
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	if err := os.Remove(backup); err != nil {
		t.Fatalf("Remove() = %v", err)
	}
	secondInode := writeLogFile(t, backup, "generation two!")
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	if firstInode == secondInode {
		t.Skip("filesystem reused the inode; identity cannot be distinguished")
	}
	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", firstInode, len("generation one")):   "generation one",
		testLogPrefix + mustRotatedName(t, "raylet.out.1", secondInode, len("generation two!")): "generation two!",
	})
}

func TestCollectRotatedLogRetriesAfterWriteFailure(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	writer.setWriteErr(errors.New("object store unavailable"))
	handler := newRotatedTestHandler(writer)

	backup := filepath.Join(logsDir, "raylet.out.1")
	inode := writeLogFile(t, backup, "rotated raylet")

	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)
	assertWritten(t, writer, map[string]string{})

	writer.setWriteErr(nil)
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)
	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", inode, len("rotated raylet")): "rotated raylet",
	})
}

// The upload must keep reading from the descriptor it opened, even once Ray has
// removed the path it came from.
func TestCollectRotatedLogReadsThroughUnlinkedPath(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	backup := filepath.Join(logsDir, "raylet.out.1")
	inode := writeLogFile(t, backup, "rotated raylet")
	writer.beforeWrite = func() {
		if err := os.Remove(backup); err != nil {
			t.Errorf("Remove() = %v", err)
		}
	}

	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)
	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", inode, len("rotated raylet")): "rotated raylet",
	})
}

func TestCollectRotatedLogToleratesVanishedPath(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	if handled := handler.collectRotatedLog(filepath.Join(logsDir, "raylet.out.1"), logsDir, testSessionID, testNodeID); !handled {
		t.Fatal("collectRotatedLog() = false, want true for a rotation backup name")
	}
	assertWritten(t, writer, map[string]string{})
}

func TestCollectRotatedLogAttributesSessionAndNode(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	backup := filepath.Join(logsDir, "raylet.out.1")
	inode := writeLogFile(t, backup, "rotated raylet")

	handler.collectRotatedLogsUnder(logsDir, "session-old", "node-old")
	handler.collectRotatedLogsUnder(logsDir, "session-new", "node-new")

	name := mustRotatedName(t, "raylet.out.1", inode, len("rotated raylet"))
	assertWritten(t, writer, map[string]string{
		"root/cluster-history/raycluster/default/rc/session-old/node-old/logs/" + name: "rotated raylet",
		"root/cluster-history/raycluster/default/rc/session-new/node-new/logs/" + name: "rotated raylet",
	})
}

func TestCollectRotatedLogSkipsUnknownSessionOrNode(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)
	writeLogFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated raylet")

	handler.collectRotatedLogsUnder(logsDir, testSessionID, "")
	handler.collectRotatedLogsUnder(logsDir, "", testNodeID)
	assertWritten(t, writer, map[string]string{})
}

// A restarted collector has an empty uploaded set, so it re-uploads what is still
// on disk; the object key is unchanged, which keeps the re-upload idempotent.
func TestCollectRotatedLogRestartKeepsObjectName(t *testing.T) {
	logsDir := t.TempDir()
	backup := filepath.Join(logsDir, "raylet.out.1")
	inode := writeLogFile(t, backup, "rotated raylet")
	want := map[string]string{testLogPrefix + mustRotatedName(t, "raylet.out.1", inode, len("rotated raylet")): "rotated raylet"}

	beforeRestart := NewMockStorageWriter()
	newRotatedTestHandler(beforeRestart).collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)
	assertWritten(t, beforeRestart, want)

	afterRestart := NewMockStorageWriter()
	newRotatedTestHandler(afterRestart).collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)
	assertWritten(t, afterRestart, want)
}

func TestCollectRotatedLogIsUploadedOnceUnderConcurrency(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	backup := filepath.Join(logsDir, "raylet.out.1")
	inode := writeLogFile(t, backup, "rotated raylet")

	var uploads int
	var mu sync.Mutex
	writer.beforeWrite = func() {
		mu.Lock()
		uploads++
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)
		})
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if uploads != 1 {
		t.Fatalf("WriteFile called %d times, want 1", uploads)
	}
	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", inode, len("rotated raylet")): "rotated raylet",
	})
}

// The first scan must not wait for the ticker: a collector restarting under a
// live Ray node would otherwise miss a whole interval of rotations.
func TestScanRotatedLogsScansBeforeFirstTick(t *testing.T) {
	rayRoot := t.TempDir()
	t.Setenv("RAY_TMP_ROOT", rayRoot)
	logsDir := linkSessionLatest(t, rayRoot, testSessionID)
	inode := writeLogFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated raylet")

	writer := NewMockStorageWriter()
	uploaded := make(chan struct{})
	writer.beforeWrite = func() { close(uploaded) }

	handler := newRotatedTestHandler(writer)
	// Long enough that only the immediate scan can produce the upload.
	handler.RotatedLogScanInterval = time.Hour

	stop := make(chan struct{})
	defer close(stop)
	go handler.scanRotatedLogs(stop)

	select {
	case <-uploaded:
	case <-time.After(10 * time.Second):
		t.Fatal("no rotated log uploaded before the first ticker interval")
	}
	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", inode, len("rotated raylet")): "rotated raylet",
	})
}

func TestScanRotatedLogsStopsOnSignal(t *testing.T) {
	t.Setenv("RAY_TMP_ROOT", t.TempDir())
	handler := newRotatedTestHandler(NewMockStorageWriter())
	handler.RotatedLogScanInterval = time.Millisecond

	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		handler.scanRotatedLogs(stop)
	}()

	close(stop)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("scanRotatedLogs did not exit after stop")
	}
}

// Shutdown is the first pass to see this generation, so it must still use the
// deterministic name rather than the raw rotation index.
func TestProcessSessionLatestLogsUsesRotatedName(t *testing.T) {
	rayRoot := t.TempDir()
	t.Setenv("RAY_TMP_ROOT", rayRoot)
	logsDir := linkSessionLatest(t, rayRoot, testSessionID)

	inode := writeLogFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated raylet")
	writeLogFile(t, filepath.Join(logsDir, "raylet.out"), "active raylet")

	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)
	handler.processSessionLatestLogs()

	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", inode, len("rotated raylet")): "rotated raylet",
		testLogPrefix + "raylet.out": "active raylet",
	})
}

func TestProcessSessionLatestLogsSkipsAlreadyUploadedRotation(t *testing.T) {
	rayRoot := t.TempDir()
	t.Setenv("RAY_TMP_ROOT", rayRoot)
	logsDir := linkSessionLatest(t, rayRoot, testSessionID)
	inode := writeLogFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated raylet")

	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	var uploadsAfterScan int
	writer.beforeWrite = func() { uploadsAfterScan++ }
	handler.processSessionLatestLogs()

	if uploadsAfterScan != 0 {
		t.Fatalf("shutdown uploaded %d objects, want 0", uploadsAfterScan)
	}
	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", inode, len("rotated raylet")): "rotated raylet",
	})
}

func TestProcessPrevLogsDirUsesRotatedName(t *testing.T) {
	rayRoot := t.TempDir()
	t.Setenv("RAY_TMP_ROOT", rayRoot)

	nodeDir := filepath.Join(rayRoot, "prev-logs", testSessionID, testNodeID)
	logsDir := filepath.Join(nodeDir, utils.RAY_SESSIONDIR_LOGDIR_NAME)
	inode := writeLogFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated raylet")
	writeLogFile(t, filepath.Join(logsDir, "raylet.out"), "active raylet")

	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)
	handler.prevLogsDir = utils.GetRayPrevLogsPath()
	handler.persistCompleteLogsDir = utils.GetRayPersistCompletePath()
	handler.processPrevLogsDir(nodeDir)

	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", inode, len("rotated raylet")): "rotated raylet",
		testLogPrefix + "raylet.out": "active raylet",
	})
}

func linkSessionLatest(t *testing.T, rayRoot, sessionID string) string {
	t.Helper()
	logsDir := filepath.Join(rayRoot, sessionID, utils.RAY_SESSIONDIR_LOGDIR_NAME)
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) = %v", logsDir, err)
	}
	if err := os.Symlink(filepath.Join(rayRoot, sessionID), filepath.Join(rayRoot, "session_latest")); err != nil {
		t.Fatalf("Symlink() = %v", err)
	}
	return logsDir
}

func mustRotatedName(t *testing.T, backupName string, inode uint64, size int) string {
	t.Helper()
	name, ok := rotatedLogName(backupName, inode, int64(size))
	if !ok {
		t.Fatalf("rotatedLogName(%q) reported no rotation backup", backupName)
	}
	return name
}

func assertWritten(t *testing.T, writer *MockStorageWriter, want map[string]string) {
	t.Helper()
	got := writer.written()
	if len(got) != len(want) {
		t.Fatalf("wrote objects %v, want %v", keysOf(got), keysOf(want))
	}
	for name, content := range want {
		if got[name] != content {
			t.Fatalf("object %q = %q, want %q (wrote %v)", name, got[name], content, keysOf(got))
		}
	}
}

func keysOf(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}
