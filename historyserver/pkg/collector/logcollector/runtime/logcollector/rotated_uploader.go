package logcollector

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// objectWriter is the slice of storage.StorageWriter the uploader needs. Keeping it
// narrow means the collector can be tested against a fake that blocks, fails or
// counts calls without constructing a cloud client, and it documents that a capture
// upload is exactly one object write and nothing else.
type objectWriter interface {
	WriteFile(file string, reader io.ReadSeeker) error
}

// defaultUploadBackoff is the delay before each successive retry, capped at the last
// entry, which repeats for as long as the failure lasts. No jitter: a node runs one
// collector uploading one capture at a time, so there is no herd to spread out, and
// a fixed sequence is exactly assertable in tests.
var defaultUploadBackoff = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	15 * time.Second,
	30 * time.Second,
	1 * time.Minute,
	2 * time.Minute,
	5 * time.Minute,
}

// defaultWorkerStopGrace bounds how long Stop waits for the upload worker. An idle
// worker exits at once; one inside a storage write cannot be interrupted, because
// storage.StorageWriter.WriteFile takes no context, so the wait has to be bounded.
const defaultWorkerStopGrace = 100 * time.Millisecond

var (
	// errUploadInFlightAtStop reports the one case Stop cannot resolve: an
	// uncancelable storage write outliving the collector. The worker cannot touch
	// collector state afterwards, so the only consequence is that the write's result
	// is discarded and the capture stays pending for the next run to retry.
	errUploadInFlightAtStop = errors.New("rotated log upload was still running at stop: storage.StorageWriter.WriteFile cannot be canceled, so its result is discarded and the capture stays pending")

	// errUploadStale marks a result that no longer describes anything the collector
	// is tracking, which is how a result that outlived its capture is discarded
	// instead of applied to whatever now occupies the same inode.
	errUploadStale = errors.New("upload result no longer matches the capture it was submitted for")

	// errStagingInconsistent marks the staging volume contradicting the index. It is
	// fatal by design: retrying cannot make a missing or replaced staged file into
	// the capture the index says it is, so the collector stops with the staging tree
	// untouched rather than scheduling work that can never succeed.
	errStagingInconsistent = errors.New("staging volume contradicts the capture index")
)

// uploadIdentity is everything that has to still be true for an upload result to be
// applied. It is compared as a whole, so a capture that was replaced, promoted or
// re-staged between submission and completion cannot be mutated by the old result.
//
// Every field is comparable, which is what makes that check one ==.
type uploadIdentity struct {
	inode     inodeKey
	entry     stagedEntry
	localPath string
	objectKey string
}

// uploadJob is one immutable unit of work handed to the worker. attempt is carried
// for diagnostics only and is deliberately outside uploadIdentity: a retry of the
// same capture is the same object write to the same key.
type uploadJob struct {
	uploadIdentity
	attempt int
}

// uploadResult is what the worker returns. It repeats the submitted job so the owner
// can decide, without consulting any worker-owned state, whether the result still
// applies.
//
// local separates "the staged file is not what we were asked to upload" from "the
// object store rejected the write". Only the second is a transport problem; the first
// means the staging volume changed under us and the retry will fail the same way
// until reconciliation re-establishes the truth.
type uploadResult struct {
	err   error
	job   uploadJob
	local bool
}

// uploadWorker performs the blocking object write and nothing else.
//
// It never touches captureIndex, capture state, retry state or byte accounting: it
// opens a file, validates that the descriptor is the capture it was given, writes it,
// and returns an immutable result. Every decision that follows belongs to the owner
// goroutine, which is why object-store latency cannot delay event handling,
// reconciliation, snapshots or stop.
type uploadWorker struct {
	writer  objectWriter
	jobs    <-chan uploadJob
	results chan<- uploadResult
	quit    <-chan struct{}
	// beforeWrite runs after local validation and before the shutdown check that
	// guards the remote write. It is nil in production and exists so a test can hold
	// the worker in exactly that window.
	beforeWrite func()
	stagingRoot string
}

// stopping reports whether shutdown has begun. A nil quit channel never fires, so a
// worker constructed without one — as tests that call execute directly do — simply
// never sees a stop.
func (w *uploadWorker) stopping() bool {
	select {
	case <-w.quit:
		return true
	default:
		return false
	}
}

// run processes jobs until quit is closed. Both the receive and the send are guarded
// by quit, so the worker can never block forever on an owner that has stopped, and it
// never sends on a closed channel because quit is closed by the owner and only ever
// received from here.
func (w *uploadWorker) run(done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-w.quit:
			return
		case job := <-w.jobs:
			// A job can already be buffered when quit closes, and select chooses
			// at random between two ready cases. So the guard has to be re-checked
			// here: an upload that has not started must never start after Stop
			// began. Only a WriteFile already under way outlives the owner, and
			// that is the one case the storage interface gives no way to cancel.
			if w.stopping() {
				return
			}

			res, ok := w.execute(job)
			if !ok {
				// Shutdown overtook the job during local validation, so there is no
				// result to report: nothing was sent to the object store.
				return
			}
			select {
			case w.results <- res:
			case <-w.quit:
				return
			}
		}
	}
}

// execute validates the staged file and writes it to storage.
//
// Validation is done on the open descriptor, not on the path: fstat after open
// answers "what did I actually open?", so a staging path that was replaced between
// the check and the read cannot be uploaded under another capture's key. A path-based
// stat would leave exactly that window open.
//
// The descriptor is handed to the writer directly. The staged file is a hard link to
// an inode Ray has already rotated away, so its bytes cannot change under the read,
// and streaming it avoids holding a whole segment in memory.
//
// ok is false when shutdown began before the remote write started. Nothing was sent,
// so there is no result for the owner to apply.
func (w *uploadWorker) execute(job uploadJob) (res uploadResult, ok bool) {
	if want := job.entry.path(w.stagingRoot); want != job.localPath {
		return uploadResult{job: job, local: true, err: fmt.Errorf(
			"upload job for capture %s names %s but its entry stages at %s", job.entry.CaptureID, job.localPath, want)}, true
	}

	f, err := os.Open(job.localPath)
	if err != nil {
		return uploadResult{job: job, local: true, err: fmt.Errorf("open staged capture %s: %w", job.localPath, err)}, true
	}
	defer func() {
		if err := f.Close(); err != nil {
			logrus.Debugf("Rotated log uploader: close %s: %v", job.localPath, err)
		}
	}()

	fi, err := f.Stat()
	if err != nil {
		return uploadResult{job: job, local: true, err: fmt.Errorf("stat staged capture %s: %w", job.localPath, err)}, true
	}
	if !fi.Mode().IsRegular() {
		return uploadResult{job: job, local: true, err: fmt.Errorf(
			"staged capture %s is not a regular file (%s)", job.localPath, fi.Mode())}, true
	}
	opened, _, err := inodeFromFileInfo(fi)
	if err != nil {
		return uploadResult{job: job, local: true, err: fmt.Errorf("read inode of staged capture %s: %w", job.localPath, err)}, true
	}
	if opened != job.inode {
		return uploadResult{job: job, local: true, err: fmt.Errorf(
			"staged capture %s holds %s, not the captured %s", job.localPath, opened, job.inode)}, true
	}

	if w.beforeWrite != nil {
		w.beforeWrite()
	}
	// Local validation can take a while on a loaded node, and Stop may have begun
	// during it. A remote write that has not started must not start now.
	if w.stopping() {
		return uploadResult{}, false
	}

	if err := w.writer.WriteFile(job.objectKey, f); err != nil {
		return uploadResult{job: job, err: fmt.Errorf("write object %s: %w", job.objectKey, err)}, true
	}
	return uploadResult{job: job}, true
}

// uploadPhase is where one capture currently sits in the upload pipeline. A capture
// with no phase at all is one the owner has nothing outstanding for.
type uploadPhase int

const (
	// phaseQueued: waiting for the worker to be free.
	phaseQueued uploadPhase = iota
	// phaseInFlight: handed to the worker; no second job may be created for it.
	phaseInFlight
	// phaseBackoff: the upload failed and a retry is due at dueAt.
	phaseBackoff
	// phaseAwaitingPromotion: the object write succeeded but the local pending ->
	// uploaded promotion did not. Re-uploading would be pointless, so only the
	// promotion is retried.
	phaseAwaitingPromotion
)

func (p uploadPhase) String() string {
	switch p {
	case phaseQueued:
		return "queued"
	case phaseInFlight:
		return "in flight"
	case phaseBackoff:
		return "backing off"
	case phaseAwaitingPromotion:
		return "awaiting promotion"
	default:
		return fmt.Sprintf("unknown phase %d", int(p))
	}
}

// uploadState is the owner's record of one capture's progress. Only the owner
// goroutine reads or writes it, so it needs no synchronization.
type uploadState struct {
	dueAt    time.Time
	job      uploadJob
	attempts int
	phase    uploadPhase
}

// queuedUpload keeps the capture ID beside the key so the queue stays ordered without
// consulting the index.
type queuedUpload struct {
	captureID string
	key       inodeKey
}

// uploadScheduler is the owner's side of the pipeline: the queue, the per-capture
// state, the retry timer and the channels to the single worker. Like captureIndex it
// is lock-free because one goroutine owns it.
type uploadScheduler struct {
	writer     objectWriter
	states     map[inodeKey]*uploadState
	jobs       chan uploadJob
	results    chan uploadResult
	quit       chan struct{}
	workerDone chan struct{}
	retryC     <-chan time.Time
	stopTimer  func()
	backoff    []time.Duration
	queue      []queuedUpload
	armedFor   time.Time
	inFlight   int
}

func newUploadScheduler(writer objectWriter, backoff []time.Duration) *uploadScheduler {
	return &uploadScheduler{
		writer:  writer,
		states:  make(map[inodeKey]*uploadState),
		backoff: backoff,
	}
}

// enabled reports whether uploads happen at all. Production always supplies a writer;
// without one the collector still captures, tracks bytes and reconstructs staging, but
// has nowhere to send the bytes.
func (u *uploadScheduler) enabled() bool { return u.writer != nil }

// start launches the worker. It is called once, from the owner goroutine, after
// startup reconstruction.
func (u *uploadScheduler) start(stagingRoot string) {
	if !u.enabled() {
		return
	}
	// One job at a time. The owner only sends when nothing is in flight, so this
	// buffer is never full when it sends, which is what keeps submission from
	// blocking the owner loop.
	u.jobs = make(chan uploadJob, 1)
	u.results = make(chan uploadResult, 1)
	u.quit = make(chan struct{})
	u.workerDone = make(chan struct{})

	w := &uploadWorker{
		writer:      u.writer,
		jobs:        u.jobs,
		results:     u.results,
		quit:        u.quit,
		stagingRoot: stagingRoot,
	}
	go w.run(u.workerDone)
}

// stop shuts the worker down and waits for it, but only for grace.
//
// An idle worker is parked on a select and leaves immediately. One inside
// WriteFile cannot be interrupted — the storage interface takes no context — so
// waiting for it would let object-store latency delay Stop, which is exactly what
// this design forbids. After quit is closed the worker can no longer deliver a
// result, so outliving the collector costs correctness nothing.
func (u *uploadScheduler) stop(grace time.Duration, report func(error)) {
	if u.quit == nil {
		return
	}
	close(u.quit)
	u.disarm()

	t := time.NewTimer(grace)
	defer t.Stop()
	select {
	case <-u.workerDone:
	case <-t.C:
		report(errUploadInFlightAtStop)
	}
}

func (u *uploadScheduler) disarm() {
	if u.stopTimer != nil {
		u.stopTimer()
		u.stopTimer = nil
	}
	u.retryC = nil
	u.armedFor = time.Time{}
}

// queued reports whether key is already in the queue. The state map is the real
// guard against duplicates; this exists so the queue can be kept consistent with it.
func (u *uploadScheduler) queuedAt(key inodeKey) int {
	return slices.IndexFunc(u.queue, func(q queuedUpload) bool { return q.key == key })
}

// enqueue inserts a capture in capture-ID order, so the oldest capture is uploaded
// first and the order does not depend on Go's map iteration.
func (u *uploadScheduler) enqueue(key inodeKey, captureID string) {
	if u.queuedAt(key) >= 0 {
		return
	}
	i := sort.Search(len(u.queue), func(i int) bool { return u.queue[i].captureID > captureID })
	u.queue = slices.Insert(u.queue, i, queuedUpload{key: key, captureID: captureID})
}

func (u *uploadScheduler) dequeue() (queuedUpload, bool) {
	if len(u.queue) == 0 {
		return queuedUpload{}, false
	}
	head := u.queue[0]
	u.queue = u.queue[1:]
	return head, true
}

// forget drops every trace of a capture from the pipeline. It is only safe once the
// capture can no longer produce a result the owner would act on.
func (u *uploadScheduler) forget(key inodeKey) {
	delete(u.states, key)
	if i := u.queuedAt(key); i >= 0 {
		u.queue = slices.Delete(u.queue, i, i+1)
	}
}

// delay returns the wait before attempt n (1-based), capped at the final entry.
func (u *uploadScheduler) delay(attempt int) time.Duration {
	if len(u.backoff) == 0 {
		return 0
	}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(u.backoff) {
		attempt = len(u.backoff)
	}
	return u.backoff[attempt-1]
}

// stagedBytes is the owner's accounting of the staging volume, kept as two separate
// totals because they answer two different questions.
//
//   - total is how many logical bytes the collector has pinned. One inode is counted
//     once, at the size of its staged regular file. It is a diagnostic.
//   - retained is how many of those bytes exist *because of* the collector: the ones
//     the filesystem would have reclaimed already if this feature were not running.
//     It is what backpressure is measured against.
//
// They differ because a capture is a hard link, not a copy. While Ray still holds its
// own link to a rotated segment the blocks belong to Ray's backup ring, and releasing
// the capture would free nothing, so the capture contributes zero to retained. Only
// once Ray rolls the segment off its ring — leaving the staging link as the last
// reference, nlink == 1 — is the collector keeping those blocks alive, and only then
// does the capture count. Gating on total instead would charge the collector for Ray's
// entire backup ring and pause capture during perfectly healthy rotation.
type stagedBytes struct {
	sizes map[inodeKey]int64
	// retainedSizes holds an entry only for captures the collector is solely
	// responsible for. Presence is the record that nlink was last seen at 1.
	retainedSizes map[inodeKey]int64
	total         int64
	retained      int64
	// stale records that an incremental update did not add up. Rather than let the
	// totals drift, the next maintenance sweep recomputes them from the index, which
	// is the authoritative list of what the collector is holding.
	stale bool
}

func newStagedBytes() *stagedBytes {
	return &stagedBytes{
		sizes:         make(map[inodeKey]int64),
		retainedSizes: make(map[inodeKey]int64),
	}
}

// observe records a capture's size and whether the collector is now its only owner.
// nlink comes from the same stat as size, so the two describe one moment.
func (b *stagedBytes) observe(key inodeKey, size int64, nlink uint64) {
	if prev, ok := b.sizes[key]; ok {
		b.total += size - prev
	} else {
		b.total += size
	}
	b.sizes[key] = size

	prev, wasRetained := b.retainedSizes[key]
	if nlink == 1 {
		if wasRetained {
			b.retained += size - prev
		} else {
			b.retained += size
		}
		b.retainedSizes[key] = size
		return
	}
	// Someone else still holds a link, so these blocks are not this feature's doing.
	if wasRetained {
		b.retained -= prev
		delete(b.retainedSizes, key)
	}
}

func (b *stagedBytes) forget(key inodeKey) {
	if size, ok := b.retainedSizes[key]; ok {
		b.retained -= size
		delete(b.retainedSizes, key)
	}
	size, ok := b.sizes[key]
	if !ok {
		// The caller released something accounting never saw, so the totals are no
		// longer trustworthy.
		b.stale = true
		return
	}
	b.total -= size
	delete(b.sizes, key)
}

func (b *stagedBytes) markStale() { b.stale = true }

// recompute rebuilds both totals from the captures the index actually holds.
//
// It is also how a capture's ownership is re-read. Ray rolling a segment off its
// backup ring drops nlink from 2 to 1 without touching the staging path, so no
// filesystem event on the staging tree announces it; this sweep is what notices.
//
// Every entry must be proven: a regular file at the entry's own staging path holding
// exactly the inode the index pinned, with size and link count taken from that same
// stat. An entry that cannot be proven keeps its last known size and is counted as
// retained, rather than dropping to zero, and leaves accounting stale. Counting an
// unreadable capture as zero would make retained bytes fall, and a falling total is
// what releases backpressure; the collector would conclude the pressure was over
// precisely because it had lost track of what it was holding.
//
// Only a sweep in which every indexed capture verified may clear stale.
func (b *stagedBytes) recompute(stagingRoot string, ix *captureIndex, report func(error)) {
	sizes := make(map[inodeKey]int64, ix.len())
	retainedSizes := make(map[inodeKey]int64, len(b.retainedSizes))
	var total, retained int64
	verified := true

	for key, c := range ix.byInode {
		size, nlink, err := verifiedStagedSize(c, stagingRoot)
		if err != nil {
			verified = false
			report(fmt.Errorf("recompute staged bytes: %w", err))
			// Retain what was last known about this capture, and charge it as
			// retained: an entry the collector cannot inspect is one it must assume
			// it is holding. If nothing was ever known it contributes nothing — but
			// accounting stays stale, so that gap can never be mistaken for relieved
			// pressure.
			if prev, known := b.sizes[key]; known {
				sizes[key] = prev
				total += prev
				retainedSizes[key] = prev
				retained += prev
			}
			continue
		}
		sizes[key] = size
		total += size
		if nlink == 1 {
			retainedSizes[key] = size
			retained += size
		}
	}

	b.sizes = sizes
	b.retainedSizes = retainedSizes
	b.total = total
	b.retained = retained
	b.stale = !verified
}

// trusted reports whether the totals are currently believed to describe the volume.
func (b *stagedBytes) trusted() bool { return !b.stale }

// verifiedStagedSize returns the size and link count of a capture's staged file, but
// only once that path has been proven to still be a regular file holding the pinned
// inode. Size, link count and identity come from one stat, so the numbers returned
// describe the file that was checked rather than whatever the path names a moment
// later.
func verifiedStagedSize(c *capture, stagingRoot string) (int64, uint64, error) {
	p := c.Entry.path(stagingRoot)
	fi, err := os.Lstat(p)
	if err != nil {
		return 0, 0, fmt.Errorf("stat capture %s at %s: %w", c.Entry.CaptureID, p, err)
	}
	if !fi.Mode().IsRegular() {
		return 0, 0, fmt.Errorf("capture %s at %s is not a regular file (%s)", c.Entry.CaptureID, p, fi.Mode())
	}
	staged, nlink, err := inodeFromFileInfo(fi)
	if err != nil {
		return 0, 0, fmt.Errorf("read inode of capture %s at %s: %w", c.Entry.CaptureID, p, err)
	}
	if staged != c.Inode {
		return 0, 0, fmt.Errorf("capture %s at %s now holds %s, not the pinned %s", c.Entry.CaptureID, p, staged, c.Inode)
	}
	return fi.Size(), nlink, nil
}

// intakeGate decides whether new captures may be created.
//
// It is a fail-safe against filling the shared volume Ray is still writing its own
// logs to, not a promise of lossless capture: while intake is paused, a backup that
// Ray rotates away is gone. Nothing already captured is ever evicted to make room —
// deleting pending data to accept newer pending data would trade one loss for
// another and lose the older segment for certain.
type intakeGate struct {
	high int64
	low  int64
	// watermark is set once staged bytes reach high and cleared once they fall to
	// low. The gap is what stops a volume hovering at the threshold from flapping.
	watermark bool
	// diskFull is set when the filesystem itself refused a link. Watermarks cannot
	// predict this: another writer can fill the volume regardless of what the
	// collector is holding.
	diskFull bool
	// spaceObserved records that a filesystem operation proved the volume has room
	// again — a release that freed blocks, or a probe link that succeeded. It is what
	// clears diskFull, and it is a request rather than the clearing itself so that
	// evaluate stays the only place the paused state changes and no transition can be
	// lost by a caller flipping the flag behind its back.
	//
	// Nothing here is time-based. Whoever filled the volume is under no obligation to
	// empty it, so only a successful operation may lift the condition.
	spaceObserved bool
	// probe allows exactly one real capture attempt through a disk-full pause during
	// one reconciliation sweep. Without it the pause would be self-latching: capture
	// returns before it ever tries to link, so if the volume was filled by another
	// process and this collector holds nothing it can release, nothing would ever
	// discover that space came back.
	probe bool
}

func (g *intakeGate) enabled() bool { return g.high > 0 }

func (g *intakeGate) paused() bool { return g.watermark || g.diskFull }

// applyHighWater engages the watermark the instant tracked bytes reach it, and
// reports whether that is the transition into a paused state.
//
// This exists separately from evaluate because it has to be safe to call in the
// middle of a scan. A scan can register hundreds of captures without returning to
// the event loop, so waiting for the loop's own settling pass would let every
// remaining backup through after the limit was already breached — overshooting the
// high-water mark by an unbounded amount, which defeats the point of having one.
//
// It only ever engages the watermark. It never clears one, never touches diskFull
// and never consumes a capacity observation, so it cannot erase a resume transition
// that the event loop has not seen yet.
func (g *intakeGate) applyHighWater(total int64) (paused bool) {
	if !g.enabled() || g.watermark || total < g.high {
		return false
	}
	was := g.paused()
	g.watermark = true
	// A watermark pause is this collector's own limit, so an outstanding disk-full
	// probe must not carry a capture through it.
	g.probe = false
	return !was
}

// observedSpace records that a filesystem operation demonstrated the volume has room
// — a release that freed blocks, or a link that succeeded.
func (g *intakeGate) observedSpace() { g.spaceObserved = true }

// observedFull records that the volume refused a link, and reports whether that is
// the transition into the disk-full state.
//
// It discards any earlier capacity observation. Within one scan an early link can
// succeed and a later one hit ENOSPC; keeping the stale success would let the gate
// clear diskFull on the strength of an observation the filesystem has since
// contradicted. Only an operation that succeeds after this point may lift the
// condition.
func (g *intakeGate) observedFull() (first bool) {
	g.spaceObserved = false
	first = !g.diskFull
	g.diskFull = true
	return first
}

// armProbe permits one capacity probe for the sweep that is starting.
//
// Only a disk-full pause is probed. A watermark pause is this collector's own
// accounting saying it already holds too much, and letting a probe through it would
// be bypassing the very limit it asked for.
func (g *intakeGate) armProbe() {
	if g.diskFull && !g.watermark {
		g.probe = true
	}
}

func (g *intakeGate) disarmProbe() { g.probe = false }

// takeProbe consumes the sweep's single allowance, if there is one.
func (g *intakeGate) takeProbe() bool {
	if !g.probe {
		return false
	}
	g.probe = false
	return true
}

// evaluate folds the current total into the gate and reports which transition, if
// any, happened. Reporting only on change is what keeps a paused volume from filling
// the log with the same line on every event.
//
// trusted says whether the byte total can be believed. Untrusted accounting may still
// pause intake — erring towards holding back is safe — but it may never resume it: a
// total that fell because the collector lost track of a capture must not be mistaken
// for pressure that genuinely eased.
func (g *intakeGate) evaluate(total int64, trusted bool) (paused, resumed bool) {
	was := g.paused()
	if g.spaceObserved {
		// The volume has room again. If it does not, the next link attempt says so
		// and pauses intake once more.
		g.diskFull = false
		g.spaceObserved = false
	}
	if g.enabled() {
		switch {
		case !g.watermark && total >= g.high:
			g.watermark = true
		case g.watermark && trusted && total <= g.low:
			g.watermark = false
		}
	}
	now := g.paused()
	return now && !was, !now && was
}

// validateWatermarks rejects a configuration that could never resume, or that would
// resume the moment it paused.
func validateWatermarks(high, low int64) error {
	if high < 0 || low < 0 {
		return fmt.Errorf("rotated collector: watermarks must not be negative (high=%d, low=%d)", high, low)
	}
	if high == 0 {
		if low != 0 {
			return fmt.Errorf("rotated collector: LowWaterBytes=%d needs a HighWaterBytes to sit below", low)
		}
		return nil
	}
	if low >= high {
		return fmt.Errorf("rotated collector: LowWaterBytes=%d must be below HighWaterBytes=%d", low, high)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Owner-goroutine scheduling. Everything below runs on the collector's own
// goroutine and is the only code allowed to mutate captureIndex, capture state,
// retry state, byte accounting and the intake gate.
// ---------------------------------------------------------------------------

func (rc *rotatedCollector) now() time.Time { return rc.cfg.Now() }

// uploadResults is the owner's receive side. A disabled uploader leaves it nil, and a
// nil channel simply never fires.
func (rc *rotatedCollector) uploadResults() <-chan uploadResult { return rc.up.results }

func (rc *rotatedCollector) retryTimer() <-chan time.Time { return rc.up.retryC }

// intakePaused reports whether capture may create new staging links.
func (rc *rotatedCollector) intakePaused() bool { return rc.gate.paused() }

// pump is the owner's between-events housekeeping: settle the intake gate, hand work
// to the worker, and arm the retry timer. It runs at the top of every loop iteration,
// before any request is served, so a caller that observes a snapshot is observing
// state the scheduler has already acted on.
//
// It never blocks. Dispatch only sends when nothing is in flight, and the job channel
// has room for exactly that one job.
//
// It returns an error only for a condition no retry can fix, which stops the
// collector rather than letting it schedule impossible work forever.
func (rc *rotatedCollector) pump() error {
	if rc.settleIntake() {
		// Resuming means the volume has room again, and backups that were skipped
		// while it did not may still be on disk. Reconcile at once rather than wait
		// for the tick, because rotation is still deleting them.
		rc.maintain()
		rc.settleIntake()
	}
	if err := rc.dispatchUploads(); err != nil {
		return err
	}
	rc.armRetryTimer()
	return nil
}

// enforceHighWater settles the watermark against the retained total right where that
// total changed, so a sweep cannot keep capturing past the limit.
func (rc *rotatedCollector) enforceHighWater() {
	if rc.gate.applyHighWater(rc.bytes.retained) {
		rc.reportIntakePaused()
	}
}

func (rc *rotatedCollector) reportIntakePaused() {
	rc.intakePauses++
	rc.report(fmt.Errorf("%w: %d retained byte(s) reached the high-water mark of %d; captured data is kept and uploads continue, but backups rotated away from now on cannot be preserved",
		errIntakePaused, rc.bytes.retained, rc.gate.high))
}

// settleIntake applies the retained total to the gate and reports transitions.
//
// Retained rather than logical bytes: the gate exists to bound the disk this feature
// keeps allocated, and a capture Ray still has its own link to keeps none.
//
// The transition counters matter beyond diagnostics: a resume followed immediately by
// a fresh pause leaves the gate looking untouched, so the count is the only evidence
// that intake was ever wrongly reopened.
func (rc *rotatedCollector) settleIntake() (resumed bool) {
	paused, resumed := rc.gate.evaluate(rc.bytes.retained, rc.bytes.trusted())
	if paused {
		rc.reportIntakePaused()
	}
	if resumed {
		rc.intakeResumes++
		logrus.Infof("Rotated log collector: retained staging fell to %d byte(s); resuming intake", rc.bytes.retained)
	}
	return resumed
}

// maintain is the backstop sweep: rediscover the tree, queue anything pending that is
// not already scheduled, retry releases, re-establish accounting and act on anything
// whose retry has come due.
//
// Accounting is recomputed on every sweep rather than only when it is marked stale,
// because a capture's ownership changes without anything touching the staging tree:
// when Ray rolls a segment off its backup ring the staging link's nlink falls from 2
// to 1, and that is the moment the collector starts retaining blocks the filesystem
// would otherwise have reclaimed. Nothing reports that, so the sweep re-reads it. It
// costs one Lstat per tracked capture and reuses the pass that already existed.
//
// It runs after the releases so the totals describe what is still held once this
// sweep's releases are done.
//
// The sweep is also the only place a disk-full pause can be tested, so it opens with
// a single probe allowance and closes by withdrawing whatever is left of it. That
// bound is what keeps a full volume from being retried once per filesystem event.
func (rc *rotatedCollector) maintain() {
	rc.gate.armProbe()
	rc.scanTree()
	rc.gate.disarmProbe()

	rc.sweepUploads()
	rc.sweepReleases()
	rc.bytes.recompute(rc.cfg.StagingRoot, rc.ix, rc.report)
	rc.processDue()
}

// sweepUploads queues every pending capture the pipeline is not already handling.
// It is idempotent: a capture that is queued, in flight, backing off or awaiting
// promotion already has state, and state is what stops a second job being created.
func (rc *rotatedCollector) sweepUploads() {
	if !rc.up.enabled() {
		return
	}
	for key, c := range rc.ix.byInode {
		if c.Entry.State != statePending {
			continue
		}
		rc.enqueueUpload(key)
	}
}

// enqueueUpload schedules one pending capture for upload.
func (rc *rotatedCollector) enqueueUpload(key inodeKey) {
	if !rc.up.enabled() {
		return
	}
	c, ok := rc.ix.lookup(key)
	if !ok || c.Entry.State != statePending {
		return
	}
	if _, busy := rc.up.states[key]; busy {
		return
	}
	rc.up.states[key] = &uploadState{phase: phaseQueued}
	rc.up.enqueue(key, c.Entry.CaptureID)
}

// dispatchUploads hands queued work to the worker while it is free.
//
// The loop condition is the deduplication that matters: one job may be outstanding,
// and inFlight is only cleared when its result is applied, so a capture cannot be
// uploaded twice concurrently no matter how many events name it.
func (rc *rotatedCollector) dispatchUploads() error {
	if !rc.up.enabled() {
		return nil
	}
	for rc.up.inFlight == 0 {
		head, ok := rc.up.dequeue()
		if !ok {
			return nil
		}
		st, tracked := rc.up.states[head.key]
		if !tracked || st.phase != phaseQueued {
			// The queue entry lost its state, or the state moved on without it.
			// Whatever the state says is authoritative.
			continue
		}
		c, present := rc.ix.lookup(head.key)
		if !present || c.Entry.State != statePending {
			// Released, or already uploaded by an earlier result. Nothing to send.
			rc.up.forget(head.key)
			continue
		}

		job, err := rc.newUploadJob(c, st.attempts+1)
		if err != nil {
			// The key is a pure function of validated capture identity and static
			// configuration, so if it cannot be built now it will never be built.
			// Retrying it on the transport schedule would spin forever, so this
			// stops the collector with the pending staging link untouched.
			rc.up.forget(head.key)
			return fmt.Errorf("%w: %w", errStagingInconsistent, err)
		}

		st.phase = phaseInFlight
		st.job = job
		st.dueAt = time.Time{}
		rc.up.inFlight++
		// Cannot block: inFlight was zero, so the worker holds no job and the
		// capacity-1 channel is empty.
		rc.up.jobs <- job
	}
	return nil
}

// newUploadJob freezes everything the worker and the result check need.
//
// The object key comes from the capture's own identity — session, node, relative
// directory, original name, capture ID — and never from its staging state, so a
// retry, a restart and a promotion all address the same object.
func (rc *rotatedCollector) newUploadJob(c *capture, attempt int) (uploadJob, error) {
	key := c.Entry.objectKey(rc.cfg.Cluster)
	prefix := rc.cfg.Cluster.logsPrefix(c.Entry.SessionName, c.Entry.NodeName)
	if !strings.HasPrefix(key, prefix+"/") {
		return uploadJob{}, fmt.Errorf("object key %q for capture %s escapes the cluster log prefix %q",
			key, c.Entry.CaptureID, prefix)
	}
	return uploadJob{
		uploadIdentity: uploadIdentity{
			inode:     c.Inode,
			entry:     c.Entry,
			localPath: c.Entry.path(rc.cfg.StagingRoot),
			objectKey: key,
		},
		attempt: attempt,
	}, nil
}

// applyUploadResult is the only place a result may change anything.
//
// Nothing is trusted from the worker except the job it was given: the result is
// matched against the state the owner recorded when it submitted, and then against
// the capture the index currently holds. Only a result that still describes exactly
// that capture is acted on.
//
// It returns an error only when the staging volume contradicted the index. That is
// not an outage and no amount of retrying resolves it, so it stops the collector.
func (rc *rotatedCollector) applyUploadResult(res uploadResult) error {
	if rc.up.inFlight > 0 {
		rc.up.inFlight--
	}

	st, tracked := rc.up.states[res.job.inode]
	if !tracked || st.phase != phaseInFlight || st.job.uploadIdentity != res.job.uploadIdentity {
		rc.report(fmt.Errorf("%w: capture %s (%s)", errUploadStale, res.job.entry.CaptureID, res.job.inode))
		return nil
	}
	c, present := rc.ix.lookup(res.job.inode)
	if !present || c.Entry != res.job.entry {
		rc.up.forget(res.job.inode)
		rc.report(fmt.Errorf("%w: capture %s (%s) is no longer staged as submitted", errUploadStale, res.job.entry.CaptureID, res.job.inode))
		return nil
	}

	if res.err != nil {
		// A local validation failure is categorically different from an object-store
		// one. The store being unreachable is temporary and the same bytes will go
		// out later; a staged path that is missing, non-regular, or holding a
		// different inode than the one the index pinned is a durable contradiction
		// between memory and disk. Reconciliation cannot repair it — the index
		// entry keeps naming a file that is not there — and no storage call will
		// ever succeed, so retrying on the transport schedule would mean retrying
		// forever. Fail closed instead, leaving the pending staging state exactly as
		// it is for an operator, or the next run's reconstruction, to resolve.
		if res.local {
			rc.up.forget(res.job.inode)
			return fmt.Errorf("%w: capture %s staged at %s: %w",
				errStagingInconsistent, res.job.entry.CaptureID, res.job.localPath, res.err)
		}
		rc.failUpload(st, res.job.entry.CaptureID, res.err)
		return nil
	}

	// The bytes are in storage. From here the capture is only ever promoted, never
	// re-uploaded, even if the promotion itself has to be retried.
	st.phase = phaseAwaitingPromotion
	st.attempts = 0
	rc.completeUpload(res.job.inode)
	return nil
}

// failUpload keeps the capture pending and schedules a retry. The capture ID, the
// staged link and the object key are all untouched, so the retry is the identical
// write to the identical key.
func (rc *rotatedCollector) failUpload(st *uploadState, captureID string, err error) {
	st.phase = phaseBackoff
	st.attempts++
	delay := rc.up.delay(st.attempts)
	st.dueAt = rc.now().Add(delay)
	rc.report(fmt.Errorf("upload of capture %s failed (attempt %d), retrying in %s: %w",
		captureID, st.attempts, delay, err))
}

// completeUpload promotes a capture whose object write has succeeded.
//
// Promotion is the durable record that the bytes are safe, so it happens only after
// the write returns, and the pending staging link is kept until it does. If the
// rename fails the capture stays pending on disk and in the index — consistent, just
// not yet promoted — and only the promotion is retried. Re-uploading would be a
// second write of bytes storage already has.
func (rc *rotatedCollector) completeUpload(key inodeKey) {
	st, tracked := rc.up.states[key]
	if !tracked || st.phase != phaseAwaitingPromotion {
		return
	}
	c, present := rc.ix.lookup(key)
	if !present || c.Entry != st.job.entry || c.Entry.State != statePending {
		rc.up.forget(key)
		rc.report(fmt.Errorf("%w: capture %s cannot be promoted after upload", errUploadStale, st.job.entry.CaptureID))
		return
	}

	if _, err := promoteCapture(rc.cfg.StagingRoot, rc.ix, key); err != nil {
		st.attempts++
		delay := rc.up.delay(st.attempts)
		st.dueAt = rc.now().Add(delay)
		rc.report(fmt.Errorf("capture %s is uploaded to %s but could not be promoted locally (attempt %d); the pending staging link is kept and promotion retries in %s: %w",
			st.job.entry.CaptureID, st.job.objectKey, st.attempts, delay, err))
		return
	}

	// Promotion rewrote the entry, so the submitted identity no longer matches
	// anything: the pipeline is done with this capture.
	rc.up.forget(key)
	rc.releaseIfUnheld(key)
}

// processDue acts on every capture whose retry time has arrived.
func (rc *rotatedCollector) processDue() {
	if !rc.up.enabled() {
		return
	}
	now := rc.now()

	var promotions []queuedUpload
	for key, st := range rc.up.states {
		if st.dueAt.IsZero() || now.Before(st.dueAt) {
			continue
		}
		switch st.phase {
		case phaseBackoff:
			// Queue order comes from the index rather than the submitted job: a
			// capture whose very first job could not be built has no job to read an
			// ID from, and the index is authoritative in any case.
			c, present := rc.ix.lookup(key)
			if !present {
				delete(rc.up.states, key)
				continue
			}
			st.phase = phaseQueued
			st.dueAt = time.Time{}
			rc.up.enqueue(key, c.Entry.CaptureID)
		case phaseAwaitingPromotion:
			promotions = append(promotions, queuedUpload{key: key, captureID: st.job.entry.CaptureID})
		case phaseQueued, phaseInFlight:
		}
	}

	// completeUpload mutates the state map, so it runs outside the range, and in
	// capture-ID order so a sweep is not at the mercy of map iteration.
	sort.Slice(promotions, func(i, j int) bool { return promotions[i].captureID < promotions[j].captureID })
	for _, p := range promotions {
		rc.completeUpload(p.key)
	}
}

// armRetryTimer keeps one timer armed for the earliest outstanding retry. Re-arming
// for a time already armed is skipped so that an idle loop does not churn timers.
func (rc *rotatedCollector) armRetryTimer() {
	if !rc.up.enabled() {
		return
	}
	var earliest time.Time
	for _, st := range rc.up.states {
		if st.dueAt.IsZero() {
			continue
		}
		if earliest.IsZero() || st.dueAt.Before(earliest) {
			earliest = st.dueAt
		}
	}
	if earliest.IsZero() {
		rc.up.disarm()
		return
	}
	if earliest.Equal(rc.up.armedFor) {
		return
	}
	if rc.up.stopTimer != nil {
		rc.up.stopTimer()
	}
	wait := max(earliest.Sub(rc.now()), 0)
	rc.up.retryC, rc.up.stopTimer = rc.cfg.NewTimer(wait)
	rc.up.armedFor = earliest
}

// sweepReleases retries the release of every uploaded capture, in capture-ID order.
// Reconstructed uploaded captures come through here too: they are never re-uploaded,
// but they are always candidates for release.
func (rc *rotatedCollector) sweepReleases() {
	var uploaded []queuedUpload
	for key, c := range rc.ix.byInode {
		if c.Entry.State == stateUploaded {
			uploaded = append(uploaded, queuedUpload{key: key, captureID: c.Entry.CaptureID})
		}
	}
	sort.Slice(uploaded, func(i, j int) bool { return uploaded[i].captureID < uploaded[j].captureID })
	for _, u := range uploaded {
		rc.releaseIfUnheld(u.key)
	}
}

// releaseIfUnheld drops an uploaded capture's staging link once Ray has let go of its
// own.
//
// A remaining link is the expected steady state, not a failure: Ray keeps a rotated
// segment until its backup count rolls it off, and the collector's whole purpose is to
// hold the inode until then. That case is logged at debug and retried by the next
// sweep. A missing file or a different inode is different in kind — the index and the
// volume disagree — and is reported, with byte accounting marked for recomputation.
//
// The link count is checked here only to tell the expected case from the surprising
// one. releaseCapture re-reads it immediately before unlinking, so the decision that
// actually removes the file rests on its own fresh evidence.
func (rc *rotatedCollector) releaseIfUnheld(key inodeKey) {
	c, present := rc.ix.lookup(key)
	if !present || c.Entry.State != stateUploaded {
		return
	}

	p := c.Entry.path(rc.cfg.StagingRoot)
	staged, nlink, err := statInode(p)
	if err != nil {
		rc.bytes.markStale()
		rc.report(fmt.Errorf("release capture %s: index and staging volume disagree about %s: %w", c.Entry.CaptureID, p, err))
		return
	}
	if staged != c.Inode {
		rc.bytes.markStale()
		rc.report(fmt.Errorf("release capture %s: %s now holds %s, not the pinned %s", c.Entry.CaptureID, p, staged, c.Inode))
		return
	}
	if nlink != 1 {
		logrus.Debugf("Rotated log collector: capture %s still has %d link(s); Ray has not finished with the segment",
			c.Entry.CaptureID, nlink)
		return
	}

	if err := releaseCapture(rc.cfg.StagingRoot, rc.ix, key); err != nil {
		rc.report(fmt.Errorf("release capture %s: %w", c.Entry.CaptureID, err))
		return
	}

	rc.bytes.forget(key)
	// Space came back, so a filesystem that refused a link may accept one now. The
	// gate decides what that means: clearing the flag here would erase the paused ->
	// resumed transition before anything could act on it. The watermark is untouched
	// either way, because only crossing the low-water mark clears that.
	rc.gate.spaceObserved = true
}
