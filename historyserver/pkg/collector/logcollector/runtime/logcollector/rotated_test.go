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

// writeLogFile creates path with content and returns its rotated identity.
func writeLogFile(t *testing.T, path, content string) rotatedIdentity {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) = %v", path, err)
	}
	return identityOf(t, path)
}

// openDescriptorCount reports how many descriptors this process holds.
func openDescriptorCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skipf("cannot enumerate open descriptors: %v", err)
	}
	return len(entries)
}

func setModTime(t *testing.T, path string, nanos int64) {
	t.Helper()
	when := time.Unix(0, nanos)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("Chtimes(%s) = %v", path, err)
	}
}

func identityOf(t *testing.T, path string) rotatedIdentity {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) = %v", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("Stat(%s): inode unavailable on this platform", path)
	}
	return rotatedIdentity{inode: stat.Ino, size: info.Size(), modTimeNs: info.ModTime().UnixNano()}
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
		name string
		id   rotatedIdentity
		want string
	}{
		"worker stdout": {
			name: "worker-abc123-01000000-123.out.1",
			id:   rotatedIdentity{inode: 4390125, size: 1048576, modTimeNs: 1788398123456789012},
			want: "worker-abc123-01000000-123.rotated.4390125-1048576-1788398123456789012.out",
		},
		"worker stderr": {
			name: "worker-abc123-01000000-123.err.5",
			id:   rotatedIdentity{inode: 4390125, size: 64, modTimeNs: 17},
			want: "worker-abc123-01000000-123.rotated.4390125-64-17.err",
		},
		"component stdout": {
			name: "raylet.out.2",
			id:   rotatedIdentity{inode: 4390126, size: 2048, modTimeNs: 99},
			want: "raylet.rotated.4390126-2048-99.out",
		},
		"dot log": {
			name: "python-core-worker-abc_123.log.3",
			id:   rotatedIdentity{inode: 7, size: 9, modTimeNs: 11},
			want: "python-core-worker-abc_123.rotated.7-9-11.log",
		},
		"no extension": {
			name: "raylet.4",
			id:   rotatedIdentity{inode: 11, size: 12, modTimeNs: 13},
			want: "raylet.rotated.11-12-13",
		},
		"dotfile base": {
			name: ".out.1",
			id:   rotatedIdentity{inode: 13, size: 14, modTimeNs: 15},
			want: ".out.rotated.13-14-15",
		},
		"not a rotation backup": {name: "raylet.out", id: rotatedIdentity{inode: 1, size: 1, modTimeNs: 1}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := rotatedLogName(test.name, test.id)
			if got != test.want || ok != (test.want != "") {
				t.Fatalf("rotatedLogName(%q, %v) = (%q, %v), want %q", test.name, test.id, got, ok, test.want)
			}
		})
	}
}

// Ray rotates at a byte threshold so generations of one stream repeat their
// size, and Linux reuses the inode of a generation it drops from the ring.
// Without the modification time those two would share an object name.
func TestRotatedLogNameSeparatesReusedInodeAndSize(t *testing.T) {
	const backupName = "worker-abc123-01000000-123.out.1"
	first := rotatedIdentity{inode: 4390125, size: 65536, modTimeNs: 1788398100000000000}
	reused := rotatedIdentity{inode: first.inode, size: first.size, modTimeNs: 1788398200000000000}

	firstName, _ := rotatedLogName(backupName, first)
	reusedName, _ := rotatedLogName(backupName, reused)
	if firstName == reusedName {
		t.Fatalf("inode and size reuse collides on %q", firstName)
	}
}

// One generation keeps its identity as Ray renames it down the ring, so every
// index must map to a single object name.
func TestRotatedLogNameIsStableAcrossRotationIndex(t *testing.T) {
	id := rotatedIdentity{inode: 4390125, size: 65536, modTimeNs: 1788398100000000000}
	want := "raylet.rotated.4390125-65536-1788398100000000000.out"
	for _, backupName := range []string{"raylet.out.1", "raylet.out.2", "raylet.out.3"} {
		if got, _ := rotatedLogName(backupName, id); got != want {
			t.Fatalf("rotatedLogName(%q) = %q, want %q", backupName, got, want)
		}
	}
}

func TestCollectRotatedLogUploadsDeterministicObject(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	backup := filepath.Join(logsDir, "raylet.out.1")
	id := writeLogFile(t, backup, "rotated raylet")
	nested := filepath.Join(logsDir, "old", "worker-abc123-01000000-123.err.2")
	nestedID := writeLogFile(t, nested, "rotated worker")
	active := filepath.Join(logsDir, "raylet.out")
	writeLogFile(t, active, "active raylet")

	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	want := map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", id):                                    "rotated raylet",
		testLogPrefix + "old/" + mustRotatedName(t, "worker-abc123-01000000-123.err.2", nestedID): "rotated worker",
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
	id := writeLogFile(t, first, "generation one")
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	second := filepath.Join(logsDir, "raylet.out.2")
	if err := os.Rename(first, second); err != nil {
		t.Fatalf("Rename() = %v", err)
	}
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", id): "generation one",
	})
}

// Ray reuses .1 for the next generation. Linux commonly hands back the inode of
// the generation it just evicted, and rotation at a byte threshold makes equal
// sizes the norm, so both generations must still reach distinct objects.
func TestCollectRotatedLogUploadsNewGenerationReusingIndex(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	// Equal-length content models rotation at a fixed byte threshold.
	const firstContent, secondContent = "generation one", "generation two"
	backup := filepath.Join(logsDir, "raylet.out.1")

	writeLogFile(t, backup, firstContent)
	setModTime(t, backup, 1788398100000000000)
	firstID := identityOf(t, backup)
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	if err := os.Remove(backup); err != nil {
		t.Fatalf("Remove() = %v", err)
	}
	writeLogFile(t, backup, secondContent)
	setModTime(t, backup, 1788398200000000000)
	secondID := identityOf(t, backup)
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	if firstID.size != secondID.size {
		t.Fatalf("test setup: generations must share a size, got %d and %d", firstID.size, secondID.size)
	}
	if firstID.inode == secondID.inode {
		t.Logf("filesystem reused inode %d, exercising the collision directly", firstID.inode)
	}
	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", firstID):  firstContent,
		testLogPrefix + mustRotatedName(t, "raylet.out.1", secondID): secondContent,
	})
}

// Ray evicts the oldest backup while an earlier upload is still in flight. The
// later generation was opened during discovery, so it must still upload from its
// pinned descriptor rather than be skipped as a lost open race.
func TestCollectRotatedLogsPinsLaterCandidatesDuringSlowUpload(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	// WalkDir visits in lexical order, so a-stream uploads first.
	firstPath := filepath.Join(logsDir, "a-stream.out.1")
	evictedPath := filepath.Join(logsDir, "b-stream.out.1")
	firstID := writeLogFile(t, firstPath, "first generation")
	evictedID := writeLogFile(t, evictedPath, "evicted generation")

	var evictOnce sync.Once
	writer.beforeWrite = func() {
		evictOnce.Do(func() {
			if err := os.Remove(evictedPath); err != nil {
				t.Errorf("Remove() = %v", err)
			}
		})
	}

	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "a-stream.out.1", firstID):   "first generation",
		testLogPrefix + mustRotatedName(t, "b-stream.out.1", evictedID): "evicted generation",
	})
}

// A candidate that disappears before it can be opened is an ordinary rotation
// race and must not stop the rest of the scan.
func TestCollectRotatedLogsContinuesAfterLostOpenRace(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	survivorID := writeLogFile(t, filepath.Join(logsDir, "b-stream.out.1"), "survivor")
	lostPath := filepath.Join(logsDir, "a-stream.out.1")
	writeLogFile(t, lostPath, "lost to the ring")
	if err := os.Remove(lostPath); err != nil {
		t.Fatalf("Remove() = %v", err)
	}

	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "b-stream.out.1", survivorID): "survivor",
	})
}

// Descriptors must not leak on the success, failure or already-uploaded paths.
func TestCollectRotatedLogsClosesEveryDescriptor(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	writeLogFile(t, filepath.Join(logsDir, "a-stream.out.1"), "uploaded")
	writeLogFile(t, filepath.Join(logsDir, "b-stream.out.1"), "upload fails")

	before := openDescriptorCount(t)

	// Success and failure in one pass, then a pass where both are already known.
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)
	writer.setWriteErr(errors.New("object store unavailable"))
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)
	writer.setWriteErr(nil)
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)

	if after := openDescriptorCount(t); after > before {
		t.Fatalf("open descriptors grew from %d to %d", before, after)
	}
}

func TestCollectRotatedLogRetriesAfterWriteFailure(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	writer.setWriteErr(errors.New("object store unavailable"))
	handler := newRotatedTestHandler(writer)

	backup := filepath.Join(logsDir, "raylet.out.1")
	id := writeLogFile(t, backup, "rotated raylet")

	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)
	assertWritten(t, writer, map[string]string{})

	writer.setWriteErr(nil)
	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)
	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", id): "rotated raylet",
	})
}

// The upload must keep reading from the descriptor it opened, even once Ray has
// removed the path it came from.
func TestCollectRotatedLogReadsThroughUnlinkedPath(t *testing.T) {
	logsDir := t.TempDir()
	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)

	backup := filepath.Join(logsDir, "raylet.out.1")
	id := writeLogFile(t, backup, "rotated raylet")
	writer.beforeWrite = func() {
		if err := os.Remove(backup); err != nil {
			t.Errorf("Remove() = %v", err)
		}
	}

	handler.collectRotatedLogsUnder(logsDir, testSessionID, testNodeID)
	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", id): "rotated raylet",
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
	id := writeLogFile(t, backup, "rotated raylet")

	handler.collectRotatedLogsUnder(logsDir, "session-old", "node-old")
	handler.collectRotatedLogsUnder(logsDir, "session-new", "node-new")

	name := mustRotatedName(t, "raylet.out.1", id)
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
	id := writeLogFile(t, backup, "rotated raylet")
	want := map[string]string{testLogPrefix + mustRotatedName(t, "raylet.out.1", id): "rotated raylet"}

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
	id := writeLogFile(t, backup, "rotated raylet")

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
		testLogPrefix + mustRotatedName(t, "raylet.out.1", id): "rotated raylet",
	})
}

// The first scan must not wait for the ticker: a collector restarting under a
// live Ray node would otherwise miss a whole interval of rotations.
func TestScanRotatedLogsScansBeforeFirstTick(t *testing.T) {
	rayRoot := t.TempDir()
	t.Setenv("RAY_TMP_ROOT", rayRoot)
	logsDir := linkSessionLatest(t, rayRoot, testSessionID)
	id := writeLogFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated raylet")

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
		testLogPrefix + mustRotatedName(t, "raylet.out.1", id): "rotated raylet",
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

	id := writeLogFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated raylet")
	writeLogFile(t, filepath.Join(logsDir, "raylet.out"), "active raylet")

	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)
	handler.processSessionLatestLogs()

	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", id): "rotated raylet",
		testLogPrefix + "raylet.out":                           "active raylet",
	})
}

func TestProcessSessionLatestLogsSkipsAlreadyUploadedRotation(t *testing.T) {
	rayRoot := t.TempDir()
	t.Setenv("RAY_TMP_ROOT", rayRoot)
	logsDir := linkSessionLatest(t, rayRoot, testSessionID)
	id := writeLogFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated raylet")

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
		testLogPrefix + mustRotatedName(t, "raylet.out.1", id): "rotated raylet",
	})
}

func TestProcessPrevLogsDirUsesRotatedName(t *testing.T) {
	rayRoot := t.TempDir()
	t.Setenv("RAY_TMP_ROOT", rayRoot)

	nodeDir := filepath.Join(rayRoot, "prev-logs", testSessionID, testNodeID)
	logsDir := filepath.Join(nodeDir, utils.RAY_SESSIONDIR_LOGDIR_NAME)
	id := writeLogFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated raylet")
	writeLogFile(t, filepath.Join(logsDir, "raylet.out"), "active raylet")

	writer := NewMockStorageWriter()
	handler := newRotatedTestHandler(writer)
	handler.prevLogsDir = utils.GetRayPrevLogsPath()
	handler.persistCompleteLogsDir = utils.GetRayPersistCompletePath()
	handler.processPrevLogsDir(nodeDir)

	assertWritten(t, writer, map[string]string{
		testLogPrefix + mustRotatedName(t, "raylet.out.1", id): "rotated raylet",
		testLogPrefix + "raylet.out":                           "active raylet",
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

func mustRotatedName(t *testing.T, backupName string, id rotatedIdentity) string {
	t.Helper()
	name, ok := rotatedLogName(backupName, id)
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
