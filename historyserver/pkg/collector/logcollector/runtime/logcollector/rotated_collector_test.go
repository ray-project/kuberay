package logcollector

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// fakeWatcher lets tests deliver events exactly when they choose. Everything else in
// these tests is a real directory, a real file and a real hard link.
type fakeWatcher struct {
	mu      sync.Mutex
	added   []string
	closed  bool
	failAdd map[string]error // path -> error returned by Add

	events chan fsnotify.Event
	errs   chan error
}

func newFakeWatcher() *fakeWatcher { return newFakeWatcherBuffered(0) }

// newFakeWatcherBuffered mirrors fsnotify's buffered queue. Most tests use an
// unbuffered channel so that delivering an event and then round-tripping a snapshot
// proves the event was handled; buffering is only for tests that must queue events
// while the loop is busy.
func newFakeWatcherBuffered(n int) *fakeWatcher {
	return &fakeWatcher{
		events: make(chan fsnotify.Event, n),
		errs:   make(chan error, n),
	}
}

func (w *fakeWatcher) Add(name string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err, ok := w.failAdd[name]; ok {
		return err
	}
	w.added = append(w.added, name)
	return nil
}

func (w *fakeWatcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

func (w *fakeWatcher) Events() <-chan fsnotify.Event { return w.events }
func (w *fakeWatcher) Errors() <-chan error          { return w.errs }

func (w *fakeWatcher) watched() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.added...)
}

func (w *fakeWatcher) isClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// issueLog records what the collector reported instead of failing on.
type issueLog struct {
	mu     sync.Mutex
	issues []error
}

func (l *issueLog) add(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.issues = append(l.issues, err)
}

func (l *issueLog) all() []error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]error(nil), l.issues...)
}

func (l *issueLog) matching(substr string) []error {
	var out []error
	for _, err := range l.all() {
		if strings.Contains(err.Error(), substr) {
			out = append(out, err)
		}
	}
	return out
}

// harness is one collector running against real temp directories.
type harness struct {
	rc          *rotatedCollector
	watcher     *fakeWatcher
	tick        chan time.Time
	issues      *issueLog
	logsDir     string
	stagingRoot string
	runErr      chan error
}

// start builds and runs a collector. It returns once startup reconstruction and the
// startup scan have finished, because the first snapshot round-trip is only served
// after the owner loop reaches its select.
func start(t *testing.T, dir string) *harness {
	t.Helper()
	return startWith(t, dir, func(*rotatedCollectorConfig) {})
}

func startWith(t *testing.T, dir string, tweak func(*rotatedCollectorConfig)) *harness {
	t.Helper()
	logsDir := filepath.Join(dir, "session", "logs")
	if err := os.MkdirAll(logsDir, 0o750); err != nil {
		t.Fatalf("create logs dir: %v", err)
	}

	h := &harness{
		watcher:     newFakeWatcher(),
		tick:        make(chan time.Time),
		issues:      &issueLog{},
		logsDir:     logsDir,
		stagingRoot: filepath.Join(dir, "rotated-staging"),
		runErr:      make(chan error, 1),
	}

	cfg := rotatedCollectorConfig{
		LogsDir:     logsDir,
		StagingRoot: h.stagingRoot,
		SessionName: "session-1",
		NodeName:    "node-1",
		NewWatcher:  func() (fsWatcher, error) { return h.watcher, nil },
		NewTicker:   func(time.Duration) (<-chan time.Time, func()) { return h.tick, func() {} },
		OnIssue:     h.issues.add,
	}
	tweak(&cfg)

	rc, err := newRotatedCollector(cfg)
	if err != nil {
		t.Fatalf("newRotatedCollector() error: %v", err)
	}
	h.rc = rc

	go func() { h.runErr <- rc.Run() }()
	t.Cleanup(func() { rc.Stop() })

	rc.snapshot() // wait for startup to finish
	return h
}

// sendEvent delivers an event and returns once the loop has finished handling it.
func (h *harness) sendEvent(t *testing.T, name string) {
	t.Helper()
	select {
	case h.watcher.events <- fsnotify.Event{Name: name, Op: fsnotify.Create}:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out delivering event")
	}
	h.rc.snapshot()
}

// fireTick runs one periodic reconciliation and waits for it to complete.
func (h *harness) fireTick(t *testing.T) {
	t.Helper()
	select {
	case h.tick <- time.Now():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out firing tick")
	}
	h.rc.snapshot()
}

func (h *harness) writeLog(t *testing.T, rel, content string) string {
	t.Helper()
	path := filepath.Join(h.logsDir, filepath.FromSlash(rel))
	writeFile(t, path, content)
	return path
}

func (h *harness) stagedPaths(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(h.stagingRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			rel, relErr := filepath.Rel(h.stagingRoot, p)
			if relErr != nil {
				return relErr
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk staging root: %v", err)
	}
	return out
}

func TestCollectorStartupWatchesTreeAndCapturesExistingBackups(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	writeFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated")
	writeFile(t, filepath.Join(logsDir, "events", "event.log"), "active nested")
	writeFile(t, filepath.Join(logsDir, "events", "event.log.2"), "rotated nested")

	h := start(t, dir)

	// 1. Every directory in the tree is watched.
	watched := h.watcher.watched()
	for _, want := range []string{logsDir, filepath.Join(logsDir, "events")} {
		found := false
		for _, got := range watched {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("directory %s was not watched, watched = %v", want, watched)
		}
	}

	// 3. Backups that already existed are captured by the startup scan.
	entries := h.rc.snapshot()
	if len(entries) != 2 {
		t.Fatalf("captured %d segments, want 2: %+v", len(entries), entries)
	}
	byName := map[string]stagedEntry{}
	for _, e := range entries {
		byName[e.OriginalName] = e
	}
	if e, ok := byName["raylet.out.1"]; !ok || e.RelDir != "" {
		t.Errorf("raylet.out.1 captured as %+v", e)
	}
	if e, ok := byName["event.log.2"]; !ok || e.RelDir != "events" {
		t.Errorf("event.log.2 captured as %+v (want RelDir \"events\")", e)
	}
	for _, e := range entries {
		if e.State != statePending {
			t.Errorf("capture %s state = %q, want %q", e.CaptureID, e.State, statePending)
		}
		if _, err := os.Lstat(e.path(h.stagingRoot)); err != nil {
			t.Errorf("staged link missing for %s: %v", e.OriginalName, err)
		}
	}
}

func TestCollectorRemembersActiveBases(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	// The active file exists at startup and is recorded as a base...
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	h := start(t, dir)

	// ...then a rotation cascade briefly leaves the active name unlinked while the
	// backup appears. Without the remembered base this would look like an unrelated
	// file ending in ".1" and be skipped.
	if err := os.Remove(filepath.Join(logsDir, "raylet.out")); err != nil {
		t.Fatalf("remove active file: %v", err)
	}
	backup := h.writeLog(t, "raylet.out.1", "rotated")
	h.sendEvent(t, backup)

	// A file with no base, remembered or present, stays untouched.
	unrelated := h.writeLog(t, "user-data.1", "not a ray log")
	h.sendEvent(t, unrelated)

	entries := h.rc.snapshot()
	if len(entries) != 1 {
		t.Fatalf("captured %d segments, want only the rotation backup: %+v", len(entries), entries)
	}
	if entries[0].OriginalName != "raylet.out.1" {
		t.Errorf("captured %q, want raylet.out.1", entries[0].OriginalName)
	}
}

func TestCollectorCapturesBeforeSourceIsDeleted(t *testing.T) {
	dir := t.TempDir()
	h := start(t, dir)
	h.writeLog(t, "raylet.out", "active")

	const content = "the segment rotation is about to delete"
	backup := h.writeLog(t, "raylet.out.1", content)
	h.sendEvent(t, backup)

	// Rotation deletes it immediately afterwards; the captured bytes must survive.
	if err := os.Remove(backup); err != nil {
		t.Fatalf("remove source: %v", err)
	}

	entries := h.rc.snapshot()
	if len(entries) != 1 {
		t.Fatalf("captured %d segments, want 1", len(entries))
	}
	got, err := os.ReadFile(entries[0].path(h.stagingRoot))
	if err != nil {
		t.Fatalf("read staged capture: %v", err)
	}
	if string(got) != content {
		t.Errorf("staged content = %q, want %q", got, content)
	}
}

func TestCollectorWatchesAndScansNewDirectory(t *testing.T) {
	dir := t.TempDir()
	h := start(t, dir)

	// Ray creates a subdirectory that already contains an active file and a backup.
	// The watch cannot report what was there before it existed, so the scan that
	// follows the watch is what finds them.
	h.writeLog(t, "serve/replica.log", "active")
	h.writeLog(t, "serve/replica.log.1", "rotated")
	h.sendEvent(t, filepath.Join(h.logsDir, "serve"))

	watchedServe := false
	for _, got := range h.watcher.watched() {
		if got == filepath.Join(h.logsDir, "serve") {
			watchedServe = true
		}
	}
	if !watchedServe {
		t.Errorf("new directory was not watched, watched = %v", h.watcher.watched())
	}

	entries := h.rc.snapshot()
	if len(entries) != 1 || entries[0].OriginalName != "replica.log.1" || entries[0].RelDir != "serve" {
		t.Fatalf("captured %+v, want replica.log.1 under serve", entries)
	}
}

func TestCollectorCapturesOneSegmentAcrossRotationIndexes(t *testing.T) {
	dir := t.TempDir()
	h := start(t, dir)
	h.writeLog(t, "raylet.out", "active")

	first := h.writeLog(t, "raylet.out.1", "generation one")
	h.sendEvent(t, first)
	afterFirst := h.rc.snapshot()
	if len(afterFirst) != 1 {
		t.Fatalf("captured %d segments, want 1", len(afterFirst))
	}

	// The next rotation renames the same physical file to .2. It is the same pinned
	// inode, so it must not become a second capture.
	second := filepath.Join(h.logsDir, "raylet.out.2")
	if err := os.Rename(first, second); err != nil {
		t.Fatalf("rotate to .2: %v", err)
	}
	h.sendEvent(t, second)

	entries := h.rc.snapshot()
	if len(entries) != 1 {
		t.Fatalf("captured %d segments after rotation, want 1: %+v", len(entries), entries)
	}
	if entries[0].CaptureID != afterFirst[0].CaptureID {
		t.Errorf("capture ID changed across rotation: %q -> %q", afterFirst[0].CaptureID, entries[0].CaptureID)
	}
	if staged := h.stagedPaths(t); len(staged) != 1 {
		t.Errorf("staging holds %v, want one link", staged)
	}
}

func TestCollectorCapturesEachGenerationAtTheSamePath(t *testing.T) {
	dir := t.TempDir()
	h := start(t, dir)
	h.writeLog(t, "raylet.out", "active")

	backup := h.writeLog(t, "raylet.out.1", "generation one")
	h.sendEvent(t, backup)

	// Rotation deletes that segment and a later one takes the same name. Our link
	// keeps the first inode alive, so the second file is necessarily a new inode.
	if err := os.Remove(backup); err != nil {
		t.Fatalf("remove first generation: %v", err)
	}
	backup = h.writeLog(t, "raylet.out.1", "generation two")
	h.sendEvent(t, backup)

	entries := h.rc.snapshot()
	if len(entries) != 2 {
		t.Fatalf("captured %d segments, want 2: %+v", len(entries), entries)
	}
	if entries[0].CaptureID == entries[1].CaptureID {
		t.Error("both generations share a capture ID")
	}
	identity := clusterIdentity{RootDir: "root", Namespace: "default", ClusterName: "c"}
	if entries[0].objectKey(identity) == entries[1].objectKey(identity) {
		t.Error("both generations map to one object key")
	}
	contents := map[string]bool{}
	for _, e := range entries {
		data, err := os.ReadFile(e.path(h.stagingRoot))
		if err != nil {
			t.Fatalf("read staged capture: %v", err)
		}
		contents[string(data)] = true
	}
	for _, want := range []string{"generation one", "generation two"} {
		if !contents[want] {
			t.Errorf("staged captures %v do not include %q", contents, want)
		}
	}
}

func TestCollectorReconstructsStagingWithoutNewCaptureIDs(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")

	// A previous run staged one pending and one uploaded capture.
	prior := newCaptureIndex()
	ids := newCaptureIDGenerator()
	stage := func(name, content string, promote bool) stagedEntry {
		t.Helper()
		src := filepath.Join(logsDir, name)
		writeFile(t, src, content)
		id, err := ids.next()
		if err != nil {
			t.Fatalf("next() error: %v", err)
		}
		entry, err := newStagedEntry(statePending, "session-1", "node-1", "", name, id)
		if err != nil {
			t.Fatalf("newStagedEntry() error: %v", err)
		}
		if err := captureLink(src, entry.path(stagingRoot)); err != nil {
			t.Fatalf("captureLink() error: %v", err)
		}
		key, _, err := statInode(entry.path(stagingRoot))
		if err != nil {
			t.Fatalf("statInode() error: %v", err)
		}
		if _, _, err := prior.add(key, entry); err != nil {
			t.Fatalf("add() error: %v", err)
		}
		if promote {
			entry, err = promoteCapture(stagingRoot, prior, key)
			if err != nil {
				t.Fatalf("promoteCapture() error: %v", err)
			}
		}
		return entry
	}
	pending := stage("raylet.out.1", "pending segment", false)
	uploaded := stage("raylet.out.2", "uploaded segment", true)
	before := stagingFiles(t, stagingRoot)

	h := start(t, dir)

	entries := h.rc.snapshot()
	if len(entries) != 2 {
		t.Fatalf("reconstructed %d captures, want 2: %+v", len(entries), entries)
	}
	got := map[string]stagedEntry{}
	for _, e := range entries {
		got[e.CaptureID] = e
	}
	for _, want := range []stagedEntry{pending, uploaded} {
		e, ok := got[want.CaptureID]
		if !ok {
			t.Fatalf("capture %s was not reconstructed (a new ID was minted)", want.CaptureID)
		}
		if e != want {
			t.Errorf("reconstructed %+v, want %+v", e, want)
		}
	}
	// The startup scan sees the same source files, but their inodes are already
	// tracked, so nothing is staged twice.
	if after := h.stagedPaths(t); len(after) != len(before) {
		t.Errorf("staging changed during reconstruction: %v -> %v", before, after)
	}
}

// stagingFiles lists staging paths before a collector exists.
func stagingFiles(t *testing.T, stagingRoot string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(stagingRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, relErr := filepath.Rel(stagingRoot, p)
			if relErr != nil {
				return relErr
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk staging root: %v", err)
	}
	return out
}

func TestCollectorReportsConflictingStagedRecords(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	src := filepath.Join(logsDir, "raylet.out.1")
	writeFile(t, src, "one segment")

	// A corrupt staging tree: one inode recorded under two capture IDs.
	for _, id := range []string{"0001780000000000000.aaaaaaaaaaaaaaaa", "0001780000000000001.bbbbbbbbbbbbbbbb"} {
		entry, err := newStagedEntry(statePending, "session-1", "node-1", "", "raylet.out.1", id)
		if err != nil {
			t.Fatalf("newStagedEntry() error: %v", err)
		}
		if err := captureLink(src, entry.path(stagingRoot)); err != nil {
			t.Fatalf("captureLink() error: %v", err)
		}
	}
	// ...plus a file that is not a staging record at all.
	writeFile(t, filepath.Join(stagingRoot, "session-1", "node-1", "pending", "garbage.txt"), "junk")

	h := start(t, dir)

	if entries := h.rc.snapshot(); len(entries) != 1 {
		t.Fatalf("index holds %d captures, want exactly one of the conflicting records: %+v", len(entries), entries)
	}
	if got := h.issues.matching("surplus record"); len(got) == 0 {
		t.Errorf("conflicting staging record was not reported, issues = %v", h.issues.all())
	}
	// Exactly one link may remain for the inode, otherwise the surplus one would
	// pin it forever and release could never free the segment.
	if _, nlink, err := statInode(src); err != nil || nlink != 2 {
		t.Errorf("link count = %d (err %v), want 2 (the source and one staged link)", nlink, err)
	}
	if got := h.issues.matching("garbage.txt"); len(got) == 0 {
		t.Errorf("malformed staging record was not reported, issues = %v", h.issues.all())
	}
}

func TestCollectorPeriodicReconciliationCatchesMissedEvents(t *testing.T) {
	dir := t.TempDir()
	h := start(t, dir)
	h.writeLog(t, "raylet.out", "active")

	// No event is delivered for this file at all: the sweep is the only thing that
	// can find it.
	h.writeLog(t, "raylet.out.1", "missed by fsnotify")
	if entries := h.rc.snapshot(); len(entries) != 0 {
		t.Fatalf("captured %d segments before reconciliation, want 0", len(entries))
	}

	h.fireTick(t)

	if entries := h.rc.snapshot(); len(entries) != 1 {
		t.Fatalf("captured %d segments after reconciliation, want 1", len(entries))
	}
}

func TestCollectorReconcilesImmediatelyAfterOverflow(t *testing.T) {
	dir := t.TempDir()
	h := start(t, dir)
	h.writeLog(t, "raylet.out", "active")
	h.writeLog(t, "raylet.out.1", "dropped event")

	select {
	case h.watcher.errs <- fsnotify.ErrEventOverflow:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out delivering overflow")
	}
	h.rc.snapshot() // round-trip: the loop has finished reconciling

	if entries := h.rc.snapshot(); len(entries) != 1 {
		t.Fatalf("captured %d segments after overflow, want 1", len(entries))
	}
	if got := h.issues.matching("overflow"); len(got) == 0 {
		t.Errorf("overflow was not reported, issues = %v", h.issues.all())
	}
}

func TestCollectorKeepsRunningAfterWatcherError(t *testing.T) {
	dir := t.TempDir()
	h := start(t, dir)

	select {
	case h.watcher.errs <- os.ErrPermission:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out delivering watcher error")
	}

	// The loop must still serve requests and still capture.
	h.writeLog(t, "raylet.out", "active")
	backup := h.writeLog(t, "raylet.out.1", "rotated")
	h.sendEvent(t, backup)
	if entries := h.rc.snapshot(); len(entries) != 1 {
		t.Fatalf("collector stopped working after a watcher error: %+v", entries)
	}
}

func TestCollectorSurvivesCaptureFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions do not prevent staging writes")
	}
	dir := t.TempDir()
	// A staging root that cannot be written to stands in for any capture failure
	// the deployment can produce, such as EXDEV or EPERM from os.Link.
	blocked := filepath.Join(dir, "blocked")
	if err := os.MkdirAll(blocked, 0o500); err != nil {
		t.Fatalf("create blocked staging root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o750) })

	h := startWith(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.StagingRoot = filepath.Join(blocked, "rotated-staging")
	})

	h.writeLog(t, "raylet.out", "active")
	backup := h.writeLog(t, "raylet.out.1", "rotated")
	h.sendEvent(t, backup)

	if entries := h.rc.snapshot(); len(entries) != 0 {
		t.Errorf("failed capture was registered anyway: %+v", entries)
	}
	if len(h.issues.all()) == 0 {
		t.Error("capture failure was not reported")
	}

	// The loop is still alive and still discovering.
	select {
	case err := <-h.runErr:
		t.Fatalf("collector exited after a capture failure: %v", err)
	default:
	}
	h.fireTick(t)
}

func TestCollectorIgnoresVanishedAndOutOfTreePaths(t *testing.T) {
	dir := t.TempDir()
	h := start(t, dir)

	// A file that rotation removed between the event and our stat.
	h.sendEvent(t, filepath.Join(h.logsDir, "raylet.out.1"))
	// A path that is not under the active logs tree at all.
	h.sendEvent(t, filepath.Join(dir, "elsewhere", "raylet.out.1"))

	if entries := h.rc.snapshot(); len(entries) != 0 {
		t.Errorf("captured %+v, want nothing", entries)
	}
	if issues := h.issues.all(); len(issues) != 0 {
		t.Errorf("expected races and foreign paths to be silent, got %v", issues)
	}
}

func TestCollectorDoesNotFollowSymlinkedDirectories(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	writeFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated")
	// A symlink pointing back at the tree would make a naive walk recurse forever.
	if err := os.Symlink(logsDir, filepath.Join(logsDir, "loop")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	// A symlinked file is not a log Ray rotated either.
	if err := os.Symlink(filepath.Join(logsDir, "raylet.out.1"), filepath.Join(logsDir, "alias.out.1")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	h := start(t, dir) // would hang or overflow the stack if symlinks were followed

	for _, watched := range h.watcher.watched() {
		if strings.Contains(watched, "loop") {
			t.Errorf("symlinked directory was watched: %s", watched)
		}
	}
	entries := h.rc.snapshot()
	if len(entries) != 1 || entries[0].OriginalName != "raylet.out.1" {
		t.Errorf("captured %+v, want only the real backup", entries)
	}
}

func TestRegisterStagedRollsBackOnRegistrationFailure(t *testing.T) {
	dir := t.TempDir()
	stagingRoot := filepath.Join(dir, "rotated-staging")
	src := filepath.Join(dir, "logs", "raylet.out.1")
	writeFile(t, src, "rotated")

	ix := newCaptureIndex()
	first, err := newStagedEntry(statePending, "session-1", "node-1", "", "raylet.out.1", "0001780000000000000.aaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	if err := captureLink(src, first.path(stagingRoot)); err != nil {
		t.Fatalf("captureLink() error: %v", err)
	}
	key, _, err := statInode(first.path(stagingRoot))
	if err != nil {
		t.Fatalf("statInode() error: %v", err)
	}
	if err := registerStaged(stagingRoot, ix, key, first); err != nil {
		t.Fatalf("registerStaged() error: %v", err)
	}

	// A second capture ID for an inode that is already registered: the link must be
	// undone, or it would pin blocks that nothing ever releases.
	second, err := newStagedEntry(statePending, "session-1", "node-1", "", "raylet.out.1", "0001780000000000001.bbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	if err := captureLink(src, second.path(stagingRoot)); err != nil {
		t.Fatalf("captureLink() error: %v", err)
	}

	if err := registerStaged(stagingRoot, ix, key, second); err == nil {
		t.Fatal("registerStaged() accepted a duplicate inode")
	}
	if _, err := os.Lstat(second.path(stagingRoot)); !isVanished(err) {
		t.Errorf("the rejected capture's staging link was left behind: %v", err)
	}
	if _, err := os.Lstat(first.path(stagingRoot)); err != nil {
		t.Errorf("rollback removed the wrong link: %v", err)
	}
	if ix.len() != 1 {
		t.Errorf("index holds %d captures, want 1", ix.len())
	}
}

func TestCollectorCaptureIsNotBlockedByOtherWork(t *testing.T) {
	// The loop performs no storage calls, so discovery latency depends only on the
	// filesystem. Capturing a burst of segments must be prompt.
	dir := t.TempDir()
	h := start(t, dir)
	h.writeLog(t, "raylet.out", "active")

	const segments = 50
	for i := range segments {
		h.writeLog(t, filepath.Join("burst", "worker.out."+strconv.Itoa(i+1)), "segment")
	}
	h.writeLog(t, "burst/worker.out", "active")

	deadline := time.Now()
	h.fireTick(t)
	elapsed := time.Since(deadline)

	entries := h.rc.snapshot()
	if len(entries) != segments {
		t.Fatalf("captured %d of %d segments", len(entries), segments)
	}
	if elapsed > 5*time.Second {
		t.Errorf("capturing %d segments took %v, which suggests blocking work on the loop", segments, elapsed)
	}
}

func TestCollectorStateIsOnlyTouchedByTheOwnerLoop(t *testing.T) {
	// Concurrent readers go through the loop's request channel rather than the
	// index. Under -race this proves there is no unsynchronised access.
	dir := t.TempDir()
	h := start(t, dir)
	h.writeLog(t, "raylet.out", "active")

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				h.rc.snapshot()
			}
		})
	}
	for i := range 20 {
		h.writeLog(t, "raylet.out."+strconv.Itoa(i+1), "segment")
	}
	h.rc.reconcileNow()
	wg.Wait()

	if entries := h.rc.snapshot(); len(entries) != 20 {
		t.Errorf("captured %d segments, want 20", len(entries))
	}
}

func TestCollectorStopClosesWatcherAndExits(t *testing.T) {
	before := runtime.NumGoroutine()
	dir := t.TempDir()
	h := start(t, dir)

	h.rc.Stop()
	h.rc.Stop() // idempotent

	select {
	case err := <-h.runErr:
		if err != nil {
			t.Errorf("Run() returned %v, want nil on a deliberate stop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after Stop()")
	}
	if !h.watcher.isClosed() {
		t.Error("Stop() did not close the watcher")
	}
	if entries := h.rc.snapshot(); entries != nil {
		t.Errorf("snapshot() after Stop() = %+v, want nil", entries)
	}

	// Goroutines settle asynchronously; give the runtime a moment before comparing.
	for range 20 {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("goroutines leaked: %d before, %d after", before, runtime.NumGoroutine())
}

func TestCollectorWithRealWatcher(t *testing.T) {
	// One end-to-end run against the real fsnotify adapter, so the interface seam
	// used by every other test is known to match the kernel's behavior.
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	if err := os.MkdirAll(logsDir, 0o750); err != nil {
		t.Fatalf("create logs dir: %v", err)
	}
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")

	rc, err := newRotatedCollector(rotatedCollectorConfig{
		LogsDir:     logsDir,
		StagingRoot: filepath.Join(dir, "rotated-staging"),
		SessionName: "session-1",
		NodeName:    "node-1",
		NewTicker:   func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} },
		OnIssue:     func(error) {},
	})
	if err != nil {
		t.Fatalf("newRotatedCollector() error: %v", err)
	}
	runErr := make(chan error, 1)
	go func() { runErr <- rc.Run() }()
	t.Cleanup(func() { rc.Stop() })
	rc.snapshot()

	// Rotation: the active file is renamed to .1, which inotify reports as a create.
	if err := os.Rename(filepath.Join(logsDir, "raylet.out"), filepath.Join(logsDir, "raylet.out.1")); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		entries := rc.snapshot()
		if len(entries) == 1 && entries[0].OriginalName == "raylet.out.1" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("real watcher did not lead to a capture, entries = %+v", entries)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// replaceBeforeLink returns a Link function that swaps the source path for a brand
// new file just before the hard link is created. That is exactly what a rotation
// cascade does to a reused name like "raylet.out.1", and doing it inside the seam
// makes the race deterministic instead of timing-dependent.
func replaceBeforeLink(t *testing.T, target, content string) func(string, string) error {
	t.Helper()
	var once sync.Once
	return func(src, dst string) error {
		once.Do(func() {
			if src != target {
				return
			}
			if err := os.Remove(src); err != nil {
				t.Errorf("replace source: %v", err)
				return
			}
			writeFile(t, src, content)
		})
		return captureLink(src, dst)
	}
}

func TestCollectorPinsTheInodeItActuallyLinked(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	backup := filepath.Join(logsDir, "raylet.out.1")
	writeFile(t, backup, "generation A")

	h := startWith(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.Link = replaceBeforeLink(t, backup, "generation B")
	})

	entries := h.rc.snapshot()
	if len(entries) != 1 {
		t.Fatalf("captured %d segments, want 1: %+v", len(entries), entries)
	}

	// The link pinned whatever the path named at link time, so the index must hold
	// that file — not the one that was validated a moment earlier.
	staged := entries[0].path(h.stagingRoot)
	got, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("read staged capture: %v", err)
	}
	if string(got) != "generation B" {
		t.Errorf("staged content = %q, want the file that was actually linked", got)
	}

	stagedKey, _, err := statInode(staged)
	if err != nil {
		t.Fatalf("statInode(staged) error: %v", err)
	}
	liveKey, _, err := statInode(backup)
	if err != nil {
		t.Fatalf("statInode(source) error: %v", err)
	}
	if stagedKey != liveKey {
		t.Errorf("staged link pinned %s but the source is %s", stagedKey, liveKey)
	}
	// Capturing the same inode again must be recognized as a duplicate, which only
	// works if the index holds the pinned inode rather than the pre-link one.
	h.rc.reconcileNow()
	if after := h.rc.snapshot(); len(after) != 1 {
		t.Errorf("index recorded the wrong inode: reconciliation added %d more captures", len(after)-1)
	}
}

func TestCollectorCapturesGenerationThatReplacedACapturedOne(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	backup := filepath.Join(logsDir, "raylet.out.1")
	writeFile(t, backup, "generation A")

	// Generation A is captured normally by the startup scan.
	h := start(t, dir)
	first := h.rc.snapshot()
	if len(first) != 1 {
		t.Fatalf("captured %d segments at startup, want 1", len(first))
	}

	// Now the path is replaced by a new generation just before the link. A dedup
	// shortcut based on the pre-link inode would decide "already captured" and skip
	// generation B entirely, losing it at the next rotation.
	h.rc.cfg.Link = replaceBeforeLink(t, backup, "generation B")
	h.rc.reconcileNow()

	entries := h.rc.snapshot()
	if len(entries) != 2 {
		t.Fatalf("captured %d segments, want both generations: %+v", len(entries), entries)
	}
	if entries[0].CaptureID == entries[1].CaptureID {
		t.Error("both generations share a capture ID")
	}
	contents := map[string]bool{}
	for _, e := range entries {
		data, err := os.ReadFile(e.path(h.stagingRoot))
		if err != nil {
			t.Fatalf("read staged capture: %v", err)
		}
		contents[string(data)] = true
	}
	for _, want := range []string{"generation A", "generation B"} {
		if !contents[want] {
			t.Errorf("staged captures %v are missing %q", contents, want)
		}
	}
}

func TestCollectorDiscardsSurplusLinkForAnAlreadyCapturedInode(t *testing.T) {
	dir := t.TempDir()
	h := start(t, dir)
	h.writeLog(t, "raylet.out", "active")

	backup := h.writeLog(t, "raylet.out.1", "one segment")
	h.sendEvent(t, backup)
	first := h.rc.snapshot()
	if len(first) != 1 {
		t.Fatalf("captured %d segments, want 1", len(first))
	}

	// A second name for the same physical file: the collector links it, discovers
	// the pinned inode is already tracked, and must drop the surplus link.
	second := filepath.Join(h.logsDir, "raylet.out.2")
	if err := os.Link(backup, second); err != nil {
		t.Fatalf("create second name: %v", err)
	}
	h.sendEvent(t, second)

	entries := h.rc.snapshot()
	if len(entries) != 1 {
		t.Fatalf("index holds %d captures, want 1: %+v", len(entries), entries)
	}
	if entries[0] != first[0] {
		t.Errorf("the original capture changed: %+v -> %+v", first[0], entries[0])
	}
	if staged := h.stagedPaths(t); len(staged) != 1 {
		t.Errorf("staging holds %v, want exactly one link", staged)
	}
	if len(h.issues.all()) != 0 {
		t.Errorf("a duplicate is normal and must not be reported as a problem: %v", h.issues.all())
	}
}

func TestCollectorRejectsNonRegularStagedObject(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	writeFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated")

	// Stand in for a source that became a symlink before it was linked: what ends up
	// at the staging path is not a regular file, so it must not be registered.
	h := startWith(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.Link = func(src, dst string) error {
			if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
				return err
			}
			return os.Symlink(src, dst)
		}
	})

	if entries := h.rc.snapshot(); len(entries) != 0 {
		t.Errorf("registered a non-regular staged object: %+v", entries)
	}
	if staged := h.stagedPaths(t); len(staged) != 0 {
		t.Errorf("staging still holds %v, want the rejected object removed", staged)
	}
	if got := h.issues.matching("not a regular file"); len(got) == 0 {
		t.Errorf("rejection was not reported, issues = %v", h.issues.all())
	}
}

func TestCollectorRemovesLinkWhenPostLinkStatFails(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	writeFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated")

	// The link disappears before it can be read back.
	h := startWith(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.Link = func(string, string) error { return nil }
	})

	if entries := h.rc.snapshot(); len(entries) != 0 {
		t.Errorf("registered a capture whose link could not be read back: %+v", entries)
	}
	if got := h.issues.matching("stat staged capture"); len(got) == 0 {
		t.Errorf("post-link stat failure was not reported, issues = %v", h.issues.all())
	}
}

func TestCollectorIgnoresWriteEvents(t *testing.T) {
	dir := t.TempDir()
	h := start(t, dir)
	active := h.writeLog(t, "raylet.out", "active")

	// A busy Ray node appends constantly. None of that may reach the capture path,
	// or the queue fills and the Create that matters is delayed or dropped.
	for range 200 {
		select {
		case h.watcher.events <- fsnotify.Event{Name: active, Op: fsnotify.Write}:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out delivering write burst: the loop is not draining events")
		}
	}
	h.rc.snapshot() // the loop is still responsive

	if entries := h.rc.snapshot(); len(entries) != 0 {
		t.Fatalf("writes to an active file produced captures: %+v", entries)
	}

	// The Create for a rotation backup is still handled.
	backup := h.writeLog(t, "raylet.out.1", "rotated")
	h.sendEvent(t, backup)
	if entries := h.rc.snapshot(); len(entries) != 1 {
		t.Fatalf("captured %d segments after the write burst, want 1", len(entries))
	}
}

func TestCollectorStagingConflictResolutionIsDeterministic(t *testing.T) {
	// A corrupt staging tree holds a pending and an uploaded record for one inode.
	// Whichever the walk happens to reach first must not decide the outcome: pending
	// always wins, so the data is still guaranteed to be uploaded at least once.
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	src := filepath.Join(logsDir, "raylet.out.1")
	writeFile(t, src, "one segment")

	pending, err := newStagedEntry(statePending, "session-1", "node-1", "", "raylet.out.1", "0001780000000000009.ffffffffffffffff")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	uploaded, err := newStagedEntry(stateUploaded, "session-1", "node-1", "", "raylet.out.1", "0001780000000000000.aaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	// The uploaded record sorts first by capture ID and by directory name, so a
	// walk-order or ID-order rule would pick it.
	for _, e := range []stagedEntry{uploaded, pending} {
		if err := captureLink(src, e.path(stagingRoot)); err != nil {
			t.Fatalf("captureLink() error: %v", err)
		}
	}

	h := start(t, dir)

	entries := h.rc.snapshot()
	if len(entries) != 1 {
		t.Fatalf("index holds %d captures, want 1: %+v", len(entries), entries)
	}
	if entries[0] != pending {
		t.Errorf("kept %+v, want the pending record %+v", entries[0], pending)
	}
	if got := h.issues.matching("surplus record"); len(got) == 0 {
		t.Errorf("the conflict was not reported, issues = %v", h.issues.all())
	}
	if _, err := os.Lstat(uploaded.path(stagingRoot)); !isVanished(err) {
		t.Errorf("the losing uploaded link was left behind: %v", err)
	}
}

func TestCollectorWatchesTreeBeforeReconstruction(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	writeFile(t, filepath.Join(logsDir, "events", "event.log"), "active nested")

	watcher := newFakeWatcherBuffered(16)
	reconstructing := make(chan struct{})
	release := make(chan struct{})
	issues := &issueLog{}

	rc, err := newRotatedCollector(rotatedCollectorConfig{
		LogsDir:     logsDir,
		StagingRoot: filepath.Join(dir, "rotated-staging"),
		SessionName: "session-1",
		NodeName:    "node-1",
		NewWatcher:  func() (fsWatcher, error) { return watcher, nil },
		NewTicker:   func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} },
		OnIssue:     issues.add,
		BeforeReconstruct: func() {
			close(reconstructing)
			<-release
		},
	})
	if err != nil {
		t.Fatalf("newRotatedCollector() error: %v", err)
	}
	go func() { _ = rc.Run() }()
	t.Cleanup(func() { rc.Stop() })

	select {
	case <-reconstructing:
	case <-time.After(5 * time.Second):
		t.Fatal("collector never reached staging reconstruction")
	}

	// The whole tree must already be watched: this is the window in which a
	// short-lived segment could otherwise be created and deleted unseen.
	watched := watcher.watched()
	for _, want := range []string{logsDir, filepath.Join(logsDir, "events")} {
		if !slices.Contains(watched, want) {
			t.Errorf("%s was not watched before reconstruction, watched = %v", want, watched)
		}
	}

	// Ray rotates while reconstruction is still running. The event queues in the
	// watcher channel, exactly as the kernel would queue it.
	backup := filepath.Join(logsDir, "raylet.out.1")
	writeFile(t, backup, "rotated during reconstruction")
	nested := filepath.Join(logsDir, "serve")
	writeFile(t, filepath.Join(nested, "replica.log"), "active")
	writeFile(t, filepath.Join(nested, "replica.log.1"), "rotated in a new directory")
	watcher.events <- fsnotify.Event{Name: backup, Op: fsnotify.Create}
	watcher.events <- fsnotify.Event{Name: nested, Op: fsnotify.Create}

	close(release)

	// Both segments are captured, and the queued events plus the startup scan must
	// not produce duplicates.
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries := rc.snapshot()
		if len(entries) == 2 {
			names := []string{entries[0].OriginalName, entries[1].OriginalName}
			slices.Sort(names)
			if !slices.Equal(names, []string{"raylet.out.1", "replica.log.1"}) {
				t.Fatalf("captured %v, want both segments once each", names)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("captured %+v, want exactly two segments", entries)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCollectorRemovesSurplusStagingLinks(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	src := filepath.Join(logsDir, "raylet.out.1")
	writeFile(t, src, "one segment")

	// Same state and same capture ID, different paths: only the path tie-breaker
	// can decide this deterministically.
	const id = "0001780000000000000.aaaaaaaaaaaaaaaa"
	first, err := newStagedEntry(statePending, "session-1", "node-1", "a", "raylet.out.1", id)
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	second, err := newStagedEntry(statePending, "session-1", "node-1", "b", "raylet.out.1", id)
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	for _, e := range []stagedEntry{second, first} { // created in the "wrong" order
		if err := captureLink(src, e.path(stagingRoot)); err != nil {
			t.Fatalf("captureLink() error: %v", err)
		}
	}
	if _, nlink, err := statInode(src); err != nil || nlink != 3 {
		t.Fatalf("link count = %d (err %v), want 3 before reconstruction", nlink, err)
	}

	h := start(t, dir)

	entries := h.rc.snapshot()
	if len(entries) != 1 {
		t.Fatalf("index holds %d captures, want 1: %+v", len(entries), entries)
	}
	if entries[0] != first {
		t.Errorf("kept %+v, want the lexicographically first path %+v", entries[0], first)
	}
	if _, err := os.Lstat(first.path(stagingRoot)); err != nil {
		t.Errorf("the winning link was removed: %v", err)
	}
	if _, err := os.Lstat(second.path(stagingRoot)); !isVanished(err) {
		t.Errorf("the surplus link was left behind: %v", err)
	}
	// The surplus link no longer pins the inode, so release can eventually work.
	if _, nlink, err := statInode(src); err != nil || nlink != 2 {
		t.Errorf("link count = %d (err %v), want 2 after cleanup", nlink, err)
	}
	if got := h.issues.matching("surplus record"); len(got) == 0 {
		t.Errorf("the conflict was not reported, issues = %v", h.issues.all())
	}
}

func TestCollectorFailsWhenSurplusLinkCannotBeRemoved(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions do not prevent unlink")
	}
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	src := filepath.Join(logsDir, "raylet.out.1")
	writeFile(t, src, "one segment")

	winner, err := newStagedEntry(statePending, "session-1", "node-1", "", "raylet.out.1", "0001780000000000000.aaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	loser, err := newStagedEntry(stateUploaded, "session-1", "node-1", "", "raylet.out.1", "0001780000000000001.bbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	for _, e := range []stagedEntry{winner, loser} {
		if err := captureLink(src, e.path(stagingRoot)); err != nil {
			t.Fatalf("captureLink() error: %v", err)
		}
	}
	loserDir := filepath.Dir(loser.path(stagingRoot))
	if err := os.Chmod(loserDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(loserDir, 0o750) })

	rc, err := newRotatedCollector(rotatedCollectorConfig{
		LogsDir:     logsDir,
		StagingRoot: stagingRoot,
		SessionName: "session-1",
		NodeName:    "node-1",
		NewWatcher:  func() (fsWatcher, error) { return newFakeWatcher(), nil },
		NewTicker:   func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} },
		OnIssue:     func(error) {},
	})
	if err != nil {
		t.Fatalf("newRotatedCollector() error: %v", err)
	}

	// Starting with a link nothing tracks would pin the inode forever, so the
	// collector must refuse to start rather than run with a broken invariant.
	runErr := runExpectingFailure(t, rc)
	if !strings.Contains(runErr.Error(), "staging volume is inconsistent") {
		t.Errorf("Run() error = %v, want it to name the inconsistency", runErr)
	}
}

func TestRemoveSurplusLinkLeavesReplacedPathAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pending", "raylet.out.1.rotated.0001780000000000000.aaaaaaaaaaaaaaaa")
	writeFile(t, path, "a different file now lives here")

	// The record was taken when the path held another inode; the file there now is
	// not ours to delete.
	err := removeSurplusLink(stagedRecord{key: inodeKey{Dev: 1, Ino: 999999}, path: path})
	if err == nil {
		t.Fatal("removeSurplusLink() removed a path that no longer holds the recorded inode")
	}
	if !strings.Contains(err.Error(), "left in place") {
		t.Errorf("error = %v, want it to say the link was left in place", err)
	}
	if _, statErr := os.Lstat(path); statErr != nil {
		t.Errorf("the replaced file was removed: %v", statErr)
	}
}

func TestCollectorIgnoresSpecialStagingFiles(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")

	fifo := filepath.Join(stagingRoot, "session-1", "node-1", "pending",
		"raylet.out.1.rotated.0001780000000000000.aaaaaaaaaaaaaaaa")
	if err := os.MkdirAll(filepath.Dir(fifo), 0o750); err != nil {
		t.Fatalf("create staging directory: %v", err)
	}
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("cannot create a FIFO on this platform: %v", err)
	}

	h := start(t, dir)

	// A FIFO with a plausible staging name must never be indexed: an uploader
	// opening it could block forever.
	if entries := h.rc.snapshot(); len(entries) != 0 {
		t.Errorf("a special file was restored into the index: %+v", entries)
	}
	if got := h.issues.matching("not a regular file"); len(got) == 0 {
		t.Errorf("the special file was not reported, issues = %v", h.issues.all())
	}
}

// runExpectingFailure runs the collector and returns the error startup failed with.
// If startup wrongly succeeds the loop would run forever, so this reports that
// directly instead of letting the test hang until the package timeout.
func runExpectingFailure(t *testing.T, rc *rotatedCollector) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- rc.Run() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run() returned nil: startup was expected to fail")
		}
		return err
	case <-time.After(10 * time.Second):
		rc.Stop()
		t.Fatal("Run() entered its event loop: startup was expected to fail")
		return nil
	}
}

// startForFailure builds a collector without running it, so a test can observe how
// startup fails.
func startForFailure(t *testing.T, dir string, tweak func(*rotatedCollectorConfig)) (*rotatedCollector, *fakeWatcher) {
	t.Helper()
	logsDir := filepath.Join(dir, "session", "logs")
	if err := os.MkdirAll(logsDir, 0o750); err != nil {
		t.Fatalf("create logs dir: %v", err)
	}
	watcher := newFakeWatcher()
	issues := &issueLog{}
	cfg := rotatedCollectorConfig{
		LogsDir:     logsDir,
		StagingRoot: filepath.Join(dir, "rotated-staging"),
		SessionName: "session-1",
		NodeName:    "node-1",
		NewWatcher:  func() (fsWatcher, error) { return watcher, nil },
		NewTicker:   func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} },
		OnIssue:     issues.add,
	}
	tweak(&cfg)
	rc, err := newRotatedCollector(cfg)
	if err != nil {
		t.Fatalf("newRotatedCollector() error: %v", err)
	}
	return rc, watcher
}

func TestCollectorFailsWhenWatchCoverageIsIncomplete(t *testing.T) {
	// Starting with part of the tree unwatched looks healthy but loses any segment
	// created and deleted in the gap, so it must be fatal.
	tests := []struct {
		name    string
		prepare func(t *testing.T, dir, logsDir string, w *fakeWatcher)
		wantIn  string
	}{
		{
			name: "root watch fails",
			prepare: func(_ *testing.T, _, logsDir string, w *fakeWatcher) {
				w.failAdd = map[string]error{logsDir: os.ErrPermission}
			},
			wantIn: "logs",
		},
		{
			name: "nested watch fails",
			prepare: func(t *testing.T, _, logsDir string, w *fakeWatcher) {
				writeFile(t, filepath.Join(logsDir, "events", "event.log"), "active")
				w.failAdd = map[string]error{filepath.Join(logsDir, "events"): os.ErrPermission}
			},
			wantIn: "events",
		},
		{
			name: "directory enumeration fails",
			prepare: func(t *testing.T, _, logsDir string, _ *fakeWatcher) {
				if os.Geteuid() == 0 {
					t.Skip("running as root: directory permissions do not prevent reads")
				}
				nested := filepath.Join(logsDir, "serve")
				if err := os.MkdirAll(nested, 0o750); err != nil {
					t.Fatalf("create nested dir: %v", err)
				}
				if err := os.Chmod(nested, 0o000); err != nil {
					t.Fatalf("chmod: %v", err)
				}
				t.Cleanup(func() { _ = os.Chmod(nested, 0o750) })
			},
			wantIn: "serve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			logsDir := filepath.Join(dir, "session", "logs")
			if err := os.MkdirAll(logsDir, 0o750); err != nil {
				t.Fatalf("create logs dir: %v", err)
			}
			// A backup that would be captured if startup wrongly continued.
			writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
			writeFile(t, filepath.Join(logsDir, "raylet.out.1"), "rotated")

			reconstructed := false
			rc, watcher := startForFailure(t, dir, func(cfg *rotatedCollectorConfig) {
				cfg.BeforeReconstruct = func() { reconstructed = true }
			})
			tt.prepare(t, dir, logsDir, watcher)

			err := runExpectingFailure(t, rc)
			if !strings.Contains(err.Error(), "cannot watch the whole logs tree") || !strings.Contains(err.Error(), tt.wantIn) {
				t.Errorf("Run() error = %v, want it to name the failure and the directory %q", err, tt.wantIn)
			}
			if reconstructed {
				t.Error("reconstruction ran even though watch installation failed")
			}
			if _, statErr := os.Lstat(filepath.Join(dir, "rotated-staging")); !isVanished(statErr) {
				t.Error("captures were staged even though watch installation failed")
			}
			if !watcher.isClosed() {
				t.Error("the watcher was not closed when startup failed")
			}
		})
	}
}

func TestCollectorFailsWhenStagingCannotBeRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions do not prevent reads")
	}
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")

	// An unreadable subtree may hold real staged links; adopting only what is
	// readable would leave those pinning inodes with no owner.
	unreadable := filepath.Join(stagingRoot, "session-1", "node-1", "pending")
	if err := os.MkdirAll(unreadable, 0o750); err != nil {
		t.Fatalf("create staging dir: %v", err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o750) })

	rc, watcher := startForFailure(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.StagingRoot = stagingRoot
	})

	err := runExpectingFailure(t, rc)
	if !strings.Contains(err.Error(), "read staging volume") {
		t.Errorf("Run() error = %v, want it to name the unreadable staging volume", err)
	}
	if !watcher.isClosed() {
		t.Error("the watcher was not closed when startup failed")
	}
}

func TestCollectorFailsWhenSurplusPathWasReplaced(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	src := filepath.Join(logsDir, "raylet.out.1")
	writeFile(t, src, "one segment")

	winner, err := newStagedEntry(statePending, "session-1", "node-1", "", "raylet.out.1", "0001780000000000000.aaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	loser, err := newStagedEntry(statePending, "session-1", "node-1", "", "raylet.out.1", "0001780000000000001.bbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	for _, e := range []stagedEntry{winner, loser} {
		if err := captureLink(src, e.path(stagingRoot)); err != nil {
			t.Fatalf("captureLink() error: %v", err)
		}
	}

	// The conflict is reported immediately before the surplus link is removed, so
	// swapping the file from inside OnIssue lands exactly in that window: the
	// record says one inode, the path now holds another.
	replaced := loser.path(stagingRoot)
	var once sync.Once
	watcher := newFakeWatcher()
	rc, err := newRotatedCollector(rotatedCollectorConfig{
		LogsDir:     logsDir,
		StagingRoot: stagingRoot,
		SessionName: "session-1",
		NodeName:    "node-1",
		NewWatcher:  func() (fsWatcher, error) { return watcher, nil },
		NewTicker:   func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} },
		OnIssue: func(issue error) {
			if !strings.Contains(issue.Error(), "surplus record") {
				return
			}
			once.Do(func() {
				if err := os.Remove(replaced); err != nil {
					t.Errorf("remove surplus link: %v", err)
					return
				}
				writeFile(t, replaced, "an unrelated file")
			})
		},
	})
	if err != nil {
		t.Fatalf("newRotatedCollector() error: %v", err)
	}

	// The collector must not delete a file it did not link, and must not carry on
	// with a staging tree it cannot account for.
	runErr := runExpectingFailure(t, rc)
	if !strings.Contains(runErr.Error(), "left in place") || !strings.Contains(runErr.Error(), "staging volume is inconsistent") {
		t.Errorf("Run() error = %v, want an inconsistency naming the untouched path", runErr)
	}
	if got, readErr := os.ReadFile(replaced); readErr != nil || string(got) != "an unrelated file" {
		t.Errorf("the replacement file was modified: %q (err %v)", got, readErr)
	}
	if _, statErr := os.Lstat(winner.path(stagingRoot)); statErr != nil {
		t.Errorf("the winning link was removed: %v", statErr)
	}
	if entries := rc.snapshot(); entries != nil {
		t.Errorf("the collector began operating after a failed reconstruction: %+v", entries)
	}
	if !watcher.isClosed() {
		t.Error("the watcher was not closed when startup failed")
	}
}
