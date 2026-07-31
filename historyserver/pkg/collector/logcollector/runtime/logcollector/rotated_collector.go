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
// data but never deletes what it already holds; acting on this is the backpressure
// tranche's job.
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

	ReconcileInterval time.Duration
	NewWatcher        func() (fsWatcher, error)
	NewTicker         func(time.Duration) (<-chan time.Time, func())
	CaptureIDs        *captureIDGenerator

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
// without a single mutex. Nothing here talks to object storage: uploads are slow and
// must never sit between a rotation and its capture.
type rotatedCollector struct {
	cfg     rotatedCollectorConfig
	ix      *captureIndex
	watcher fsWatcher

	stopCh chan struct{}
	doneCh chan struct{}

	snapshotReq  chan chan []stagedEntry
	reconcileReq chan chan struct{}
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

	return &rotatedCollector{
		cfg:          cfg,
		ix:           newCaptureIndex(),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		snapshotReq:  make(chan chan []stagedEntry),
		reconcileReq: make(chan chan struct{}),
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

	rc.scanTree()

	tick, stopTicker := rc.cfg.NewTicker(rc.cfg.ReconcileInterval)
	defer stopTicker()

	for {
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
				rc.scanTree()
				continue
			}
			rc.report(fmt.Errorf("watcher error: %w", err))

		case <-tick:
			rc.scanTree()

		case reply := <-rc.snapshotReq:
			reply <- rc.ix.entries()

		case reply := <-rc.reconcileReq:
			rc.scanTree()
			reply <- struct{}{}
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
func (rc *rotatedCollector) capture(path, name string) error {
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
			return fmt.Errorf("%w: %w", errIntakePaused, err)
		default:
			return err
		}
	}

	key, err := rc.pinnedInode(staged)
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
		// original capture: one inode is one capture, with one object key.
		logrus.Debugf("Rotated log collector: %s is already captured as %s", path, existing.Entry.CaptureID)
		return discardStagingLink(staged)
	}

	return registerStaged(rc.cfg.StagingRoot, rc.ix, key, entry)
}

// pinnedInode reads the identity of the file the staging link actually pinned. The
// link must still be a regular file: if the source turned into a symlink or another
// non-regular object first, what was linked is not a log segment.
func (rc *rotatedCollector) pinnedInode(staged string) (inodeKey, error) {
	fi, err := os.Lstat(staged)
	if err != nil {
		return inodeKey{}, fmt.Errorf("stat staged capture %s: %w", staged, err)
	}
	if !fi.Mode().IsRegular() {
		return inodeKey{}, fmt.Errorf("staged capture %s is not a regular file (%s)", staged, fi.Mode())
	}
	key, _, err := inodeFromFileInfo(fi)
	if err != nil {
		return inodeKey{}, fmt.Errorf("read inode of staged capture %s: %w", staged, err)
	}
	return key, nil
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
	key   inodeKey
	entry stagedEntry
	path  string
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
		records = append(records, stagedRecord{key: key, entry: entry, path: path})
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
