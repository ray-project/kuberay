package logcollector

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/storage/clusterlogs"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// These tests exercise the production wiring: the RayLogHandler builds, replaces and
// retires rotatedCollectors, and the legacy shutdown walk keeps behaving exactly as it
// did. Everything below runs against real directories, real files and real hard links;
// only the fsnotify watcher, the reconcile ticker, the supervisor's clock and the
// object store are substituted.

// disabledReason and durablyDisabled read the supervisor's failure record. Production
// never asks either question — it decides through startable — so they live here rather
// than adding permanently unused accessors to the supervisor. They take mu the same way
// the production readers do, so they are safe to call while a collector is running.

// disabledReason returns why rotated collection has no collector for an identity,
// whether that condition is durable or merely not due for a retry yet.
func (s *rotatedSupervisor) disabledReason(key rotatedKey) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.failures[key]; ok {
		return f.err
	}
	return nil
}

// durablyDisabled reports whether an identity was switched off for a condition that
// will not be retried.
func (s *rotatedSupervisor) durablyDisabled(key rotatedKey) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.failures[key]
	return ok && f.durable
}

// runtimeWriter is the one storage writer both halves of the runtime share, exactly as
// production does: the rotated uploader and the legacy walk both write through it, so a
// single recording shows which objects production produced.
type runtimeWriter struct {
	written  map[string]string
	gate     chan struct{}
	entered  chan string
	dirs     []string
	attempts []string
	mu       sync.Mutex
	failAll  bool
}

func newRuntimeWriter() *runtimeWriter {
	return &runtimeWriter{written: make(map[string]string), entered: make(chan string, 64)}
}

func (w *runtimeWriter) CreateDirectory(p string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dirs = append(w.dirs, p)
	return nil
}

func (w *runtimeWriter) WriteFile(file string, r io.ReadSeeker) error {
	w.mu.Lock()
	w.attempts = append(w.attempts, file)
	gate, fail := w.gate, w.failAll
	w.mu.Unlock()

	select {
	case w.entered <- file:
	default:
	}
	if gate != nil {
		<-gate
	}
	if fail {
		return errors.New("object store is unavailable")
	}

	content, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.written[file] = string(content)
	return nil
}

// block makes every write wait until the returned function is called. It is how a test
// holds an upload inside the uncancelable storage call.
func (w *runtimeWriter) block(t *testing.T) func() {
	t.Helper()
	w.mu.Lock()
	w.gate = make(chan struct{})
	gate := w.gate
	w.mu.Unlock()

	var once sync.Once
	release := func() {
		once.Do(func() {
			w.mu.Lock()
			w.gate = nil
			w.mu.Unlock()
			close(gate)
		})
	}
	t.Cleanup(release)
	return release
}

func (w *runtimeWriter) setFailAll(v bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failAll = v
}

func (w *runtimeWriter) keys() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, 0, len(w.written))
	for k := range w.written {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (w *runtimeWriter) has(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.written[key]
	return ok
}

func (w *runtimeWriter) content(key string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.written[key]
}

func (w *runtimeWriter) attemptCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.attempts)
}

// watcherFactory hands out one fakeWatcher per collector the supervisor builds and
// remembers them in order, so a test can tell one session's watcher from the next.
type watcherFactory struct {
	mu   sync.Mutex
	made []*fakeWatcher
}

func (f *watcherFactory) next() (fsWatcher, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := newFakeWatcher()
	f.made = append(f.made, w)
	return w, nil
}

func (f *watcherFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.made)
}

func (f *watcherFactory) at(i int) *fakeWatcher {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.made) {
		return nil
	}
	return f.made[i]
}

// testCollector reaches the running collector. Production never needs this — the
// supervisor deliberately does not expose it — but a test has to be able to round-trip
// the owner goroutine and read the configuration production built.
func (s *rotatedSupervisor) testCollector() *rotatedCollector {
	run := s.activeRun()
	if run == nil {
		return nil
	}
	return run.rc
}

func (s *rotatedSupervisor) failureCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.failures)
}

func (s *rotatedSupervisor) failureRecord(key rotatedKey) (rotatedFailure, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.failures[key]
	if !ok {
		return rotatedFailure{}, false
	}
	return *f, true
}

const (
	testSessionA = "session_2026-07-31_10-00-00_000001"
	testSessionB = "session_2026-07-31_11-00-00_000002"
	// A session change is normally also a node change, though nothing in this
	// repository guarantees it (see currentNodeID). The two are deliberately different
	// values here because a test that reuses one node ID cannot see a collector built
	// under the wrong one.
	testNodeID  = "0a1b2c3d4e5f60718293a4b5c6d7e8f9"
	testNodeIDB = "fedcba98765432100123456789abcdef"
)

// runtimeHarness is one RayLogHandler wired the way NewCollector wires it, with its
// rotated subsystem pointed at real temp directories.
type runtimeHarness struct {
	t        *testing.T
	handler  *RayLogHandler
	writer   *runtimeWriter
	watchers *watcherFactory
	root     string
	ticks    chan time.Time

	mu sync.Mutex
	// clock is what the supervisor reads for retry scheduling, so a test can make a
	// backoff expire without sleeping through it.
	clock time.Time
	// nodeID is what the handler's node discovery returns, and nodeErr makes that
	// discovery fail. Production reaches the dashboard over HTTP; a test moves the
	// answer instead.
	nodeID    string
	nodeErr   bool
	nodeCalls int
	// livePredecessor records that a collector was constructed while the supervisor was
	// still attached to its predecessor — which would mean two owners of one staging
	// root. Ordering cannot be observed from outside ensure, so it is sampled from
	// inside, in the moment between the retirement and the construction.
	livePredecessor bool
	// builds counts collector constructions, sampled at the same point, and builtKeys
	// records the identity each one was constructed for. A collector built under the
	// wrong node is corrected by the next one, so only the full history shows it.
	builds    int
	builtKeys []rotatedKey
}

func (h *runtimeHarness) built() []rotatedKey {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]rotatedKey(nil), h.builtKeys...)
}

func (h *runtimeHarness) sawLivePredecessor() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.livePredecessor
}

func (h *runtimeHarness) buildCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.builds
}

func (h *runtimeHarness) advanceClock(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clock = h.clock.Add(d)
}

func (h *runtimeHarness) now() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.clock
}

// discoverNodeB makes the handler's node discovery answer with the node ID a restarted
// raylet would report, which is how a session change carries a node change.
func (h *runtimeHarness) discoverNodeB() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodeID, h.nodeErr = testNodeIDB, false
}

// failNodeDiscovery makes discovery fail, as it does while the dashboard is still
// coming up after a session restart.
func (h *runtimeHarness) failNodeDiscovery() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodeErr = true
}

func (h *runtimeHarness) discoverNode() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodeCalls++
	if h.nodeErr {
		return "", false
	}
	return h.nodeID, true
}

func newRuntimeHarness(t *testing.T) *runtimeHarness {
	t.Helper()
	// The temp root is resolved once, because production resolves the session symlink
	// and macOS puts /var behind a symlink to /private/var. Without this the harness
	// would be comparing two spellings of the same directory.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp root: %v", err)
	}
	t.Setenv("RAY_TMP_ROOT", root)

	h := &runtimeHarness{
		t:        t,
		root:     root,
		writer:   newRuntimeWriter(),
		watchers: &watcherFactory{},
		ticks:    make(chan time.Time),
		clock:    time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
		nodeID:   testNodeID,
	}
	h.makeSession(testSessionA)
	h.pointSessionLatest(testSessionA)

	h.handler = &RayLogHandler{
		Writer:              h.writer,
		RootDir:             "/history",
		RayClusterName:      "raycluster-sample",
		RayClusterNamespace: "ray-system",
		OwnerKind:           "RayJob",
		OwnerName:           "rayjob-sample",
		RayNodeName:         testNodeID,
		SessionDir:          h.sessionDir(testSessionA),
		LogDir:              h.logsDir(testSessionA),
		ShutdownChan:        make(chan struct{}),
		discoverNodeID:      h.discoverNode,
		// The production tick is five seconds; a test that waits for several polling
		// cycles would otherwise spend most of its time asleep.
		sessionPollInterval: 20 * time.Millisecond,
	}
	h.handler.rotated = h.newSupervisor()
	t.Cleanup(func() { h.handler.rotatedCollection().shutdown() })
	return h
}

// newSupervisor builds the supervisor with exactly the arguments startRotatedCollection
// uses, so the identity, writer and staging root under test are the production ones.
// tune only substitutes the watcher and the reconcile ticker.
func (h *runtimeHarness) newSupervisor() *rotatedSupervisor {
	sup := newRotatedSupervisor(h.handler.clusterIdentity(), h.handler.Writer, utils.GetRayRotatedStagingPath())
	sup.now = h.now
	sup.tune = func(cfg *rotatedCollectorConfig) {
		// tune runs inside ensure, in the moment between retiring the predecessor and
		// constructing the replacement. A predecessor still attached here would mean
		// the new collector is being built while the old one may still own the staging
		// tree.
		live := sup.activeRun() != nil
		h.mu.Lock()
		h.builds++
		h.builtKeys = append(h.builtKeys, rotatedKey{session: cfg.SessionName, node: cfg.NodeName})
		if live {
			h.livePredecessor = true
		}
		h.mu.Unlock()
		cfg.NewWatcher = h.watchers.next
		cfg.NewTicker = func(time.Duration) (<-chan time.Time, func()) { return h.ticks, func() {} }
	}
	// Short enough that a test which deliberately leaves work pending does not pay the
	// production budget on every cleanup; tests that care about the budget set their own.
	sup.drainBudget = 200 * time.Millisecond
	sup.drainPoll = time.Millisecond
	return sup
}

func (h *runtimeHarness) sessionDir(name string) string { return filepath.Join(h.root, name) }

func (h *runtimeHarness) logsDir(name string) string {
	return filepath.Join(h.sessionDir(name), utils.RAY_SESSIONDIR_LOGDIR_NAME)
}

func (h *runtimeHarness) makeSession(name string) {
	h.t.Helper()
	if err := os.MkdirAll(h.logsDir(name), 0o750); err != nil {
		h.t.Fatalf("create session %s: %v", name, err)
	}
}

func (h *runtimeHarness) pointSessionLatest(name string) {
	h.t.Helper()
	link := utils.GetRaySessionLatestPath()
	_ = os.Remove(link)
	if err := os.Symlink(h.sessionDir(name), link); err != nil {
		h.t.Fatalf("point session_latest at %s: %v", name, err)
	}
}

// write puts a file in a session's logs directory and returns its path.
func (h *runtimeHarness) write(session, name, content string) string {
	h.t.Helper()
	p := filepath.Join(h.logsDir(session), filepath.FromSlash(name))
	writeFile(h.t, p, content)
	return p
}

// start brings the rotated subsystem up exactly as Run does, and waits until the
// collector's owner goroutine has finished startup.
func (h *runtimeHarness) start() *rotatedCollector {
	h.t.Helper()
	h.handler.startRotatedCollection()
	return h.awaitCollector()
}

func (h *runtimeHarness) awaitCollector() *rotatedCollector {
	h.t.Helper()
	rc := h.handler.rotatedCollection().testCollector()
	if rc == nil {
		h.t.Fatal("no rotated collector is active")
	}
	rc.snapshot() // round-trips the owner goroutine, so startup has completed
	return rc
}

// capture creates the active raylet.out log plus one rotation backup in the given
// session, and waits for the collector to have pinned it.
func (h *runtimeHarness) captureIn(session, backup, content string) stagedEntry {
	h.t.Helper()
	h.write(session, "raylet.out", "active")
	p := h.write(session, backup, content)

	rc := h.handler.rotatedCollection().testCollector()
	if rc == nil {
		h.t.Fatal("no rotated collector is active")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rc.reconcileNow()
		for _, e := range rc.snapshot() {
			if e.OriginalName == backup {
				return e
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	h.t.Fatalf("%s was never captured", p)
	return stagedEntry{}
}

func (h *runtimeHarness) capture(content string) stagedEntry {
	h.t.Helper()
	return h.captureIn(testSessionA, "raylet.out.1", content)
}

func (h *runtimeHarness) waitForOneUploaded(rc *rotatedCollector) {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range rc.snapshot() {
			if e.State == stateUploaded {
				return
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for an uploaded capture: %+v", rc.snapshot())
}

// legacyLogsPrefix is where the legacy walk puts a session's logs for this node —
// computed from clusterlogs, not from the collector. Nothing in this tranche may
// change it.
func (h *runtimeHarness) logsPrefixFor(session, node string) string {
	return clusterlogs.LogsDir(
		h.handler.RootDir,
		h.handler.OwnerKind,
		h.handler.OwnerName,
		h.handler.RayClusterNamespace,
		h.handler.RayClusterName,
		session,
		node,
	)
}

func (h *runtimeHarness) legacyLogsPrefixFor(session string) string {
	return h.logsPrefixFor(session, h.handler.GetRayNodeName())
}

func (h *runtimeHarness) legacyLogsPrefix() string { return h.legacyLogsPrefixFor(testSessionA) }

func (h *runtimeHarness) stagingFiles() []string {
	h.t.Helper()
	root := utils.GetRayRotatedStagingPath()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				return relErr
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		h.t.Fatalf("walk staging root: %v", err)
	}
	sort.Strings(out)
	return out
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	eventuallyWithin(t, 10*time.Second, what, cond)
}

// eventuallyWithin is for the rare wait that needs a budget other than the default.
func eventuallyWithin(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ---------------------------------------------------------------------------
// Startup and session identity
// ---------------------------------------------------------------------------

// 1. The handler starts a collector for the session it was configured with.
func TestRuntimeStartsRotatedCollectorForActiveSession(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()

	key, ok := h.handler.rotatedCollection().activeKey()
	if !ok {
		t.Fatal("no rotated collector is active after startRotatedCollection")
	}
	if key.session != testSessionA || key.node != testNodeID {
		t.Errorf("active collector is for %+v, want session %s on node %s", key, testSessionA, testNodeID)
	}
	if h.watchers.count() != 1 {
		t.Errorf("watchers created = %d, want exactly 1", h.watchers.count())
	}
}

// 2. It receives the real storage writer and the complete owner-aware identity.
func TestRuntimePassesRealWriterAndClusterIdentity(t *testing.T) {
	h := newRuntimeHarness(t)
	rc := h.start()

	if rc.cfg.Writer != objectWriter(h.writer) {
		t.Errorf("collector writer = %#v, want the handler's storage writer", rc.cfg.Writer)
	}
	want := clusterIdentity{
		RootDir:     "/history",
		OwnerKind:   "RayJob",
		OwnerName:   "rayjob-sample",
		Namespace:   "ray-system",
		ClusterName: "raycluster-sample",
	}
	if rc.cfg.Cluster != want {
		t.Errorf("collector cluster identity = %+v, want %+v", rc.cfg.Cluster, want)
	}
}

// 3. It receives the active session's logs directory, the shared staging root, and the
// session and node the handler is actually running for.
func TestRuntimePassesSessionNodeAndPaths(t *testing.T) {
	h := newRuntimeHarness(t)
	rc := h.start()

	if rc.cfg.LogsDir != h.logsDir(testSessionA) {
		t.Errorf("LogsDir = %s, want %s", rc.cfg.LogsDir, h.logsDir(testSessionA))
	}
	if rc.cfg.StagingRoot != utils.GetRayRotatedStagingPath() {
		t.Errorf("StagingRoot = %s, want %s", rc.cfg.StagingRoot, utils.GetRayRotatedStagingPath())
	}
	if rc.cfg.SessionName != testSessionA {
		t.Errorf("SessionName = %s, want %s", rc.cfg.SessionName, testSessionA)
	}
	if rc.cfg.NodeName != testNodeID {
		t.Errorf("NodeName = %s, want %s", rc.cfg.NodeName, testNodeID)
	}
}

// TestRuntimeConfiguresBoundedIntake is the wiring regression test for the watermarks.
//
// The collector-level tests already prove what the intake gate does once it has a
// limit; what they cannot show is whether production ever gives it one. A collector
// built with zero watermarks captures without any staging bound, so an object store
// that stops accepting writes would let capture pin every byte Ray logs from then on —
// a hard link keeps the blocks alive after Ray unlinks its own name.
func TestRuntimeConfiguresBoundedIntake(t *testing.T) {
	h := newRuntimeHarness(t)
	rc := h.start()

	if rc.cfg.HighWaterBytes <= 0 {
		t.Errorf("HighWaterBytes = %d, want a positive bound: capture is otherwise unbounded while uploads fail",
			rc.cfg.HighWaterBytes)
	}
	if rc.cfg.LowWaterBytes <= 0 {
		t.Errorf("LowWaterBytes = %d, want a positive resume threshold", rc.cfg.LowWaterBytes)
	}
	if rc.cfg.LowWaterBytes >= rc.cfg.HighWaterBytes {
		t.Errorf("watermarks = %d/%d, want LowWaterBytes below HighWaterBytes so the gate has hysteresis",
			rc.cfg.HighWaterBytes, rc.cfg.LowWaterBytes)
	}
}

// 4. A handler configured with the session_latest symlink — not a resolved directory —
// still runs under the real session ID, in staging and in storage.
//
// This is the difference between writing beside the legacy objects and writing under a
// "session_latest" prefix nothing reads.
func TestRuntimeResolvesSessionLatestToTheRealSessionID(t *testing.T) {
	h := newRuntimeHarness(t)
	h.handler.SessionDir = utils.GetRaySessionLatestPath()

	rc := h.start()
	if rc.cfg.SessionName != testSessionA {
		t.Errorf("SessionName = %s, want the resolved session %s", rc.cfg.SessionName, testSessionA)
	}
	if rc.cfg.LogsDir != h.logsDir(testSessionA) {
		t.Errorf("LogsDir = %s, want the resolved %s", rc.cfg.LogsDir, h.logsDir(testSessionA))
	}
	key, _ := h.handler.rotatedCollection().activeKey()
	if key.session != testSessionA {
		t.Errorf("collector identity = %+v, want session %s", key, testSessionA)
	}

	// The object key it would write has to sit under the same node prefix the legacy
	// walk uses, which is only true when the session name is the real one.
	entry := h.capture("rotated segment")
	if got := entry.SessionName; got != testSessionA {
		t.Errorf("staged entry session = %s, want %s", got, testSessionA)
	}
	if k := entry.objectKey(rc.cfg.Cluster); !strings.HasPrefix(k, h.legacyLogsPrefix()+"/") {
		t.Errorf("object key %s is not under the legacy node prefix %s", k, h.legacyLogsPrefix())
	}
}

// 5. A session directory that cannot be resolved at all is skipped, not started under a
// made-up name, and the handler survives it.
func TestRuntimeUnresolvableSessionDirStartsNothing(t *testing.T) {
	h := newRuntimeHarness(t)
	h.handler.SessionDir = filepath.Join(h.root, "session_that_does_not_exist")

	h.handler.startRotatedCollection()

	if _, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Error("a collector was started for a session directory that does not exist")
	}
	if h.watchers.count() != 0 {
		t.Errorf("watchers created = %d, want none", h.watchers.count())
	}
	if got := h.handler.rotatedCollection().failureCount(); got != 0 {
		t.Errorf("failures recorded = %d, want none: an unresolved path is not a failure of any identity", got)
	}
}

// 6. An empty node ID starts nothing: objects would land under a key no reader looks at.
func TestRuntimeUnknownNodeStartsNothing(t *testing.T) {
	h := newRuntimeHarness(t)
	h.handler.SetRayNodeName("")

	h.handler.startRotatedCollection()
	if _, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Error("a collector was started before the node ID was known")
	}

	// And it starts normally once the node ID is discovered.
	h.handler.SetRayNodeName(testNodeID)
	h.handler.ensureRotatedCollection(h.handler.SessionDir)
	if key, ok := h.handler.rotatedCollection().activeKey(); !ok || key.node != testNodeID {
		t.Errorf("collector after node discovery = %+v (active=%v), want %s", key, ok, testNodeID)
	}
}

// ---------------------------------------------------------------------------
// Error policy: transient conditions recover, durable ones do not loop
// ---------------------------------------------------------------------------

// 7. A logs directory that does not exist yet is a transient condition. The session
// poller can see a new session directory before Ray has created logs/ inside it, and
// that must not cost the whole session its rotated protection.
func TestRuntimeMissingLogsDirRecoversWithoutPermanentDisable(t *testing.T) {
	h := newRuntimeHarness(t)
	sessionDir := h.sessionDir(testSessionB)
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		t.Fatalf("create session dir: %v", err)
	}
	key := rotatedKey{session: testSessionB, node: testNodeID}

	h.handler.ensureRotatedCollection(sessionDir)
	if _, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Fatal("a collector was started for a session with no logs directory")
	}
	reason := h.handler.rotatedCollection().disabledReason(key)
	if reason == nil {
		t.Fatal("the missing logs directory was not recorded at all")
	}
	if h.handler.rotatedCollection().durablyDisabled(key) {
		t.Fatalf("a missing logs directory was classified as permanent: %v", reason)
	}

	// Ray creates it a moment later. The retry is scheduled, so nothing happens until
	// it comes due — and then it starts normally.
	h.makeSession(testSessionB)
	h.handler.ensureRotatedCollection(sessionDir)
	if _, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Error("the retry ran before its backoff expired")
	}

	h.advanceClock(rotatedRetryBase + time.Second)
	h.handler.ensureRotatedCollection(sessionDir)
	got, ok := h.handler.rotatedCollection().activeKey()
	if !ok || got != key {
		t.Fatalf("collector after the logs directory appeared = %+v (active=%v), want %+v", got, ok, key)
	}
	h.awaitCollector()
}

// 7b. A recovered identity keeps no trace of the condition that stopped it: the record
// is gone, diagnostics agree with the running collector, and a later, unrelated
// failure starts its backoff from the beginning.
func TestRuntimeSuccessfulStartClearsTheFailureRecord(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()
	sessionDir := h.sessionDir(testSessionB)
	if err := os.MkdirAll(sessionDir, 0o750); err != nil {
		t.Fatalf("create session dir: %v", err)
	}
	key := rotatedKey{session: testSessionB, node: testNodeID}

	h.handler.ensureRotatedCollection(sessionDir) // fails: no logs/ yet
	first, ok := sup.failureRecord(key)
	if !ok || first.attempts != 1 {
		t.Fatalf("first failure record = %+v (present=%v), want attempt 1", first, ok)
	}

	h.makeSession(testSessionB)
	h.advanceClock(rotatedRetryBase + time.Second)
	h.handler.ensureRotatedCollection(sessionDir)
	h.awaitCollector()

	if reason := sup.disabledReason(key); reason != nil {
		t.Errorf("disabledReason after recovery = %v, want nil while the collector runs", reason)
	}
	if _, still := sup.failureRecord(key); still {
		t.Error("the recovered identity still has a failure record")
	}
	if got := sup.failureCount(); got != 0 {
		t.Errorf("failure records after recovery = %d, want 0", got)
	}

	// A later transient failure for the same identity is charged from attempt 1, not
	// from the history of the one that recovered.
	sup.shutdown()
	h.handler.rotated = h.newSupervisor()
	sup = h.handler.rotatedCollection()
	if err := os.RemoveAll(h.logsDir(testSessionB)); err != nil {
		t.Fatalf("remove logs dir: %v", err)
	}
	h.handler.ensureRotatedCollection(sessionDir)
	again, ok := sup.failureRecord(key)
	if !ok {
		t.Fatal("the later failure was not recorded")
	}
	if again.attempts != 1 {
		t.Errorf("later failure attempts = %d, want 1", again.attempts)
	}
	if want := h.now().Add(rotatedRetryBase); !again.notBefore.Equal(want) {
		t.Errorf("later failure retries at %s, want the base backoff %s", again.notBefore, want)
	}
}

// 7c. Retryability is carried by the error, not guessed from its errno. A durable
// staging inconsistency reported by the uploader wraps fs.ErrNotExist exactly as a
// missing logs directory does, and it must never be retried.
func TestRuntimeMissingStagedUploadPathIsDurable(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()
	base := sup.tune
	sup.tune = func(cfg *rotatedCollectorConfig) {
		base(cfg)
		// A short retry so the collector reaches its second upload attempt — the one
		// that has to open a staged file that is no longer there — without waiting.
		cfg.UploadBackoff = []time.Duration{time.Millisecond}
	}
	h.writer.setFailAll(true) // the first attempt fails remotely, which is retryable

	h.start()
	entry := h.capture("segment")
	run := sup.activeRun()
	eventually(t, "the first upload attempt", func() bool { return h.writer.attemptCount() > 0 })

	// The staging volume now contradicts the index: the capture is still pending and
	// indexed, but its durable link is gone.
	staged := entry.path(utils.GetRayRotatedStagingPath())
	if err := os.Remove(staged); err != nil {
		t.Fatalf("remove staged capture: %v", err)
	}

	eventually(t, "the collector to stop on the staging inconsistency", func() bool {
		return run.finished()
	})
	if run.err == nil || !strings.Contains(run.err.Error(), "staging volume contradicts") {
		t.Fatalf("collector exited with %v, want the staging inconsistency", run.err)
	}

	key := rotatedKey{session: testSessionA, node: testNodeID}
	eventually(t, "the failure to be published", func() bool { return sup.disabledReason(key) != nil })
	if !sup.durablyDisabled(key) {
		t.Fatalf("a missing staged upload path was classified as retryable: %v", sup.disabledReason(key))
	}

	// No amount of waiting brings it back.
	builds := h.buildCount()
	for range 5 {
		h.advanceClock(time.Hour)
		h.handler.ensureRotatedCollection(h.handler.SessionDir)
	}
	if got := h.buildCount(); got != builds {
		t.Errorf("collectors built after a durable staging failure = %d, want none", got-builds)
	}
}

// 7d. A logs directory the collector may not read is a property of the deployment, not
// of the moment, so it is durable however many times it is looked at.
func TestRuntimeUnreadableLogsDirIsDurable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode bits do not deny access")
	}
	h := newRuntimeHarness(t)
	// A session directory that cannot be traversed makes the stat of logs/ fail with
	// EACCES rather than ENOENT.
	sessionDir := h.sessionDir(testSessionB)
	if err := os.MkdirAll(filepath.Join(sessionDir, utils.RAY_SESSIONDIR_LOGDIR_NAME), 0o750); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := os.Chmod(sessionDir, 0o000); err != nil {
		t.Fatalf("chmod session dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sessionDir, 0o750) })

	// resolveSessionIdentity resolves the session directory itself, which still works;
	// the stat of logs/ inside it is what is denied.
	h.handler.ensureRotatedCollectionForNode(sessionDir, testNodeID)

	key := rotatedKey{session: testSessionB, node: testNodeID}
	reason := h.handler.rotatedCollection().disabledReason(key)
	if reason == nil {
		t.Fatal("a permission-denied logs directory was not recorded")
	}
	if !h.handler.rotatedCollection().durablyDisabled(key) {
		t.Fatalf("a permission-denied logs directory was classified as retryable: %v", reason)
	}
	builds := h.buildCount()
	for range 5 {
		h.advanceClock(time.Hour)
		h.handler.ensureRotatedCollectionForNode(sessionDir, testNodeID)
	}
	if got := h.buildCount(); got != builds {
		t.Errorf("collectors built for a permission-denied logs directory = %d, want none", got-builds)
	}
}

// 7e. Watcher construction that fails because the kernel is out of watch resources is
// retryable at that boundary; any other construction failure is durable.
func TestRuntimeWatcherConstructionResourceFailureIsRetryable(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()
	key := rotatedKey{session: testSessionA, node: testNodeID}

	var fail atomic.Bool
	fail.Store(true)
	base := sup.tune
	sup.tune = func(cfg *rotatedCollectorConfig) {
		base(cfg)
		inner := cfg.NewWatcher
		cfg.NewWatcher = func() (fsWatcher, error) {
			if fail.Load() {
				return nil, fmt.Errorf("inotify_init: %w", syscall.EMFILE)
			}
			return inner()
		}
	}

	h.handler.startRotatedCollection()
	eventually(t, "the exhausted watch limit to be recorded", func() bool {
		return sup.disabledReason(key) != nil
	})
	if sup.durablyDisabled(key) {
		t.Fatalf("EMFILE from watcher construction was classified as durable: %v", sup.disabledReason(key))
	}

	// The limit clears and the collector starts.
	fail.Store(false)
	h.advanceClock(rotatedRetryBase + time.Second)
	h.handler.ensureRotatedCollection(h.handler.SessionDir)
	h.awaitCollector()
	if key2, ok := sup.activeKey(); !ok || key2 != key {
		t.Errorf("collector after the limit cleared = %+v (active=%v), want %+v", key2, ok, key)
	}
}

func TestRuntimeWatcherConstructionPermissionFailureIsDurable(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()
	base := sup.tune
	sup.tune = func(cfg *rotatedCollectorConfig) {
		base(cfg)
		cfg.NewWatcher = func() (fsWatcher, error) {
			return nil, fmt.Errorf("create fsnotify watcher: %w", os.ErrPermission)
		}
	}

	h.handler.startRotatedCollection()
	key := rotatedKey{session: testSessionA, node: testNodeID}
	eventually(t, "the watcher failure to be recorded", func() bool {
		return sup.disabledReason(key) != nil
	})
	if !sup.durablyDisabled(key) {
		t.Errorf("a permission failure from watcher construction was classified as retryable: %v", sup.disabledReason(key))
	}
}

// 7f. Retry history grows across repeated startup failures, because being constructed is
// not the same as having started. Only a collector that completed startup clears it.
func TestRuntimeBackoffGrowsUntilStartupActuallySucceeds(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()
	key := rotatedKey{session: testSessionA, node: testNodeID}

	var fail atomic.Bool
	fail.Store(true)
	base := sup.tune
	sup.tune = func(cfg *rotatedCollectorConfig) {
		base(cfg)
		if fail.Load() {
			// Fatal to the collector, and reached only after attach: the failure that
			// a record cleared at attach time would keep resetting to attempt 1.
			cfg.NewWatcher = func() (fsWatcher, error) {
				return nil, fmt.Errorf("inotify_init: %w", syscall.ENFILE)
			}
		}
	}

	var delays []time.Duration
	for i := range 3 {
		if i > 0 {
			h.advanceClock(rotatedRetryMax) // whatever the backoff is, it is due
		}
		h.handler.ensureRotatedCollection(h.handler.SessionDir)
		eventually(t, "the failure to be recorded", func() bool {
			f, ok := sup.failureRecord(key)
			return ok && f.attempts == i+1
		})
		f, _ := sup.failureRecord(key)
		delays = append(delays, f.notBefore.Sub(h.now()))
	}

	if delays[0] != rotatedRetryBase {
		t.Errorf("first retry delay = %s, want the base %s", delays[0], rotatedRetryBase)
	}
	for i := 1; i < len(delays); i++ {
		if delays[i] <= delays[i-1] {
			t.Errorf("retry delay %d = %s, want longer than the previous %s (backoff reset to attempt 1)",
				i, delays[i], delays[i-1])
		}
	}

	// A startup that actually completes clears the history.
	fail.Store(false)
	h.advanceClock(rotatedRetryMax)
	h.handler.ensureRotatedCollection(h.handler.SessionDir)
	h.awaitCollector()
	eventually(t, "the recovered identity's history to be cleared", func() bool {
		return sup.disabledReason(key) == nil
	})
	if _, still := sup.failureRecord(key); still {
		t.Error("a healthy startup left the failure record in place")
	}
}

// 7g. A ready signal from a run that is no longer current clears nothing — including,
// and especially, the failure record of its own identity, which by then describes a
// later attempt rather than the one that is signaling.
func TestRuntimeStaleReadySignalClearsNothing(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()
	h.start()
	stale := sup.activeRun()
	key := stale.key

	// The run is retired, and a later attempt at the same identity fails.
	sup.retireUnless("")
	sup.noteFailure(key, errors.New("a later attempt failed durably"))

	stale.rc.cfg.OnReady() // the retired run's startup callback, arriving late

	if sup.disabledReason(key) == nil {
		t.Error("a retired run's ready signal cleared the failure of a later attempt")
	}
	if !sup.durablyDisabled(key) {
		t.Error("a retired run's ready signal downgraded a durable failure")
	}
	if _, ok := sup.activeKey(); ok {
		t.Error("a retired run's ready signal re-published it as active")
	}
}

// 7h. The handover uses the node verified for the session being handed over, not
// whatever the handler's mutable node field happens to hold. Those two agree today only
// because step 1 writes the field just before step 2 reads it, and a handover that
// depends on that ordering is one edit away from addressing a session to the wrong node.
func TestAdvanceSessionHandsOffUnderTheVerifiedNode(t *testing.T) {
	h := newRuntimeHarness(t)
	h.makeSession(testSessionB)

	// The verified identity for session B, with the handler's field deliberately
	// holding something else and discovery unable to correct it.
	h.handler.SetRayNodeName("stale-node-from-a-previous-session")
	h.failNodeDiscovery()

	st := sessionTransition{dir: h.sessionDir(testSessionB), node: testNodeIDB}
	h.handler.advanceSession(&st, h.sessionDir(testSessionB))

	key, ok := h.handler.rotatedCollection().activeKey()
	if !ok {
		t.Fatal("no collector was started for the verified identity")
	}
	if key.node != testNodeIDB {
		t.Errorf("collector was built on node %s, want the verified %s", key.node, testNodeIDB)
	}
	if !st.handedOff {
		t.Error("the handover was not recorded")
	}
}

// 8. A durable failure is reported once and never restarted, and it does not take the
// legacy collector down with it.
func TestRuntimeRotatedStartupFailureLeavesLegacyCollectionWorking(t *testing.T) {
	h := newRuntimeHarness(t)
	// Incomplete watch coverage is fatal to the collector: a segment created and
	// deleted in the unwatched gap would vanish unseen. A permission the collector does
	// not have will not appear later, so this is durable.
	h.handler.rotated.tune = func(cfg *rotatedCollectorConfig) {
		cfg.NewWatcher = func() (fsWatcher, error) {
			w := newFakeWatcher()
			w.failAdd = map[string]error{cfg.LogsDir: os.ErrPermission}
			return w, nil
		}
		cfg.NewTicker = func(time.Duration) (<-chan time.Time, func()) { return h.ticks, func() {} }
	}

	h.write(testSessionA, "raylet.out", "active")
	h.handler.startRotatedCollection()

	key := rotatedKey{session: testSessionA, node: testNodeID}
	eventually(t, "the failed collector to be reported", func() bool {
		return h.handler.rotatedCollection().disabledReason(key) != nil
	})
	if _, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Error("a failed collector is still being treated as active")
	}
	if !h.handler.rotatedCollection().durablyDisabled(key) {
		t.Error("a permission failure was classified as retryable")
	}
	if reason := h.handler.rotatedCollection().disabledReason(key); !strings.Contains(reason.Error(), "watch") {
		t.Errorf("disabled reason = %v, want it to name the watch failure", reason)
	}

	// The failure must not be restarted in a loop for the same session, however long
	// the process runs.
	before := h.buildCount()
	for range 5 {
		h.advanceClock(time.Hour)
		h.handler.ensureRotatedCollection(h.sessionDir(testSessionA))
	}
	if got := h.buildCount(); got != before {
		t.Errorf("collectors built after a durable failure = %d, want none", got-before)
	}

	// A different session is a different collector over a different tree, so it starts.
	h.makeSession(testSessionB)
	h.handler.rotated.tune = h.newSupervisor().tune
	h.handler.ensureRotatedCollection(h.sessionDir(testSessionB))
	if k, ok := h.handler.rotatedCollection().activeKey(); !ok || k.session != testSessionB {
		t.Errorf("a new session did not start after an earlier session's failure: %+v (active=%v)", k, ok)
	}

	// Legacy collection keeps working throughout, and nothing is suppressed.
	h.handler.rotatedCollection().shutdown()
	h.handler.processSessionLatestLogs()
	if !h.writer.has(path.Join(h.legacyLogsPrefix(), "raylet.out")) {
		t.Errorf("legacy shutdown upload did not run; wrote %v", h.writer.keys())
	}
}

// 9. Failure records cannot grow without bound across many sessions.
func TestRuntimeFailureRecordsStayBounded(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()

	for i := range 200 {
		session := fmt.Sprintf("session_2026-07-31_%02d-%02d-%02d_000000", i/3600, (i/60)%60, i%60)
		dir := h.sessionDir(session)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("create session dir: %v", err)
		}
		// No logs/ directory, so every one of them fails.
		h.handler.ensureRotatedCollection(dir)
	}
	if got := sup.failureCount(); got > maxRotatedFailureRecords {
		t.Errorf("failure records = %d, want at most %d", got, maxRotatedFailureRecords)
	}
	// Records for sessions the runtime has moved past are pruned, not merely capped.
	if got := sup.failureCount(); got != 1 {
		t.Errorf("failure records = %d, want only the session last ensured", got)
	}
}

// 10. A collector that dies leaves no stale active pointer behind, and shutdown does not
// wait on it.
func TestRuntimeFailedCollectorLeavesNoStaleActivePointer(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()

	// The watcher's event channel closing is fatal to the collector: discovery would
	// silently stop.
	close(h.watchers.at(0).events)

	key := rotatedKey{session: testSessionA, node: testNodeID}
	eventually(t, "the collector to be recorded as failed", func() bool {
		return h.handler.rotatedCollection().disabledReason(key) != nil
	})
	if _, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Error("the supervisor still points at a collector whose goroutine has exited")
	}
	if h.handler.rotatedCollection().activeRun() != nil {
		t.Error("the failed run was not detached")
	}

	done := make(chan struct{})
	go func() { h.handler.rotatedCollection().shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown blocked on a collector that had already failed")
	}
}

// 10b. A failed collector is never restarted by an ensure that lands between the
// failure and its publication.
//
// The seam holds the collector's failure at the top of runFailed, before either the
// record or the active pointer moves. An ensure arriving here must not be able to
// observe "no collector, and no reason not to build one".
func TestRuntimeFailedRunIsNotRestartedDuringPublication(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	sup.beforeFailurePublish = func() {
		once.Do(func() {
			close(entered)
			<-release
		})
	}

	h.start()
	watchers := h.watchers.count()
	close(h.watchers.at(0).events) // fatal to the collector

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the collector never reached failure publication")
	}

	// Exactly the window the old ordering left open.
	ensured := make(chan struct{})
	go func() {
		defer close(ensured)
		h.handler.ensureRotatedCollection(h.handler.SessionDir)
	}()

	select {
	case <-ensured:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("ensure blocked indefinitely during failure publication")
	}
	if got := h.watchers.count(); got != watchers {
		t.Errorf("collectors started while the failure was being published = %d, want none", got-watchers)
	}

	close(release)

	key := rotatedKey{session: testSessionA, node: testNodeID}
	eventually(t, "the identity to be durably disabled", func() bool { return sup.durablyDisabled(key) })
	if _, ok := sup.activeKey(); ok {
		t.Error("a collector is active after the failure was published")
	}
	if got := h.watchers.count(); got != watchers {
		t.Errorf("collectors started after the failure = %d, want none", got-watchers)
	}
}

// 11. A Run failure that happens exactly while shutdown is retiring the collector is
// recorded once and deadlocks nothing.
func TestRuntimeRunFailureConcurrentWithShutdownDoesNotDeadlock(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		close(h.watchers.at(0).events) // fatal to the collector
	}()
	go func() {
		defer wg.Done()
		h.handler.rotatedCollection().shutdown()
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("a Run failure concurrent with shutdown deadlocked")
	}

	if _, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Error("a collector is still active after shutdown")
	}
	// However the race went, the failure is recorded at most once per run.
	if got := h.handler.rotatedCollection().failureCount(); got > 1 {
		t.Errorf("failure records = %d, want at most one", got)
	}
}

// ---------------------------------------------------------------------------
// Session transition
// ---------------------------------------------------------------------------

// 12. A new session retires the old collector and builds a genuinely separate one.
//
// "Separate" is about in-memory state and live intake, not about the staging volume:
// the new collector still adopts the previous session's durable records at startup,
// because the staging root is per-node and nothing else would ever drain them. What it
// must not do is share an index, a generator, a watcher or an identity.
func TestRuntimeSessionChangeReplacesTheCollector(t *testing.T) {
	h := newRuntimeHarness(t)
	first := h.start()
	oldEntry := h.capture("old segment")
	firstRun := h.handler.rotatedCollection().activeRun()

	h.makeSession(testSessionB)
	h.pointSessionLatest(testSessionB)
	h.handler.ensureRotatedCollection(h.sessionDir(testSessionB))

	second := h.awaitCollector()
	if first == second {
		t.Fatal("the old collector was reused for the new session")
	}
	if first.ix == second.ix {
		t.Error("the new session reuses the old captureIndex")
	}
	if first.cfg.CaptureIDs == second.cfg.CaptureIDs {
		t.Error("the new session reuses the old capture ID generator")
	}
	if second.cfg.SessionName != testSessionB || second.cfg.LogsDir != h.logsDir(testSessionB) {
		t.Errorf("new collector is for %s at %s, want %s at %s",
			second.cfg.SessionName, second.cfg.LogsDir, testSessionB, h.logsDir(testSessionB))
	}

	// The old collector is fully stopped before the new one exists.
	if !firstRun.finished() {
		t.Error("the old collector's goroutine was still running when the new one was built")
	}
	if h.sawLivePredecessor() {
		t.Error("the new collector was constructed while the supervisor was still attached to the old one")
	}
	if !h.watchers.at(0).isClosed() {
		t.Error("the old session's watcher was not closed")
	}
	// Its capture stays on the staging volume, under the old session's own subtree.
	staged := h.stagingFiles()
	if len(staged) != 1 || !strings.HasPrefix(staged[0], testSessionA+"/") {
		t.Fatalf("staged files = %v, want the old session's capture preserved under its own subtree", staged)
	}
	// Whatever the new collector adopted is the identical durable record: same session,
	// same capture ID. Nothing was re-minted and nothing was rewritten into session B.
	adopted := second.snapshot()
	if len(adopted) != 1 {
		t.Fatalf("new collector holds %+v, want the one adopted record", adopted)
	}
	// State is deliberately excluded: the new collector may already have uploaded and
	// promoted the adopted capture, which is the point of adopting it.
	if adopted[0].withState(statePending) != oldEntry.withState(statePending) {
		t.Errorf("adopted record = %+v, want the old session's record with only its state advanced (%+v)",
			adopted[0], oldEntry)
	}
}

// 13. Repeating the same identity builds nothing new. The session poller calls ensure on
// every tick, and a relocation that keeps failing makes it repeat the identity of a
// session change on every tick, so idempotence is what keeps a failing relocation from
// churning collectors.
func TestRuntimeEnsureIsIdempotentForTheSameIdentity(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()
	h.makeSession(testSessionB)
	h.handler.ensureRotatedCollection(h.sessionDir(testSessionB))
	h.awaitCollector()

	builds := h.buildCount()
	run := h.handler.rotatedCollection().activeRun()
	for range 10 {
		h.handler.ensureRotatedCollection(h.sessionDir(testSessionB))
	}
	if got := h.buildCount(); got != builds {
		t.Errorf("collectors built by repeated ensure = %d, want none", got-builds)
	}
	if h.handler.rotatedCollection().activeRun() != run {
		t.Error("repeated ensure replaced the running collector")
	}
	if run.finished() {
		t.Error("repeated ensure retired the running collector")
	}
}

// 14. Nothing from the old session's live tree, and no event from its watcher, can reach
// the new collector.
func TestRuntimeOldSessionCannotMutateTheNewCollector(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()
	h.capture("old segment")
	oldRun := h.handler.rotatedCollection().activeRun()
	oldWatcher := h.watchers.at(0)

	h.makeSession(testSessionB)
	h.handler.ensureRotatedCollection(h.sessionDir(testSessionB))
	second := h.awaitCollector()
	before := len(second.snapshot())

	// The old collector was isolated before the new one was built, so a segment that
	// rotates in the old tree afterwards reaches neither of them.
	if !oldRun.finished() || !oldWatcher.isClosed() || h.sawLivePredecessor() {
		t.Fatal("the old collector was not isolated before the new one started")
	}
	h.write(testSessionA, "raylet.out.2", "after the changeover")
	second.reconcileNow()

	for _, e := range second.snapshot() {
		if e.OriginalName == "raylet.out.2" {
			t.Errorf("new collector captured a file from the old session's live tree: %+v", e)
		}
	}
	if got := len(second.snapshot()); got != before {
		t.Errorf("new collector's capture count moved from %d to %d because of the old session's tree", before, got)
	}
}

// 15. Repeated, concurrent session changes never let two collectors own the staging root,
// and leave exactly one running.
func TestRuntimeConcurrentSessionChangesKeepOneOwner(t *testing.T) {
	h := newRuntimeHarness(t)
	h.handler.rotatedCollection().drainBudget = 50 * time.Millisecond
	h.start()
	h.makeSession(testSessionB)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			session := testSessionA
			if i%2 == 1 {
				session = testSessionB
			}
			h.handler.ensureRotatedCollection(h.sessionDir(session))
		}(i)
	}
	wg.Wait()

	if h.sawLivePredecessor() {
		t.Error("a collector was constructed while another was still attached")
	}
	sup := h.handler.rotatedCollection()
	run := sup.activeRun()
	if run == nil {
		t.Fatal("no collector survived the concurrent session changes")
	}
	if run.finished() {
		t.Error("the surviving collector is not running")
	}
	// ensure returns as soon as the survivor's goroutine is spawned, so wait for that
	// goroutine to reach its loop before inspecting watchers — it installs its watcher
	// on the way there. This is the same round trip awaitCollector uses.
	run.rc.snapshot()
	// Every retired collector is stopped, so its watcher is closed. Exactly one — the
	// survivor's — is open.
	open := 0
	for i := range h.watchers.count() {
		if !h.watchers.at(i).isClosed() {
			open++
		}
	}
	if open != 1 {
		t.Errorf("open watchers = %d, want exactly 1", open)
	}
}

// 16. A session change while an upload is in flight preserves the old session's pending
// capture, under the old session's own subtree.
func TestRuntimeSessionChangeDuringUploadPreservesPendingState(t *testing.T) {
	h := newRuntimeHarness(t)
	h.handler.rotatedCollection().drainBudget = 50 * time.Millisecond
	release := h.writer.block(t)
	h.start()
	h.capture("old segment")
	select {
	case <-h.writer.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the upload never reached the storage writer")
	}

	h.makeSession(testSessionB)
	h.handler.ensureRotatedCollection(h.sessionDir(testSessionB))
	h.awaitCollector()
	release()

	staged := h.stagingFiles()
	if len(staged) != 1 {
		t.Fatalf("staged files = %v, want the old session's single capture preserved", staged)
	}
	if !strings.HasPrefix(staged[0], testSessionA+"/") || !strings.Contains(staged[0], "/"+string(statePending)+"/") {
		t.Errorf("staged file = %s, want a pending capture under %s", staged[0], testSessionA)
	}
}

// 17. A capture the previous session left pending is uploaded by the next session's
// collector to the key it was always destined for: the old session's prefix, the old
// capture ID.
func TestRuntimeCrossSessionAdoptionPreservesTheObjectKey(t *testing.T) {
	h := newRuntimeHarness(t)
	h.handler.rotatedCollection().drainBudget = 10 * time.Millisecond
	release := h.writer.block(t)
	h.start()
	old := h.capture("old segment")
	select {
	case <-h.writer.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the upload never reached the storage writer")
	}

	// The old session goes away with its capture still pending: the upload was in the
	// uncancelable storage call, so its result is discarded.
	h.makeSession(testSessionB)
	h.handler.ensureRotatedCollection(h.sessionDir(testSessionB))
	second := h.awaitCollector()
	release()

	want := old.objectKey(second.cfg.Cluster)
	eventually(t, "the adopted capture to be uploaded under its original key", func() bool {
		second.reconcileNow()
		return h.writer.has(want)
	})
	if !strings.HasPrefix(want, h.legacyLogsPrefixFor(testSessionA)+"/") {
		t.Errorf("adopted object key %s is not under the old session's prefix", want)
	}
	if h.writer.content(want) != "old segment" {
		t.Errorf("adopted object content = %q, want the old session's bytes", h.writer.content(want))
	}
	// Nothing was rewritten into the new session.
	for _, k := range h.writer.keys() {
		if strings.Contains(k, testSessionB) && strings.Contains(k, captureIDSeparator) {
			t.Errorf("adopted capture was re-keyed into the new session: %s", k)
		}
	}
	for _, e := range second.snapshot() {
		if e.SessionName != testSessionA {
			t.Errorf("adopted record was rewritten to session %s, want %s", e.SessionName, testSessionA)
		}
	}
}

// ---------------------------------------------------------------------------
// Shutdown
// ---------------------------------------------------------------------------

// 18. Shutdown reconciles the tree one last time before intake stops, so a segment that
// rotated during shutdown is still captured.
func TestRuntimeShutdownReconcilesBeforeStoppingIntake(t *testing.T) {
	h := newRuntimeHarness(t)
	h.write(testSessionA, "raylet.out", "active")
	h.start()

	// Created after startup and never announced through the watcher: only the final
	// reconciliation can find it.
	h.write(testSessionA, "raylet.out.1", "rotated during shutdown")
	h.handler.rotatedCollection().shutdown()

	staged := h.stagingFiles()
	found := false
	for _, p := range staged {
		if strings.Contains(p, "raylet.out.1"+captureIDSeparator) {
			found = true
		}
	}
	if !found {
		t.Errorf("staged files = %v, want the segment reconciled at shutdown to be captured", staged)
	}
}

// 19. When the drain budget expires, captured work stays pinned on disk as pending.
func TestRuntimeShutdownTimeoutPreservesPendingCaptures(t *testing.T) {
	h := newRuntimeHarness(t)
	h.handler.rotatedCollection().drainBudget = 50 * time.Millisecond
	release := h.writer.block(t)
	h.start()
	h.capture("segment")

	h.handler.rotatedCollection().shutdown()
	release()

	staged := h.stagingFiles()
	if len(staged) != 1 || !strings.Contains(staged[0], "/"+string(statePending)+"/") {
		t.Errorf("staged files = %v, want exactly one pending capture preserved", staged)
	}
}

// 20. An upload already inside the uncancelable storage call does not hold shutdown open.
func TestRuntimeShutdownIsBoundedByAnUncancelableUpload(t *testing.T) {
	h := newRuntimeHarness(t)
	h.handler.rotatedCollection().drainBudget = 100 * time.Millisecond
	release := h.writer.block(t)
	h.start()
	h.capture("segment")

	select {
	case <-h.writer.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the upload never reached the storage writer")
	}

	start := time.Now()
	h.handler.rotatedCollection().shutdown()
	elapsed := time.Since(start)
	release()

	// The budget plus the worker's own stop grace, with generous slack for a loaded
	// test machine. What matters is that it is bounded at all: WriteFile is still
	// running and cannot be canceled.
	if elapsed > 5*time.Second {
		t.Errorf("shutdown took %s while an uncancelable upload was in flight", elapsed)
	}
}

// 21. Shutting the subsystem down twice changes nothing and does not block, and nothing
// can be started afterwards.
func TestRuntimeShutdownIsIdempotentAndFreezes(t *testing.T) {
	h := newRuntimeHarness(t)
	rc := h.start()
	h.capture("segment")
	h.waitForOneUploaded(rc)

	h.handler.rotatedCollection().shutdown()
	builds := h.buildCount()
	h.handler.rotatedCollection().shutdown()
	h.handler.rotatedCollection().shutdown()

	h.makeSession(testSessionB)
	h.handler.ensureRotatedCollection(h.sessionDir(testSessionB))
	h.handler.ensureRotatedCollection(h.sessionDir(testSessionA))
	if _, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Error("a collector was started after shutdown")
	}
	if got := h.buildCount(); got != builds {
		t.Errorf("collectors built after shutdown = %d, want none", got-builds)
	}
}

// 22. A session change that arrives while shutdown is running never leaves a collector
// behind it.
func TestRuntimeSessionChangeConcurrentWithShutdownStartsNoReplacement(t *testing.T) {
	h := newRuntimeHarness(t)
	h.handler.rotatedCollection().drainBudget = 50 * time.Millisecond
	h.start()
	h.makeSession(testSessionB)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		h.handler.rotatedCollection().shutdown()
	}()
	go func() {
		defer wg.Done()
		h.handler.ensureRotatedCollection(h.sessionDir(testSessionB))
	}()
	wg.Wait()

	if _, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Error("a collector is active after shutdown returned")
	}
	for i := range h.watchers.count() {
		if !h.watchers.at(i).isClosed() {
			t.Errorf("watcher %d is still open after shutdown", i)
		}
	}
}

// 23. A replacement that is already in flight when shutdown freezes the subsystem is
// abandoned rather than started.
//
// This is the window the early frozen check cannot cover: ensure has already decided to
// build a replacement and is retiring the predecessor, which takes as long as the drain
// budget. Shutdown freezes during that gap, and the replacement must never run —
// constructing it and then having to retire it again would mean a collector taking
// ownership of the staging root after shutdown had declared the subsystem down.
//
// A constructed-but-never-started collector is inert: it holds no watcher, no goroutine
// and no staging link, so the observable requirement is that no watcher is ever created
// for it.
func TestRuntimeShutdownFreezesAReplacementAlreadyInFlight(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()
	sup.drainBudget = 2 * time.Second
	release := h.writer.block(t)
	h.start()
	h.capture("segment") // stays pending, so the retirement spends its whole budget
	select {
	case <-h.writer.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the upload never reached the storage writer")
	}
	h.makeSession(testSessionB)
	watchers := h.watchers.count()

	ensured := make(chan struct{})
	go func() {
		defer close(ensured)
		h.handler.ensureRotatedCollection(h.sessionDir(testSessionB))
	}()

	// Detaching is the first thing a retirement does, so a nil active run means ensure
	// is inside the drain: the exact window this test is about.
	eventually(t, "the retirement of the old collector to begin", func() bool {
		return sup.activeRun() == nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		sup.shutdown()
	}()

	<-ensured
	<-done
	release()

	if got := h.watchers.count(); got != watchers {
		t.Errorf("collectors started while shutdown was freezing = %d, want none", got-watchers)
	}
	if _, ok := sup.activeKey(); ok {
		t.Error("a collector is active after shutdown returned")
	}
}

// 24. Watcher, owner and worker goroutines all go away.
func TestRuntimeLeavesNoGoroutinesBehind(t *testing.T) {
	before := runtime.NumGoroutine()

	h := newRuntimeHarness(t)
	rc := h.start()
	h.capture("segment")
	h.waitForOneUploaded(rc)

	h.makeSession(testSessionB)
	h.handler.ensureRotatedCollection(h.sessionDir(testSessionB))
	h.awaitCollector()
	h.handler.rotatedCollection().shutdown()

	for range 60 {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("goroutines leaked: %d before, %d after", before, runtime.NumGoroutine())
}

// 23b. Once shutdown has closed the transition gate, a polling tick performs none of a
// transition's side effects: no node discovery, no node mutation, no handover, no
// retirement, no relocation.
func TestRuntimeShutdownGateStopsSessionTransitions(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()
	h.write(testSessionA, "raylet.out", "active")
	run := h.handler.rotatedCollection().activeRun()

	h.handler.transitions.close()

	h.makeSession(testSessionB)
	h.write(testSessionB, "raylet.out", "active")
	h.pointSessionLatest(testSessionB)
	h.discoverNodeB()

	st := sessionTransition{dir: h.sessionDir(testSessionA), node: testNodeID, handedOff: true}
	builds := h.buildCount()
	h.handler.advanceSession(&st, h.sessionDir(testSessionB))

	h.mu.Lock()
	calls := h.nodeCalls
	h.mu.Unlock()
	if calls != 0 {
		t.Errorf("node discovery ran %d time(s) after the gate closed, want none", calls)
	}
	if got := h.handler.GetRayNodeName(); got != testNodeID {
		t.Errorf("the handler's node ID changed to %s after the gate closed", got)
	}
	if got := h.buildCount(); got != builds {
		t.Errorf("collectors built after the gate closed = %d, want none", got-builds)
	}
	if h.handler.rotatedCollection().activeRun() != run || run.finished() {
		t.Error("the running collector was retired after the gate closed")
	}
	if _, err := os.Stat(h.logsDir(testSessionA)); err != nil {
		t.Errorf("the live tree was relocated after the gate closed: %v", err)
	}
	if st.dir != h.sessionDir(testSessionA) {
		t.Errorf("transition state advanced to %s after the gate closed", st.dir)
	}
}

// 23c. Shutdown does not begin — not the rotated retirement, not the legacy walk —
// while an admitted transition is still able to change the identity those steps depend
// on. Each of a transition's three steps is held in turn.
//
// Abandoning the wait would not stop any of them: there is nothing to cancel, so the
// only thing an early return buys is running the walk concurrently with a relocation of
// the tree it is walking.
func TestRuntimeShutdownWaitsForATransitionInFlight(t *testing.T) {
	for _, tc := range []struct {
		name string
		// hold blocks the transition at one of its steps and returns a channel that is
		// closed once it is inside, plus the release.
		hold func(h *runtimeHarness) (entered <-chan struct{}, release func())
	}{
		{
			name: "inside node discovery",
			hold: func(h *runtimeHarness) (<-chan struct{}, func()) {
				entered, release := make(chan struct{}), make(chan struct{})
				h.handler.discoverNodeID = func() (string, bool) {
					close(entered)
					<-release
					return testNodeIDB, true
				}
				return entered, func() { close(release) }
			},
		},
		{
			name: "inside the collector handoff",
			hold: func(h *runtimeHarness) (<-chan struct{}, func()) {
				entered, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				base := h.handler.rotatedCollection().tune
				// tune runs inside ensure with the supervisor's lifecycle lock held,
				// which is also the lock the rotated retirement needs.
				h.handler.rotatedCollection().tune = func(cfg *rotatedCollectorConfig) {
					base(cfg)
					once.Do(func() {
						close(entered)
						<-release
					})
				}
				h.discoverNodeB()
				return entered, func() { close(release) }
			},
		},
		{
			name: "immediately before relocation",
			hold: func(h *runtimeHarness) (<-chan struct{}, func()) {
				entered, release := make(chan struct{}), make(chan struct{})
				h.discoverNodeB()
				h.handler.beforeRelocation = func() {
					close(entered)
					<-release
				}
				return entered, func() { close(release) }
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRuntimeHarness(t)
			h.start()
			h.write(testSessionA, "raylet.out", "active")
			h.makeSession(testSessionB)
			h.write(testSessionB, "raylet.out", "active")
			h.pointSessionLatest(testSessionB)

			entered, release := tc.hold(h)

			st := sessionTransition{dir: h.sessionDir(testSessionA), node: testNodeID}
			go h.handler.advanceSession(&st, h.sessionDir(testSessionB))
			<-entered

			done := make(chan struct{})
			go func() { defer close(done); h.handler.shutdownLogCollection() }()

			select {
			case <-done:
				t.Fatal("shutdown completed while a transition was still running")
			case <-time.After(250 * time.Millisecond):
			}
			// Neither later step may have begun. Freezing is the first thing the
			// rotated retirement does, and it is what the handoff case needs: by then
			// the transition has already retired the outgoing collector itself, so an
			// absent collector proves nothing, but a frozen supervisor would.
			if h.handler.rotatedCollection().isFrozen() {
				t.Error("the rotated retirement began while a transition was still running")
			}
			for _, k := range h.writer.keys() {
				if strings.Contains(k, "/logs/") {
					t.Errorf("the legacy walk wrote %s while a transition was still running", k)
				}
			}

			release()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Fatal("shutdown never completed after the transition finished")
			}

			// Afterwards: nothing is admitted, and the walk has run exactly once.
			if h.handler.transitions.enter() {
				t.Error("a transition was admitted after shutdown")
			}
			if _, ok := h.handler.rotatedCollection().activeKey(); ok {
				t.Error("a collector is still active after shutdown")
			}
		})
	}
}

// 23d. Shutdown stays bounded when the object store never returns, which is the one
// thing that genuinely cannot be waited on. That bound is the rotated drain budget and
// has nothing to do with session transitions.
func TestRuntimeShutdownIsBoundedWhenWriteFileNeverReturns(t *testing.T) {
	h := newRuntimeHarness(t)
	h.handler.rotatedCollection().drainBudget = 100 * time.Millisecond
	release := h.writer.block(t)
	h.start()
	h.capture("segment")

	select {
	case <-h.writer.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the upload never reached the storage writer")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.handler.transitions.close()
		h.handler.rotatedCollection().shutdown()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown blocked on an uncancelable upload")
	}
	release()
}

// ---------------------------------------------------------------------------
// The legacy walk is untouched
// ---------------------------------------------------------------------------

// 24. The two halves address disjoint object keys. This is the invariant the whole
// "no suppression" decision rests on: a captured segment is written as
// "<name>.rotated.<capture ID>", the legacy walk writes "<name>", and no capture ID can
// ever be empty.
func TestRotatedAndLegacyObjectKeysAreDisjoint(t *testing.T) {
	h := newRuntimeHarness(t)
	rc := h.start()
	entry := h.capture("rotated segment")

	rotatedKey := entry.objectKey(rc.cfg.Cluster)
	legacyKey := path.Join(h.legacyLogsPrefix(), entry.OriginalName)
	if rotatedKey == legacyKey {
		t.Fatalf("rotated and legacy object keys collide at %s", rotatedKey)
	}
	if !strings.HasPrefix(rotatedKey, legacyKey+captureIDSeparator) {
		t.Errorf("rotated key %s is not the legacy key %s plus a capture ID", rotatedKey, legacyKey)
	}
}

// 25. Every regular file in the live tree is uploaded by the shutdown walk, including
// one the rotated subsystem has already put in storage under its own key.
//
// Suppressing that write would not avoid a duplicate — the keys differ — it would
// delete a key the legacy walk has always produced.
func TestLegacyShutdownUploadsEveryLiveFileEvenWhenAlreadyCaptured(t *testing.T) {
	h := newRuntimeHarness(t)
	rc := h.start()
	entry := h.capture("rotated segment")
	h.waitForOneUploaded(rc)
	rotatedKey := entry.objectKey(rc.cfg.Cluster)

	h.handler.rotatedCollection().shutdown()
	h.handler.processSessionLatestLogs()

	prefix := h.legacyLogsPrefix()
	for _, want := range []string{
		path.Join(prefix, "raylet.out"),
		path.Join(prefix, "raylet.out.1"),
		rotatedKey,
	} {
		if !h.writer.has(want) {
			t.Errorf("object %s was never written; wrote %v", want, h.writer.keys())
		}
	}
	if got := h.writer.content(path.Join(prefix, "raylet.out.1")); got != "rotated segment" {
		t.Errorf("legacy object content = %q, want the segment's bytes", got)
	}
}

// 26. Ray's rotation renames a segment through raylet.out.1, .2, .3 without changing its
// inode, so at shutdown the very same physical file the collector captured as ".1" is
// sitting at ".2" — a name whose object no other writer produces.
//
// This is why physical identity cannot stand in for a remote key: an inode-based skip
// would have dropped the ".2" object entirely.
func TestLegacyShutdownUploadsARenamedCapturedSegment(t *testing.T) {
	h := newRuntimeHarness(t)
	rc := h.start()
	h.capture("the segment")
	h.waitForOneUploaded(rc)

	logs := h.logsDir(testSessionA)
	first := filepath.Join(logs, "raylet.out.1")
	second := filepath.Join(logs, "raylet.out.2")
	before, _, err := statInode(first)
	if err != nil {
		t.Fatalf("statInode(%s): %v", first, err)
	}
	if err := os.Rename(first, second); err != nil {
		t.Fatalf("rotate %s to %s: %v", first, second, err)
	}
	after, _, err := statInode(second)
	if err != nil {
		t.Fatalf("statInode(%s): %v", second, err)
	}
	if before != after {
		t.Fatalf("the rename changed the inode (%s -> %s); the test cannot show what it means to", before, after)
	}

	h.handler.rotatedCollection().shutdown()
	h.handler.processSessionLatestLogs()

	key := path.Join(h.legacyLogsPrefix(), "raylet.out.2")
	if !h.writer.has(key) {
		t.Errorf("the renamed segment was not uploaded under its current name; wrote %v", h.writer.keys())
	}
	if got := h.writer.content(key); got != "the segment" {
		t.Errorf("uploaded content = %q, want the segment's bytes", got)
	}
}

// 27. A capture whose upload failed, and one that never got to storage at all, are
// uploaded by the legacy walk exactly as they would have been without this subsystem.
func TestLegacyShutdownUploadsPendingAndFailedCaptures(t *testing.T) {
	h := newRuntimeHarness(t)
	h.handler.rotatedCollection().drainBudget = 100 * time.Millisecond
	h.writer.setFailAll(true)
	rc := h.start()
	h.capture("rotated segment")

	eventually(t, "the upload to have been attempted and failed", func() bool {
		if h.writer.attemptCount() == 0 {
			return false
		}
		for _, e := range rc.snapshot() {
			if e.OriginalName == "raylet.out.1" && e.State == statePending {
				return true
			}
		}
		return false
	})

	h.handler.rotatedCollection().shutdown()
	h.writer.setFailAll(false)
	h.handler.processSessionLatestLogs()

	if !h.writer.has(path.Join(h.legacyLogsPrefix(), "raylet.out.1")) {
		t.Errorf("a capture whose upload failed was skipped by the legacy walk; wrote %v", h.writer.keys())
	}
}

// 27b. The shutdown walk works from an immutable snapshot: a session change during the
// walk cannot move it to another session's tree or split one shutdown across two
// identities.
func TestLegacyShutdownWalksTheSnapshottedSession(t *testing.T) {
	h := newRuntimeHarness(t)
	h.write(testSessionA, "raylet.out", "session A active")
	h.write(testSessionA, "events/event_GCS.log", "session A nested")

	snap, ok := h.handler.takeShutdownSnapshot()
	if !ok {
		t.Fatal("no shutdown snapshot was taken")
	}
	if snap.sessionID != testSessionA {
		t.Fatalf("snapshot session = %s, want %s", snap.sessionID, testSessionA)
	}
	if snap.logsDir != h.logsDir(testSessionA) {
		t.Errorf("snapshot logs directory = %s, want the real %s", snap.logsDir, h.logsDir(testSessionA))
	}

	// Ray restarts the session, and the node ID moves with it, after the snapshot.
	h.makeSession(testSessionB)
	h.write(testSessionB, "raylet.out", "session B active")
	h.pointSessionLatest(testSessionB)
	h.handler.SetRayNodeName(testNodeIDB)

	h.handler.processSessionLogs(snap)

	prefixA := h.logsPrefixFor(testSessionA, testNodeID)
	prefixB := h.logsPrefixFor(testSessionB, testNodeIDB)
	for _, want := range []string{
		path.Join(prefixA, "raylet.out"),
		path.Join(prefixA, "events/event_GCS.log"),
	} {
		if !h.writer.has(want) {
			t.Errorf("object %s was not written; wrote %v", want, h.writer.keys())
		}
	}
	if got := h.writer.content(path.Join(prefixA, "raylet.out")); got != "session A active" {
		t.Errorf("object content = %q, want session A's bytes", got)
	}
	for _, k := range h.writer.keys() {
		if strings.HasPrefix(k, prefixB) {
			t.Errorf("the walk wrote %s, which belongs to the session that replaced the snapshot", k)
		}
		if strings.HasPrefix(k, h.logsPrefixFor(testSessionA, testNodeIDB)) {
			t.Errorf("the walk wrote %s, mixing session A with the new node ID", k)
		}
		if strings.Contains(k, "session B active") {
			t.Errorf("the walk uploaded session B's content: %s", k)
		}
	}
	if h.writer.has(path.Join(prefixA, "raylet.out")) && h.writer.content(path.Join(prefixA, "raylet.out")) == "session B active" {
		t.Error("the walk followed session_latest to the new session's file")
	}
}

// 28. With no rotated subsystem at all, the legacy shutdown walk behaves exactly as
// before: same files, same owner-aware keys, nothing suppressed.
func TestLegacyShutdownUnchangedWithoutRotatedCollection(t *testing.T) {
	h := newRuntimeHarness(t)
	h.handler.rotated = nil

	h.write(testSessionA, "raylet.out", "active")
	h.write(testSessionA, "raylet.out.1", "rotated")
	h.write(testSessionA, "events/event_GCS.log", "nested")

	h.handler.processSessionLatestLogs()

	prefix := h.legacyLogsPrefix()
	want := []string{
		path.Join(prefix, "events/event_GCS.log"),
		path.Join(prefix, "raylet.out"),
		path.Join(prefix, "raylet.out.1"),
	}
	var logs []string
	for _, k := range h.writer.keys() {
		// The head node also writes the session metadata marker; drop it.
		if strings.HasPrefix(k, prefix) {
			logs = append(logs, k)
		}
	}
	if strings.Join(logs, ",") != strings.Join(want, ",") {
		t.Errorf("legacy shutdown wrote %v, want %v", logs, want)
	}
}

// 29. Owner-aware object keys are unchanged by the production wiring: a captured segment
// lands beside the node's other logs, under the RayJob-nested prefix.
func TestRuntimeObjectKeysAreOwnerAwareAndUnchanged(t *testing.T) {
	h := newRuntimeHarness(t)
	rc := h.start()
	h.capture("rotated segment")
	h.waitForOneUploaded(rc)

	var rotatedKeys []string
	for _, e := range rc.snapshot() {
		rotatedKeys = append(rotatedKeys, e.objectKey(rc.cfg.Cluster))
	}
	h.handler.rotatedCollection().shutdown()
	h.handler.processSessionLatestLogs()

	prefix := h.legacyLogsPrefix()
	if !strings.HasPrefix(prefix, "/history/cluster-history/rayjob/ray-system/rayjob-sample/raycluster-sample/") {
		t.Fatalf("legacy prefix %s is not the owner-aware RayJob prefix", prefix)
	}
	for _, k := range rotatedKeys {
		if !strings.HasPrefix(k, prefix+"/") {
			t.Errorf("rotated object key %s escapes the node's log prefix %s", k, prefix)
		}
		if !h.writer.has(k) {
			t.Errorf("rotated object key %s was never written; wrote %v", k, h.writer.keys())
		}
	}
	if !h.writer.has(path.Join(prefix, "raylet.out")) {
		t.Errorf("legacy key for the active log changed; wrote %v", h.writer.keys())
	}
}

// ---------------------------------------------------------------------------
// Partial initialization
// ---------------------------------------------------------------------------

// 30. A handler that was never fully built keeps its legacy behavior and panics at
// nothing.
func TestRuntimeNilAndPartialHandlersArePassive(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RAY_TMP_ROOT", root)

	var bare RayLogHandler
	bare.ensureRotatedCollection("")
	bare.ensureRotatedCollection(filepath.Join(root, "session_nowhere"))
	bare.rotatedCollection().shutdown()
	bare.rotatedCollection().shutdown()
	if _, ok := bare.rotatedCollection().activeKey(); ok {
		t.Error("a zero-value handler reports an active rotated collector")
	}
	if bare.rotatedCollection().disabledReason(rotatedKey{}) != nil {
		t.Error("a zero-value handler reports a disabled reason")
	}

	// A handler with no storage writer still captures; it simply has nowhere to send
	// what it captured, which is what keeps segments pinned until a writer exists.
	h := newRuntimeHarness(t)
	h.handler.Writer = nil
	h.handler.rotated = h.newSupervisor()
	h.handler.rotated.writer = nil

	rc := h.start()
	if rc.up.enabled() {
		t.Error("the upload pipeline is enabled without a storage writer")
	}
	h.capture("segment")
	staged := h.stagingFiles()
	if len(staged) != 1 || !strings.Contains(staged[0], "/"+string(statePending)+"/") {
		t.Errorf("staged files = %v, want the capture pinned and pending", staged)
	}
	h.handler.rotatedCollection().shutdown()
}

// 30b. A staging root that exists but cannot be written to is a durable
// misconfiguration. Creating the root succeeds when it is already there, whatever its
// mode, so only actually creating a file in it answers the question.
func TestRuntimePreflightRejectsAnUnwritableStagingRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode bits do not deny access")
	}
	h := newRuntimeHarness(t)
	staging := utils.GetRayRotatedStagingPath()
	if err := os.MkdirAll(staging, 0o500); err != nil {
		t.Fatalf("create staging root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(staging, 0o750) })

	h.handler.startRotatedCollection()

	key := rotatedKey{session: testSessionA, node: testNodeID}
	if !h.handler.rotatedCollection().durablyDisabled(key) {
		t.Errorf("an unwritable staging root was not durably disabled: %v",
			h.handler.rotatedCollection().disabledReason(key))
	}
	if _, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Error("a collector was started against an unwritable staging root")
	}
	// The probe leaves nothing behind.
	if entries, err := os.ReadDir(staging); err == nil && len(entries) != 0 {
		t.Errorf("staging root holds %d leftover entries after preflight", len(entries))
	}
}

// 30c. The preflight probe cleans up after a successful start too.
func TestRuntimePreflightProbeLeavesNothingBehind(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()

	entries, err := os.ReadDir(utils.GetRayRotatedStagingPath())
	if err != nil {
		t.Fatalf("read staging root: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".rotated-preflight-") {
			t.Errorf("preflight probe %s was left behind", e.Name())
		}
	}
}

// 30d. A staging root that accepts a file but will not give it up is not usable.
// Promotion renames and release unlinks, so a directory whose entries cannot be removed
// would let capture pin inodes nothing could ever free.
func TestRuntimePreflightProbeRequiresCleanRemoval(t *testing.T) {
	errClose := errors.New("close refused")
	errRemove := errors.New("unlink refused")

	for _, tc := range []struct {
		name     string
		close    func(*os.File) error
		remove   func(string) error
		wantErrs []error
	}{
		{
			name:     "removal fails",
			remove:   func(string) error { return errRemove },
			wantErrs: []error{errRemove},
		},
		{
			name:     "close fails but removal is still attempted",
			close:    func(*os.File) error { return errClose },
			wantErrs: []error{errClose},
		},
		{
			name:     "both fail and both causes survive",
			close:    func(*os.File) error { return errClose },
			remove:   func(string) error { return errRemove },
			wantErrs: []error{errClose, errRemove},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newRuntimeHarness(t)
			sup := h.handler.rotatedCollection()

			var removed []string
			sup.probeClose = tc.close
			sup.probeRemove = func(name string) error {
				removed = append(removed, name)
				if tc.remove != nil {
					return tc.remove(name)
				}
				return os.Remove(name)
			}

			h.handler.startRotatedCollection()

			key := rotatedKey{session: testSessionA, node: testNodeID}
			err := sup.disabledReason(key)
			if err == nil {
				t.Fatal("an unusable staging root was accepted")
			}
			for _, want := range tc.wantErrs {
				if !errors.Is(err, want) {
					t.Errorf("preflight error %v does not carry %v", err, want)
				}
			}
			if !sup.durablyDisabled(key) {
				t.Errorf("an unusable staging root was classified as retryable: %v", err)
			}
			if _, ok := sup.activeKey(); ok {
				t.Error("a collector was started against an unusable staging root")
			}
			// Removal is attempted on every path, including the one where close failed.
			if len(removed) != 1 {
				t.Errorf("probe removal attempts = %d, want exactly 1", len(removed))
			}
			// A close that this test faked leaves the real descriptor open; clean it up
			// so the temp directory can be removed on Windows-like platforms.
			if tc.close != nil {
				_ = os.Remove(removed[0])
			}
		})
	}
}

// 30e. A staging root that is momentarily full is retryable — that is a condition this
// subsystem is built to survive — while permission and cleanup failures are not.
func TestRuntimePreflightProbeENOSPCIsRetryable(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()
	key := rotatedKey{session: testSessionA, node: testNodeID}

	var full atomic.Bool
	full.Store(true)
	sup.probeCreate = func(dir string) (*os.File, error) {
		if full.Load() {
			return nil, &os.PathError{Op: "open", Path: dir, Err: syscall.ENOSPC}
		}
		return os.CreateTemp(dir, ".rotated-preflight-*")
	}

	h.handler.startRotatedCollection()
	if _, ok := sup.activeKey(); ok {
		t.Fatal("a collector started against a full staging root")
	}
	if sup.disabledReason(key) == nil {
		t.Fatal("the full staging root was not recorded")
	}
	if sup.durablyDisabled(key) {
		t.Fatalf("a full staging root was classified as durable: %v", sup.disabledReason(key))
	}

	// Capacity comes back.
	full.Store(false)
	h.advanceClock(rotatedRetryBase + time.Second)
	h.handler.ensureRotatedCollection(h.handler.SessionDir)
	h.awaitCollector()
	if got, ok := sup.activeKey(); !ok || got != key {
		t.Errorf("collector after capacity recovery = %+v (active=%v), want %+v", got, ok, key)
	}
	if sup.disabledReason(key) != nil {
		t.Errorf("the recovered identity still reports %v", sup.disabledReason(key))
	}
}

func TestRuntimePreflightProbePermissionFailureIsDurable(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()
	sup.probeCreate = func(dir string) (*os.File, error) {
		return nil, &os.PathError{Op: "open", Path: dir, Err: syscall.EACCES}
	}

	h.handler.startRotatedCollection()
	key := rotatedKey{session: testSessionA, node: testNodeID}
	if !sup.durablyDisabled(key) {
		t.Errorf("a permission failure creating the probe was classified as retryable: %v", sup.disabledReason(key))
	}
}

// 31. A logs path that is not a directory is a durable misconfiguration, not something
// to retry forever.
func TestRuntimeLogsPathThatIsNotADirectoryIsDurable(t *testing.T) {
	h := newRuntimeHarness(t)
	sessionDir := h.sessionDir(testSessionB)
	writeFile(t, filepath.Join(sessionDir, utils.RAY_SESSIONDIR_LOGDIR_NAME), "not a directory")

	h.handler.ensureRotatedCollection(sessionDir)

	key := rotatedKey{session: testSessionB, node: testNodeID}
	if !h.handler.rotatedCollection().durablyDisabled(key) {
		t.Errorf("a logs path that is a regular file was not durably disabled: %v",
			h.handler.rotatedCollection().disabledReason(key))
	}
	if _, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Error("a collector was started for a logs path that is not a directory")
	}
}

// ---------------------------------------------------------------------------
// The production hook
// ---------------------------------------------------------------------------

// 33. A session change carries a node change, and the first collector built for the new
// session must already be on the new node.
//
// There is no second chance at this. The node ID is written into the staged entry, the
// staging path and the object key at capture time, and a later collector on the correct
// node adopts those records without rewriting them, so anything captured under the
// previous session's node stays addressed to it permanently.
func TestPollActiveSessionChangesUsesTheNewSessionsNodeID(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()
	h.write(testSessionA, "raylet.out", "active")

	h.makeSession(testSessionB)
	h.write(testSessionB, "raylet.out", "active")
	h.pointSessionLatest(testSessionB)
	h.discoverNodeB() // the raylet restarted, so the node ID moved with it

	go h.handler.PollActiveSessionChanges()
	t.Cleanup(func() { close(h.handler.ShutdownChan) })

	wantKey := rotatedKey{session: testSessionB, node: testNodeIDB}
	eventually(t, "the poller to move the collector to the new session and node", func() bool {
		key, ok := h.handler.rotatedCollection().activeKey()
		return ok && key == wantKey
	})

	// Not one collector was ever built for session B under node A — not even a
	// short-lived one that was replaced a moment later.
	for _, k := range h.built() {
		if k.session == testSessionB && k.node != testNodeIDB {
			t.Errorf("a collector for session B was built on node %s, want only %s", k.node, testNodeIDB)
		}
	}
	if got := h.handler.GetRayNodeName(); got != testNodeIDB {
		t.Errorf("handler node ID = %s, want %s", got, testNodeIDB)
	}

	// Everything the new session captures is addressed to the new node.
	entry := h.captureIn(testSessionB, "raylet.out.1", "new session segment")
	if entry.NodeName != testNodeIDB || entry.SessionName != testSessionB {
		t.Errorf("staged entry = %s/%s, want %s/%s", entry.SessionName, entry.NodeName, testSessionB, testNodeIDB)
	}
	rc := h.handler.rotatedCollection().testCollector()
	wantPrefix := h.logsPrefixFor(testSessionB, testNodeIDB) + "/"
	if k := entry.objectKey(rc.cfg.Cluster); !strings.HasPrefix(k, wantPrefix) {
		t.Errorf("object key %s is not under the new node's prefix %s", k, wantPrefix)
	}
	for _, p := range h.stagingFiles() {
		if strings.HasPrefix(p, testSessionB+"/") && !strings.HasPrefix(p, testSessionB+"/"+testNodeIDB+"/") {
			t.Errorf("session B staged a capture outside its own node's subtree: %s", p)
		}
	}

	// The old session's logs were relocated under the node they were written on.
	oldPrevLogs := filepath.Join(utils.GetRayPrevLogsPath(), testSessionA, testNodeID, "logs")
	if _, err := os.Stat(oldPrevLogs); err != nil {
		t.Errorf("old session logs were not relocated to %s: %v", oldPrevLogs, err)
	}
	if _, err := os.Stat(filepath.Join(utils.GetRayPrevLogsPath(), testSessionA, testNodeIDB)); err == nil {
		t.Error("the old session's logs were relocated under the new session's node ID")
	}
}

// 34. A session change whose node ID cannot be discovered starts no collector at all
// rather than one under the previous session's node.
//
// The old collector is still retired first — that is its final reconciliation, and it
// happens before anything touches its tree — and the relocation still runs, because
// prev-logs is the legacy path for those logs and a dashboard that is briefly
// unreachable must not strand a session's logs outside it. What must not happen is a
// collector for the new session addressed to the old session's node: that would be
// baked into its staged entries and object keys permanently.
func TestPollActiveSessionChangesWaitsForTheNewNodeID(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()
	first := h.handler.rotatedCollection().activeRun()

	h.makeSession(testSessionB)
	h.write(testSessionB, "raylet.out", "active")
	h.pointSessionLatest(testSessionB)
	h.failNodeDiscovery() // the dashboard is still coming up after the restart

	go h.handler.PollActiveSessionChanges()
	t.Cleanup(func() { close(h.handler.ShutdownChan) })

	// The relocation is the observable end of the changeover.
	oldPrevLogs := filepath.Join(utils.GetRayPrevLogsPath(), testSessionA, testNodeID, "logs")
	eventually(t, "the old session's logs to be relocated", func() bool {
		_, err := os.Stat(oldPrevLogs)
		return err == nil
	})

	if _, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Error("a collector is active even though the new session's node ID is unknown")
	}
	if !first.finished() {
		t.Error("the old collector was not retired before its tree was relocated")
	}
	for _, k := range h.built() {
		if k.session == testSessionB {
			t.Errorf("a collector for session B was built before its node ID was known: %+v", k)
		}
	}
	if _, err := os.Stat(filepath.Join(utils.GetRayPrevLogsPath(), testSessionA, testNodeIDB)); err == nil {
		t.Error("the old session's logs were relocated under a node it never ran on")
	}

	// Once the dashboard answers, protection resumes under the correct node.
	h.discoverNodeB()
	eventually(t, "rotated protection to resume on the new node", func() bool {
		key, ok := h.handler.rotatedCollection().activeKey()
		return ok && key == rotatedKey{session: testSessionB, node: testNodeIDB}
	})
	for _, k := range h.built() {
		if k.session == testSessionB && k.node != testNodeIDB {
			t.Errorf("session B was eventually built on node %s, want only %s", k.node, testNodeIDB)
		}
	}
}

// 35. A relocation that keeps failing re-runs the move without disturbing the collector
// that is already correct.
func TestPollActiveSessionChangesDoesNotChurnWhenRelocationFails(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()
	h.write(testSessionA, "raylet.out", "active")

	// prev-logs is a regular file, so MoveSessionLogsToPrevLogs cannot create its
	// destination and every relocation attempt fails.
	writeFile(t, utils.GetRayPrevLogsPath(), "not a directory")

	h.makeSession(testSessionB)
	h.write(testSessionB, "raylet.out", "active")
	h.pointSessionLatest(testSessionB)
	h.discoverNodeB()

	go h.handler.PollActiveSessionChanges()
	t.Cleanup(func() { close(h.handler.ShutdownChan) })

	wantKey := rotatedKey{session: testSessionB, node: testNodeIDB}
	eventually(t, "the collector to move to the new session", func() bool {
		key, ok := h.handler.rotatedCollection().activeKey()
		return ok && key == wantKey
	})
	run := h.handler.rotatedCollection().activeRun()
	builds := h.buildCount()

	// The relocation is retried on every tick because the session change is never
	// recorded as complete. The collector must sit still through all of it.
	eventually(t, "at least two further relocation attempts", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.nodeCalls >= 3
	})

	if got := h.handler.rotatedCollection().activeRun(); got != run {
		t.Error("a failing relocation replaced the collector that was already correct")
	}
	if run.finished() {
		t.Error("a failing relocation retired the running collector")
	}
	if got := h.buildCount(); got != builds {
		t.Errorf("collectors built by repeated relocation failures = %d, want none", got-builds)
	}
	if key, _ := h.handler.rotatedCollection().activeKey(); key != wantKey {
		t.Errorf("identity drifted to %+v across relocation retries, want %+v", key, wantKey)
	}
}

// 35b. Node discovery that keeps failing never falls back to the previous session's
// node.
//
// The tick after a session change no longer looks like a change, so a runtime that
// treats "no verified node" as "use whatever the handler holds" starts the new session
// under the old session's node on that tick instead of the first one. Nothing later
// repairs it: the node is durable in the staged entry and the object key from the first
// capture onwards.
func TestPollActiveSessionChangesNeverFallsBackToThePreviousNode(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()
	h.write(testSessionA, "raylet.out", "active")

	h.makeSession(testSessionB)
	h.write(testSessionB, "raylet.out", "active")
	h.pointSessionLatest(testSessionB)
	h.failNodeDiscovery()

	go h.handler.PollActiveSessionChanges()
	t.Cleanup(func() { close(h.handler.ShutdownChan) })

	// Three full polling cycles with no node ID available.
	eventually(t, "three failed discovery cycles", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.nodeCalls >= 3
	})

	if key, ok := h.handler.rotatedCollection().activeKey(); ok {
		t.Errorf("a collector is active for %+v while no node ID has been verified", key)
	}
	for _, k := range h.built() {
		if k.session == testSessionB {
			t.Errorf("session B was built under node %s before any node was verified", k.node)
		}
	}
	for _, p := range h.stagingFiles() {
		if strings.HasPrefix(p, testSessionB+"/"+testNodeID+"/") {
			t.Errorf("session B staged a capture under the previous session's node: %s", p)
		}
	}

	// And when the node finally is discovered, exactly one collector starts for it.
	h.discoverNodeB()
	wantKey := rotatedKey{session: testSessionB, node: testNodeIDB}
	eventually(t, "the collector to start under the verified node", func() bool {
		key, ok := h.handler.rotatedCollection().activeKey()
		return ok && key == wantKey
	})
	starts := 0
	for _, k := range h.built() {
		if k.session == testSessionB {
			if k.node != testNodeIDB {
				t.Errorf("session B was built under node %s, want only %s", k.node, testNodeIDB)
			}
			starts++
		}
	}
	if starts != 1 {
		t.Errorf("session B collectors built = %d, want exactly 1", starts)
	}
}

// 35c. A relocation retry is only a relocation retry. A node rediscovery that fails
// during one must not disturb the collector the previous tick got right.
func TestPollActiveSessionChangesRelocationRetryKeepsTheCollector(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()
	h.write(testSessionA, "raylet.out", "active")

	// prev-logs is a regular file, so every relocation attempt fails.
	writeFile(t, utils.GetRayPrevLogsPath(), "not a directory")

	h.makeSession(testSessionB)
	h.write(testSessionB, "raylet.out", "active")
	h.pointSessionLatest(testSessionB)
	h.discoverNodeB()

	go h.handler.PollActiveSessionChanges()
	t.Cleanup(func() { close(h.handler.ShutdownChan) })

	wantKey := rotatedKey{session: testSessionB, node: testNodeIDB}
	eventually(t, "the collector to move to the new session and node", func() bool {
		key, ok := h.handler.rotatedCollection().activeKey()
		return ok && key == wantKey
	})
	run := h.handler.rotatedCollection().activeRun()
	rc := h.handler.rotatedCollection().testCollector()
	watchers := h.watchers.count()
	builds := h.buildCount()
	callsBefore := func() int {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.nodeCalls
	}()

	// The dashboard goes away while the relocation is still being retried.
	h.failNodeDiscovery()
	eventually(t, "two further ticks with discovery failing", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.nodeCalls >= callsBefore+2
	})

	// A discovery that fails leaves the verified identity exactly as it was. Treating
	// "no answer" as an answer would blank the node the rest of the runtime writes
	// under — including the legacy walk, which has no other source for it.
	if got := h.handler.GetRayNodeName(); got != testNodeIDB {
		t.Errorf("handler node ID = %q after failed rediscoveries, want it left at %s", got, testNodeIDB)
	}

	if got := h.handler.rotatedCollection().activeRun(); got != run {
		t.Error("a relocation retry replaced the collector that was already correct")
	}
	if h.handler.rotatedCollection().testCollector() != rc {
		t.Error("a relocation retry rebuilt the collector")
	}
	if run.finished() {
		t.Error("a relocation retry retired the running collector")
	}
	if h.watchers.at(watchers - 1).isClosed() {
		t.Error("a relocation retry closed the running collector's watcher")
	}
	if got := h.watchers.count(); got != watchers {
		t.Errorf("watchers created during relocation retries = %d, want none", got-watchers)
	}
	if got := h.buildCount(); got != builds {
		t.Errorf("collectors built during relocation retries = %d, want none", got-builds)
	}

	// When relocation finally works, it is still the same collector.
	if err := os.Remove(utils.GetRayPrevLogsPath()); err != nil {
		t.Fatalf("clear prev-logs: %v", err)
	}
	oldPrevLogs := filepath.Join(utils.GetRayPrevLogsPath(), testSessionA, testNodeID, "logs")
	eventually(t, "the relocation to succeed", func() bool {
		_, err := os.Stat(oldPrevLogs)
		return err == nil
	})
	if got := h.handler.rotatedCollection().activeRun(); got != run {
		t.Error("the collector changed once relocation succeeded")
	}
}

// 35d. Handover state is read from the supervisor, not remembered from having called
// ensure. Run's initial start can fail retryably — the session directory exists but
// logs/ does not yet — and a poller that seeds "handed off" from the fact that
// startRotatedCollection ran would never try again.
func TestPollActiveSessionChangesRecoversFromAFailedInitialStart(t *testing.T) {
	h := newRuntimeHarness(t)
	// The handler is configured for a session whose logs/ has not appeared yet.
	if err := os.RemoveAll(h.logsDir(testSessionA)); err != nil {
		t.Fatalf("remove logs dir: %v", err)
	}
	h.handler.startRotatedCollection()

	key := rotatedKey{session: testSessionA, node: testNodeID}
	sup := h.handler.rotatedCollection()
	if _, ok := sup.activeKey(); ok {
		t.Fatal("a collector started without a logs directory")
	}
	if sup.durablyDisabled(key) {
		t.Fatalf("a missing logs directory was recorded as durable: %v", sup.disabledReason(key))
	}
	if h.handler.rotatedHandedOff(h.sessionDir(testSessionA), testNodeID) {
		t.Fatal("a failed start counts as a handover")
	}

	go h.handler.PollActiveSessionChanges()
	t.Cleanup(func() { close(h.handler.ShutdownChan) })

	// Ray creates the directory, and the retry deadline passes.
	h.makeSession(testSessionA)
	h.advanceClock(rotatedRetryBase + time.Second)

	eventually(t, "the collector to start once its logs directory exists", func() bool {
		return sup.statusFor(testSessionA, testNodeID, h.logsDir(testSessionA)) == runReady
	})
	starts := 0
	for _, k := range h.built() {
		if k == key {
			starts++
		}
	}
	if starts != 1 {
		t.Errorf("collectors built for %+v = %d, want exactly 1", key, starts)
	}
}

// 35e. A collector that fails asynchronously after it was attached is noticed, and the
// next polls try again — subject to the supervisor's backoff, not to a poller that
// believes the handover already happened.
func TestPollActiveSessionChangesRetriesAfterAsynchronousStartupFailure(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()
	key := rotatedKey{session: testSessionA, node: testNodeID}

	var fail atomic.Bool
	fail.Store(true)
	base := sup.tune
	sup.tune = func(cfg *rotatedCollectorConfig) {
		base(cfg)
		if fail.Load() {
			// Attaches, then fails during startup: retryable, and only observable
			// through the supervisor.
			cfg.NewWatcher = func() (fsWatcher, error) {
				return nil, fmt.Errorf("inotify_init: %w", syscall.EMFILE)
			}
		}
	}

	h.handler.startRotatedCollection()
	eventually(t, "the asynchronous failure to be published", func() bool {
		return sup.disabledReason(key) != nil
	})
	if sup.durablyDisabled(key) {
		t.Fatal("an exhausted watch limit was recorded as durable")
	}
	if h.handler.rotatedHandedOff(h.sessionDir(testSessionA), testNodeID) {
		t.Fatal("a collector that failed after attaching still counts as a handover")
	}

	go h.handler.PollActiveSessionChanges()
	t.Cleanup(func() { close(h.handler.ShutdownChan) })

	// Backoff is respected: nothing is rebuilt until the deadline passes.
	builds := h.buildCount()
	time.Sleep(150 * time.Millisecond) // several polling cycles at the test interval
	if got := h.buildCount(); got != builds {
		t.Errorf("collectors built before the retry deadline = %d, want none", got-builds)
	}

	fail.Store(false)
	h.advanceClock(rotatedRetryBase + time.Second)
	eventually(t, "the retry to start and reach ready", func() bool {
		return sup.statusFor(testSessionA, testNodeID, h.logsDir(testSessionA)) == runReady
	})
	if sup.disabledReason(key) != nil {
		t.Errorf("the recovered identity still reports %v", sup.disabledReason(key))
	}
}

// 35e2. A run whose goroutine has exited is not a handover, even in the window before
// its failure has been published and its pointer cleared.
//
// That window is short but it is the only time runFinished is observable, and it is
// exactly when a poller that counted it as a handover would stop retrying.
func TestPollActiveSessionChangesTreatsAFinishedRunAsNotHandedOff(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()

	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	sup.beforeFailurePublish = func() {
		once.Do(func() {
			close(entered)
			<-release
		})
	}

	h.start()
	close(h.watchers.at(0).events) // fatal to the collector
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the collector never reached failure publication")
	}
	defer close(release)

	// Attached, but its goroutine has exited.
	if got := sup.statusFor(testSessionA, testNodeID, h.logsDir(testSessionA)); got != runFinished {
		t.Fatalf("status = %v, want %v", got, runFinished)
	}
	if h.handler.rotatedHandedOff(h.sessionDir(testSessionA), testNodeID) {
		t.Error("a run whose goroutine has exited counts as a completed handover")
	}
}

// 35f. A durable failure is not retried by the poller either, however many ticks pass.
func TestPollActiveSessionChangesDoesNotHotLoopADurableFailure(t *testing.T) {
	h := newRuntimeHarness(t)
	sup := h.handler.rotatedCollection()
	base := sup.tune
	sup.tune = func(cfg *rotatedCollectorConfig) {
		base(cfg)
		cfg.NewWatcher = func() (fsWatcher, error) {
			return nil, fmt.Errorf("create fsnotify watcher: %w", os.ErrPermission)
		}
	}

	h.handler.startRotatedCollection()
	key := rotatedKey{session: testSessionA, node: testNodeID}
	eventually(t, "the durable failure to be published", func() bool {
		return sup.durablyDisabled(key)
	})
	builds := h.buildCount()

	go h.handler.PollActiveSessionChanges()
	t.Cleanup(func() { close(h.handler.ShutdownChan) })

	h.advanceClock(24 * time.Hour)
	time.Sleep(200 * time.Millisecond) // many polling cycles at the test interval
	if got := h.buildCount(); got != builds {
		t.Errorf("collectors built for a durable failure = %d, want none", got-builds)
	}
	if _, ok := sup.activeKey(); ok {
		t.Error("a durably failed identity became active")
	}
}

// 35g. Every observed session is relocated under the node it actually ran on, however
// many sessions pass while one of them is stuck.
func TestPollActiveSessionChangesFilesEachSessionUnderItsOwnNode(t *testing.T) {
	const testSessionC = "session_2026-07-31_12-00-00_000003"
	const testNodeIDC = "00112233445566778899aabbccddeeff"

	h := newRuntimeHarness(t)
	h.start()
	h.write(testSessionA, "raylet.out", "session A")

	// prev-logs is a regular file, so relocating A fails.
	writeFile(t, utils.GetRayPrevLogsPath(), "not a directory")

	h.makeSession(testSessionB)
	h.write(testSessionB, "raylet.out", "session B")
	h.pointSessionLatest(testSessionB)
	h.discoverNodeB()

	go h.handler.PollActiveSessionChanges()
	t.Cleanup(func() { close(h.handler.ShutdownChan) })

	eventually(t, "the collector to move to session B", func() bool {
		key, ok := h.handler.rotatedCollection().activeKey()
		return ok && key == rotatedKey{session: testSessionB, node: testNodeIDB}
	})

	// A third session arrives while A is still stuck.
	h.makeSession(testSessionC)
	h.write(testSessionC, "raylet.out", "session C")
	h.pointSessionLatest(testSessionC)
	h.mu.Lock()
	h.nodeID = testNodeIDC
	h.mu.Unlock()

	wantC := rotatedKey{session: testSessionC, node: testNodeIDC}
	eventually(t, "the collector to move to session C", func() bool {
		key, ok := h.handler.rotatedCollection().activeKey()
		return ok && key == wantC
	})
	runC := h.handler.rotatedCollection().activeRun()

	// Relocation starts working.
	if err := os.Remove(utils.GetRayPrevLogsPath()); err != nil {
		t.Fatalf("clear prev-logs: %v", err)
	}
	prev := utils.GetRayPrevLogsPath()
	wantA := filepath.Join(prev, testSessionA, testNodeID, "logs", "raylet.out")
	wantB := filepath.Join(prev, testSessionB, testNodeIDB, "logs", "raylet.out")
	eventually(t, "both stuck sessions to be relocated", func() bool {
		_, errA := os.Stat(wantA)
		_, errB := os.Stat(wantB)
		return errA == nil && errB == nil
	})

	// Neither landed under the other's node, or under the current one.
	for _, wrong := range []string{
		filepath.Join(prev, testSessionA, testNodeIDB),
		filepath.Join(prev, testSessionA, testNodeIDC),
		filepath.Join(prev, testSessionB, testNodeID),
		filepath.Join(prev, testSessionB, testNodeIDC),
	} {
		if _, err := os.Stat(wrong); err == nil {
			t.Errorf("logs were filed under the wrong node: %s", wrong)
		}
	}
	// And session C's collector was never disturbed by any of it.
	if got := h.handler.rotatedCollection().activeRun(); got != runC || runC.finished() {
		t.Error("the current session's collector was replaced or retired by relocation retries")
	}
}

// 35h. The broad sweep waits while a known session's own relocation is stuck.
//
// The sweep files every inactive session directory under one node, so running it while
// session A is still waiting would put A's logs under whatever node is current now.
// Blocking only A's exact destination is what separates the two: A's own move fails
// while the sweep would succeed.
func TestRelocationSweepWaitsForAStuckKnownSession(t *testing.T) {
	h := newRuntimeHarness(t)
	h.write(testSessionA, "raylet.out", "session A")

	// prev-logs/<A>/<node-A> is a regular file, so only A's exact move can fail.
	blocked := filepath.Join(utils.GetRayPrevLogsPath(), testSessionA, testNodeID)
	writeFile(t, blocked, "not a directory")

	h.makeSession(testSessionB)
	h.write(testSessionB, "raylet.out", "session B")
	h.discoverNodeB()

	st := sessionTransition{dir: h.sessionDir(testSessionA), node: testNodeID, handedOff: true}
	h.handler.advanceSession(&st, h.sessionDir(testSessionB))

	if len(st.pending) != 1 || st.pending[0].node != testNodeID {
		t.Fatalf("pending relocations = %+v, want session A still waiting under its own node", st.pending)
	}
	if _, err := os.Stat(h.logsDir(testSessionA)); err != nil {
		t.Errorf("session A's logs were moved even though its own destination is blocked: %v", err)
	}
	wrong := filepath.Join(utils.GetRayPrevLogsPath(), testSessionA, testNodeIDB)
	if _, err := os.Stat(wrong); err == nil {
		t.Errorf("the broad sweep filed session A under %s while its own relocation was still waiting", testNodeIDB)
	}
}

// 36. The session poller is the production hook for a session change, so it is exercised
// end to end rather than only through ensureRotatedCollection.
func TestPollActiveSessionChangesReplacesTheRotatedCollector(t *testing.T) {
	h := newRuntimeHarness(t)
	h.start()
	first := h.handler.rotatedCollection().activeRun()

	h.makeSession(testSessionB)
	h.write(testSessionB, "raylet.out", "active")
	h.pointSessionLatest(testSessionB)

	go h.handler.PollActiveSessionChanges()
	t.Cleanup(func() { close(h.handler.ShutdownChan) })

	eventually(t, "the poller to move the collector to the new session", func() bool {
		key, ok := h.handler.rotatedCollection().activeKey()
		return ok && key.session == testSessionB
	})
	if !first.finished() {
		t.Error("the old session's collector is still running")
	}
	// The handover happened before the old tree was relocated, which is the only order
	// in which the old collector's final reconciliation can see anything.
	if h.sawLivePredecessor() {
		t.Error("the poller let two collectors overlap")
	}
}
