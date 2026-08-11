package logcollector

import (
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/storage"
)

// The uploader is written against the storage writer the rest of the collector
// already uses; nothing in this tranche widens that interface.
var _ objectWriter = (storage.StorageWriter)(nil)

// errUploadRejected is what the fake object store returns when a test wants a write
// to fail.
var errUploadRejected = errors.New("object store rejected the write")

// testCluster is a plain RayCluster, whose logs are not nested under an owner.
var testCluster = clusterIdentity{
	RootDir:     "root",
	OwnerKind:   "RayCluster",
	Namespace:   "ns",
	ClusterName: "cluster-a",
}

// testBackoff is short and obvious so the retry schedule can be asserted exactly.
var testBackoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// writeCall records one attempted object write.
type writeCall struct {
	key     string
	content string
	err     error
}

// fakeWriter is an object store that can block, fail and count. It never touches the
// filesystem, so a test can prove the worker read the staged descriptor it was given
// by comparing the bytes that arrived.
type fakeWriter struct {
	failures  map[string]int
	release   chan struct{}
	entered   chan string
	calls     []writeCall
	mu        sync.Mutex
	failAll   bool
	active    int
	maxActive int
}

func newFakeWriter() *fakeWriter {
	return &fakeWriter{
		failures: make(map[string]int),
		entered:  make(chan string, 64),
	}
}

func (w *fakeWriter) WriteFile(file string, reader io.ReadSeeker) error {
	w.mu.Lock()
	w.active++
	if w.active > w.maxActive {
		w.maxActive = w.active
	}
	fail := w.failAll
	if n := w.failures[file]; n > 0 {
		w.failures[file] = n - 1
		fail = true
	}
	release := w.release
	w.mu.Unlock()

	select {
	case w.entered <- file:
	default:
	}
	if release != nil {
		<-release
	}

	body, err := io.ReadAll(reader)

	w.mu.Lock()
	defer w.mu.Unlock()
	w.active--
	switch {
	case err != nil:
		w.calls = append(w.calls, writeCall{key: file, err: err})
		return err
	case fail:
		w.calls = append(w.calls, writeCall{key: file, err: errUploadRejected})
		return errUploadRejected
	default:
		w.calls = append(w.calls, writeCall{key: file, content: string(body)})
		return nil
	}
}

// blockWrites makes every write wait until the returned function is called.
func (w *fakeWriter) blockWrites() func() {
	release := make(chan struct{})
	w.mu.Lock()
	w.release = release
	w.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			w.mu.Lock()
			w.release = nil
			w.mu.Unlock()
			close(release)
		})
	}
}

func (w *fakeWriter) setFailAll(v bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failAll = v
}

func (w *fakeWriter) attempts() []writeCall {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]writeCall(nil), w.calls...)
}

func (w *fakeWriter) attemptCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.calls)
}

func (w *fakeWriter) concurrentPeak() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.maxActive
}

// stored returns the content of the last successful write for key.
func (w *fakeWriter) stored(key string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := len(w.calls) - 1; i >= 0; i-- {
		if w.calls[i].key == key && w.calls[i].err == nil {
			return w.calls[i].content, true
		}
	}
	return "", false
}

// fakeClock is the collector's clock. Retry deadlines are computed from it, so a test
// can move time instead of waiting for it.
type fakeClock struct {
	t  time.Time
	mu sync.Mutex
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fakeTimers records every retry delay the collector asks for and fires on demand.
// The channel is unbuffered, so firing it also proves the owner loop received it.
type fakeTimers struct {
	c     chan time.Time
	durs  []time.Duration
	mu    sync.Mutex
	stops int
}

func newFakeTimers() *fakeTimers {
	return &fakeTimers{c: make(chan time.Time)}
}

func (f *fakeTimers) newTimer(d time.Duration) (<-chan time.Time, func()) {
	f.mu.Lock()
	f.durs = append(f.durs, d)
	f.mu.Unlock()
	return f.c, func() {
		f.mu.Lock()
		f.stops++
		f.mu.Unlock()
	}
}

func (f *fakeTimers) delays() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.durs...)
}

// fire delivers one retry tick and waits for the loop to take it.
func (f *fakeTimers) fire(t *testing.T) {
	t.Helper()
	select {
	case f.c <- time.Now():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out firing the retry timer: nothing was armed")
	}
}

// upHarness is a running collector with an uploader attached.
type upHarness struct {
	*harness
	writer *fakeWriter
	clock  *fakeClock
	timers *fakeTimers
}

func startUploading(t *testing.T, dir string, tweak func(*rotatedCollectorConfig)) *upHarness {
	t.Helper()
	u := &upHarness{
		writer: newFakeWriter(),
		clock:  newFakeClock(),
		timers: newFakeTimers(),
	}
	u.harness = startWith(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.Writer = u.writer
		cfg.Cluster = testCluster
		cfg.Now = u.clock.now
		cfg.NewTimer = u.timers.newTimer
		cfg.UploadBackoff = testBackoff
		cfg.WorkerStopGrace = 50 * time.Millisecond
		if tweak != nil {
			tweak(cfg)
		}
	})
	return u
}

// waitFor round-trips the owner loop until cond holds. The round-trip is the
// synchronization point: anything the scheduler was going to do has been done by the
// time a request is served.
func (u *upHarness) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		u.rc.stats()
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s (attempts=%d, stats=%+v)", what, u.writer.attemptCount(), u.rc.stats())
}

func (u *upHarness) waitForAttempts(t *testing.T, n int) {
	t.Helper()
	u.waitFor(t, "storage attempt "+strconv.Itoa(n), func() bool { return u.writer.attemptCount() >= n })
}

// waitForUploaded waits until exactly n captures have been promoted.
func (u *upHarness) waitForUploaded(t *testing.T, n int) {
	t.Helper()
	u.waitFor(t, strconv.Itoa(n)+" uploaded capture(s)", func() bool {
		count := 0
		for _, e := range u.rc.snapshot() {
			if e.State == stateUploaded {
				count++
			}
		}
		return count == n
	})
}

// captureOne writes the active raylet.out log and one rotation backup, then delivers
// the event and waits for the capture to be registered.
func (u *upHarness) captureOne(t *testing.T, backup, content string) stagedEntry {
	t.Helper()
	u.writeLog(t, "raylet.out", "active")
	p := u.writeLog(t, backup, content)
	u.sendEvent(t, p)
	for _, e := range u.rc.snapshot() {
		if e.OriginalName == filepath.Base(backup) {
			return e
		}
	}
	t.Fatalf("%s was not captured: %+v", backup, u.rc.snapshot())
	return stagedEntry{}
}

// stageManually pins a backup exactly as a previous collector run would have, without
// a collector running. It returns the entry and the inode it pinned.
func stageManually(t *testing.T, logsDir, stagingRoot, name, content string, promote bool) (stagedEntry, inodeKey) {
	t.Helper()
	src := filepath.Join(logsDir, name)
	writeFile(t, src, content)

	id, err := newCaptureIDGenerator().next()
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
	if !promote {
		return entry, key
	}

	ix := newCaptureIndex()
	if _, _, err := ix.add(key, entry); err != nil {
		t.Fatalf("add() error: %v", err)
	}
	promoted, err := promoteCapture(stagingRoot, ix, key)
	if err != nil {
		t.Fatalf("promoteCapture() error: %v", err)
	}
	return promoted, key
}

// 1. A newly captured pending entry is scheduled for upload.
func TestUploaderSchedulesNewlyCapturedPendingEntry(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)

	const content = "rotated bytes"
	entry := u.captureOne(t, "raylet.out.1", content)
	wantKey := entry.objectKey(testCluster)

	u.waitForAttempts(t, 1)
	got, ok := u.writer.stored(wantKey)
	if !ok {
		t.Fatalf("no successful write to %s, attempts = %+v", wantKey, u.writer.attempts())
	}
	if got != content {
		t.Errorf("uploaded %q, want %q", got, content)
	}
}

// 2. A blocked upload does not block fsnotify handling or snapshots.
func TestUploaderBlockedUploadDoesNotBlockTheOwnerLoop(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)
	release := u.writer.blockWrites()
	defer release()

	u.captureOne(t, "raylet.out.1", "first")
	select {
	case <-u.writer.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first upload never started")
	}

	// The worker is now parked inside the object store. Every owner-loop duty must
	// still be prompt.
	start := time.Now()
	for i := 2; i <= 6; i++ {
		p := u.writeLog(t, "raylet.out."+strconv.Itoa(i), "segment")
		u.sendEvent(t, p)
	}
	u.rc.reconcileNow()
	entries := u.rc.snapshot()
	elapsed := time.Since(start)

	if len(entries) != 6 {
		t.Errorf("captured %d segments while an upload was blocked, want 6", len(entries))
	}
	if elapsed > 5*time.Second {
		t.Errorf("event handling took %v while an upload was blocked, which means uploads run on the owner loop", elapsed)
	}
}

// 3. Only one upload is in flight for one capture, and only one at a time overall.
func TestUploaderKeepsOneUploadInFlight(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)
	release := u.writer.blockWrites()

	u.writeLog(t, "raylet.out", "active")
	for i := 1; i <= 4; i++ {
		p := u.writeLog(t, "raylet.out."+strconv.Itoa(i), "segment "+strconv.Itoa(i))
		u.sendEvent(t, p)
	}
	select {
	case <-u.writer.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no upload started")
	}

	s := u.rc.stats()
	if s.InFlightUploads != 1 {
		t.Errorf("InFlightUploads = %d, want exactly 1", s.InFlightUploads)
	}
	if got := u.writer.attemptCount(); got != 0 {
		t.Errorf("%d writes completed while the store was blocked, want 0", got)
	}

	release()
	u.waitForUploaded(t, 4)
	if peak := u.writer.concurrentPeak(); peak != 1 {
		t.Errorf("peak concurrent writes = %d, want 1", peak)
	}
	if got := u.writer.attemptCount(); got != 4 {
		t.Errorf("%d storage attempts for 4 captures, want 4: %+v", got, u.writer.attempts())
	}
}

// 4. Upload success promotes pending -> uploaded, on disk and in the index.
func TestUploadSuccessPromotesPendingToUploaded(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)

	entry := u.captureOne(t, "raylet.out.1", "segment")
	u.waitForUploaded(t, 1)

	got := u.rc.snapshot()[0]
	if got.CaptureID != entry.CaptureID {
		t.Errorf("capture ID changed on promotion: %s -> %s", entry.CaptureID, got.CaptureID)
	}
	if _, err := os.Lstat(got.path(u.stagingRoot)); err != nil {
		t.Errorf("uploaded staging link missing: %v", err)
	}
	if _, err := os.Lstat(entry.path(u.stagingRoot)); !os.IsNotExist(err) {
		t.Errorf("pending staging link still present after promotion: %v", err)
	}
}

// 5. Upload failure leaves the entry pending, with its link and identity intact.
func TestUploadFailureLeavesCapturePending(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)
	u.writer.setFailAll(true)

	entry := u.captureOne(t, "raylet.out.1", "segment")
	u.waitForAttempts(t, 1)

	entries := u.rc.snapshot()
	if len(entries) != 1 || entries[0] != entry {
		t.Fatalf("capture changed after a failed upload: %+v, want %+v", entries, entry)
	}
	if _, err := os.Lstat(entry.path(u.stagingRoot)); err != nil {
		t.Errorf("pending staging link was not kept after a failed upload: %v", err)
	}
	if s := u.rc.stats(); s.Uploaded != 0 || s.Pending != 1 {
		t.Errorf("stats = %+v, want one pending and nothing uploaded", s)
	}
}

// 6. Failed uploads retry on the injected backoff schedule, capped at its last entry.
// 7. Every retry writes the identical object key.
func TestUploadRetriesFollowBackoffAndReuseTheSameKey(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)
	u.writer.setFailAll(true)

	entry := u.captureOne(t, "raylet.out.1", "segment")
	wantKey := entry.objectKey(testCluster)

	// testBackoff is 1s, 2s, 4s, and 4s repeats once the sequence is exhausted.
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second}
	for i, delay := range want {
		u.waitForAttempts(t, i+1)
		u.waitFor(t, "retry timer armed", func() bool { return len(u.timers.delays()) >= i+1 })

		if got := u.timers.delays()[i]; got != delay {
			t.Errorf("retry %d armed for %v, want %v (all delays: %v)", i+1, got, delay, u.timers.delays())
		}
		u.clock.advance(delay)
		u.timers.fire(t)
	}
	u.waitForAttempts(t, len(want)+1)

	for i, call := range u.writer.attempts() {
		if call.key != wantKey {
			t.Errorf("attempt %d wrote key %q, want the original %q", i+1, call.key, wantKey)
		}
	}
	if entries := u.rc.snapshot(); len(entries) != 1 || entries[0] != entry {
		t.Errorf("capture identity changed across retries: %+v", entries)
	}
}

// 8. A restart uploads reconstructed pending entries under their original identity.
func TestRestartUploadsPendingWithoutMintingNewIDs(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	entry, _ := stageManually(t, logsDir, stagingRoot, "raylet.out.1", "left over", false)

	u := startUploading(t, dir, nil)
	u.waitForAttempts(t, 1)

	if got := u.writer.attempts()[0].key; got != entry.objectKey(testCluster) {
		t.Errorf("restart uploaded key %q, want the original %q", got, entry.objectKey(testCluster))
	}
	u.waitForUploaded(t, 1)
	if got := u.rc.snapshot()[0].CaptureID; got != entry.CaptureID {
		t.Errorf("restart minted capture ID %s for an existing staged capture %s", got, entry.CaptureID)
	}
}

// 9. A restart never re-uploads an entry reconstructed as uploaded.
func TestRestartDoesNotReuploadUploadedEntries(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	uploaded, _ := stageManually(t, logsDir, stagingRoot, "raylet.out.1", "already sent", true)

	u := startUploading(t, dir, nil)
	// Give every path that could schedule an upload a chance to run.
	u.rc.reconcileNow()
	u.fireTick(t)
	u.rc.reconcileNow()

	if got := u.writer.attemptCount(); got != 0 {
		t.Errorf("an already-uploaded capture was re-uploaded %d time(s): %+v", got, u.writer.attempts())
	}
	entries := u.rc.snapshot()
	if len(entries) != 1 || entries[0] != uploaded {
		t.Errorf("reconstructed uploaded capture = %+v, want %+v", entries, uploaded)
	}
}

// 10. A restart retries release of reconstructed uploaded entries.
func TestRestartReleasesReconstructedUploadedEntries(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	uploaded, _ := stageManually(t, logsDir, stagingRoot, "raylet.out.1", "already sent", true)

	// Ray has since dropped its own link, so the staged link is the last one.
	if err := os.Remove(filepath.Join(logsDir, "raylet.out.1")); err != nil {
		t.Fatalf("remove Ray's link: %v", err)
	}

	u := startUploading(t, dir, nil)

	if entries := u.rc.snapshot(); len(entries) != 0 {
		t.Errorf("startup did not release the uploaded capture: %+v", entries)
	}
	if _, err := os.Lstat(uploaded.path(stagingRoot)); !os.IsNotExist(err) {
		t.Errorf("uploaded staging link was not unlinked: %v", err)
	}
	if got := u.writer.attemptCount(); got != 0 {
		t.Errorf("released capture was also uploaded %d time(s)", got)
	}
}

// 11. A successful upload while Ray still holds a link leaves the entry uploaded.
// 12. Once Ray drops its link, maintenance releases the staged link.
// 27. A successful release decreases accounting exactly once.
func TestUploadedCaptureIsReleasedOnlyAfterRayLetsGo(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)

	const content = "segment bytes"
	entry := u.captureOne(t, "raylet.out.1", content)
	u.waitForUploaded(t, 1)

	// Ray still holds raylet.out.1, so the segment must stay pinned.
	u.fireTick(t)
	if entries := u.rc.snapshot(); len(entries) != 1 || entries[0].State != stateUploaded {
		t.Fatalf("capture was released while Ray still held a link: %+v", entries)
	}
	before := u.rc.stats()
	if before.StagedBytes != int64(len(content)) {
		t.Fatalf("StagedBytes = %d, want %d", before.StagedBytes, len(content))
	}

	if err := os.Remove(filepath.Join(u.logsDir, "raylet.out.1")); err != nil {
		t.Fatalf("remove Ray's link: %v", err)
	}
	u.fireTick(t)

	if entries := u.rc.snapshot(); len(entries) != 0 {
		t.Errorf("capture was not released after Ray dropped its link: %+v", entries)
	}
	if _, err := os.Lstat(entry.withState(stateUploaded).path(u.stagingRoot)); !os.IsNotExist(err) {
		t.Errorf("staged link still present after release: %v", err)
	}
	after := u.rc.stats()
	if after.StagedBytes != 0 {
		t.Errorf("StagedBytes = %d after release, want 0", after.StagedBytes)
	}

	// A second sweep must not double-count.
	u.fireTick(t)
	if got := u.rc.stats().StagedBytes; got != 0 {
		t.Errorf("StagedBytes = %d after a second sweep, want 0", got)
	}
}

// 13. A release that cannot proceed retains the index entry and the byte accounting.
func TestReleaseFailureRetainsEntryAndAccounting(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)

	const content = "still held by ray"
	u.captureOne(t, "raylet.out.1", content)
	u.waitForUploaded(t, 1)

	for range 3 {
		u.fireTick(t)
	}
	s := u.rc.stats()
	if s.Captures != 1 || s.Uploaded != 1 {
		t.Errorf("stats = %+v, want the uploaded capture retained", s)
	}
	if s.StagedBytes != int64(len(content)) {
		t.Errorf("StagedBytes = %d, want %d retained across failed releases", s.StagedBytes, len(content))
	}
}

// 14. A stale success result cannot promote a replaced or untracked capture.
func TestStaleUploadResultCannotPromote(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	entry, key := stageManually(t, logsDir, stagingRoot, "raylet.out.1", "segment", false)

	// A collector that is not running: this test is the only goroutine, so it may
	// drive the owner-side functions directly.
	issues := &issueLog{}
	rc, err := newRotatedCollector(rotatedCollectorConfig{
		LogsDir:     logsDir,
		StagingRoot: stagingRoot,
		SessionName: "session-1",
		NodeName:    "node-1",
		Writer:      newFakeWriter(),
		Cluster:     testCluster,
		OnIssue:     issues.add,
	})
	if err != nil {
		t.Fatalf("newRotatedCollector() error: %v", err)
	}
	if _, err := rc.ix.restore(key, entry); err != nil {
		t.Fatalf("restore() error: %v", err)
	}

	job := uploadJob{uploadIdentity: uploadIdentity{
		inode:     key,
		entry:     entry,
		localPath: entry.path(stagingRoot),
		objectKey: entry.objectKey(testCluster),
	}}

	// (a) No state was ever recorded for this job, so it belongs to nothing.
	if err := rc.applyUploadResult(uploadResult{job: job}); err != nil {
		t.Fatalf("applyUploadResult() on a stale result returned %v, want nil: a stale result is discarded, not fatal", err)
	}
	if c, _ := rc.ix.lookup(key); c.Entry.State != statePending {
		t.Errorf("an untracked result promoted the capture to %q", c.Entry.State)
	}

	// (b) The capture at that inode was replaced by a different one between
	// submission and completion.
	rc.up.states[key] = &uploadState{phase: phaseInFlight, job: job}
	replacement, err := newStagedEntry(statePending, "session-1", "node-1", "", "raylet.out.1", "1234567890123456789.abcdefabcdef0123")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	rc.ix.byInode[key].Entry = replacement

	if err := rc.applyUploadResult(uploadResult{job: job}); err != nil {
		t.Fatalf("applyUploadResult() on a replaced capture returned %v, want nil", err)
	}
	if c, _ := rc.ix.lookup(key); c.Entry != replacement {
		t.Errorf("a stale result mutated the replacement capture: %+v", c.Entry)
	}
	if len(issues.matching("no longer matches the capture")) == 0 {
		t.Errorf("stale results were not reported: %v", issues.all())
	}
}

// stallFirstUpload blocks the first upload inside the object store and returns once
// it is parked there, plus the function that releases it. While it is held, the
// collector is guaranteed not to dispatch anything else, which is what lets a test
// change a queued capture's staged file underneath the worker deterministically.
func (u *upHarness) stallFirstUpload(t *testing.T) func() {
	t.Helper()
	release := u.writer.blockWrites()
	u.captureOne(t, "raylet.out.9", "the upload that stalls")
	select {
	case <-u.writer.entered:
	case <-time.After(5 * time.Second):
		release()
		t.Fatal("the first upload never started")
	}
	return release
}

// A local validation failure is a durable contradiction between the index and the
// staging volume. Retrying it on the transport schedule would retry forever, so the
// collector fails closed and leaves the staging state for the next run to reconstruct.
func TestLocalValidationFailureStopsTheCollector(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(t *testing.T, staged string)
		wantIn  string
	}{
		{
			name: "staged path holds a different inode",
			corrupt: func(t *testing.T, staged string) {
				t.Helper()
				if err := os.Remove(staged); err != nil {
					t.Fatalf("remove staged link: %v", err)
				}
				writeFile(t, staged, "an entirely different file")
			},
			wantIn: "not the captured",
		},
		{
			name: "staged path is gone",
			corrupt: func(t *testing.T, staged string) {
				t.Helper()
				if err := os.Remove(staged); err != nil {
					t.Fatalf("remove staged link: %v", err)
				}
			},
			wantIn: "open staged capture",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			u := startUploading(t, dir, nil)
			release := u.stallFirstUpload(t)

			// A second capture, queued behind the stalled one.
			doomed := u.captureOne(t, "raylet.out.1", "segment")
			staged := doomed.path(u.stagingRoot)
			if _, err := os.Lstat(staged); err != nil {
				t.Fatalf("second capture was not staged: %v", err)
			}
			tc.corrupt(t, staged)

			release()

			var runErr error
			select {
			case runErr = <-u.runErr:
			case <-time.After(10 * time.Second):
				t.Fatal("Run() did not return after a local consistency failure")
			}
			if !errors.Is(runErr, errStagingInconsistent) {
				t.Fatalf("Run() returned %v, want an error wrapping errStagingInconsistent", runErr)
			}
			for _, want := range []string{doomed.CaptureID, staged, tc.wantIn} {
				if !strings.Contains(runErr.Error(), want) {
					t.Errorf("Run() error %q does not name %q", runErr, want)
				}
			}

			// Only the healthy capture reached storage: a locally rejected file is
			// never sent, and nothing is retried.
			if got := u.writer.attemptCount(); got != 1 {
				t.Errorf("%d storage attempts, want only the first capture's: %+v", got, u.writer.attempts())
			}
			if delays := u.timers.delays(); len(delays) != 0 {
				t.Errorf("a retry timer was armed for a local failure: %v", delays)
			}

			// The staging volume is left exactly as it was found.
			if _, err := os.Lstat(doomed.withState(stateUploaded).path(u.stagingRoot)); !os.IsNotExist(err) {
				t.Errorf("the rejected capture was promoted: %v", err)
			}
			if tc.name == "staged path holds a different inode" {
				if _, err := os.Lstat(staged); err != nil {
					t.Errorf("the pending staging path was not left in place: %v", err)
				}
			}

			// Nothing further happens once the collector has stopped.
			time.Sleep(100 * time.Millisecond)
			if got := u.writer.attemptCount(); got != 1 {
				t.Errorf("%d storage attempts after Run returned, want 1", got)
			}
		})
	}
}

// The policy itself, asserted directly: a transport error retries, a local one does
// not, and neither promotes.
func TestUploadResultFailurePolicySeparatesLocalFromTransport(t *testing.T) {
	newCollector := func(t *testing.T, dir string) (*rotatedCollector, uploadJob, inodeKey) {
		t.Helper()
		logsDir := filepath.Join(dir, "session", "logs")
		stagingRoot := filepath.Join(dir, "rotated-staging")
		writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
		entry, key := stageManually(t, logsDir, stagingRoot, "raylet.out.1", "segment", false)

		rc, err := newRotatedCollector(rotatedCollectorConfig{
			LogsDir:       logsDir,
			StagingRoot:   stagingRoot,
			SessionName:   "session-1",
			NodeName:      "node-1",
			Writer:        newFakeWriter(),
			Cluster:       testCluster,
			UploadBackoff: testBackoff,
			OnIssue:       func(error) {},
		})
		if err != nil {
			t.Fatalf("newRotatedCollector() error: %v", err)
		}
		if _, err := rc.ix.restore(key, entry); err != nil {
			t.Fatalf("restore() error: %v", err)
		}
		job := uploadJob{uploadIdentity: uploadIdentity{
			inode:     key,
			entry:     entry,
			localPath: entry.path(stagingRoot),
			objectKey: entry.objectKey(testCluster),
		}}
		rc.up.states[key] = &uploadState{phase: phaseInFlight, job: job}
		rc.up.inFlight = 1
		return rc, job, key
	}

	t.Run("transport failure retries", func(t *testing.T) {
		rc, job, key := newCollector(t, t.TempDir())
		err := rc.applyUploadResult(uploadResult{job: job, err: errUploadRejected})
		if err != nil {
			t.Fatalf("a transport failure returned %v, want nil so the retry can happen", err)
		}
		st, tracked := rc.up.states[key]
		if !tracked || st.phase != phaseBackoff {
			t.Fatalf("state = %+v, want the capture backing off", st)
		}
		if st.dueAt.IsZero() {
			t.Error("no retry deadline was scheduled for a transport failure")
		}
	})

	t.Run("local failure fails closed", func(t *testing.T) {
		rc, job, key := newCollector(t, t.TempDir())
		err := rc.applyUploadResult(uploadResult{job: job, local: true, err: errors.New("inode mismatch")})
		if !errors.Is(err, errStagingInconsistent) {
			t.Fatalf("a local failure returned %v, want an error wrapping errStagingInconsistent", err)
		}
		if st, tracked := rc.up.states[key]; tracked {
			t.Errorf("upload state %+v was left behind, so a retry could still be scheduled", st)
		}
		if len(rc.up.queue) != 0 {
			t.Errorf("queue = %+v, want nothing requeued after a local failure", rc.up.queue)
		}
		if c, present := rc.ix.lookup(key); !present || c.Entry.State != statePending {
			t.Errorf("capture = %+v, want it left pending and tracked", c)
		}
	})
}

// 15. The worker refuses a staged path whose inode no longer matches its job.
func TestWorkerRejectsMismatchedInode(t *testing.T) {
	dir := t.TempDir()
	stagingRoot := filepath.Join(dir, "staging")
	entry, err := newStagedEntry(statePending, "session-1", "node-1", "", "raylet.out.1", "1234567890123456789.abcdefabcdef0123")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	staged := entry.path(stagingRoot)
	writeFile(t, staged, "the real capture")

	key, _, err := statInode(staged)
	if err != nil {
		t.Fatalf("statInode() error: %v", err)
	}
	writer := newFakeWriter()
	w := &uploadWorker{writer: writer, stagingRoot: stagingRoot}
	job := uploadJob{uploadIdentity: uploadIdentity{
		inode:     key,
		entry:     entry,
		localPath: staged,
		objectKey: entry.objectKey(testCluster),
	}}

	// The happy path first, so the rejection below is known to be about the inode.
	if res, ok := w.execute(job); !ok || res.err != nil {
		t.Fatalf("execute() on a matching file returned (%+v, ok=%v)", res, ok)
	}

	// Rotation replaced the staged path with a different file.
	if err := os.Remove(staged); err != nil {
		t.Fatalf("remove staged file: %v", err)
	}
	writeFile(t, staged, "a different file entirely")

	before := writer.attemptCount()
	res, ok := w.execute(job)
	if !ok {
		t.Fatal("execute() reported a shutdown that was not requested")
	}
	if res.err == nil {
		t.Fatal("execute() accepted a staged path holding a different inode")
	}
	if !res.local {
		t.Errorf("inode mismatch reported as a transport failure: %v", res.err)
	}
	if got := writer.attemptCount(); got != before {
		t.Errorf("the store was called %d extra time(s) for a rejected file", got-before)
	}
	if res.job.uploadIdentity != job.uploadIdentity {
		t.Errorf("result identity = %+v, want the submitted %+v", res.job.uploadIdentity, job.uploadIdentity)
	}

	// A vanished file is also a local failure, not a transport one.
	if err := os.Remove(staged); err != nil {
		t.Fatalf("remove staged file: %v", err)
	}
	if res, ok := w.execute(job); !ok || res.err == nil || !res.local {
		t.Errorf("execute() on a missing file = (%+v, ok=%v), want a local failure", res, ok)
	}
}

// 16. Repeated events and repeated reconciliation create no duplicate work.
func TestNoDuplicateQueueOrInFlightJobs(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)
	release := u.writer.blockWrites()

	backupPath := filepath.Join(u.logsDir, "raylet.out.1")
	u.captureOne(t, "raylet.out.1", "segment")
	select {
	case <-u.writer.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no upload started")
	}

	// The same file, seen again and again: fsnotify repeats, and every sweep looks
	// at the whole tree.
	for range 5 {
		u.sendEvent(t, backupPath)
		u.rc.reconcileNow()
	}

	s := u.rc.stats()
	if s.Captures != 1 {
		t.Fatalf("Captures = %d, want 1", s.Captures)
	}
	if s.InFlightUploads != 1 || s.QueuedUploads != 0 {
		t.Errorf("stats = %+v, want exactly one in-flight upload and an empty queue", s)
	}

	release()
	u.waitForUploaded(t, 1)
	u.rc.reconcileNow()
	if got := u.writer.attemptCount(); got != 1 {
		t.Errorf("%d storage attempts for one capture, want 1: %+v", got, u.writer.attempts())
	}
}

// blockPromotion makes promoteCapture's MkdirAll fail by occupying the uploaded
// state directory with a regular file. Removing that file makes promotion possible
// again.
func blockPromotion(t *testing.T, stagingRoot string) string {
	t.Helper()
	p := filepath.Join(stagingRoot, "session-1", "node-1", string(stateUploaded))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("create staging parent: %v", err)
	}
	writeFile(t, p, "not a directory")
	return p
}

// 17. A remote success whose local promotion fails is not uploaded again.
// 18. The promotion retry eventually succeeds without another storage call.
func TestRemoteSuccessWithLocalPromotionFailureRetriesPromotionOnly(t *testing.T) {
	dir := t.TempDir()
	blocker := blockPromotion(t, filepath.Join(dir, "rotated-staging"))
	u := startUploading(t, dir, nil)

	entry := u.captureOne(t, "raylet.out.1", "segment")
	u.waitForAttempts(t, 1)
	u.waitFor(t, "the capture to be awaiting promotion", func() bool {
		return u.rc.stats().AwaitingPromotion == 1
	})

	// The bytes are in storage but the capture is still pending on disk.
	if entries := u.rc.snapshot(); len(entries) != 1 || entries[0] != entry {
		t.Fatalf("capture = %+v, want it left exactly as submitted", entries)
	}
	if _, err := os.Lstat(entry.path(u.stagingRoot)); err != nil {
		t.Errorf("pending staging link was not preserved: %v", err)
	}
	if len(u.issues.matching("could not be promoted locally")) == 0 {
		t.Errorf("the promotion failure was not reported: %v", u.issues.all())
	}

	// Retrying must not send the bytes a second time.
	u.clock.advance(10 * time.Second)
	u.timers.fire(t)
	u.rc.reconcileNow()
	if got := u.writer.attemptCount(); got != 1 {
		t.Errorf("%d storage attempts while promotion was failing, want 1", got)
	}

	// Once promotion can succeed, the retry finishes the job with no further write.
	if err := os.Remove(blocker); err != nil {
		t.Fatalf("unblock promotion: %v", err)
	}
	u.clock.advance(10 * time.Second)
	u.timers.fire(t)
	u.waitForUploaded(t, 1)

	if got := u.writer.attemptCount(); got != 1 {
		t.Errorf("%d storage attempts in total, want 1: %+v", got, u.writer.attempts())
	}
	if got := u.rc.snapshot()[0].CaptureID; got != entry.CaptureID {
		t.Errorf("capture ID changed during promotion retry: %s -> %s", entry.CaptureID, got)
	}
	if s := u.rc.stats(); s.AwaitingPromotion != 0 {
		t.Errorf("stats = %+v, want nothing awaiting promotion", s)
	}
}

// segment writes a rotation backup of an exact size and delivers its event.
func (u *upHarness) segment(t *testing.T, name string, size int) {
	t.Helper()
	u.sendEvent(t, u.writeLog(t, name, strings.Repeat("x", size)))
}

// hasCapturedName reports whether any uploaded object key is a capture of originalName,
// whose key carries a capture ID the test cannot predict.
func hasCapturedName(uploaded map[string]bool, originalName string) bool {
	for name := range uploaded {
		if got, _, ok := parseCaptureFileName(name); ok && got == originalName {
			return true
		}
	}
	return false
}

// rolledOffSegment captures a rotation backup and then takes Ray's own link away, which
// is what Ray does when the segment falls off the end of its backup ring.
//
// Until that happens the capture shares Ray's blocks and retains nothing, so this is
// the only way a test can put real pressure on the intake watermark. The reconcile is
// what re-reads the link count: nothing touches the staging path when Ray unlinks its
// own name, so no event announces it.
func (u *upHarness) rolledOffSegment(t *testing.T, name string, size int) {
	t.Helper()
	u.segment(t, name, size)
	if err := os.Remove(filepath.Join(u.logsDir, name)); err != nil {
		t.Fatalf("remove Ray's link to %s: %v", name, err)
	}
	u.rc.reconcileNow()
}

// 19. Reaching the high-water mark pauses intake.
// 20. Uploads and releases keep running while intake is paused.
// 21. Nothing already staged is evicted at high water.
// 22. Falling to the low-water mark resumes intake.
// 23. Resuming reconciles immediately.
func TestBackpressurePausesIntakeWithoutEviction(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.HighWaterBytes = 200
		cfg.LowWaterBytes = 100
	})
	// Hold the uploads so the captures are still pending while the volume is over
	// its high-water mark: pending data is precisely what must never be evicted.
	release := u.writer.blockWrites()

	u.writeLog(t, "raylet.out", "active")
	// Ray has rolled both of these off its backup ring, so the collector is now the
	// only thing keeping their blocks allocated. That — not the logical size — is what
	// the watermark measures.
	u.rolledOffSegment(t, "raylet.out.1", 120)
	u.rolledOffSegment(t, "raylet.out.2", 120)

	// 19: 240 retained bytes is past the high-water mark of 200.
	s := u.rc.stats()
	if !s.IntakePaused {
		t.Fatalf("stats = %+v, want intake paused at 240 retained bytes", s)
	}
	if s.Captures != 2 || s.Pending != 2 || s.StagedBytes != 240 || s.RetainedBytes != 240 {
		t.Fatalf("stats = %+v, want 2 pending captures totalling 240 bytes, all retained", s)
	}
	if len(u.issues.matching("intake paused")) != 1 {
		t.Errorf("the pause was reported %d time(s), want exactly 1: %v",
			len(u.issues.matching("intake paused")), u.issues.all())
	}

	// 21: a new backup arriving under pressure is skipped, and no capture the
	// collector already holds is evicted to make room for it.
	u.segment(t, "raylet.out.3", 120)
	if s := u.rc.stats(); s.Captures != 2 || s.Pending != 2 || s.StagedBytes != 240 {
		t.Errorf("stats = %+v, want the two pending captures kept and the new one skipped", s)
	}
	staged := u.stagedPaths(t)
	if len(staged) != 2 {
		t.Errorf("staging holds %v, want the two captures made before the pause", staged)
	}

	// The pause is a transition, not a per-event message, and repeated pressure must
	// not start evicting either.
	for range 3 {
		u.segment(t, "raylet.out.4", 10)
	}
	if got := len(u.issues.matching("intake paused")); got != 1 {
		t.Errorf("the pause was reported %d times, want 1", got)
	}
	if s := u.rc.stats(); s.Captures != 2 || s.Pending != 2 {
		t.Errorf("stats = %+v after sustained pressure, want both captures still held", s)
	}

	// 20: the uploader is unaffected by the gate — it is working on a capture right now,
	// with intake shut, and finishing that work is what relieves the pressure.
	if s := u.rc.stats(); s.InFlightUploads != 1 || !s.IntakePaused {
		t.Errorf("stats = %+v, want an upload in flight while intake stays paused", s)
	}
	release()

	// 22 + 23: each capture that reaches storage is released — the collector holds the
	// only link, so unlinking actually frees the blocks — retained bytes fall to the
	// low-water mark, and the resume rescans the tree in the same pass, so the backup
	// skipped while paused is captured with no further event.
	u.waitFor(t, "intake to resume", func() bool { return !u.rc.stats().IntakePaused })

	s = u.rc.stats()
	if s.RetainedBytes != 0 {
		t.Errorf("stats = %+v, want every retained capture released once uploaded", s)
	}
	uploaded := map[string]bool{}
	for _, c := range u.writer.attempts() {
		uploaded[path.Base(c.key)] = true
	}
	for _, want := range []string{"raylet.out.1", "raylet.out.2"} {
		if !hasCapturedName(uploaded, want) {
			t.Errorf("%s never reached storage while intake was paused: %v", want, uploaded)
		}
	}
	names := map[string]bool{}
	for _, e := range u.rc.snapshot() {
		names[e.OriginalName] = true
	}
	if !names["raylet.out.3"] {
		t.Errorf("resuming did not reconcile: raylet.out.3 was never captured, got %v", names)
	}
}

// The high-water mark has to bite inside the scan that breaches it. A scan registers
// every backup it finds without returning to the event loop, so a limit that is only
// checked afterwards can be overshot by however many files happen to be on disk.
func TestHighWaterStopsCaptureWithinOneScan(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	// Four backups, 100 bytes each, all present before the collector starts. The
	// second one takes the total to 200, which is the limit.
	for i := 1; i <= 4; i++ {
		writeFile(t, filepath.Join(logsDir, "raylet.out."+strconv.Itoa(i)), strings.Repeat("x", 100))
	}

	u := startUploading(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.HighWaterBytes = 200
		cfg.LowWaterBytes = 100
		// Ray rolls each segment off its ring the instant it is captured, so every
		// capture is retained the moment it is made. Without that the scan could not
		// breach the limit at all: a capture Ray still has a link to shares Ray's
		// blocks and retains nothing.
		cfg.Link = func(src, dst string) error {
			if err := captureLink(src, dst); err != nil {
				return err
			}
			return os.Remove(src)
		}
	})

	s := u.rc.stats()
	if !s.IntakePaused {
		t.Fatalf("stats = %+v, want intake paused", s)
	}
	if s.Captures != 2 || s.StagedBytes != 200 || s.RetainedBytes != 200 {
		t.Fatalf("stats = %+v, want the scan stopped at the two captures that reach the limit", s)
	}

	captured := map[string]bool{}
	for _, e := range u.rc.snapshot() {
		captured[e.OriginalName] = true
	}
	for _, want := range []string{"raylet.out.1", "raylet.out.2"} {
		if !captured[want] {
			t.Errorf("%s was not captured: %v", want, captured)
		}
	}
	for _, notWant := range []string{"raylet.out.3", "raylet.out.4"} {
		if captured[notWant] {
			t.Errorf("%s was captured after the limit was already reached: %v", notWant, captured)
		}
	}
	if got := len(u.issues.matching("intake paused")); got != 1 {
		t.Errorf("the pause was reported %d time(s), want exactly 1: %v", got, u.issues.all())
	}

	// A further sweep must not quietly take the rest either.
	u.fireTick(t)
	if s := u.rc.stats(); s.Captures != 2 {
		t.Errorf("stats = %+v after another sweep, want still 2 captures", s)
	}

	// And the resume path still works: once Ray drops its links the sweep releases
	// both captures, the total falls to the low-water mark, intake resumes and the
	// same pass reconciles — picking up the backups that were skipped earlier. Those
	// are another 200 bytes, so the limit engages again, which is the loop working
	// exactly as intended rather than a failure.
	// Ray's links are already gone — the Link hook above dropped them at capture time —
	// so uploading is all that stands between these captures and release. Each release
	// frees real blocks, retained bytes fall to the low-water mark, intake resumes and
	// the same pass reconciles, which is what finally picks up the skipped backups.
	// They breach the limit again on the way through, and that loop is the design
	// working rather than a failure, so what is asserted is the outcome: everything
	// reaches storage and nothing is left retained.
	uploaded := map[string]bool{}
	u.waitFor(t, "the skipped backups to be captured and uploaded", func() bool {
		u.fireTick(t)
		for _, c := range u.writer.attempts() {
			uploaded[path.Base(c.key)] = true
		}
		return hasCapturedName(uploaded, "raylet.out.3") && hasCapturedName(uploaded, "raylet.out.4")
	})

	for _, want := range []string{"raylet.out.1", "raylet.out.2"} {
		if !hasCapturedName(uploaded, want) {
			t.Errorf("%s never reached storage: %v", want, uploaded)
		}
	}
	if s := u.rc.stats(); s.RetainedBytes != 0 || s.IntakePaused {
		t.Errorf("stats = %+v, want everything drained and intake open once the tree is exhausted", s)
	}
}

// A restart that adopts a volume already over its limit must not capture its way
// further past it during the startup scan.
func TestStartupPausesBeforeScanningWhenReconstructionIsAboveHighWater(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")

	// The previous run left 250 staged bytes behind, and Ray has since rolled that
	// segment off its backup ring, so the staging link is the only thing keeping those
	// blocks allocated — which is what makes them count against the limit.
	reconstructed, _ := stageManually(t, logsDir, stagingRoot, "raylet.out.5", strings.Repeat("r", 250), false)
	if err := os.Remove(filepath.Join(logsDir, "raylet.out.5")); err != nil {
		t.Fatalf("remove Ray's link to raylet.out.5: %v", err)
	}
	// ...and an eligible backup is sitting in the live tree.
	writeFile(t, filepath.Join(logsDir, "raylet.out.1"), strings.Repeat("x", 10))

	u := startUploading(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.HighWaterBytes = 200
		cfg.LowWaterBytes = 100
		cfg.Writer = nil // uploads off: this test is only about the startup gate
	})

	s := u.rc.stats()
	if !s.IntakePaused {
		t.Fatalf("stats = %+v, want intake paused before the startup live scan", s)
	}
	if s.Captures != 1 || s.StagedBytes != 250 {
		t.Fatalf("stats = %+v, want only the reconstructed capture", s)
	}

	entries := u.rc.snapshot()
	if len(entries) != 1 || entries[0].CaptureID != reconstructed.CaptureID {
		t.Fatalf("snapshot = %+v, want just the reconstructed capture %s", entries, reconstructed.CaptureID)
	}
	if _, err := os.Lstat(reconstructed.path(stagingRoot)); err != nil {
		t.Errorf("the reconstructed capture was not retained: %v", err)
	}
	if got := u.stagedPaths(t); len(got) != 1 {
		t.Errorf("staging holds %v, want only the reconstructed capture", got)
	}
}

// Within one sweep an early link can succeed and a later one hit ENOSPC. The stale
// success must not be what clears the newer failure.
func TestLaterENOSPCIsNotClearedByAnEarlierSuccess(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")
	writeFile(t, filepath.Join(logsDir, "raylet.out.1"), "first, links fine")
	writeFile(t, filepath.Join(logsDir, "raylet.out.2"), "second, no space left")

	var mu sync.Mutex
	calls := 0
	full := true
	u := startUploading(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.Link = func(src, dst string) error {
			mu.Lock()
			calls++
			// The very first link of the run succeeds; while the volume is full
			// every later one fails.
			fail := full && calls > 1
			mu.Unlock()
			if fail {
				return syscall.ENOSPC
			}
			return captureLink(src, dst)
		}
	})

	// The startup scan captured the first and hit ENOSPC on the second, in that
	// order. The gate must reflect the last thing the filesystem said.
	s := u.rc.stats()
	if !s.IntakePaused {
		t.Fatalf("stats = %+v, want intake paused: an earlier success cleared a later ENOSPC", s)
	}
	if s.Captures != 1 {
		t.Fatalf("stats = %+v, want the first capture kept and the second refused", s)
	}
	if len(u.issues.matching("intake paused")) == 0 {
		t.Errorf("the ENOSPC pause was not reported: %v", u.issues.all())
	}
	// The count, not the flag, is the real evidence. A gate wrongly reopened by the
	// stale success would be shut again by the very next failing scan, leaving
	// IntakePaused looking correct while intake had in fact been reopened.
	if s.IntakeResumes != 0 {
		t.Errorf("intake was resumed %d time(s) while the volume was full: a stale success cleared a later ENOSPC", s.IntakeResumes)
	}

	// It must stay paused across further evaluations, not just the first.
	for range 3 {
		u.fireTick(t)
		s := u.rc.stats()
		if !s.IntakePaused {
			t.Fatalf("stats = %+v, want intake still paused while the volume is full", s)
		}
		if s.IntakeResumes != 0 {
			t.Fatalf("intake was resumed %d time(s) across sweeps while the volume was full", s.IntakeResumes)
		}
	}

	// Once capacity genuinely comes back, the next bounded probe succeeds and lifts
	// the pause.
	mu.Lock()
	full = false
	mu.Unlock()
	u.fireTick(t)

	s = u.rc.stats()
	if s.IntakePaused {
		t.Errorf("stats = %+v, want intake resumed after a probe succeeded", s)
	}
	if s.IntakeResumes != 1 {
		t.Errorf("intake resumed %d time(s), want exactly the one that followed real capacity recovery", s.IntakeResumes)
	}
	names := map[string]bool{}
	for _, e := range u.rc.snapshot() {
		names[e.OriginalName] = true
	}
	if !names["raylet.out.2"] {
		t.Errorf("the refused backup was not captured after recovery: %v", names)
	}
}

// 24. ENOSPC pauses intake, and a later successful release lets it resume.
func TestENOSPCPausesIntakeUntilSpaceIsReleased(t *testing.T) {
	dir := t.TempDir()

	var mu sync.Mutex
	full := false
	setFull := func(v bool) {
		mu.Lock()
		defer mu.Unlock()
		full = v
	}
	u := startUploading(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.Link = func(src, dst string) error {
			mu.Lock()
			defer mu.Unlock()
			if full {
				return syscall.ENOSPC
			}
			return captureLink(src, dst)
		}
	})

	// One capture the collector owns and can eventually release: that release is
	// what will free space later.
	held := u.captureOne(t, "raylet.out.9", "old segment")
	u.waitForUploaded(t, 1)
	if err := os.Remove(filepath.Join(u.logsDir, "raylet.out.9")); err != nil {
		t.Fatalf("remove Ray's link: %v", err)
	}

	// The volume fills up — not because of what the collector holds, which is why
	// watermarks cannot see this coming.
	setFull(true)
	backup := u.writeLog(t, "raylet.out.1", "new segment")
	u.sendEvent(t, backup)

	s := u.rc.stats()
	if !s.IntakePaused {
		t.Fatalf("stats = %+v, want intake paused after ENOSPC", s)
	}
	if s.Captures != 1 {
		t.Errorf("stats = %+v, want nothing new captured while the volume was full", s)
	}
	if len(u.issues.matching("intake paused")) == 0 {
		t.Errorf("ENOSPC was not reported as an intake pause: %v", u.issues.all())
	}

	// Space comes back, and the sweep that releases the held capture is what lets
	// intake resume and rescan.
	setFull(false)
	u.fireTick(t)

	if s := u.rc.stats(); s.IntakePaused {
		t.Errorf("stats = %+v, want intake resumed after a successful release", s)
	}
	if _, err := os.Lstat(held.withState(stateUploaded).path(u.stagingRoot)); !os.IsNotExist(err) {
		t.Errorf("the releasable capture was not released: %v", err)
	}
	names := map[string]bool{}
	for _, e := range u.rc.snapshot() {
		names[e.OriginalName] = true
	}
	if !names["raylet.out.1"] {
		t.Errorf("the backup skipped during ENOSPC was not captured after resuming: %v", names)
	}
}

// countingLink wraps captureLink with an ENOSPC switch and an attempt counter, so a
// test can prove how often a paused collector actually touches the filesystem.
type countingLink struct {
	mu       sync.Mutex
	attempts int
	full     bool
}

func (l *countingLink) link(src, dst string) error {
	l.mu.Lock()
	l.attempts++
	full := l.full
	l.mu.Unlock()
	if full {
		return syscall.ENOSPC
	}
	return captureLink(src, dst)
}

func (l *countingLink) setFull(v bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.full = v
}

func (l *countingLink) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.attempts
}

// A disk-full pause must not latch. The volume can be filled and then emptied by
// something that is not this collector, and with nothing of its own to release the
// collector would otherwise never find out.
func TestENOSPCRecoversWhenSpaceIsFreedExternally(t *testing.T) {
	dir := t.TempDir()
	link := &countingLink{full: true}
	u := startUploading(t, dir, func(cfg *rotatedCollectorConfig) { cfg.Link = link.link })

	// Nothing has ever been captured, so there is nothing this collector could
	// release to free space.
	u.writeLog(t, "raylet.out", "active")
	backup := u.writeLog(t, "raylet.out.1", "the surviving backup")
	u.sendEvent(t, backup)

	s := u.rc.stats()
	if !s.IntakePaused {
		t.Fatalf("stats = %+v, want intake paused after ENOSPC", s)
	}
	if s.Captures != 0 {
		t.Fatalf("stats = %+v, want nothing captured", s)
	}
	if got := len(u.issues.matching("intake paused")); got != 1 {
		t.Fatalf("the pause was reported %d time(s), want exactly 1: %v", got, u.issues.all())
	}
	afterPause := link.count()

	// Further sweeps while the volume is still full: exactly one probe each, no
	// captures, no queued work, and no repeat of the pause report.
	for i := range 3 {
		u.fireTick(t)
		if got := link.count() - afterPause; got != i+1 {
			t.Errorf("after %d sweep(s) the collector made %d link attempts, want one probe per sweep", i+1, got)
		}
		s := u.rc.stats()
		if !s.IntakePaused || s.Captures != 0 || s.QueuedUploads != 0 || s.InFlightUploads != 0 {
			t.Fatalf("stats = %+v after a failed probe, want still paused with no work", s)
		}
	}
	if got := len(u.issues.matching("intake paused")); got != 1 {
		t.Errorf("failed probes reported the pause %d times, want 1: %v", got, u.issues.all())
	}
	if got := u.stagedPaths(t); len(got) != 0 {
		t.Errorf("failed probes created staging links: %v", got)
	}

	// Something else frees the volume. The next sweep's probe succeeds, which is the
	// only evidence the collector will accept, and intake resumes without this
	// collector having released anything of its own.
	link.setFull(false)
	u.fireTick(t)

	s = u.rc.stats()
	if s.IntakePaused {
		t.Fatalf("stats = %+v, want intake resumed once a probe proved there was space", s)
	}
	names := map[string]bool{}
	for _, e := range u.rc.snapshot() {
		names[e.OriginalName] = true
	}
	if !names["raylet.out.1"] {
		t.Errorf("the surviving backup was not captured after recovery: %v", names)
	}
	u.waitForUploaded(t, 1)
}

// A watermark pause is the collector's own limit, so the disk-full probe must not
// punch through it.
func TestCapacityProbeDoesNotBypassTheWatermark(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.HighWaterBytes = 100
		cfg.LowWaterBytes = 50
	})
	release := u.writer.blockWrites()
	defer release()

	u.writeLog(t, "raylet.out", "active")
	// Ray has rolled this one off, so the collector alone retains its 150 bytes and the
	// watermark engages.
	u.rolledOffSegment(t, "raylet.out.1", 150)
	if s := u.rc.stats(); !s.IntakePaused || s.Captures != 1 {
		t.Fatalf("stats = %+v, want intake paused at the high-water mark with one capture", s)
	}

	u.writeLog(t, "raylet.out.2", "would be captured if the probe leaked through")
	for range 3 {
		u.fireTick(t)
	}
	if s := u.rc.stats(); s.Captures != 1 {
		t.Errorf("stats = %+v, want the watermark pause to hold: a probe bypassed it", s)
	}
}

// 25. Startup accounting counts pending and uploaded entries exactly once each.
func TestStartupAccountingCountsEveryStagedInodeOnce(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")

	const pendingSize, uploadedSize = 40, 25
	stageManually(t, logsDir, stagingRoot, "raylet.out.1", strings.Repeat("p", pendingSize), false)
	stageManually(t, logsDir, stagingRoot, "raylet.out.2", strings.Repeat("u", uploadedSize), true)

	u := startUploading(t, dir, nil)
	u.writer.setFailAll(true) // keep the pending entry pending

	s := u.rc.stats()
	if s.Captures != 2 || s.Pending != 1 || s.Uploaded != 1 {
		t.Fatalf("stats = %+v, want one pending and one uploaded capture", s)
	}
	if want := int64(pendingSize + uploadedSize); s.StagedBytes != want {
		t.Errorf("StagedBytes = %d, want %d", s.StagedBytes, want)
	}

	// Repeated sweeps re-see the same staged files and must not count them again.
	u.fireTick(t)
	u.rc.reconcileNow()
	if got := u.rc.stats().StagedBytes; got != int64(pendingSize+uploadedSize) {
		t.Errorf("StagedBytes = %d after further sweeps, want %d", got, pendingSize+uploadedSize)
	}
}

// 26. Promotion does not alter byte accounting.
func TestPromotionDoesNotChangeAccounting(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)
	release := u.writer.blockWrites()

	const size = 64
	u.writeLog(t, "raylet.out", "active")
	u.segment(t, "raylet.out.1", size)

	before := u.rc.stats()
	if before.StagedBytes != size || before.Pending != 1 {
		t.Fatalf("stats before promotion = %+v, want %d pending bytes", before, size)
	}

	release()
	u.waitForUploaded(t, 1)

	after := u.rc.stats()
	if after.StagedBytes != before.StagedBytes {
		t.Errorf("StagedBytes changed on promotion: %d -> %d", before.StagedBytes, after.StagedBytes)
	}
	if after.Uploaded != 1 {
		t.Errorf("stats after promotion = %+v, want one uploaded capture", after)
	}
}

// 28. Storage calls use the owner-aware key beneath the node's logs directory.
func TestObjectKeysAreOwnerAwareAndUnderTheClusterPrefix(t *testing.T) {
	dir := t.TempDir()
	owned := clusterIdentity{
		RootDir:     "root",
		OwnerKind:   "RayJob",
		OwnerName:   "job-1",
		Namespace:   "ns",
		ClusterName: "cluster-a",
	}
	u := startUploading(t, dir, func(cfg *rotatedCollectorConfig) { cfg.Cluster = owned })

	u.writeLog(t, "raylet.out", "active")
	u.writeLog(t, "events/event.log", "active nested")
	flat := u.captureOne(t, "raylet.out.1", "flat segment")

	nested := u.writeLog(t, "events/event.log.1", "nested segment")
	u.sendEvent(t, nested)
	u.waitForUploaded(t, 2)

	prefix := "root/cluster-history/rayjob/ns/job-1/cluster-a/session-1/node-1/logs"
	wantFlat := path.Join(prefix, "raylet.out.1"+captureIDSeparator+flat.CaptureID)

	var nestedEntry stagedEntry
	for _, e := range u.rc.snapshot() {
		if e.OriginalName == "event.log.1" {
			nestedEntry = e
		}
	}
	wantNested := path.Join(prefix, "events", "event.log.1"+captureIDSeparator+nestedEntry.CaptureID)

	keys := map[string]bool{}
	for _, c := range u.writer.attempts() {
		keys[c.key] = true
		if !strings.HasPrefix(c.key, prefix+"/") {
			t.Errorf("key %q escapes the cluster log prefix %q", c.key, prefix)
		}
	}
	for _, want := range []string{wantFlat, wantNested} {
		if !keys[want] {
			t.Errorf("no write to %q, got %v", want, keys)
		}
	}
}

// 29. Successive generations at one X.N path upload under distinct keys.
func TestSuccessiveGenerationsProduceDistinctKeys(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)
	u.writeLog(t, "raylet.out", "active")

	backup := filepath.Join(u.logsDir, "raylet.out.1")
	writeFile(t, backup, "first generation")
	u.sendEvent(t, backup)
	u.waitForAttempts(t, 1)

	// Rotation replaces the same name with a completely different file. Removing the
	// old one also lets the first capture be released, so the proof that both
	// generations were preserved is in the object keys, not in the index.
	if err := os.Remove(backup); err != nil {
		t.Fatalf("remove first generation: %v", err)
	}
	writeFile(t, backup, "second generation")
	u.sendEvent(t, backup)
	u.waitForAttempts(t, 2)

	keys := map[string]string{}
	for _, c := range u.writer.attempts() {
		if c.err == nil {
			keys[c.key] = c.content
		}
	}
	if len(keys) != 2 {
		t.Fatalf("two generations produced %d distinct keys: %v", len(keys), keys)
	}
	if u.writer.attemptCount() != 2 {
		t.Errorf("%d storage attempts for two generations, want 2", u.writer.attemptCount())
	}
	contents := map[string]bool{}
	for _, v := range keys {
		contents[v] = true
	}
	if !contents["first generation"] || !contents["second generation"] {
		t.Errorf("both generations were not uploaded: %v", keys)
	}
}

// 30. Worker and owner exit cleanly, with no goroutine left behind.
func TestUploaderStopsWithoutLeakingGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	dir := t.TempDir()
	u := startUploading(t, dir, nil)

	u.captureOne(t, "raylet.out.1", "segment")
	u.waitForUploaded(t, 1)

	u.rc.Stop()
	u.rc.Stop() // idempotent

	select {
	case err := <-u.runErr:
		if err != nil {
			t.Errorf("Run() returned %v, want nil on a deliberate stop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after Stop()")
	}

	for range 40 {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("goroutines leaked: %d before, %d after", before, runtime.NumGoroutine())
}

// 31. Stop is prompt even mid-upload, and the late result changes nothing.
func TestLateUploadResultAfterStopCannotMutateState(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)
	release := u.writer.blockWrites()

	entry := u.captureOne(t, "raylet.out.1", "segment")
	select {
	case <-u.writer.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no upload started")
	}

	// The worker is inside an object write that cannot be canceled. Stop must not
	// wait for it.
	start := time.Now()
	u.rc.Stop()
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Stop() took %v while an upload was blocked", elapsed)
	}
	select {
	case err := <-u.runErr:
		if err != nil {
			t.Errorf("Run() returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return while an upload was blocked")
	}
	if len(u.issues.matching("still running at stop")) == 0 {
		t.Errorf("the uncancelable upload was not reported at stop: %v", u.issues.all())
	}

	// The upload now completes with nobody listening.
	release()
	u.waitFor(t, "the abandoned upload to finish", func() bool { return u.writer.attemptCount() == 1 })
	time.Sleep(100 * time.Millisecond)

	// Disk still says pending, which is what lets the next run retry it.
	if _, err := os.Lstat(entry.path(u.stagingRoot)); err != nil {
		t.Errorf("pending staging link was mutated after stop: %v", err)
	}
	if _, err := os.Lstat(entry.withState(stateUploaded).path(u.stagingRoot)); !os.IsNotExist(err) {
		t.Errorf("a result delivered after stop promoted the capture: %v", err)
	}
	if entries := u.rc.snapshot(); entries != nil {
		t.Errorf("snapshot() after Stop() = %+v, want nil", entries)
	}
}

// 32. Everything above runs under -race; this exercises the request seams against a
// live uploader so that concurrent readers are covered too.
func TestUploaderStateIsOnlyTouchedByTheOwnerLoop(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, nil)
	u.writeLog(t, "raylet.out", "active")

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 25 {
				u.rc.snapshot()
				u.rc.stats()
			}
		})
	}
	for i := range 15 {
		u.writeLog(t, "raylet.out."+strconv.Itoa(i+1), "segment "+strconv.Itoa(i))
	}
	u.rc.reconcileNow()
	wg.Wait()

	u.waitForUploaded(t, 15)
	if got := u.writer.attemptCount(); got != 15 {
		t.Errorf("%d storage attempts for 15 captures, want 15", got)
	}
}

// The watermark configuration has to be self-consistent or backpressure could pause
// and never resume.
func TestWatermarkConfigurationIsValidated(t *testing.T) {
	tests := []struct {
		name    string
		high    int64
		low     int64
		wantErr bool
	}{
		{name: "disabled", high: 0, low: 0},
		{name: "valid", high: 100, low: 50},
		{name: "zero low water", high: 100, low: 0},
		{name: "low equals high", high: 100, low: 100, wantErr: true},
		{name: "low above high", high: 100, low: 200, wantErr: true},
		{name: "low without high", high: 0, low: 50, wantErr: true},
		{name: "negative", high: -1, low: 0, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWatermarks(tc.high, tc.low)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateWatermarks(%d, %d) = %v, wantErr %v", tc.high, tc.low, err, tc.wantErr)
			}
		})
	}
}

// A non-positive retry delay would make a failed upload due the moment it failed,
// turning the backoff schedule into a spin against the object store.
func TestUploadBackoffMustBePositive(t *testing.T) {
	base := func(dir string) rotatedCollectorConfig {
		return rotatedCollectorConfig{
			LogsDir:     filepath.Join(dir, "logs"),
			StagingRoot: filepath.Join(dir, "staging"),
			SessionName: "session-1",
			NodeName:    "node-1",
		}
	}
	tests := []struct {
		name    string
		backoff []time.Duration
		wantErr bool
	}{
		{name: "default", backoff: nil},
		{name: "positive", backoff: []time.Duration{time.Second, time.Minute}},
		{name: "contains zero", backoff: []time.Duration{time.Second, 0}, wantErr: true},
		{name: "contains negative", backoff: []time.Duration{-time.Second}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base(t.TempDir())
			cfg.UploadBackoff = tc.backoff
			_, err := newRotatedCollector(cfg)
			if (err != nil) != tc.wantErr {
				t.Errorf("newRotatedCollector(UploadBackoff=%v) error = %v, wantErr %v", tc.backoff, err, tc.wantErr)
			}
		})
	}
}

// recomputeFixture is one indexed capture with a staged file, ready to be corrupted.
type recomputeFixture struct {
	b           *stagedBytes
	ix          *captureIndex
	entry       stagedEntry
	stagingRoot string
	key         inodeKey
}

func newRecomputeFixture(t *testing.T, size int) *recomputeFixture {
	t.Helper()
	stagingRoot := filepath.Join(t.TempDir(), "staging")
	entry, err := newStagedEntry(stateUploaded, "session-1", "node-1", "", "raylet.out.1", "1234567890123456789.abcdefabcdef0123")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	writeFile(t, entry.path(stagingRoot), strings.Repeat("z", size))
	key, _, err := statInode(entry.path(stagingRoot))
	if err != nil {
		t.Fatalf("statInode() error: %v", err)
	}
	ix := newCaptureIndex()
	if _, err := ix.restore(key, entry); err != nil {
		t.Fatalf("restore() error: %v", err)
	}
	b := newStagedBytes()
	// The fixture stages a standalone file, so the collector is its only owner and it
	// is retained as well as staged.
	b.observe(key, int64(size), 1)
	return &recomputeFixture{b: b, ix: ix, entry: entry, stagingRoot: stagingRoot, key: key}
}

func (f *recomputeFixture) staged() string { return f.entry.path(f.stagingRoot) }

func (f *recomputeFixture) recompute(t *testing.T) []error {
	t.Helper()
	var issues []error
	f.b.recompute(f.stagingRoot, f.ix, func(err error) { issues = append(issues, err) })
	return issues
}

// Byte accounting that loses track of itself must recompute from the index rather
// than let the total drift.
func TestStagedBytesRecomputesWhenIncrementalAccountingIsUncertain(t *testing.T) {
	f := newRecomputeFixture(t, 32)

	// A total that was never told about a capture, and a forget for one it never
	// knew, are both ways of ending up wrong.
	f.b.total = 999
	f.b.forget(inodeKey{Dev: 1, Ino: 2})
	if !f.b.stale {
		t.Fatal("forgetting an untracked capture did not mark accounting stale")
	}

	if issues := f.recompute(t); len(issues) != 0 {
		t.Fatalf("recompute reported %v on a healthy staging volume", issues)
	}
	if f.b.total != 32 {
		t.Errorf("total = %d after recompute, want 32", f.b.total)
	}
	if !f.b.trusted() {
		t.Error("accounting is still stale after a fully verified recompute")
	}
}

// A capture the collector cannot understand must not be silently written down to
// zero: a falling total is what releases backpressure, so an unverifiable entry would
// otherwise look exactly like pressure that had eased.
func TestStagedBytesRecomputeIsConservativeAboutUnverifiableCaptures(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(t *testing.T, f *recomputeFixture)
		wantIn  string
	}{
		{
			name: "staged file is missing",
			corrupt: func(t *testing.T, f *recomputeFixture) {
				t.Helper()
				if err := os.Remove(f.staged()); err != nil {
					t.Fatalf("remove staged file: %v", err)
				}
			},
			wantIn: "stat capture",
		},
		{
			name: "staged path holds another inode",
			corrupt: func(t *testing.T, f *recomputeFixture) {
				t.Helper()
				if err := os.Remove(f.staged()); err != nil {
					t.Fatalf("remove staged file: %v", err)
				}
				// A much larger replacement: counting it would corrupt the total in
				// the other direction.
				writeFile(t, f.staged(), strings.Repeat("q", 500))
			},
			wantIn: "not the pinned",
		},
		{
			name: "staged path is no longer a regular file",
			corrupt: func(t *testing.T, f *recomputeFixture) {
				t.Helper()
				if err := os.Remove(f.staged()); err != nil {
					t.Fatalf("remove staged file: %v", err)
				}
				if err := os.Mkdir(f.staged(), 0o750); err != nil {
					t.Fatalf("replace staged file with a directory: %v", err)
				}
			},
			wantIn: "not a regular file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newRecomputeFixture(t, 32)
			tc.corrupt(t, f)

			issues := f.recompute(t)
			if len(issues) != 1 || !strings.Contains(issues[0].Error(), tc.wantIn) {
				t.Fatalf("recompute reported %v, want one issue mentioning %q", issues, tc.wantIn)
			}
			if f.b.total != 32 {
				t.Errorf("total = %d, want the last known 32 retained rather than replaced", f.b.total)
			}
			if f.b.trusted() {
				t.Error("accounting was marked trusted despite an unverifiable capture")
			}

			// Untrusted accounting may pause intake but must never resume it.
			g := &intakeGate{high: 100, low: 50, watermark: true}
			if _, resumed := g.evaluate(f.b.total, f.b.trusted()); resumed || !g.paused() {
				t.Errorf("gate resumed on an unverified total (resumed=%v, paused=%v)", resumed, g.paused())
			}

			// Re-stage the capture so the path and the index agree again. The next
			// sweep must then be able to trust itself.
			if err := os.RemoveAll(f.staged()); err != nil {
				t.Fatalf("clear staged path: %v", err)
			}
			writeFile(t, f.staged(), strings.Repeat("z", 32))
			restored, _, err := statInode(f.staged())
			if err != nil {
				t.Fatalf("statInode() error: %v", err)
			}
			delete(f.ix.byInode, f.key)
			if _, err := f.ix.restore(restored, f.entry); err != nil {
				t.Fatalf("restore() error: %v", err)
			}

			if issues := f.recompute(t); len(issues) != 0 {
				t.Fatalf("recompute still reported %v after the capture was restored", issues)
			}
			if !f.b.trusted() {
				t.Error("accounting is still stale after every capture verified")
			}
			if f.b.total != 32 {
				t.Errorf("total = %d after recovery, want 32", f.b.total)
			}
		})
	}
}

// uploadableJob stages a real file and returns a job that would upload cleanly.
func uploadableJob(t *testing.T) (uploadJob, string) {
	t.Helper()
	stagingRoot := filepath.Join(t.TempDir(), "staging")
	entry, err := newStagedEntry(statePending, "session-1", "node-1", "", "raylet.out.1", "1234567890123456789.abcdefabcdef0123")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	writeFile(t, entry.path(stagingRoot), "a perfectly uploadable capture")
	key, _, err := statInode(entry.path(stagingRoot))
	if err != nil {
		t.Fatalf("statInode() error: %v", err)
	}
	return uploadJob{uploadIdentity: uploadIdentity{
		inode:     key,
		entry:     entry,
		localPath: entry.path(stagingRoot),
		objectKey: entry.objectKey(testCluster),
	}}, stagingRoot
}

// Local validation takes real syscalls, and Stop can begin while they run. A job that
// has passed validation but has not reached the object store must not reach it.
func TestWorkerDoesNotStartTheRemoteWriteAfterQuit(t *testing.T) {
	job, stagingRoot := uploadableJob(t)
	writer := newFakeWriter()

	reached := make(chan struct{})
	release := make(chan struct{})
	quit := make(chan struct{})
	jobs := make(chan uploadJob, 1)
	results := make(chan uploadResult, 1)
	done := make(chan struct{})

	w := &uploadWorker{
		writer:      writer,
		jobs:        jobs,
		results:     results,
		quit:        quit,
		stagingRoot: stagingRoot,
		// Hold the worker in the window between validating the staged file and
		// deciding whether to write it.
		beforeWrite: func() {
			close(reached)
			<-release
		},
	}
	jobs <- job
	go w.run(done)

	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never reached the pre-write checkpoint")
	}

	// Stop begins while the worker sits between validation and the write.
	close(quit)
	close(release)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not exit after quit closed during validation")
	}
	if got := writer.attemptCount(); got != 0 {
		t.Errorf("%d storage call(s) started after shutdown began, want 0", got)
	}
	if len(results) != 0 {
		t.Errorf("a discarded job produced a result: %+v", <-results)
	}
}

// An upload that has not begun must not begin after Stop. A buffered job and a closed
// quit channel are both ready, and select chooses between ready cases at random, so
// the guard has to be re-checked after the job is taken.
func TestWorkerDoesNotStartAQueuedJobAfterQuit(t *testing.T) {
	job, stagingRoot := uploadableJob(t)

	// Repeated because select is random when both cases are ready: one run proves
	// nothing, many runs cover both orderings.
	const rounds = 200
	writer := newFakeWriter()
	for range rounds {
		jobs := make(chan uploadJob, 1)
		results := make(chan uploadResult, 1)
		quit := make(chan struct{})
		done := make(chan struct{})

		jobs <- job // queued, not started
		close(quit) // Stop begins before the worker ever looks at the job

		w := &uploadWorker{writer: writer, jobs: jobs, results: results, quit: quit, stagingRoot: stagingRoot}
		go w.run(done)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the worker did not exit after quit closed")
		}
		if len(results) != 0 {
			t.Fatalf("the worker produced a result for a job it should not have started: %+v", <-results)
		}
	}

	if got := writer.attemptCount(); got != 0 {
		t.Errorf("%d storage call(s) were made for jobs queued before quit, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Retained-byte accounting. A capture is a hard link, so it costs no additional
// blocks while Ray still has its own link to the segment. Only once Ray rolls the
// segment off its backup ring is the collector keeping those blocks alive, and only
// that is what the intake watermark may measure.
// ---------------------------------------------------------------------------

// A capture Ray still owns must not count against the watermark. Gating on logical
// staged bytes charged the collector for Ray's entire backup ring, which pauses intake
// during ordinary healthy rotation and silently stops the feature doing its job.
func TestRetainedBytesExcludeBlocksRayStillOwns(t *testing.T) {
	dir := t.TempDir()
	// A watermark far below the segment: logical accounting would pause immediately.
	u := startUploading(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.HighWaterBytes = 10
		cfg.LowWaterBytes = 5
		cfg.Writer = nil // uploads off: this is only about accounting
	})

	u.writeLog(t, "raylet.out", "active")
	u.segment(t, "raylet.out.1", 500)

	s := u.rc.stats()
	if s.Captures != 1 {
		t.Fatalf("stats = %+v, want the backup captured", s)
	}
	if s.StagedBytes != 500 {
		t.Errorf("StagedBytes = %d, want the full logical size 500", s.StagedBytes)
	}
	if s.RetainedBytes != 0 {
		t.Errorf("RetainedBytes = %d, want 0 while Ray still holds its own link", s.RetainedBytes)
	}
	if s.IntakePaused {
		t.Errorf("stats = %+v, want intake open: the capture shares Ray's blocks and retains nothing", s)
	}

	// Sweeps must not drift into charging for it either.
	u.fireTick(t)
	if s := u.rc.stats(); s.RetainedBytes != 0 || s.IntakePaused {
		t.Errorf("stats = %+v after a sweep, want the capture still retaining nothing", s)
	}
}

// Once Ray rolls the segment off its ring the collector's link is the last one, so the
// blocks exist only because of this feature and must be charged for. Nothing touches
// the staging path when Ray unlinks its own name, so the reconcile sweep is what has
// to notice.
func TestRetainedBytesCountCapturesRayHasRolledOff(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.Writer = nil
	})

	u.writeLog(t, "raylet.out", "active")
	u.segment(t, "raylet.out.1", 500)
	if s := u.rc.stats(); s.RetainedBytes != 0 {
		t.Fatalf("stats = %+v, want nothing retained while Ray holds its link", s)
	}

	// Ray rolls the segment off the end of its backup ring.
	if err := os.Remove(filepath.Join(u.logsDir, "raylet.out.1")); err != nil {
		t.Fatalf("remove Ray's link: %v", err)
	}
	// No event fires for a path outside the staging tree, so only the sweep can see it.
	u.fireTick(t)

	s := u.rc.stats()
	if s.StagedBytes != 500 {
		t.Errorf("StagedBytes = %d, want the logical size unchanged at 500", s.StagedBytes)
	}
	if s.RetainedBytes != 500 {
		t.Errorf("RetainedBytes = %d, want 500 now that the collector holds the only link", s.RetainedBytes)
	}
}

// The property B1 exists for: a storage outage cannot grow local disk without bound,
// and recovery restores capture on its own.
func TestOutageRetainsBoundedDiskThenResumes(t *testing.T) {
	dir := t.TempDir()
	u := startUploading(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.HighWaterBytes = 200
		cfg.LowWaterBytes = 100
	})
	// The object store is refusing writes, so nothing can be released.
	u.writer.setFailAll(true)

	u.writeLog(t, "raylet.out", "active")
	u.rolledOffSegment(t, "raylet.out.1", 120)
	u.rolledOffSegment(t, "raylet.out.2", 120)

	s := u.rc.stats()
	if s.RetainedBytes != 240 || !s.IntakePaused {
		t.Fatalf("stats = %+v, want 240 retained bytes and intake paused", s)
	}
	if s.Captures != 2 || s.Pending != 2 {
		t.Fatalf("stats = %+v, want both captures held as pending", s)
	}

	// Nothing already captured is evicted to make room, and new backups are skipped
	// rather than displacing what is already held.
	u.rolledOffSegment(t, "raylet.out.3", 120)
	s = u.rc.stats()
	if s.Captures != 2 || s.Pending != 2 || s.RetainedBytes != 240 {
		t.Fatalf("stats = %+v, want the held captures kept and the new backup skipped", s)
	}
	if got := u.stagedPaths(t); len(got) != 2 {
		t.Errorf("staging holds %v, want only the two captures made before the pause", got)
	}

	// Retry work continues while intake is shut — that is what eventually clears it.
	before := u.writer.attemptCount()
	u.clock.advance(time.Hour)
	u.fireTick(t)
	u.waitFor(t, "uploads to keep being retried while paused", func() bool {
		u.clock.advance(time.Hour)
		u.fireTick(t)
		return u.writer.attemptCount() > before
	})
	if s := u.rc.stats(); !s.IntakePaused {
		t.Errorf("stats = %+v, want intake still paused while nothing has been released", s)
	}

	// Storage recovers. The uploads succeed, the collector is the only link holder so
	// each release frees real blocks, retained bytes fall past the low mark and intake
	// reopens on its own.
	u.writer.setFailAll(false)
	u.waitFor(t, "intake to resume once the retained captures drain", func() bool {
		u.clock.advance(time.Hour)
		u.fireTick(t)
		return !u.rc.stats().IntakePaused
	})
	if s := u.rc.stats(); s.RetainedBytes > u.rc.gate.low {
		t.Errorf("stats = %+v, want retained bytes at or below the low-water mark", s)
	}
}

// A restart has no persisted record of which captures the collector alone was holding,
// so retention has to be re-derived from the link counts on the staging volume itself.
func TestReconstructionComputesRetainedFromLinkCounts(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "session", "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")

	// One staged capture Ray still has a link to...
	stageManually(t, logsDir, stagingRoot, "raylet.out.1", strings.Repeat("a", 300), false)
	// ...and one Ray has already rolled off, leaving staging as the only reference.
	stageManually(t, logsDir, stagingRoot, "raylet.out.2", strings.Repeat("b", 700), false)
	if err := os.Remove(filepath.Join(logsDir, "raylet.out.2")); err != nil {
		t.Fatalf("remove Ray's link to raylet.out.2: %v", err)
	}

	u := startUploading(t, dir, func(cfg *rotatedCollectorConfig) {
		cfg.Writer = nil // uploads off, so reconstruction is what is measured
	})

	s := u.rc.stats()
	if s.Captures != 2 {
		t.Fatalf("stats = %+v, want both staged captures adopted", s)
	}
	if s.StagedBytes != 1000 {
		t.Errorf("StagedBytes = %d, want both logical sizes, 1000", s.StagedBytes)
	}
	if s.RetainedBytes != 700 {
		t.Errorf("RetainedBytes = %d, want only the sole-owned capture, 700", s.RetainedBytes)
	}
}
