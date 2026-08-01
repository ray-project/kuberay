package logcollector

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
)

// defaultReconcileInterval is the backstop sweep period. fsnotify can drop events
// under load, and a watch added to a directory cannot see what was already in it,
// so discovery never relies on events alone.
const defaultReconcileInterval = 30 * time.Second

// errIntakePaused reports that the staging volume is full. Capture stops adding new
// data but never deletes what it already holds. Uploads, promotions and releases keep
// running, and it is a release — or a probe link that succeeds — that lifts the pause.
var errIntakePaused = errors.New("staging volume full: rotated log intake paused")

// fsWatcher is the slice of fsnotify the collector uses, so tests can drive events
// deterministically instead of waiting on the kernel.
type fsWatcher interface {
	Add(name string) error
	Close() error
	Events() <-chan fsnotify.Event
	Errors() <-chan error
}

// fsnotifyWatcher adapts *fsnotify.Watcher, whose Events and Errors are fields.
type fsnotifyWatcher struct{ w *fsnotify.Watcher }

func newFsnotifyWatcher() (fsWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}
	return &fsnotifyWatcher{w: w}, nil
}

func (f *fsnotifyWatcher) Add(name string) error         { return f.w.Add(name) }
func (f *fsnotifyWatcher) Close() error                  { return f.w.Close() }
func (f *fsnotifyWatcher) Events() <-chan fsnotify.Event { return f.w.Events }
func (f *fsnotifyWatcher) Errors() <-chan error          { return f.w.Errors }

// rotatedCollectorConfig configures one collector. Everything the loop touches that
// is not a plain filesystem operation is injectable, so tests can be deterministic
// while still exercising real files, real hard links and real link counts.
type rotatedCollectorConfig struct {
	LogsDir     string // the active session's logs directory
	StagingRoot string
	SessionName string
	NodeName    string

	// Cluster is where captures land in object storage. Writer is what puts them
	// there; a nil Writer leaves the collector capturing and accounting normally with
	// the upload pipeline switched off, which is how it runs until the wiring tranche
	// hands it the runtime's storage client.
	Cluster clusterIdentity
	Writer  objectWriter

	// HighWaterBytes pauses new intake once the staging volume holds that many bytes,
	// and LowWaterBytes resumes it once the total falls back to that. Zero
	// HighWaterBytes disables the watermarks; a filesystem that reports ENOSPC still
	// pauses intake either way. Defaults belong to the tranche that configures this
	// from the operator, not here.
	HighWaterBytes int64
	LowWaterBytes  int64

	ReconcileInterval time.Duration
	NewWatcher        func() (fsWatcher, error)
	NewTicker         func(time.Duration) (<-chan time.Time, func())
	CaptureIDs        *captureIDGenerator

	// UploadBackoff is the delay before each successive upload retry, with the last
	// entry repeating. Now and NewTimer are the clock the retry schedule runs on, so
	// tests can advance time instead of waiting for it.
	UploadBackoff []time.Duration
	Now           func() time.Time
	NewTimer      func(time.Duration) (<-chan time.Time, func())

	// WorkerStopGrace bounds how long Stop waits for an upload the storage interface
	// gives it no way to cancel.
	WorkerStopGrace time.Duration

	// Link creates the staging hard link. Production always uses captureLink; tests
	// replace it to make the race between validating a candidate and pinning it
	// deterministic.
	Link func(src, dst string) error

	// BeforeReconstruct runs after watches are installed and before staging
	// reconstruction. Tests hold the collector in that window to prove the tree is
	// already covered while reconstruction is in progress.
	BeforeReconstruct func()

	// OnIssue receives problems that must not stop discovery: a segment lost to a
	// rotation race, an unsupported filesystem, a corrupt staging record.
	OnIssue func(error)
}

// rotatedCollector discovers Ray's rotated log segments and pins them with hard
// links before rotation can delete them.
//
// One goroutine owns all state. fsnotify events, the reconcile ticker, startup
// reconstruction and callers' requests all become work performed by that goroutine,
// so "look up the inode, create the link, register the capture" is indivisible
// without a single mutex.
//
// Object storage is reached only from the upload worker, never from this goroutine.
// An upload is slow and, through storage.StorageWriter, uncancelable, so letting one
// sit between a rotation and its capture would lose exactly the segments this
// collector exists to keep. The owner decides everything about an upload — when it
// starts, whether its result still applies, what happens next — but never waits for
// one.
type rotatedCollector struct {
	cfg     rotatedCollectorConfig
	ix      *captureIndex
	watcher fsWatcher

	// up, bytes and gate are owned by the same goroutine as ix. Uploading is the one
	// slow thing the collector does, so it is pushed onto a worker that returns an
	// immutable result; every decision that result leads to is made here.
	up    *uploadScheduler
	bytes *stagedBytes
	gate  *intakeGate

	// intakePauses and intakeResumes count gate transitions. A wrongly reopened gate
	// can be closed again before anyone looks at it, so the counts — not the current
	// flag — are what show intake was reopened at all.
	intakePauses  int
	intakeResumes int

	stopCh chan struct{}
	doneCh chan struct{}

	snapshotReq  chan chan []stagedEntry
	reconcileReq chan chan struct{}
	statsReq     chan chan collectorStats
}

// collectorStats is a consistent view of the collector's own bookkeeping. Like
// snapshot it is produced by the owner goroutine, because staged bytes, the upload
// queue and the intake gate are owner-owned and must never be read from outside it.
type collectorStats struct {
	StagedBytes       int64
	Captures          int
	Pending           int
	Uploaded          int
	QueuedUploads     int
	InFlightUploads   int
	AwaitingPromotion int
	IntakePauses      int
	IntakeResumes     int
	IntakePaused      bool
}

func newRotatedCollector(cfg rotatedCollectorConfig) (*rotatedCollector, error) {
	if cfg.LogsDir == "" || cfg.StagingRoot == "" {
		return nil, fmt.Errorf("rotated collector: LogsDir and StagingRoot are required")
	}
	if err := validatePathSegment("session name", cfg.SessionName); err != nil {
		return nil, err
	}
	if err := validatePathSegment("node name", cfg.NodeName); err != nil {
		return nil, err
	}
	if cfg.ReconcileInterval <= 0 {
		cfg.ReconcileInterval = defaultReconcileInterval
	}
	if cfg.NewWatcher == nil {
		cfg.NewWatcher = newFsnotifyWatcher
	}
	if cfg.NewTicker == nil {
		cfg.NewTicker = func(d time.Duration) (<-chan time.Time, func()) {
			t := time.NewTicker(d)
			return t.C, t.Stop
		}
	}
	if cfg.CaptureIDs == nil {
		cfg.CaptureIDs = newCaptureIDGenerator()
	}
	if cfg.Link == nil {
		cfg.Link = captureLink
	}
	if cfg.OnIssue == nil {
		cfg.OnIssue = func(err error) { logrus.Warnf("Rotated log collector: %v", err) }
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.NewTimer == nil {
		cfg.NewTimer = func(d time.Duration) (<-chan time.Time, func()) {
			t := time.NewTimer(d)
			return t.C, func() { t.Stop() }
		}
	}
	if len(cfg.UploadBackoff) == 0 {
		cfg.UploadBackoff = defaultUploadBackoff
	}
	for i, d := range cfg.UploadBackoff {
		// A zero or negative delay would make a failing upload due the instant it
		// failed, turning the retry schedule into a spin against the object store.
		if d <= 0 {
			return nil, fmt.Errorf("rotated collector: UploadBackoff[%d] is %s, but every retry delay must be positive", i, d)
		}
	}
	if cfg.WorkerStopGrace <= 0 {
		cfg.WorkerStopGrace = defaultWorkerStopGrace
	}
	if err := validateWatermarks(cfg.HighWaterBytes, cfg.LowWaterBytes); err != nil {
		return nil, err
	}

	return &rotatedCollector{
		cfg:          cfg,
		ix:           newCaptureIndex(),
		up:           newUploadScheduler(cfg.Writer, cfg.UploadBackoff),
		bytes:        newStagedBytes(),
		gate:         &intakeGate{high: cfg.HighWaterBytes, low: cfg.LowWaterBytes},
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		snapshotReq:  make(chan chan []stagedEntry),
		reconcileReq: make(chan chan struct{}),
		statsReq:     make(chan chan collectorStats),
	}, nil
}

// Run owns the collector's state until Stop is called. Callers run it in its own
// goroutine.
func (rc *rotatedCollector) Run() error {
	defer close(rc.doneCh)

	w, err := rc.cfg.NewWatcher()
	if err != nil {
		return err
	}
	rc.watcher = w
	defer func() {
		if err := w.Close(); err != nil {
			rc.report(fmt.Errorf("close watcher: %w", err))
		}
	}()

	// Startup order is load-bearing.
	//
	// Watches go on first, and install nothing else: until a directory is watched,
	// a segment can be created and deleted inside it without leaving any trace for
	// a later scan to find. Reconstruction can take a while on a large staging
	// volume, and that whole window would otherwise be blind.
	//
	// Reconstruction then runs before any capture, so a restart adopts what the
	// previous run pinned instead of minting a second ID for it. Only then does the
	// full scan capture, and only then are the events that queued in the watcher
	// channel since step one processed.
	if err := rc.installWatchesRecursive(rc.cfg.LogsDir); err != nil {
		return fmt.Errorf("rotated log collector cannot watch the whole logs tree: %w", err)
	}

	if rc.cfg.BeforeReconstruct != nil {
		rc.cfg.BeforeReconstruct()
	}
	if err := rc.reconstructStaging(); err != nil {
		return err
	}

	// Settle the gate on what the previous run left staged before the live scan can
	// add to it. A restart that adopts a volume already over its limit must not then
	// capture its way further past it.
	rc.enforceHighWater()

	rc.scanTree()

	// The worker starts only once reconstruction has established what is already
	// staged, so nothing is uploaded before the collector knows whether a previous
	// run already sent it.
	rc.up.start(rc.cfg.StagingRoot)
	defer rc.up.stop(rc.cfg.WorkerStopGrace, rc.report)

	// Adopt the previous run's work: pending captures are queued, uploaded ones are
	// never re-sent but are always candidates for release.
	rc.sweepUploads()
	rc.sweepReleases()

	tick, stopTicker := rc.cfg.NewTicker(rc.cfg.ReconcileInterval)
	defer stopTicker()

	for {
		// Housekeeping runs before any request is served, so a caller that gets a
		// snapshot is looking at state the scheduler has already acted on. It never
		// blocks and never performs a storage call. It fails only for a condition
		// that retrying cannot fix.
		if err := rc.pump(); err != nil {
			return err
		}

		select {
		case <-rc.stopCh:
			return nil

		case event, ok := <-w.Events():
			if !ok {
				return errors.New("rotated log watcher event channel closed")
			}
			rc.handleEvent(event)

		case err, ok := <-w.Errors():
			if !ok {
				return errors.New("rotated log watcher error channel closed")
			}
			// An overflow means events were dropped, so the only safe response is
			// to look at the tree again immediately rather than wait for the tick.
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				rc.report(fmt.Errorf("watcher overflowed, reconciling immediately: %w", err))
				rc.maintain()
				continue
			}
			rc.report(fmt.Errorf("watcher error: %w", err))

		case <-tick:
			rc.maintain()

		// An upload finished. Applying its result is pure bookkeeping: the slow part
		// already happened on the worker. It stops the collector only when the
		// worker found the staging volume contradicting the index.
		case res := <-rc.uploadResults():
			if err := rc.applyUploadResult(res); err != nil {
				return err
			}

		case <-rc.retryTimer():
			rc.processDue()

		case reply := <-rc.snapshotReq:
			reply <- rc.ix.entries()

		case reply := <-rc.statsReq:
			reply <- rc.currentStats()

		case reply := <-rc.reconcileReq:
			rc.maintain()
			// The caller is answered even when the pump fails, so a reconcileNow in
			// flight cannot be left waiting on a collector that is shutting down.
			err := rc.pump()
			reply <- struct{}{}
			if err != nil {
				return err
			}
		}
	}
}

// Stop asks the loop to exit and waits for it. It is safe to call more than once.
func (rc *rotatedCollector) Stop() {
	select {
	case <-rc.stopCh:
	default:
		close(rc.stopCh)
	}
	<-rc.doneCh
}

// snapshot returns the tracked captures. The answer is produced by the owner
// goroutine, so callers never read the index themselves.
func (rc *rotatedCollector) snapshot() []stagedEntry {
	reply := make(chan []stagedEntry, 1)
	select {
	case rc.snapshotReq <- reply:
		return <-reply
	case <-rc.doneCh:
		return nil
	}
}

// stats returns the collector's bookkeeping, computed by the owner goroutine.
func (rc *rotatedCollector) stats() collectorStats {
	reply := make(chan collectorStats, 1)
	select {
	case rc.statsReq <- reply:
		return <-reply
	case <-rc.doneCh:
		return collectorStats{}
	}
}

// currentStats runs on the owner goroutine.
func (rc *rotatedCollector) currentStats() collectorStats {
	s := collectorStats{
		StagedBytes:     rc.bytes.total,
		Captures:        rc.ix.len(),
		QueuedUploads:   len(rc.up.queue),
		InFlightUploads: rc.up.inFlight,
		IntakePauses:    rc.intakePauses,
		IntakeResumes:   rc.intakeResumes,
		IntakePaused:    rc.intakePaused(),
	}
	for _, c := range rc.ix.byInode {
		if c.Entry.State == stateUploaded {
			s.Uploaded++
		} else {
			s.Pending++
		}
	}
	for _, st := range rc.up.states {
		if st.phase == phaseAwaitingPromotion {
			s.AwaitingPromotion++
		}
	}
	return s
}

// reconcileNow runs one full sweep on the owner goroutine and waits for it.
func (rc *rotatedCollector) reconcileNow() {
	reply := make(chan struct{}, 1)
	select {
	case rc.reconcileReq <- reply:
		<-reply
	case <-rc.doneCh:
	}
}

func (rc *rotatedCollector) report(err error) {
	if err != nil {
		rc.cfg.OnIssue(err)
	}
}

// handleEvent turns one filesystem event into discovery work. It performs no
// storage calls and no waiting, so a rotation cannot delete a segment while the
// loop is busy elsewhere.
func (rc *rotatedCollector) handleEvent(event fsnotify.Event) {
	if !rc.underLogsDir(event.Name) {
		return
	}

	// Only creations matter. Rotation's rename surfaces as a Create on the
	// destination, and removals leave nothing to inspect.
	//
	// Writes are deliberately ignored: every append to every active log would enter
	// this loop, and that traffic can fill the kernel queue and delay the one event
	// that matters — the Create of a rotation backup. Active file names are learned
	// from the startup scan, from their own Create events, from the scan that
	// follows watching a new directory, and from the periodic sweep.
	if !event.Op.Has(fsnotify.Create) {
		return
	}

	fi, err := os.Lstat(event.Name)
	if err != nil {
		if !isVanished(err) {
			rc.report(fmt.Errorf("stat %s: %w", event.Name, err))
		}
		return
	}

	switch {
	case fi.IsDir():
		// A watch cannot see what a directory already contained, so scan it the
		// moment it is watched.
		rc.watchAndScan(event.Name)
	case fi.Mode().IsRegular():
		rc.inspectFile(event.Name, fi)
	}
}

// underLogsDir reports whether path is inside the active logs tree. Events for
// anything else are not this collector's business.
func (rc *rotatedCollector) underLogsDir(path string) bool {
	rel, err := filepath.Rel(rc.cfg.LogsDir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// scanTree walks the active logs tree, watching every directory and inspecting
// every file. It is the startup scan, the periodic backstop and the overflow
// recovery, and it is idempotent: captures are keyed by inode, so a file seen twice
// is captured once.
func (rc *rotatedCollector) scanTree() {
	rc.watchAndScan(rc.cfg.LogsDir)
}

// installWatchesRecursive watches dir and every directory beneath it and does
// nothing else. It runs before staging reconstruction so that the tree is covered
// while that work happens; capturing at this point would mint IDs for segments the
// previous run may already hold.
//
// The parent is watched before its children are enumerated, so a directory created
// during the walk is reported through its parent's watch even if the enumeration
// missed it.
//
// Incomplete coverage is fatal. Starting with part of the tree unwatched looks
// healthy but silently loses any segment that is created and deleted in the gap
// between sweeps, which is exactly the failure this collector exists to prevent.
// The one tolerated failure is a directory that disappeared after its parent was
// watched: that is an ordinary race, and if it comes back the parent's watch
// reports it.
func (rc *rotatedCollector) installWatchesRecursive(dir string) error {
	isRoot := dir == rc.cfg.LogsDir

	if rc.watcher != nil {
		if err := rc.watcher.Add(dir); err != nil {
			if isVanished(err) && !isRoot {
				return nil
			}
			return fmt.Errorf("watch %s: %w", dir, err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if isVanished(err) && !isRoot {
			return nil
		}
		return fmt.Errorf("read directory %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.Type()&fs.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		if err := rc.installWatchesRecursive(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// watchAndScan watches dir and everything beneath it, scanning each directory
// immediately after its watch is added so nothing that already existed is missed.
func (rc *rotatedCollector) watchAndScan(dir string) {
	if rc.watcher != nil {
		if err := rc.watcher.Add(dir); err != nil {
			if isVanished(err) {
				return
			}
			rc.report(fmt.Errorf("watch %s: %w", dir, err))
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if !isVanished(err) {
			rc.report(fmt.Errorf("read directory %s: %w", dir, err))
		}
		return
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		// Never follow symlinks: a symlinked directory could point anywhere, and a
		// symlinked file is not a log Ray rotated.
		if entry.Type()&fs.ModeSymlink != 0 {
			continue
		}
		if entry.IsDir() {
			rc.watchAndScan(path)
			continue
		}

		fi, err := entry.Info()
		if err != nil {
			if !isVanished(err) {
				rc.report(fmt.Errorf("stat %s: %w", path, err))
			}
			continue
		}
		rc.inspectFile(path, fi)
	}
}

// inspectFile records an active log file or captures a rotation backup.
func (rc *rotatedCollector) inspectFile(path string, fi fs.FileInfo) {
	if !fi.Mode().IsRegular() {
		return
	}
	dir, name := filepath.Split(path)
	dir = filepath.Clean(dir)

	if _, _, isBackup := parseBackupName(name); !isBackup {
		// Remembering the active file is what lets a backup still be recognized
		// during the moment a rotation cascade leaves the active name unlinked.
		rc.ix.observeBase(dir, name)
		return
	}

	switch status := evaluateCandidate(dir, name, fi.Mode(), baseKnownWith(rc.ix)); status {
	case candidateEligible:
		rc.report(rc.capture(path, name))
	case candidateNotBackupName, candidateNotRegular, candidateUnknownBase:
		logrus.Debugf("Rotated log collector: skipping %s: %v", path, status)
	}
}

// capture pins one rotation backup.
//
// The hard link — not the source path — decides which inode was captured. A
// rotation filename is reused constantly, so between validating the candidate and
// linking it, "raylet.out.1" can already name a different file. Reading the inode
// from the source beforehand would let the staging link pin one file while the index
// records another, which would break deduplication, restart recovery and release.
// So the inode is read back from the link we created, and that value alone is
// registered.
//
// For the same reason there is no dedup shortcut before linking: an early return
// because the source's current inode looks familiar could skip a generation that had
// just replaced it, and rotation would then delete that generation unseen.
// Intake is the only thing backpressure stops. Nothing already captured is evicted,
// and uploads, promotions and releases keep running, which is what eventually brings
// the total back down. The cost is honest and unavoidable: a backup Ray rotates away
// while intake is paused is lost, because there is nowhere to pin it. This is a
// fail-safe against exhausting the volume Ray itself is writing to, not a promise of
// lossless capture under unbounded pressure.
// While paused, one capture per maintenance sweep is still allowed through as a
// capacity probe. Without it a disk-full pause would latch permanently: capture would
// return before ever attempting a link, so if another process filled the volume and
// this collector holds nothing it can release, nothing would ever discover that space
// came back. Using a real eligible backup as the probe means a successful attempt
// preserves data instead of merely testing the filesystem.
func (rc *rotatedCollector) capture(path, name string) error {
	if rc.intakePaused() && !rc.gate.takeProbe() {
		logrus.Debugf("Rotated log collector: intake is paused, not capturing %s", path)
		return nil
	}

	fi, err := os.Lstat(path)
	if err != nil {
		if isVanished(err) {
			// Rotation deleted it first. Expected under fast rotation, not an error.
			logrus.Debugf("Rotated log collector: %s vanished before capture", path)
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return nil
	}
	// Diagnostics only. This value must never reach captureIndex.
	sourceInode, _, inodeErr := inodeFromFileInfo(fi)
	if inodeErr != nil {
		return fmt.Errorf("read inode of %s: %w", path, inodeErr)
	}

	relDir, err := relDirFor(rc.cfg.LogsDir, path)
	if err != nil {
		return err
	}
	id, err := rc.cfg.CaptureIDs.next()
	if err != nil {
		return err
	}
	entry, err := newStagedEntry(statePending, rc.cfg.SessionName, rc.cfg.NodeName, relDir, name, id)
	if err != nil {
		return err
	}
	staged := entry.path(rc.cfg.StagingRoot)

	if err := rc.cfg.Link(path, staged); err != nil {
		switch {
		case isVanished(err):
			logrus.Debugf("Rotated log collector: %s vanished before it could be linked", path)
			return nil
		case isUnsupportedLinkError(err):
			// A different filesystem or a Ray container running as another user.
			// Skip the segment and keep discovering; v1 has no copy fallback.
			return fmt.Errorf("cannot capture %s on this deployment: %w", path, err)
		case errors.Is(err, syscall.ENOSPC):
			// The volume is full regardless of what the watermarks think the
			// collector is holding: something else may have filled it. Stop taking
			// new data until some operation proves there is room again — and discard
			// any earlier success from this same sweep, because the filesystem has
			// just contradicted it.
			//
			// Only the transition is reported. Every later sweep spends its probe
			// here while the volume stays full, and repeating the same line once per
			// sweep would bury the state change that mattered.
			if !rc.gate.observedFull() {
				logrus.Debugf("Rotated log collector: staging volume is still full, %s was not captured", path)
				return nil
			}
			return fmt.Errorf("%w: %w", errIntakePaused, err)
		default:
			return err
		}
	}

	// A link that succeeded is proof the volume has room, whoever freed it. That is
	// what lifts a disk-full pause, and it is the only thing that can: elapsed time
	// says nothing about a volume another process filled. A later ENOSPC in this same
	// sweep discards this observation again.
	rc.gate.observedSpace()

	key, size, err := rc.pinnedInode(staged)
	if err != nil {
		return errors.Join(err, discardStagingLink(staged))
	}
	if key != sourceInode {
		logrus.Debugf("Rotated log collector: %s changed from %s to %s before it was pinned; the pinned file wins",
			path, sourceInode, key)
	}

	if existing, alreadyCaptured := rc.ix.lookup(key); alreadyCaptured {
		// Another name for a file we already hold, most often the same segment seen
		// again after rotation renamed it. Drop the surplus link and keep the
		// original capture: one inode is one capture, with one object key. The
		// surplus link was never accounted for, so removing it changes no total.
		logrus.Debugf("Rotated log collector: %s is already captured as %s", path, existing.Entry.CaptureID)
		return discardStagingLink(staged)
	}

	if err := registerStaged(rc.cfg.StagingRoot, rc.ix, key, entry); err != nil {
		return err
	}
	// Accounting and scheduling follow registration, never precede it: a capture the
	// index rejected has had its link removed and must leave no trace behind.
	rc.bytes.track(key, size)
	// The limit is enforced here, not on the next trip through the event loop. A
	// scan registers every backup it finds without returning to that loop, so
	// deferring this would let the rest of the scan through after the volume was
	// already over its limit. This capture stands; the next one in the same scan
	// sees a paused gate and is skipped.
	rc.enforceHighWater()
	rc.enqueueUpload(key)
	return nil
}

// pinnedInode reads the identity and size of the file the staging link actually
// pinned. The link must still be a regular file: if the source turned into a symlink
// or another non-regular object first, what was linked is not a log segment.
//
// The size is read from the same stat, so accounting describes exactly the file that
// was pinned rather than whatever the source path holds a moment later.
func (rc *rotatedCollector) pinnedInode(staged string) (inodeKey, int64, error) {
	fi, err := os.Lstat(staged)
	if err != nil {
		return inodeKey{}, 0, fmt.Errorf("stat staged capture %s: %w", staged, err)
	}
	if !fi.Mode().IsRegular() {
		return inodeKey{}, 0, fmt.Errorf("staged capture %s is not a regular file (%s)", staged, fi.Mode())
	}
	key, _, err := inodeFromFileInfo(fi)
	if err != nil {
		return inodeKey{}, 0, fmt.Errorf("read inode of staged capture %s: %w", staged, err)
	}
	return key, fi.Size(), nil
}

// discardStagingLink removes a link the collector created but will not track. An
// untracked link would pin blocks that nothing ever releases.
func discardStagingLink(staged string) error {
	if err := os.Remove(staged); err != nil && !isVanished(err) {
		return fmt.Errorf("remove surplus staging link %s: %w", staged, err)
	}
	return nil
}

// registerStaged records a freshly linked capture, and removes the link it just
// created if that fails. An untracked hard link would pin blocks nothing will ever
// release, so the staging volume must never keep one.
func registerStaged(stagingRoot string, ix *captureIndex, key inodeKey, e stagedEntry) error {
	_, added, err := ix.add(key, e)
	if err == nil && !added {
		err = fmt.Errorf("capture %s: inode %s was already registered", e.CaptureID, key)
	}
	if err != nil {
		if rmErr := os.Remove(e.path(stagingRoot)); rmErr != nil && !isVanished(rmErr) {
			return fmt.Errorf("%w (and its staging link could not be removed: %w)", err, rmErr)
		}
		return err
	}
	return nil
}

// stagedRecord is one staging file found during reconstruction, together with the
// inode it was holding at that moment.
type stagedRecord struct {
	entry stagedEntry
	path  string
	key   inodeKey
	size  int64
}

// reconstructStaging rebuilds the index from the staging volume so a restarted
// collector adopts what the previous run pinned instead of capturing it again.
// Capture identity comes from the filenames; no new IDs are minted here.
//
// It returns an error when it cannot establish one staged link per inode. Starting
// with a link nothing tracks would be worse than not starting: that link pins the
// inode forever, so releaseCapture would always see an extra link count and could
// never free the segment.
func (rc *rotatedCollector) reconstructStaging() error {
	root := rc.cfg.StagingRoot
	if _, err := os.Lstat(root); err != nil {
		if isVanished(err) {
			return nil
		}
		return fmt.Errorf("stat staging root %s: %w", root, err)
	}

	records, err := rc.collectStagedRecords(root)
	if err != nil {
		return err
	}

	// A total order, so nothing depends on the order the filesystem was walked.
	// Pending beats uploaded, keeping the guarantee that captured data is uploaded
	// at least once; then the lower capture ID; then the path, so two records that
	// are otherwise identical still have one deterministic winner.
	slices.SortFunc(records, func(a, b stagedRecord) int {
		if a.entry.State != b.entry.State {
			if a.entry.State == statePending {
				return -1
			}
			return 1
		}
		if c := strings.Compare(a.entry.CaptureID, b.entry.CaptureID); c != 0 {
			return c
		}
		return strings.Compare(a.path, b.path)
	})

	var failures []error
	winners := make(map[inodeKey]stagedRecord, len(records))
	for _, r := range records {
		winner, taken := winners[r.key]
		if !taken {
			if _, err := rc.ix.restore(r.key, r.entry); err != nil {
				failures = append(failures, fmt.Errorf("restore staged capture %s: %w", r.path, err))
				continue
			}
			// One inode, counted once, whether the previous run left it pending or
			// uploaded: both states pin the same blocks.
			rc.bytes.track(r.key, r.size)
			winners[r.key] = r
			continue
		}

		rc.report(fmt.Errorf("staging volume holds a surplus record for %s: keeping %s, removing %s",
			r.key, winner.path, r.path))
		if err := removeSurplusLink(r); err != nil {
			failures = append(failures, err)
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("staging volume is inconsistent: %w", errors.Join(failures...))
	}
	return nil
}

// collectStagedRecords reads every usable staging record under root. Unreadable
// subtrees and unusable files are reported and skipped; only failures that would
// leave the index inconsistent are returned.
func (rc *rotatedCollector) collectStagedRecords(root string) ([]stagedRecord, error) {
	var records []stagedRecord

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if isVanished(err) {
				return nil // drained or cleaned up concurrently; nothing to adopt
			}
			// A subtree we cannot read may hold real staged links. Adopting the
			// rest and starting anyway would leave those pinning their inodes with
			// no owner, so this has to stop startup.
			return fmt.Errorf("read staging volume at %s: %w", path, err)
		}
		if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		// A name that does not parse cannot be a link this collector made: every
		// capture is written as <session>/<node>/<state>/<dir>/<name>.rotated.<id>.
		// So it pins nothing the index needs to own, and ignoring it is safe.
		entry, err := parseStagedPath(root, path)
		if err != nil {
			rc.report(err)
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			if isVanished(err) {
				return nil
			}
			return fmt.Errorf("stat staged capture %s: %w", path, err)
		}
		// Only a regular file can be a captured segment: captures are made with
		// os.Link from a regular log file, so a FIFO, socket or device here was
		// never ours and pins nothing we must track. Indexing one would also let a
		// later uploader block forever trying to read it.
		if !fi.Mode().IsRegular() {
			rc.report(fmt.Errorf("staged capture %s is not a regular file (%s), ignoring it", path, fi.Mode()))
			return nil
		}
		key, _, err := inodeFromFileInfo(fi)
		if err != nil {
			return fmt.Errorf("read inode of staged capture %s: %w", path, err)
		}
		records = append(records, stagedRecord{key: key, entry: entry, path: path, size: fi.Size()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reconstruct staging volume %s: %w", root, err)
	}
	return records, nil
}

// removeSurplusLink drops a staging link that lost a conflict, but only after
// confirming it still holds the inode it was recorded with. If the path has since
// become something else, removing it could destroy an unrelated capture, so it is
// reported and left alone instead.
func removeSurplusLink(r stagedRecord) error {
	fi, err := os.Lstat(r.path)
	if err != nil {
		if isVanished(err) {
			return nil // already gone; nothing pins the inode through this name
		}
		return fmt.Errorf("stat surplus staging link %s: %w", r.path, err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("surplus staging link %s is no longer a regular file (%s)", r.path, fi.Mode())
	}
	current, _, err := inodeFromFileInfo(fi)
	if err != nil {
		return fmt.Errorf("read inode of surplus staging link %s: %w", r.path, err)
	}
	if current != r.key {
		return fmt.Errorf("surplus staging link %s now holds %s, not %s, so it was left in place",
			r.path, current, r.key)
	}
	if err := os.Remove(r.path); err != nil && !isVanished(err) {
		return fmt.Errorf("remove surplus staging link %s: %w", r.path, err)
	}
	return nil
}
