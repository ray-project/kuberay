package logcollector

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

// A rotatedCollector is scoped to exactly one Ray session on one node, because both
// values are baked into every staging path and every object key it writes. Ray
// restarts a session by pointing session_latest at a new directory, and the node ID
// can be rediscovered at any time, so production needs something that owns the
// lifetime of one collector and replaces it when either half of that identity
// changes. That is rotatedSupervisor.
//
// Everything below runs on the RayLogHandler's goroutines — Run and
// PollActiveSessionChanges — never on a collector's owner goroutine, and it reaches a
// collector only through Run, Stop, reconcileNow and stats.

const (
	// defaultRotatedDrainBudget bounds how long a retiring collector is given to get
	// already-captured work into storage.
	//
	// It has to be a budget rather than a wait. storage.StorageWriter.WriteFile takes
	// no context, so an upload that has already entered the object client cannot be
	// canceled; an unreachable object store would otherwise hold shutdown open for as
	// long as its own timeouts last, and a capture whose uploads keep failing backs
	// off into the minutes.
	//
	// The size is bounded from above by the pod's termination grace period, which
	// KubeRay never sets for the collector sidecar and which therefore defaults to
	// Kubernetes' 30s. Everything this drain spends is taken from the legacy shutdown
	// walk that runs after it, and that walk is the pre-existing data path for every
	// log file still in the live tree — so the drain must stay a small fraction of the
	// window. Five seconds leaves roughly 25s for the legacy walk and the final
	// endpoint poll.
	//
	// Anything unfinished when the budget expires stays pinned on the staging volume
	// as pending. That is recoverable only where the staging volume outlives the
	// process — a collector restart inside a surviving pod. When the pod itself is
	// going away and /tmp/ray is an emptyDir, this drain is the last chance those
	// captures get, which is why it is not zero either.
	defaultRotatedDrainBudget = 5 * time.Second

	// defaultRotatedDrainPoll is how often the drain re-reads the collector's stats.
	// Each read is one round trip through the owner goroutine, so this is deliberately
	// coarse enough not to add load to a collector that is trying to finish.
	defaultRotatedDrainPoll = 20 * time.Millisecond

	// rotatedRetryBase and rotatedRetryMax schedule the retry of an identity whose
	// collector failed for a condition that can clear on its own. The session poller
	// calls ensure every five seconds, so without a schedule a session whose logs
	// directory does not exist yet would rebuild a collector twice a minute forever.
	rotatedRetryBase = 15 * time.Second
	rotatedRetryMax  = 5 * time.Minute

	// maxRotatedFailureRecords caps the failure map. Records for sessions other than
	// the one being ensured are dropped first — Ray session names are timestamps and
	// never come back — and this is the backstop for anything that pruning misses, so
	// a process that outlives thousands of session changes cannot grow this map.
	maxRotatedFailureRecords = 16

	// defaultStagingHighWaterBytes and defaultStagingLowWaterBytes bound the disk this
	// feature keeps allocated.
	//
	// They are measured against retained bytes, not logical staged bytes: a capture
	// counts only once the collector holds the last link to its inode. A segment Ray
	// still has in its own backup ring costs nothing extra — the hard link shares Ray's
	// blocks — so healthy rotation never approaches these marks. What does approach them
	// is an object store that has stopped accepting writes, where captures Ray has since
	// rolled off accumulate with nothing able to release them.
	//
	// Reaching the high mark pauses new capture only: nothing already captured is
	// evicted, uploads, promotions and releases keep running, and a segment larger than
	// the whole budget is still captured, because the limit is applied after the capture
	// that crossed it. The gap to the low mark is hysteresis, so a total sitting near the
	// limit does not flap.
	defaultStagingHighWaterBytes int64 = 1 << 30 // 1 GiB
	defaultStagingLowWaterBytes  int64 = 1 << 29 // 512 MiB
)

// rotatedKey is the identity a collector is built for. A change to either half means
// a different collector, never a mutated one: the old session's captureIndex, capture
// IDs, staging subtree and object prefix all belong to the old session.
type rotatedKey struct {
	session string
	node    string
}

func (k rotatedKey) String() string { return k.session + "/" + k.node }

// rotatedRun is one running collector together with the identity it was built for.
type rotatedRun struct {
	rc      *rotatedCollector
	done    chan struct{}
	logsDir string
	key     rotatedKey

	// err is the collector's exit status. It is written before done is closed and
	// read only after, so it needs no lock of its own.
	err error

	// ready records that this run completed startup. It is written and read under the
	// supervisor's mu.
	ready bool

	// noted makes the failure of this run recorded exactly once, no matter whether the
	// collector's own goroutine or the retirement that joined it gets there first.
	noted sync.Once
}

// finished reports whether the collector's goroutine has already exited, which is how
// a failed run is prevented from being treated as active.
func (r *rotatedRun) finished() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// errRotatedRetryable marks a failure whose condition can clear on its own.
//
// Retryability is a property of the operation that failed, never of the errno it
// carries, and only the layer that performed the operation knows which it was. A logs
// directory that does not exist yet and a durable staging link that has disappeared
// both surface as fs.ErrNotExist, and they are opposites: the first is the session
// poller running ahead of Ray's own directory creation, the second is the staging
// volume contradicting the index, which is fatal by design and which no retry can
// repair. Classifying on the errno would restart the second every fifteen seconds
// forever.
//
// So nothing is retryable unless it was explicitly marked at the point of failure by
// retryableRotated, and the default for everything else — including every error that
// merely happens to wrap ENOENT — is durable.
var errRotatedRetryable = errors.New("rotated log collection may be retried for this identity")

// retryableRotated marks err as a condition that can clear on its own.
func retryableRotated(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w [%w]", err, errRotatedRetryable)
}

// isRetryableRotatedFailure reports whether a failure was explicitly marked retryable.
func isRetryableRotatedFailure(err error) bool {
	return errors.Is(err, errRotatedRetryable)
}

// rotatedFailure is why an identity has no collector, and when it may be tried again.
//
// durable separates the two kinds of failure that matter. A staging volume that
// contradicts itself, a watcher the collector may not install, a filesystem that
// cannot hard-link: retrying those in fifteen seconds produces the identical failure
// and a log line per attempt for the life of the session. A logs directory that does
// not exist yet is the opposite — the session poller can observe a new session
// directory before Ray has created logs/ inside it — and refusing to try again would
// leave that entire session without rotated protection.
type rotatedFailure struct {
	err       error
	notBefore time.Time
	attempts  int
	seq       int64
	durable   bool
}

// rotatedSupervisor owns at most one rotatedCollector at a time and mediates every
// question the rest of the runtime asks about it.
//
// It never exposes the collector itself. Callers get idempotent lifecycle operations,
// so nothing outside can hold a pointer to a collector that has since been retired.
//
// Two locks, in one order only:
//
//   - lifecycle serializes the operations that start and stop collectors. It is what
//     guarantees that exactly one collector owns the staging root at a time, and it is
//     held across the slow parts of retirement — the final reconcile, the drain, Stop
//     and the goroutine join.
//   - mu guards the fields, and is never held across anything that waits: not a
//     collector round trip, not a construction, not a storage call, not the tune hook.
//
// A holder of lifecycle may take mu. Nothing ever takes lifecycle while holding mu, so
// the two cannot invert. The consequence that matters is that a collector goroutine
// reporting its own failure, and every observer of the current run, take only mu and so
// never wait behind a drain.
//
// Neither lock is ever held while calling into RayLogHandler, and the handler never
// calls in while holding its own lock, so there is no inversion with the node-name
// lock either.
type rotatedSupervisor struct {
	// tune adjusts a collector's configuration just before it is built. It is nil in
	// production and exists so tests can substitute the watcher, the clock and the
	// storage writer without a second construction path.
	tune func(*rotatedCollectorConfig)

	// beforeFailurePublish runs at the top of runFailed, before the failed run is
	// either recorded or detached. It is nil in production and exists so a test can
	// hold a collector's failure in exactly the window where a non-atomic publication
	// would let ensure restart the identity that just failed.
	beforeFailurePublish func()

	// probeCreate, probeClose and probeRemove replace the three fallible steps of the
	// staging writability probe. They are nil in production and exist so a test can
	// fail a create, a close or an unlink without a filesystem that behaves that way.
	probeCreate func(string) (*os.File, error)
	probeClose  func(*os.File) error
	probeRemove func(string) error

	now func() time.Time

	// failures records identities whose collector could not be started or did not
	// survive, together with why and when they may be retried. Keeping the error,
	// rather than only logging it, is what lets the reason be reported rather than
	// inferred.
	failures map[rotatedKey]*rotatedFailure

	current *rotatedRun

	writer      objectWriter
	stagingRoot string
	cluster     clusterIdentity

	drainBudget time.Duration
	drainPoll   time.Duration

	failureSeq int64

	// lifecycle serializes starting and stopping collectors. See the type comment.
	lifecycle sync.Mutex

	mu sync.Mutex
	// frozen stops new collectors being started once shutdown has begun, so a session
	// change observed by the poller cannot resurrect the subsystem behind shutdown's
	// back. It is set before shutdown takes lifecycle, so an ensure that is already
	// running loses the race to start a replacement rather than winning it and having
	// the replacement immediately retired.
	frozen bool
}

func newRotatedSupervisor(cluster clusterIdentity, writer objectWriter, stagingRoot string) *rotatedSupervisor {
	return &rotatedSupervisor{
		cluster:     cluster,
		writer:      writer,
		stagingRoot: stagingRoot,
		failures:    make(map[rotatedKey]*rotatedFailure),
		drainBudget: defaultRotatedDrainBudget,
		drainPoll:   defaultRotatedDrainPoll,
		now:         time.Now,
	}
}

// ensure points rotated-log protection at the session and node that are active now.
//
// It is idempotent: called with the identity that is already running, it does
// nothing, which is what lets the session poller call it on every tick. Called with a
// different identity it retires the old collector completely — final reconciliation,
// bounded drain, watcher and owner stopped, goroutine joined — before the new one is
// constructed, so no event from the old session can reach the new collector, no state
// is shared between them, and the staging root never has two owners.
//
// A nil supervisor is a working no-op: that is how the handler behaves when rotated
// collection was never started, and it keeps every legacy test path untouched.
func (s *rotatedSupervisor) ensure(session, node, logsDir string) {
	if s == nil {
		return
	}
	if session == "" || node == "" || logsDir == "" {
		// Identity is not established yet — most often a node ID that has not been
		// discovered. A later tick retries; starting with a placeholder would write
		// objects under a key no reader looks at.
		return
	}
	key := rotatedKey{session: session, node: node}

	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()

	if s.isFrozen() {
		return
	}

	if cur := s.activeRun(); cur != nil {
		if cur.key == key && cur.logsDir == logsDir && !cur.finished() {
			return
		}
		// Retiring the predecessor before anything else is what makes the handover
		// safe: it is detached, reconciled one last time over a logs tree that still
		// exists, drained, stopped and joined here, so by the time the replacement is
		// constructed the previous owner of the staging root is gone.
		s.stopRun(cur)
	}

	if !s.startable(key) {
		return
	}
	if err := s.preflight(key, logsDir); err != nil {
		s.noteFailure(key, err)
		return
	}

	cfg := rotatedCollectorConfig{
		LogsDir:     logsDir,
		StagingRoot: s.stagingRoot,
		SessionName: session,
		NodeName:    node,
		Cluster:     s.cluster,
		Writer:      s.writer,
		// Bound what capture may pin, so an object store that stops accepting writes
		// cannot turn every rotated segment into a permanent hold on the volume Ray is
		// still logging to. ENOSPC handling is separate and unaffected: it comes from
		// the filesystem refusing a link, which catches a volume another process filled.
		//
		// Everything else — reconcile interval, upload backoff, worker stop grace,
		// capture IDs — takes the package defaults.
		HighWaterBytes: defaultStagingHighWaterBytes,
		LowWaterBytes:  defaultStagingLowWaterBytes,
	}
	if s.tune != nil {
		s.tune(&cfg)
	}

	rc, err := newRotatedCollector(cfg)
	if err != nil {
		s.noteFailure(key, fmt.Errorf("rotated log collection cannot be configured for %s: %w", key, err))
		return
	}

	run := &rotatedRun{rc: rc, key: key, logsDir: logsDir, done: make(chan struct{})}
	// Run has not started, so nothing reads cfg yet and this is the last moment the
	// callback can be given the run it belongs to.
	rc.cfg.OnReady = func() { s.markReady(run) }

	if !s.attach(run) {
		// Shutdown froze the subsystem while the predecessor was being retired. The
		// collector has been constructed but never run: it holds no watcher, no
		// goroutine and no staging link, so dropping it here is complete.
		logrus.Debugf("Rotated log collection was not started for %s because shutdown had begun", key)
		return
	}
	go func() {
		err := rc.Run()
		run.err = err
		// done is closed before the supervisor is told, so a retirement that is
		// already waiting on this run is released before this goroutine competes for
		// the lock.
		close(run.done)
		if err != nil {
			s.runFailed(run, err)
		}
	}()
	logrus.Infof("Rotated log collection started for %s: logs=%s staging=%s", key, logsDir, s.stagingRoot)
}

// preflight rejects, before a collector is built, deployment conditions that the
// collector itself could otherwise only discover one segment at a time.
//
// What it establishes, and nothing more:
//
//   - the logs directory exists and is a directory;
//   - the staging root exists, or can be created, and the collector can create a file
//     in it;
//   - inode identity is available on this platform, without which there are neither
//     hard links nor a way to tell two captures apart;
//   - the staging root and the logs directory are on the same device, since a hard
//     link cannot cross one.
//
// What it does not establish is whether this collector may hard-link Ray's files. That
// depends on the ownership and mode of each source file — a custom image whose Ray user
// differs from the collector's UID gives EPERM on os.Link even though every check here
// passes — and probing it would mean linking a real Ray log during startup. Those
// deployments still degrade the way they did before: isUnsupportedLinkError reports the
// segment and capture continues.
//
// A logs directory that is not there yet is reported as the transient condition it is:
// the session poller can see a new session directory before Ray has created logs/
// inside it, and the collector's own startup treats an unwatchable root as fatal.
func (s *rotatedSupervisor) preflight(key rotatedKey, logsDir string) error {
	logsInfo, err := os.Stat(logsDir)
	if err != nil {
		// Only an absent path is the "not yet" case. A permission the collector does
		// not have, or an I/O error, is a property of the deployment and reads exactly
		// the same way on every retry, so marking those retryable would turn a
		// misconfiguration into a rebuild every few minutes for the life of the pod.
		if isVanished(err) {
			return retryableRotated(fmt.Errorf("rotated log collection is not started for %s yet: %w", key, err))
		}
		return fmt.Errorf("rotated log collection is disabled for %s: %w", key, err)
	}
	if !logsInfo.IsDir() {
		return fmt.Errorf("rotated log collection is disabled for %s: %s is not a directory", key, logsDir)
	}
	if err := os.MkdirAll(s.stagingRoot, stagingDirPerm); err != nil {
		err = fmt.Errorf("create staging root %s: %w", s.stagingRoot, err)
		// A full volume is the one staging-root failure that can clear on its own, and
		// this subsystem exists to survive one. Permissions and read-only mounts cannot.
		if errors.Is(err, syscall.ENOSPC) {
			return retryableRotated(fmt.Errorf("rotated log collection is not started for %s yet: %w", key, err))
		}
		return fmt.Errorf("rotated log collection is disabled for %s: %w", key, err)
	}
	stagingInfo, err := os.Stat(s.stagingRoot)
	if err != nil {
		return fmt.Errorf("rotated log collection is disabled for %s: %w", key, err)
	}
	if err := s.probeWritable(s.stagingRoot); err != nil {
		// MkdirAll succeeds on a staging root that already exists, whatever its mode
		// or owner, so this is the only thing that answers "can we actually stage
		// here?" — a read-only remount or a root left behind by another UID reaches
		// exactly this line.
		return fmt.Errorf("rotated log collection is disabled for %s: %w", key, err)
	}

	logsDev, _, logsErr := inodeFromFileInfo(logsInfo)
	stagingDev, _, stagingErr := inodeFromFileInfo(stagingInfo)
	if logsErr != nil || stagingErr != nil {
		// No inode identity means no hard links and no way to tell one captured
		// segment from another. Saying so once is the graceful outcome; the
		// alternative is a collector that fails every capture individually.
		return fmt.Errorf("rotated log collection is disabled for %s: %w",
			key, errors.Join(logsErr, stagingErr))
	}
	if logsDev.Dev != stagingDev.Dev {
		return fmt.Errorf("rotated log collection is disabled for %s: the staging root %s and the Ray logs directory %s are on different filesystems, so segments cannot be captured by hard link: %w",
			key, s.stagingRoot, logsDir, syscall.EXDEV)
	}
	return nil
}

// probeWritable proves the collector can create a file in dir *and remove it again*.
//
// Both halves are the point. This staging tree is not somewhere the collector only
// writes: promotion renames an entry from pending/ to uploaded/ and release unlinks it,
// so a directory that accepts new entries but will not give them up would let capture
// pin inodes that nothing could ever free. A probe that could not be cleaned up is
// therefore a failure, not a warning — and leaving it behind would also litter the
// staging root with files reconstruction has to skip on every start.
//
// Every path attempts both the close and the removal, and both results reach the
// caller, so a failure to clean up is never hidden by a failure to close.
//
// The probe name is prefixed so that a crash between create and remove leaves
// something an operator can recognize, and so that parseStagedPath ignores it if
// reconstruction ever sees it.
func (s *rotatedSupervisor) probeWritable(dir string) error {
	f, err := s.probeCreateFile(dir)
	if err != nil {
		err = fmt.Errorf("staging root %s cannot be written to: %w", dir, err)
		// A full volume is the one probe failure that is about the moment rather than
		// the deployment, and this subsystem is built to survive one: the intake gate
		// already pauses capture on ENOSPC and resumes when space returns. Marking it
		// here is what lets a start that lost the race with a full disk be retried at
		// all. Permissions and read-only mounts read the same way on every attempt and
		// stay durable, as do the close and removal failures below — those say the
		// directory cannot be used, not that it is momentarily full.
		if errors.Is(err, syscall.ENOSPC) {
			return retryableRotated(err)
		}
		return err
	}
	name := f.Name()

	closeErr := s.probeCloseFile(f)
	removeErr := s.probeRemoveFile(name)
	if removeErr != nil && isVanished(removeErr) {
		removeErr = nil // something else cleaned it up; the entry is gone either way
	}
	if closeErr == nil && removeErr == nil {
		return nil
	}
	return fmt.Errorf("staging root %s cannot be used: %w", dir, errors.Join(closeErr, removeErr))
}

// probeCreateFile, probeCloseFile and probeRemoveFile are the probe's fallible steps,
// isolated so a test can fail any of them without needing a filesystem that behaves
// that way.
func (s *rotatedSupervisor) probeCreateFile(dir string) (*os.File, error) {
	if s.probeCreate != nil {
		return s.probeCreate(dir)
	}
	return os.CreateTemp(dir, ".rotated-preflight-*")
}

func (s *rotatedSupervisor) probeCloseFile(f *os.File) error {
	if s.probeClose != nil {
		return s.probeClose(f)
	}
	return f.Close()
}

func (s *rotatedSupervisor) probeRemoveFile(name string) error {
	if s.probeRemove != nil {
		return s.probeRemove(name)
	}
	return os.Remove(name)
}

// runFailed reacts to a collector whose Run returned an error.
//
// The error is reported rather than swallowed, the identity is recorded so nothing
// restarts it in a loop, and the active pointer is cleared so a dead goroutine is
// never mistaken for live protection. Legacy collection is deliberately untouched:
// rotated capture stopping is a loss of the extra protection this subsystem adds, not
// a reason to take down the collector that is still uploading everything else.
//
// Recording and detaching are one state transition under one lock. Doing them in two
// steps leaves a window in which the failed run is already unreachable but its failure
// is not yet recorded, and an ensure landing in that window sees an identity with no
// collector and no reason not to build one — which is precisely the restart of a
// just-failed identity this is here to prevent.
func (s *rotatedSupervisor) runFailed(run *rotatedRun, err error) {
	if s.beforeFailurePublish != nil {
		s.beforeFailurePublish()
	}
	run.noted.Do(func() {
		s.publishFailure(run, err)
	})
}

// publishFailure records a run's failure and retires its pointer atomically.
func (s *rotatedSupervisor) publishFailure(run *rotatedRun, err error) {
	wrapped := fmt.Errorf("rotated log collection stopped for %s; legacy log collection continues: %w", run.key, err)

	s.mu.Lock()
	f := s.recordFailureLocked(run.key, wrapped)
	if s.current == run {
		s.current = nil
	}
	s.mu.Unlock()

	logFailure(f)
}

// noteFailure records why an identity has no collector and when it may be tried again.
// It is for failures with no run behind them — anything that went wrong before a
// collector existed.
func (s *rotatedSupervisor) noteFailure(key rotatedKey, err error) {
	s.mu.Lock()
	f := s.recordFailureLocked(key, err)
	s.mu.Unlock()

	logFailure(f)
}

// recordFailureLocked writes the failure record and returns it for logging, which the
// caller does after releasing the lock.
func (s *rotatedSupervisor) recordFailureLocked(key rotatedKey, err error) rotatedFailure {
	durable := !isRetryableRotatedFailure(err)

	s.failureSeq++
	attempts := 1
	if prev := s.failures[key]; prev != nil {
		attempts = prev.attempts + 1
	}
	f := &rotatedFailure{err: err, durable: durable, attempts: attempts, seq: s.failureSeq}
	if !durable {
		f.notBefore = s.now().Add(retryDelay(attempts))
	}
	s.failures[key] = f
	s.pruneFailuresLocked(key)
	return *f
}

func logFailure(f rotatedFailure) {
	switch {
	case f.durable:
		logrus.Errorf("%v", f.err)
	case f.attempts == 1:
		logrus.Warnf("%v (retrying in %s)", f.err, retryDelay(f.attempts))
	default:
		// A condition that has not cleared after several minutes is worth one line
		// per retry at most, and by then the retry interval is measured in minutes.
		logrus.Debugf("%v (attempt %d, retrying in %s)", f.err, f.attempts, retryDelay(f.attempts))
	}
}

// retryDelay backs a retryable identity off from one poll tick to one every few
// minutes, so a condition that never clears costs a rebuild an hour rather than one
// every five seconds.
func retryDelay(attempts int) time.Duration {
	d := rotatedRetryBase
	for i := 1; i < attempts; i++ {
		d *= 2
		if d >= rotatedRetryMax {
			return rotatedRetryMax
		}
	}
	return d
}

// pruneFailuresLocked keeps the failure map bounded.
//
// Records for other sessions go first: a Ray session name is a timestamp, so a
// session the runtime has moved past is never ensured again and its record can only
// accumulate. The size cap is the backstop for identities within one session.
func (s *rotatedSupervisor) pruneFailuresLocked(keep rotatedKey) {
	for k := range s.failures {
		if k.session != keep.session {
			delete(s.failures, k)
		}
	}
	for len(s.failures) > maxRotatedFailureRecords {
		var oldest rotatedKey
		var oldestSeq int64
		first := true
		for k, f := range s.failures {
			if k == keep {
				continue
			}
			if first || f.seq < oldestSeq {
				oldest, oldestSeq, first = k, f.seq, false
			}
		}
		if first {
			return
		}
		delete(s.failures, oldest)
	}
}

// startable reports whether a collector may be built for this identity now.
func (s *rotatedSupervisor) startable(key rotatedKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, known := s.failures[key]
	if !known {
		return true
	}
	if f.durable {
		logrus.Debugf("Rotated log collection stays disabled for %s after an earlier failure: %v", key, f.err)
		return false
	}
	if s.now().Before(f.notBefore) {
		return false
	}
	return true
}

// retireUnless stops the current collector unless it belongs to session.
//
// It exists for the one changeover that cannot start a replacement: the session has
// changed but the new session's node ID is not known yet, so there is no identity to
// build a collector for. The collector of the session that is going away still has to
// be retired — this is its last chance to reconcile a tree that is about to be
// relocated — and the new session goes unprotected until a later tick can name its
// node.
//
// The exemption is what makes it safe to call on every relocation retry. Once a
// collector for the current session exists, a later tick that merely cannot reach the
// dashboard must leave it alone: rediscovery failing says nothing about the collector,
// and retiring it would cost a drain, a watcher and a staging reconstruction for
// nothing. Passing an empty session retires whatever is running, which is the honest
// answer when the caller cannot name the current session at all.
func (s *rotatedSupervisor) retireUnless(session string) {
	if s == nil {
		return
	}
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()

	run := s.activeRun()
	if run == nil || (session != "" && run.key.session == session) {
		return
	}
	s.stopRun(run)
}

// shutdown freezes the subsystem and retires the active collector.
//
// After it returns no new collector can be started and no collector goroutine is
// running. Freezing happens before the lifecycle lock is taken, so an ensure racing
// with shutdown cannot start a replacement that shutdown would then have to stop.
// Calling it again is a no-op, so a repeated shutdown neither blocks nor undoes
// anything.
func (s *rotatedSupervisor) shutdown() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.frozen = true
	s.mu.Unlock()

	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()

	if run := s.activeRun(); run != nil {
		s.stopRun(run)
	}
}

// stopRun performs the bounded shutdown of one collector, in the only order that is
// safe. It must be called with lifecycle held and mu not held.
//
//  1. detach it, so nothing can hand it more work or observe it as active;
//  2. one final reconciliation, while the logs tree still exists — this is the last
//     intake pass by design, because stopping capture first would abandon every
//     segment Ray rotates away during shutdown;
//  3. a bounded drain, giving already-captured work its chance to reach storage;
//  4. Stop, which ends intake for good and closes the watcher, waiting only the
//     worker's own grace for an upload the storage interface cannot cancel;
//  5. join the goroutine, so no goroutine can touch collector state afterwards, and
//     record its exit status if it failed.
//
// Anything the drain did not finish stays pending on the staging volume. Nothing here
// deletes a staging link.
func (s *rotatedSupervisor) stopRun(run *rotatedRun) {
	s.detach(run)

	var last collectorStats
	if !run.finished() {
		run.rc.reconcileNow()
		last = s.drain(run)
	}

	run.rc.Stop()
	<-run.done

	if run.err != nil {
		run.noted.Do(func() {
			s.noteFailure(run.key, fmt.Errorf("rotated log collection stopped for %s; legacy log collection continues: %w", run.key, run.err))
		})
	}
	logrus.Infof("Rotated log collection stopped for %s (%d capture(s) still pending on the staging volume)", run.key, last.Pending)
}

// drain waits for the collector to have nothing outstanding, but never longer than
// the budget. It returns the last stats it read.
//
// stats is answered by the owner goroutine, which is never inside a storage call, so
// this loop keeps making progress even while an uncancelable WriteFile is running —
// and a collector that has already exited answers with zeroes, so the loop ends at
// once rather than waiting for a goroutine that is gone.
func (s *rotatedSupervisor) drain(run *rotatedRun) collectorStats {
	if s.drainBudget <= 0 {
		return run.rc.stats()
	}
	deadline := time.Now().Add(s.drainBudget)
	for {
		st := run.rc.stats()
		if st.Pending == 0 && st.QueuedUploads == 0 && st.InFlightUploads == 0 && st.AwaitingPromotion == 0 {
			return st
		}
		if !time.Now().Before(deadline) {
			logrus.Warnf("Rotated log collection for %s did not finish within %s: %d capture(s) stay pending on the staging volume and the next run will retry them (queued=%d in flight=%d awaiting promotion=%d)",
				run.key, s.drainBudget, st.Pending, st.QueuedUploads, st.InFlightUploads, st.AwaitingPromotion)
			return st
		}
		time.Sleep(s.drainPoll)
	}
}

// attach publishes a run as the active one, unless shutdown has frozen the subsystem
// in the meantime. It reports whether the run may be started.
//
// It deliberately does not touch the identity's failure record. Attaching says only
// that a collector was constructed; every startup step that can fail is still ahead of
// it, and most of the retryable failures this subsystem has — a logs tree that
// disappears mid-startup, an exhausted watch limit — happen there. Clearing the record
// here would reset the attempt count before each of those, so a condition that never
// clears would retry at the base delay forever instead of backing off. markReady is
// what clears it.
func (s *rotatedSupervisor) attach(run *rotatedRun) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.frozen {
		return false
	}
	s.current = run
	return true
}

// markReady records that a run finished startup and is protecting its tree.
//
// It is the only thing that clears an identity's failure history, because it is the
// only evidence that the condition behind that history has actually cleared. A signal
// from a run that has since been retired or replaced clears nothing: the identity it
// would clear may belong to a different collector by now, and a stale clear would hand
// the next failure a fresh backoff.
func (s *rotatedSupervisor) markReady(run *rotatedRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != run {
		return
	}
	run.ready = true
	delete(s.failures, run.key)
}

// detach clears the active pointer if it still refers to this run.
func (s *rotatedSupervisor) detach(run *rotatedRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == run {
		s.current = nil
	}
}

func (s *rotatedSupervisor) activeRun() *rotatedRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *rotatedSupervisor) isFrozen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frozen
}

// runStatus is what the supervisor holds for one exact identity.
//
// "Exact" is the whole point: session, node and logs directory together. A collector
// for a different identity tells the caller nothing about the one it asked about, and
// answering "yes, something is running" is what let a failed handover look like a
// completed one.
type runStatus int

const (
	// runAbsent: nothing is attached for this identity. Either it was never started,
	// or it failed and was detached.
	runAbsent runStatus = iota
	// runStarting: attached, but its startup has not completed. It may still fail.
	runStarting
	// runReady: attached and past every startup step, so it is protecting its tree.
	runReady
	// runFinished: still attached, but its goroutine has exited. A failure that has
	// not been published yet looks like this.
	runFinished
)

func (s runStatus) String() string {
	switch s {
	case runAbsent:
		return "absent"
	case runStarting:
		return "starting"
	case runReady:
		return "ready"
	case runFinished:
		return "finished"
	default:
		return fmt.Sprintf("unknown status %d", int(s))
	}
}

// statusFor reports what the supervisor holds for one exact identity.
//
// It is the only honest basis for "has the handover happened?". Recording that from
// the fact that ensure was called records an attempt, not an outcome: ensure returns
// without a collector when the identity is backing off, when preflight rejects the
// deployment, when construction fails and when shutdown has frozen the subsystem — and
// a collector that did attach can still fail on any startup step afterwards.
func (s *rotatedSupervisor) statusFor(session, node, logsDir string) runStatus {
	if s == nil {
		return runAbsent
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	run := s.current
	if run == nil || run.key != (rotatedKey{session: session, node: node}) || run.logsDir != logsDir {
		return runAbsent
	}
	if run.finished() {
		return runFinished
	}
	if run.ready {
		return runReady
	}
	return runStarting
}

// activeKey reports the identity of the running collector, if there is one. It exists
// for diagnostics and tests; production lifecycle decisions go through ensure.
func (s *rotatedSupervisor) activeKey() (rotatedKey, bool) {
	if s == nil {
		return rotatedKey{}, false
	}
	run := s.activeRun()
	if run == nil {
		return rotatedKey{}, false
	}
	return run.key, true
}
