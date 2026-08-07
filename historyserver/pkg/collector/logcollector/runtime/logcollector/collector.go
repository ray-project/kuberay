package logcollector

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/storage/clusterlogs"
	"github.com/ray-project/kuberay/historyserver/pkg/storage/clustermetadata"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// sessionLatestLinkName is the symlink Ray keeps pointing at the active session. It
// is never a session name, which is why resolveSessionIdentity refuses to use it as
// one.
const sessionLatestLinkName = "session_latest"

// defaultSessionPollInterval is how often session_latest is re-read for a change.
const defaultSessionPollInterval = 5 * time.Second

// transitionGate decides whether a session transition may run.
//
// The session poller outlives the start of shutdown: ShutdownChan cannot close until
// after the final endpoint poll, which is after the final legacy walk. Without this
// gate a tick landing in that window could rediscover a node ID, retire a collector,
// or relocate the live tree out from under processSessionLatestLogs while it walks it.
// Freezing the supervisor does not prevent any of those, because they are the
// handler's side effects rather than the collector's.
//
// Its mutex is a leaf: held only to inspect and update the two counters, never across
// a transition, a supervisor call or a filesystem operation.
type transitionGate struct {
	cond    *sync.Cond
	mu      sync.Mutex
	running int
	closed  bool
}

// enter reports whether a transition may start, and counts it in when it may.
func (g *transitionGate) enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.running++
	return true
}

// leave counts a transition out and wakes a close that is waiting for it.
func (g *transitionGate) leave() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.running--
	if g.running == 0 && g.cond != nil {
		g.cond.Broadcast()
	}
}

// close refuses every future transition and waits for the ones already admitted.
//
// The wait has no timeout because there is nothing to cancel: every step an admitted
// transition can still be inside terminates on its own. The node-ID query is an HTTP
// call with a one-second client timeout and the relocation is a rename. The handover
// is the longest step — stopRun reconciles the outgoing collector once before draining
// it, and that reconciliation is a synchronous walk of the logs tree, so it is bounded
// by the size of that tree rather than by the drain budget; the drain budget and the
// upload worker's stop grace bound everything after it.
//
// Returning early would not shorten any of those. It would only let shutdown run the
// rotated retirement and the legacy walk concurrently with a transition still free to
// change the node identity they write under, move the tree the walk is reading, or
// take the supervisor's lifecycle lock the retirement needs.
func (g *transitionGate) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	if g.cond == nil {
		g.cond = sync.NewCond(&g.mu)
	}
	for g.running > 0 {
		g.cond.Wait()
	}
}

type RayLogHandler struct {
	Writer                 storage.StorageWriter
	LogFiles               chan string
	HttpClient             *http.Client
	ShutdownChan           chan struct{}
	logFilePaths           map[string]bool
	ClusterDir             string
	RayClusterName         string
	LogDir                 string
	RayNodeName            string
	RayClusterNamespace    string
	OwnerKind              string
	OwnerName              string
	RootDir                string
	SessionDir             string
	prevLogsDir            string
	persistCompleteLogsDir string
	// rotated owns the rotated-log subsystem. Run installs it before any other
	// goroutine starts; every method on it is nil-safe, so a handler that never ran —
	// which is how the legacy tests construct one — behaves exactly as it did before.
	// It is read through rotatedCollection() and written only by
	// startRotatedCollection, both under mu.
	rotated *rotatedSupervisor
	// discoverNodeID resolves this pod's current Ray node ID. It is nil in production,
	// where currentNodeID queries the dashboard; tests set it to make a session change
	// carry a node change without a network.
	discoverNodeID func() (string, bool)
	// beforeRelocation runs just before an outgoing session's logs are moved. It is nil
	// in production and exists so a test can hold a transition in its last step, where
	// a shutdown that did not wait would walk a tree that is being moved.
	beforeRelocation func()
	// transitions gates session transitions against shutdown. Its zero value is an
	// open gate, so a handler that was never run behaves exactly as it did before.
	transitions transitionGate
	// sessionPollInterval overrides how often session_latest is re-read. Zero means
	// defaultSessionPollInterval, which is what production uses; tests set it so that
	// waiting for several polling cycles does not mean waiting several tens of seconds.
	sessionPollInterval  time.Duration
	PushInterval         time.Duration
	LogBatching          int
	IsHead               bool
	DashboardAddress     string
	AdditionalEndpoints  []string
	EndpointPollInterval time.Duration
	mu                   sync.RWMutex
}

func (r *RayLogHandler) GetRayNodeName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.RayNodeName
}

func (r *RayLogHandler) SetRayNodeName(newNodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.RayNodeName != newNodeID {
		logrus.Infof("RayLogHandler: updated node ID: %s -> %s", r.RayNodeName, newNodeID)
		r.RayNodeName = newNodeID
	}
}

func (r *RayLogHandler) Run(stop <-chan struct{}) error {
	// watchPath := r.LogDir
	r.prevLogsDir = utils.GetRayPrevLogsPath()
	r.persistCompleteLogsDir = utils.GetRayPersistCompletePath()

	// Initialize log file paths storage
	r.logFilePaths = make(map[string]bool)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Fatalf("Create fsnotify NewWatcher error %v", err)
	}
	defer watcher.Close()

	// Rotated-log protection comes up first, and long before the final shutdown walk.
	// A segment Ray rotates away in the first seconds of a session is already gone by
	// the time that walk runs, so starting the legacy flow first would leave exactly
	// the window this subsystem exists to close.
	r.startRotatedCollection()

	// WatchPrevLogsLoops performs an initial scan of the prev-logs directory on startup
	// to process leftover log files in prev-logs/{sessionID}/{nodeID}/logs/ directories.
	// After scanning, it watches for new directories and files. This ensures incomplete
	// uploads from previous runs are resumed.
	go r.WatchPrevLogsLoops()
	go r.PollActiveSessionChanges()
	if r.IsHead {
		go r.WatchSessionLatestLoops() // Watch session_latest symlink changes
		go r.FetchAndStoreClusterMetadata()
		go r.FetchAndStoreTimezone()
		go r.PollAdditionalEndpointsPeriodically()
	}

	<-stop
	logrus.Info("Received stop signal, processing all logs...")
	r.shutdownLogCollection()
	// Perform one final poll of additional endpoints before shutting down.
	// This must happen before close(r.ShutdownChan) because pollSingleEndpoint
	// uses ShutdownChan to cancel in-flight HTTP requests.
	if r.IsHead {
		r.processAdditionalEndpoints()
	}
	close(r.ShutdownChan)

	return nil
}

// shutdownLogCollection brings log collection down in the only order that is safe, and
// each step must complete before the next begins.
//
//  1. Close session-transition admission and wait for one that was already admitted.
//     The poller outlives this point — ShutdownChan cannot close until after the final
//     endpoint poll — so anything less would leave a transition free to relocate the
//     tree the walk below reads, change the node identity it writes under, or hold the
//     supervisor's lifecycle lock that step 2 needs.
//  2. Retire the rotated subsystem: one last reconciliation of the live tree, then a
//     bounded drain. Bounded because storage.StorageWriter.WriteFile cannot be
//     canceled and the walk below is the pre-existing data path, which must still get
//     its share of the pod's termination grace period.
//  3. Walk the live tree, from an immutable snapshot of what "the live tree" meant
//     when this began.
//
// The walk is untouched by the rotated subsystem: the two write disjoint object keys,
// so neither suppresses the other; see startRotatedCollection.
func (r *RayLogHandler) shutdownLogCollection() {
	r.transitions.close()
	r.rotatedCollection().shutdown()
	r.processSessionLatestLogs()
}

// clusterIdentity is the owner-aware cluster address every object this handler writes
// lives under. It is assembled from the same fields the legacy upload paths use, so a
// captured segment and its uncaptured siblings land beside each other and RayJob and
// RayService clusters keep nesting their logs under the owner name.
func (r *RayLogHandler) clusterIdentity() clusterIdentity {
	return clusterIdentity{
		RootDir:     r.RootDir,
		OwnerKind:   r.OwnerKind,
		OwnerName:   r.OwnerName,
		Namespace:   r.RayClusterNamespace,
		ClusterName: r.RayClusterName,
	}
}

// rotatedCollection returns the rotated-log subsystem, or nil when this handler never
// started one. Every method on the result is nil-safe.
//
// The pointer is read under mu and the supervisor is called with mu released. That
// order is the whole reason there can be no lock inversion between the handler's node
// lock and the supervisor's: the supervisor never calls back into the handler, and the
// handler never calls into the supervisor while holding its own lock.
func (r *RayLogHandler) rotatedCollection() *rotatedSupervisor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.rotated
}

// startRotatedCollection installs the rotated-log subsystem and points it at the
// session this handler started with.
//
// What it adds is strictly additive to the legacy flow, and that is a property of the
// object keys rather than of any coordination between the two. A captured segment is
// written as "<name>.rotated.<capture ID>" — a capture ID is what makes two
// generations of "raylet.out.1" two distinct objects — while the legacy walk writes
// "<name>". The two key spaces cannot intersect, so neither half can overwrite or
// stand in for the other, and nothing the rotated subsystem does may suppress a legacy
// upload: doing so would delete a key the legacy walk has always produced, in exchange
// for an object with a different name.
func (r *RayLogHandler) startRotatedCollection() {
	sup := newRotatedSupervisor(r.clusterIdentity(), r.Writer, utils.GetRayRotatedStagingPath())

	r.mu.Lock()
	if r.rotated == nil {
		r.rotated = sup
	}
	r.mu.Unlock()

	r.ensureRotatedCollection(r.SessionDir)
}

// ensureRotatedCollection points rotated-log protection at the session directory that
// is active now, under the node ID the handler currently holds.
//
// It is idempotent, so the session poller can call it on every tick: that is what
// picks up a session directory that appeared late, a session that was replaced, and a
// node ID that was rediscovered.
func (r *RayLogHandler) ensureRotatedCollection(sessionDir string) {
	r.ensureRotatedCollectionForNode(sessionDir, r.GetRayNodeName())
}

// ensureRotatedCollectionForNode points rotated-log protection at one session on one
// explicitly named node.
//
// The node is a parameter rather than something read back out of the handler because a
// session change has to build the new collector under the *new* session's node ID, and
// that ID is discovered during the changeover. Writing it into the handler first, only
// so that ensure could read it, would make the handler's node ID mean "the node of the
// collector I am about to build" for the duration of the changeover — and every other
// reader of GetRayNodeName, including the relocation of the old session's logs, would
// see the wrong answer.
//
// Getting this wrong is not recoverable by restarting the collector. The node ID is
// baked into the staged entry, the staging path and the object key at capture time, and
// cross-session adoption deliberately preserves a record's original identity, so a
// segment captured under the previous session's node stays addressed to it forever.
func (r *RayLogHandler) ensureRotatedCollectionForNode(sessionDir, nodeID string) {
	sup := r.rotatedCollection()
	if sup == nil {
		return
	}
	session, logsDir, ok := resolveSessionIdentity(sessionDir)
	if !ok {
		return
	}
	sup.ensure(session, strings.TrimSpace(nodeID), logsDir)
}

// sessionTransition is what the session poller knows about where the runtime is
// between two Ray sessions.
//
// The four facts are kept apart because they become true at different times and fail
// independently. Inferring them from one "last directory" variable plus the handler's
// mutable node ID is what let a session be protected under its predecessor's node, and
// let a failed relocation retry retire a collector that was already correct.
type sessionTransition struct {
	// dir is the session directory session_latest resolves to.
	dir string
	// node is the node ID this runtime is running the session at dir under. Empty means
	// not established yet, and empty is never filled in from the handler's current
	// node: that value belongs to whichever session was active last, and carrying it
	// forward is what addressed a session's objects to its predecessor's node.
	//
	// "Verified" here means only that the dashboard answered after this session was
	// observed — see currentNodeID for exactly how weak that binding is.
	node string
	// handedOff mirrors the supervisor: it records that an exact run for dir+node is
	// attached. It is refreshed from the supervisor on every observation and is never
	// trusted across one, because a run can fail at any point after it is attached.
	handedOff bool
	// pending is every outgoing session observed by this poller whose logs have not
	// reached prev-logs yet, oldest first, each with the node it actually ran under.
	// One shared node ID would file them all under whichever node was verified last.
	pending []pendingRelocation
	// sweepUnobserved records that the broad legacy sweep is still owed. It catches
	// session directories this poller never observed — ones already stale when the
	// collector started, or created and replaced between two ticks — which have no
	// node of their own to be filed under.
	sweepUnobserved bool
}

// pendingRelocation is one outgoing session and the node it ran under.
type pendingRelocation struct {
	dir  string
	node string
}

// maxPendingRelocations bounds the queue. Each entry is one observed session change
// whose relocation has not succeeded, so reaching this means the filesystem has been
// refusing renames across many sessions; the oldest are handed to the broad sweep,
// which can still move them, rather than accumulating without limit.
const maxPendingRelocations = 16

// advanceSession moves the transition state on by one observation of session_latest.
//
// It is one function for the poller's first look and for every tick, because the two
// were the same problem and diverged: the startup path used to relocate and hand off
// under whatever node the handler happened to hold.
//
// The three steps are independent and individually idempotent, which is what makes a
// retry of one of them not a repeat of the others:
//
//  1. verify the node ID for the session that is active now;
//  2. hand rotated protection over, once, when that identity is known;
//  3. relocate the previous session's logs, retried until it succeeds.
//
// The ordering constraint between 2 and 3 is that nothing may own a tree that is about
// to move: step 2 retires the old collector as part of building the new one, and when
// step 2 cannot run — the node is unknown — step 3 retires it explicitly instead, but
// only if it belongs to a session that is no longer current.
func (r *RayLogHandler) advanceSession(st *sessionTransition, newDir string) {
	if newDir == "" {
		return
	}
	if !r.transitions.enter() {
		// Shutdown has begun. A transition from here could move the live tree out from
		// under the final legacy walk, or change the node identity that walk writes
		// under, so none of its three steps may start.
		return
	}
	defer r.transitions.leave()

	if newDir != st.dir {
		if st.dir != "" {
			logrus.Infof("PollActiveSessionChanges: session changed from %s to %s. Relocating old logs.", st.dir, newDir)
			// Each outgoing session is queued with the node it actually ran under, so
			// that a session whose relocation fails is still filed under its own node
			// when it is retried, however many sessions come and go in between.
			st.enqueueRelocation(pendingRelocation{dir: st.dir, node: st.node})
			st.sweepUnobserved = true
		}
		st.dir, st.node, st.handedOff = newDir, "", false
	}

	// 1. Node identity. A rediscovery that disagrees is a genuine node change and needs
	//    a collector of its own; a rediscovery that fails leaves everything as it is,
	//    because failing to reach the dashboard says nothing about the collector that
	//    is already running.
	if id, ok := r.currentNodeID(); ok && id != st.node {
		st.node, st.handedOff = id, false
		r.SetRayNodeName(id)
	}

	// 2. Hand over rotated protection until the supervisor actually holds a run for
	//    this exact identity. handedOff is re-derived from the supervisor rather than
	//    remembered, because ensure returning is not the same as a collector existing,
	//    and a collector that attached can still fail on any startup step afterwards.
	//    Rebuilding is cheap to attempt and the supervisor's own backoff decides when
	//    it may actually happen.
	st.handedOff = r.rotatedHandedOff(st.dir, st.node)
	if st.node != "" && !st.handedOff {
		r.ensureRotatedCollectionForNode(st.dir, st.node)
		st.handedOff = r.rotatedHandedOff(st.dir, st.node)
	}

	// 3. Relocate what earlier sessions left behind.
	r.relocateOutgoingSessions(st)
}

// enqueueRelocation records one outgoing session, oldest first and bounded.
func (st *sessionTransition) enqueueRelocation(p pendingRelocation) {
	for _, existing := range st.pending {
		if existing.dir == p.dir {
			return
		}
	}
	st.pending = append(st.pending, p)
	for len(st.pending) > maxPendingRelocations {
		dropped := st.pending[0]
		st.pending = st.pending[1:]
		// The broad sweep can still move it; only its exact node is lost.
		st.sweepUnobserved = true
		logrus.Warnf("PollActiveSessionChanges: %d session relocations are outstanding, so %s is left to the broad sweep and may be filed under a later node.",
			maxPendingRelocations, dropped.dir)
	}
}

// rotatedHandedOff reports whether the supervisor holds a run for this exact identity
// that is either starting or ready. A run whose goroutine has exited is not a handover:
// it is one that has to be made again.
func (r *RayLogHandler) rotatedHandedOff(sessionDir, nodeID string) bool {
	sup := r.rotatedCollection()
	if sup == nil || nodeID == "" {
		return false
	}
	session, logsDir, ok := resolveSessionIdentity(sessionDir)
	if !ok {
		return false
	}
	switch sup.statusFor(session, strings.TrimSpace(nodeID), logsDir) {
	case runStarting, runReady:
		return true
	case runAbsent, runFinished:
		return false
	default:
		return false
	}
}

// relocateOutgoingSessions moves each observed outgoing session's logs into prev-logs,
// under the node that session actually ran on, and only then lets the broad legacy
// sweep collect whatever this poller never observed.
//
// The order is what keeps the labels honest. MoveLeftoverSessionLogs takes a single
// node ID and files *every* inactive session_* directory beneath it, so running it
// while a known session is still waiting would put that session's logs under a node
// that never wrote them. Each known session is therefore moved by itself first, and
// the sweep runs only once none are left.
//
// None of this is conditional on rotated collection having started. These logs are the
// legacy path's, this subsystem has no claim on them, and a dashboard that is briefly
// unreachable must not strand a whole session outside prev-logs.
func (r *RayLogHandler) relocateOutgoingSessions(st *sessionTransition) {
	if len(st.pending) == 0 && !st.sweepUnobserved {
		return
	}
	if r.beforeRelocation != nil {
		r.beforeRelocation()
	}

	// Nothing may own a tree that is about to move: a collector still gets its final
	// reconciliation, and it gets it before the move rather than after, when there
	// would be nothing left to scan. A collector for the *current* session is exempt —
	// its tree is not the one moving, and retiring it because an unrelated relocation
	// is being retried would cost a drain, a watcher and a staging reconstruction for
	// nothing.
	r.retireRotatedCollectionUnless(st.dir)

	// The label for anything whose own node is not known. The node verified for the
	// current session is preferred; failing that — no dashboard has answered since this
	// process started — it is the handler's node ID, which is what the legacy sweep has
	// always used and is the only identity the runtime has. It is never an older
	// session's verified node: that would file logs under a node they demonstrably did
	// not run on.
	fallbackNode := st.node
	if fallbackNode == "" {
		fallbackNode = strings.TrimSpace(r.GetRayNodeName())
	}

	var stuck []pendingRelocation
	for _, p := range st.pending {
		if p.node == "" {
			// Its own node was never established, so it can only be filed under the
			// fallback, which is exactly what the broad sweep below does.
			st.sweepUnobserved = true
			continue
		}
		if err := utils.MoveSessionLogsToPrevLogs(p.dir, p.node); err != nil {
			logrus.Warnf("PollActiveSessionChanges: failed to relocate the logs of %s under node %s: %v. Retrying on next tick.", p.dir, p.node, err)
			stuck = append(stuck, p)
			continue
		}
		logrus.Infof("PollActiveSessionChanges: relocated the logs of %s under node %s", p.dir, p.node)
	}
	st.pending = stuck

	if len(stuck) > 0 {
		// The sweep would file those sessions under the wrong node. It waits.
		return
	}
	if !st.sweepUnobserved {
		return
	}
	if fallbackNode == "" {
		logrus.Warnf("PollActiveSessionChanges: no node ID is known, so any unobserved session directories stay in %s for now.", utils.GetTmpRayRoot())
		return
	}
	// Whatever is left was never observed by this poller — a directory that was already
	// stale when the collector started, or one created and replaced between two ticks —
	// so there is no node it can be said to belong to and the fallback is the only label
	// available. This is the same choice the legacy sweep has always made.
	if err := utils.MoveLeftoverSessionLogs(st.dir, fallbackNode); err != nil {
		logrus.Warnf("PollActiveSessionChanges: failed to relocate leftover session logs: %v. Retrying on next tick.", err)
		return
	}
	st.sweepUnobserved = false
}

// retireRotatedCollectionUnless stops rotated collection unless it is already running
// for the session at sessionDir. A session directory that cannot be resolved names
// nothing, so nothing can be exempt from retirement.
func (r *RayLogHandler) retireRotatedCollectionUnless(sessionDir string) {
	sup := r.rotatedCollection()
	if sup == nil {
		return
	}
	session, _, ok := resolveSessionIdentity(sessionDir)
	if !ok {
		session = ""
	}
	sup.retireUnless(session)
}

// currentNodeID rediscovers this pod's Ray node ID, normalized to hex. The query is a
// live HTTP call with its own one-second timeout, and it fails routinely in the seconds
// after a session restart.
//
// utils.FetchCurrentNodeID asks "which ALIVE node does the dashboard report for this
// pod's IP?". It takes no session, so the answer is the freshest identity available
// rather than proof of which session it belongs to: a session change seen on the
// filesystem can precede the dashboard dropping the outgoing node, and the outgoing ID
// is then accepted for the new session. That is deliberate — refusing an unchanged ID
// would stop rotated collection entirely on a deployment where node IDs legitimately
// persist, which is worse than a mislabeled changeover window.
//
// Because it is the only identity the runtime has, callers must treat a failure as
// "not now" and wait rather than substitute a guess.
func (r *RayLogHandler) currentNodeID() (string, bool) {
	if r.discoverNodeID != nil {
		return r.discoverNodeID()
	}
	rawID, err := utils.FetchCurrentNodeID()
	if err != nil || rawID == "" {
		logrus.Debugf("Cannot discover the current Ray node ID: %v", err)
		return "", false
	}
	hexID, err := utils.ConvertBase64ToHex(rawID)
	if err != nil || hexID == "" {
		logrus.Debugf("Cannot normalize the Ray node ID %q: %v", rawID, err)
		return "", false
	}
	return hexID, true
}

// resolveSessionIdentity turns a session directory into the real session name and the
// logs directory beneath it.
//
// The symlink is resolved first, and that is load-bearing rather than defensive. Ray's
// active session is reached through /tmp/ray/session_latest, and the session name is
// half of every staging path and every object key this subsystem writes. Taking the
// base of an unresolved path would name the session "session_latest": captures would
// be staged under a subtree no later collector recognizes as a session, and uploaded
// under a prefix the History Server never lists, while the legacy walk — which does
// resolve the symlink — kept writing the real session ID beside it.
//
// Failure to resolve is reported as "not now" rather than as a fault. A session
// directory that is briefly absent is exactly what the poller sees between two Ray
// sessions, and the next tick tries again.
func resolveSessionIdentity(sessionDir string) (session, logsDir string, ok bool) {
	if strings.TrimSpace(sessionDir) == "" {
		return "", "", false
	}
	resolved, err := filepath.EvalSymlinks(sessionDir)
	if err != nil {
		logrus.Debugf("Rotated log collection: cannot resolve session directory %s yet: %v", sessionDir, err)
		return "", "", false
	}
	session = filepath.Base(resolved)
	if session == sessionLatestLinkName || session == "." || session == string(filepath.Separator) {
		logrus.Warnf("Rotated log collection: %s did not resolve to a real Ray session directory (got %q), so it is not started for it", sessionDir, session)
		return "", "", false
	}
	return session, filepath.Join(resolved, utils.RAY_SESSIONDIR_LOGDIR_NAME), true
}

// shutdownSnapshot is what "the live session" meant at the moment shutdown began.
//
// Every field is taken once and then never re-read. session_latest is a symlink Ray
// repoints whenever it restarts a session, and the node ID is a mutable field the
// session poller writes; re-reading either one per file would let a session change
// during the walk split one shutdown across two identities — some objects under the
// old session, some under the new, and relative paths computed against a logs
// directory that is no longer the one being walked.
type shutdownSnapshot struct {
	// sessionDir is the resolved real session directory, never the symlink.
	sessionDir string
	sessionID  string
	nodeID     string
	// logsDir is beneath sessionDir, so the walk stays inside the real tree even if
	// session_latest is repointed while it runs.
	logsDir string
}

// takeShutdownSnapshot resolves session_latest and the node ID exactly once.
func (r *RayLogHandler) takeShutdownSnapshot() (shutdownSnapshot, bool) {
	sessionRealDir, err := filepath.EvalSymlinks(utils.GetRaySessionLatestPath())
	if err != nil {
		logrus.Errorf("Failed to resolve session_latest symlink: %v", err)
		return shutdownSnapshot{}, false
	}
	return shutdownSnapshot{
		sessionDir: sessionRealDir,
		sessionID:  filepath.Base(sessionRealDir),
		// Use the already discovered node ID instead of retrying network requests
		// during shutdown.
		nodeID:  strings.TrimSpace(r.GetRayNodeName()),
		logsDir: filepath.Join(sessionRealDir, utils.RAY_SESSIONDIR_LOGDIR_NAME),
	}, true
}

// processSessionLatestLogs processes logs in the active session's logs directory on
// shutdown, using the real session ID and node ID.
func (r *RayLogHandler) processSessionLatestLogs() {
	snap, ok := r.takeShutdownSnapshot()
	if !ok {
		return
	}
	r.processSessionLogs(snap)
}

// processSessionLogs walks one immutable snapshot of the live tree.
func (r *RayLogHandler) processSessionLogs(snap shutdownSnapshot) {
	logrus.Infof("Processing logs of session %s on shutdown...", snap.sessionID)

	sessionID := snap.sessionID
	if r.IsHead {
		metafile := clustermetadata.EncodePath(
			utils.ClusterInfo{
				Name:      r.RayClusterName,
				Namespace: r.RayClusterNamespace,
				OwnerKind: r.OwnerKind,
				OwnerName: r.OwnerName},
			r.RootDir,
			sessionID,
		)
		if err := r.Writer.CreateDirectory(path.Dir(metafile)); err != nil {
			logrus.Errorf("Failed to create directory %s error %v", path.Dir(metafile), err)
			return
		}
		if err := r.Writer.WriteFile(metafile, strings.NewReader("")); err != nil {
			logrus.Errorf("Failed to write session file %s error %v", metafile, err)
			return
		}
	}

	// Process the logs of the snapshotted session, beneath its real directory.
	logsDir := snap.logsDir
	dirExist := false
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(logsDir); os.IsNotExist(err) {
			logrus.Warnf("Logs directory does not exist: %s", logsDir)
			time.Sleep(time.Millisecond * 10)
		} else {
			dirExist = true
			break
		}
	}
	if !dirExist {
		logrus.Errorf("Logs directory does not exist after 10 attempts: %s", logsDir)
		return
	}

	// Walk through the logs directory and process all files
	err := filepath.WalkDir(logsDir, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			logrus.Errorf("Error walking logs path %s: %v", path, err)
			return nil
		}

		// Skip non-regular files (e.g. symlinks, directories, sockets, devices)
		if !info.Type().IsRegular() {
			return nil
		}

		// Every regular file in the live tree is uploaded here, exactly as it was
		// before rotated collection existed. A file the rotated subsystem has already
		// captured is not a duplicate of anything this walk writes: that capture went
		// to "<name>.rotated.<capture ID>", and the key below is "<name>". Skipping it
		// would not save a write, it would delete a key.
		//
		// Process log file against the snapshot, so every object of this shutdown
		// belongs to one session, one node and one logs directory.
		if err := r.processSessionLogFile(snap, path); err != nil {
			logrus.Errorf("Failed to process session log file %s: %v", path, err)
		}

		return nil
	})
	if err != nil {
		logrus.Errorf("Error walking logs directory %s: %v", logsDir, err)
	}

	logrus.Infof("Finished processing logs of session %s", snap.sessionID)
}

// processSessionLogFile processes a single log file from the snapshotted session.
func (r *RayLogHandler) processSessionLogFile(snap shutdownSnapshot, absoluteLogPathName string) error {
	sessionID, nodeID := snap.sessionID, snap.nodeID
	// The relative path is computed against the same real logs directory the walk
	// used, never against session_latest, which may point elsewhere by now.
	relativePath, err := filepath.Rel(snap.logsDir, absoluteLogPathName)
	if err != nil {
		return fmt.Errorf("failed to get relative path for %s: %w", absoluteLogPathName, err)
	}

	// Split relative path into subdirectory and filename
	subdir, _ := filepath.Split(relativePath)

	// Build the object name using the standard path structure
	logDir := clusterlogs.LogsDir(r.RootDir, r.OwnerKind, r.OwnerName, r.RayClusterNamespace, r.RayClusterName, sessionID, nodeID)

	if subdir != "" && subdir != "." {
		// Remove trailing separator if present
		subdir = strings.TrimSuffix(subdir, string(filepath.Separator))
		dirName := path.Join(logDir, subdir)
		if err := r.Writer.CreateDirectory(dirName); err != nil {
			logrus.Errorf("Failed to create directory '%s': %v", dirName, err)
			return err
		}
	}

	objectName := path.Join(logDir, relativePath)
	logrus.Infof("Processing session_latest log file %s (object: %s)", absoluteLogPathName, objectName)

	// Read the entire file content
	content, err := os.ReadFile(absoluteLogPathName)
	if err != nil {
		logrus.Errorf("Failed to read file %s: %v", absoluteLogPathName, err)
		return err
	}

	// Write to storage
	err = r.Writer.WriteFile(objectName, bytes.NewReader(content))
	if err != nil {
		logrus.Errorf("Failed to write object %s: %v", objectName, err)
		return err
	}

	logrus.Infof("Successfully wrote object %s, size: %d bytes", objectName, len(content))
	return nil
}

func (r *RayLogHandler) WatchPrevLogsLoops() {
	watchPath := r.prevLogsDir

	// Check if prev-logs directory exists
	if _, err := os.Stat(watchPath); os.IsNotExist(err) {
		logrus.Infof("prev-logs directory does not exist, creating it: %s", watchPath)
		if err := os.MkdirAll(watchPath, 0o777); err != nil {
			logrus.Errorf("Failed to create prev-logs directory %s: %v", watchPath, err)
			return
		}
		if err := os.Chmod(watchPath, 0o777); err != nil {
			logrus.Errorf("Failed to create prev-logs directory %s: %v", watchPath, err)
			return
		}
	}

	// Also check and create persist-complete-logs directory
	completeLogsDir := r.persistCompleteLogsDir
	if _, err := os.Stat(completeLogsDir); os.IsNotExist(err) {
		logrus.Infof("persist-complete-logs directory does not exist, creating it: %s", completeLogsDir)
		if err := os.MkdirAll(completeLogsDir, 0o777); err != nil {
			logrus.Errorf("Failed to create persist-complete-logs directory %s: %v", completeLogsDir, err)
			return
		}
		if err := os.Chmod(completeLogsDir, 0o777); err != nil {
			logrus.Errorf("Failed to create prev-logs directory %s: %v", completeLogsDir, err)
			return
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Errorf("Failed to create fsnotify watcher for prev-logs: %v", err)
		return
	}
	defer watcher.Close()

	sessionWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Errorf("Failed to create fsnotify watcher for prev-logs sessions: %v", err)
		return
	}
	defer sessionWatcher.Close()

	nodeWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Errorf("Failed to create fsnotify watcher for prev-logs nodeWatcher: %v", err)
		return
	}
	defer nodeWatcher.Close()

	// Add the root prev-logs directory to watcher
	if err := watcher.Add(watchPath); err != nil {
		logrus.Errorf("Failed to add %s to watcher: %v", watchPath, err)
		return
	}

	// Walk through existing directories and add them to watcher
	err = filepath.WalkDir(watchPath, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			logrus.Errorf("Error walking path %s: %v", path, err)
			return nil
		}

		if info.IsDir() && path != watchPath {
			// Check if this is a session directory (direct subdirectory of prev-logs)
			rel, err := filepath.Rel(watchPath, path)
			if err != nil {
				logrus.Errorf("Failed to get relative path for %s: %v", path, err)
				return nil
			}

			// Split the relative path to check depth
			parts := strings.Split(filepath.ToSlash(rel), "/")

			switch len(parts) {
			case 1: // Session directory
				if err := sessionWatcher.Add(path); err != nil {
					logrus.Errorf("Failed to add session directory %s to sessionWatcher: %v", path, err)
				}
				// Process all existing node directories under this session
				r.processSessionPrevLogs(path)
				// case 2: // Node directory
				// 	if err := nodeWatcher.Add(path); err != nil {
				// 		logrus.Errorf("Failed to add node directory %s to nodeWatcher: %v", path, err)
				// 	}
				// 	// Check if this node directory has a logs subdirectory
				// 	logsDir := filepath.Join(path, utils.RAY_SESSIONDIR_LOGDIR_NAME)
				// 	if _, err := os.Stat(logsDir); err == nil {
				// 		// This is a session/node directory with logs, process its logs
				// 		go r.processPrevLogsDir(path)
				// 	}
			}
		}
		return nil
	})
	if err != nil {
		logrus.Errorf("Error walking prev-logs directory: %v", err)
		return
	}

	logrus.Infof("Started watching prev-logs directory: %s", watchPath)

	for {
		select {
		case <-r.ShutdownChan:
			logrus.Info("Received shutdown signal, stopping prev-logs watcher")
			return
		case event, ok := <-watcher.Events:
			if !ok {
				logrus.Warn("Prev-logs watcher events channel closed")
				return
			}

			logrus.Infof("File system event in prev-logs: %s %s", event.Op, event.Name)

			// Handle new directories being created
			if event.Op&fsnotify.Create == fsnotify.Create {
				info, err := os.Stat(event.Name)
				if err != nil {
					logrus.Errorf("Failed to stat %s: %v", event.Name, err)
					continue
				}

				if info.IsDir() {
					// Add new directory to sessionWatcher
					if err := sessionWatcher.Add(event.Name); err != nil {
						logrus.Errorf("Failed to add %s to sessionWatcher: %v", event.Name, err)
					}
					// Process all existing node directories under this new session
					r.processSessionPrevLogs(event.Name)
				}
			}
		case event, ok := <-sessionWatcher.Events:
			if !ok {
				logrus.Warn("Session watcher events channel closed")
				return
			}

			logrus.Infof("File system event in session directory: %s %s", event.Op, event.Name)

			// Handle new directories being created in session directories
			if event.Op&fsnotify.Create == fsnotify.Create {
				info, err := os.Stat(event.Name)
				if err != nil {
					logrus.Errorf("Failed to stat %s: %v", event.Name, err)
					continue
				}

				if info.IsDir() {
					// Add new directory to nodeWatcher
					if err := nodeWatcher.Add(event.Name); err != nil {
						logrus.Errorf("Failed to add %s to nodeWatcher: %v", event.Name, err)
					}

					// Check if this is a node directory by examining its path
					rel, err := filepath.Rel(watchPath, event.Name)
					if err != nil {
						logrus.Errorf("Failed to get relative path for %s: %v", event.Name, err)
						continue
					}

					parts := strings.Split(filepath.ToSlash(rel), "/")
					if len(parts) == 2 { // This is a node directory
						// Check if logs subdirectory exists
						go r.processPrevLogsDir(event.Name)
					}
				}
			}
		case event, ok := <-nodeWatcher.Events:
			if !ok {
				logrus.Warn("Node watcher events channel closed")
				return
			}

			logrus.Debugf("File system event in node directory: %s %s", event.Op, event.Name)

			// Handle new directories or files being created in node directories
			if event.Op&fsnotify.Create == fsnotify.Create {
				info, err := os.Stat(event.Name)
				if err != nil {
					logrus.Errorf("Failed to stat %s: %v", event.Name, err)
					continue
				}

				// Check if this is a logs directory being created
				base := filepath.Base(event.Name)
				if info.IsDir() && base == utils.RAY_SESSIONDIR_LOGDIR_NAME {
					// This is a logs directory, process its parent (node directory)
					nodeDir := filepath.Dir(event.Name)
					logrus.Infof("New logs directory detected: %s", nodeDir)
					go r.processPrevLogsDir(nodeDir)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				logrus.Warn("Prev-logs watcher errors channel closed")
				return
			}
			logrus.Errorf("Prev-logs watcher error: %v", err)
		case err, ok := <-sessionWatcher.Errors:
			if !ok {
				logrus.Warn("Session watcher errors channel closed")
				return
			}
			logrus.Errorf("Session watcher error: %v", err)
		case err, ok := <-nodeWatcher.Errors:
			if !ok {
				logrus.Warn("Node watcher errors channel closed")
				return
			}
			logrus.Errorf("Node watcher error: %v", err)
		}
	}
}

// processSessionPrevLogs processes all node logs under a session directory
func (r *RayLogHandler) processSessionPrevLogs(sessionDir string) {
	// Check if this is actually a session directory (one level deep under prev-logs)
	watchPath := r.prevLogsDir
	rel, err := filepath.Rel(watchPath, sessionDir)
	if err != nil {
		logrus.Errorf("Failed to get relative path for session directory %s: %v", sessionDir, err)
		return
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 1 {
		// Not a session directory, skip
		return
	}

	sessionID := parts[0]
	logrus.Infof("Processing all node logs for session: %s", sessionID)
	if r.IsHead {
		metafile := clustermetadata.EncodePath(
			utils.ClusterInfo{
				Name:      r.RayClusterName,
				Namespace: r.RayClusterNamespace,
				OwnerKind: r.OwnerKind,
				OwnerName: r.OwnerName},
			r.RootDir,
			sessionID)
		if err := r.Writer.CreateDirectory(path.Dir(metafile)); err != nil {
			logrus.Errorf("Failed to create directory %s error %v", path.Dir(metafile), err)
			return
		}
		if err := r.Writer.WriteFile(metafile, strings.NewReader("")); err != nil {
			logrus.Errorf("Failed to write session file %s error %v", metafile, err)
			return
		}
	}

	// Walk through all node directories under this session
	err = filepath.WalkDir(sessionDir, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			logrus.Errorf("Error walking path %s: %v", path, err)
			return nil
		}

		if info.IsDir() && path != sessionDir {
			// Check if this is a node directory (two levels deep under prev-logs)
			logrus.Infof("found node session logs in directory: %v", path)
			rel, err := filepath.Rel(watchPath, path)
			if err != nil {
				logrus.Errorf("Failed to get relative path for node directory %s: %v", path, err)
				return nil
			}

			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) == 2 {
				go r.processPrevLogsDir(path)
			}
		}
		return nil
	})
	if err != nil {
		logrus.Errorf("Error walking session directory %s: %v", sessionDir, err)
	}
}

// isFileAlreadyPersisted checks if a log file has already been uploaded to storage and moved to
// the persist-complete-logs directory. This prevents duplicate uploads during collector restarts.
//
// When a log file is successfully uploaded, it is moved from prev-logs to persist-complete-logs
// to mark it as processed. This function checks if the equivalent file path exists in the
// persist-complete-logs directory.
//
// Example:
//
//	Given absoluteLogPath = "/tmp/ray/prev-logs/session_123/node_456/logs/raylet.out"
//	This function checks if "/tmp/ray/persist-complete-logs/session_123/node_456/logs/raylet.out" exists
//	- If exists: returns true (file was already uploaded, skip it)
//	- If not exists: returns false (file needs to be uploaded)
func (r *RayLogHandler) isFileAlreadyPersisted(absoluteLogPath, sessionID, nodeID string) bool {
	// Calculate the relative path within the logs directory
	logsDir := filepath.Join(r.prevLogsDir, sessionID, nodeID, utils.RAY_SESSIONDIR_LOGDIR_NAME)
	relativeLogPath, err := filepath.Rel(logsDir, absoluteLogPath)
	if err != nil {
		logrus.Errorf("Failed to get relative path for %s: %v", absoluteLogPath, err)
		return false
	}

	// Construct the path in persist-complete-logs
	persistedPath := filepath.Join(r.persistCompleteLogsDir, sessionID, nodeID, utils.RAY_SESSIONDIR_LOGDIR_NAME, relativeLogPath)

	// Check if the file exists
	if _, err := os.Stat(persistedPath); err == nil {
		return true
	}
	return false
}

// processPrevLogsDir processes logs in a /tmp/ray/prev-logs/{sessionid}/{nodeid} directory
func (r *RayLogHandler) processPrevLogsDir(sessionNodeDir string) {
	// Extract session ID and node ID from the path
	// Path format: /tmp/ray/prev-logs/{sessionid}/{nodeid}
	parts := strings.Split(sessionNodeDir, string(filepath.Separator))
	if len(parts) < 2 {
		logrus.Errorf("Invalid path format for sessionNodeDir: %s", sessionNodeDir)
		return
	}

	// Extract nodeID and sessionID from the path
	// Path is like: /tmp/ray/prev-logs/sessionid/nodeid
	nodeID := parts[len(parts)-1]
	sessionID := parts[len(parts)-2]

	// Validate that we're not processing the root prev-logs directory
	if sessionID == "prev-logs" {
		logrus.Debugf("Skipping root prev-logs directory")
		return
	}

	logrus.Infof("Processing prev-logs for session: %s, node: %s", sessionID, nodeID)

	logsDir := filepath.Join(sessionNodeDir, utils.RAY_SESSIONDIR_LOGDIR_NAME)
	dirExist := false
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(logsDir); os.IsNotExist(err) {
			logrus.Warnf("Logs directory does not exist: %s", logsDir)
			time.Sleep(time.Millisecond * 10)
		} else {
			dirExist = true
			break
		}
	}
	if !dirExist {
		logrus.Errorf("Logs directory does not exist after 10 attempts: %s", logsDir)
		return
	}

	// Walk through the logs directory and process all files
	err := filepath.WalkDir(logsDir, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			logrus.Errorf("Error walking logs path %s: %v", path, err)
			return nil
		}

		// Skip non-regular files (e.g. symlinks, directories, sockets, devices)
		if !info.Type().IsRegular() {
			return nil
		}

		// Check if this file has already been persisted
		if r.isFileAlreadyPersisted(path, sessionID, nodeID) {
			logrus.Debugf("File %s already persisted, skipping", path)
			return nil
		}

		// Process log file
		if err := r.processPrevLogFile(path, logsDir, sessionID, nodeID); err != nil {
			logrus.Errorf("Failed to process prev-log file %s: %v", path, err)
		}

		return nil
	})
	if err != nil {
		logrus.Errorf("Error walking logs directory %s: %v", logsDir, err)
		return
	}

	// After successfully processing all files, remove the node directory
	logrus.Infof("Finished processing all logs for session: %s, node: %s. Removing node directory.", sessionID, nodeID)
	if err := os.RemoveAll(sessionNodeDir); err != nil {
		logrus.Errorf("Failed to remove node directory %s: %v", sessionNodeDir, err)
	} else {
		logrus.Infof("Successfully removed node directory: %s", sessionNodeDir)
	}
}

// processPrevLogFile processes a single log file from prev-logs
func (r *RayLogHandler) processPrevLogFile(absoluteLogPathName, localLogDir, sessionID, nodeID string) error {
	// Calculate relative path within logs directory
	// The localLogDir is /tmp/ray/prev-logs/{sessionid}/{nodeid}/logs
	relativePath, err := filepath.Rel(localLogDir, absoluteLogPathName)
	if err != nil {
		return fmt.Errorf("failed to get relative path for %s: %w", absoluteLogPathName, err)
	}

	// Split relative path into subdirectory and filename
	subdir, _ := filepath.Split(relativePath)

	// Build the object name using the standard path structure
	logDir := clusterlogs.LogsDir(r.RootDir, r.OwnerKind, r.OwnerName, r.RayClusterNamespace, r.RayClusterName, sessionID, nodeID)

	if subdir != "" && subdir != "." {
		// Remove trailing separator if present
		subdir = strings.TrimSuffix(subdir, string(filepath.Separator))
		dirName := path.Join(logDir, subdir)
		if err := r.Writer.CreateDirectory(dirName); err != nil {
			logrus.Errorf("Failed to create directory '%s': %v", dirName, err)
			return err
		}
	}

	objectName := path.Join(logDir, relativePath)
	logrus.Infof("Processing prev-log file %s (object: %s)", absoluteLogPathName, objectName)

	// Read the entire file content
	content, err := os.ReadFile(absoluteLogPathName)
	if err != nil {
		logrus.Errorf("Failed to read file %s: %v", absoluteLogPathName, err)
		return err
	}

	// Write to storage
	err = r.Writer.WriteFile(objectName, bytes.NewReader(content))
	if err != nil {
		logrus.Errorf("Failed to write object %s: %v", objectName, err)
		return err
	}

	logrus.Infof("Successfully wrote object %s, size: %d bytes", objectName, len(content))

	// Move the processed file to persist-complete-logs directory to avoid re-uploading
	completeBaseDir := filepath.Join(r.persistCompleteLogsDir, sessionID, nodeID)
	completeDir := filepath.Join(completeBaseDir, utils.RAY_SESSIONDIR_LOGDIR_NAME)

	if _, err := os.Stat(completeDir); os.IsNotExist(err) {
		// Create the target directory if it doesn't exist
		if err := os.MkdirAll(completeDir, 0o777); err != nil {
			logrus.Errorf("Failed to create complete logs directory %s: %v", completeDir, err)
			return nil // Don't fail the whole process if we can't move the file
		}
		if err := os.Chmod(completeDir, 0o777); err != nil {
			logrus.Errorf("Failed to chmod complete logs directory %s: %v", completeDir, err)
			return nil // Don't fail the whole process if we can't move the file
		}
	}

	// Construct the target file path
	targetFilePath := filepath.Join(completeDir, relativePath)
	targetFileDir := filepath.Dir(targetFilePath)

	// Create subdirectory in target location if needed
	if subdir != "" && subdir != "." {
		if _, err := os.Stat(targetFileDir); os.IsNotExist(err) {
			if err := os.MkdirAll(targetFileDir, 0o777); err != nil {
				logrus.Errorf("Failed to create target subdirectory %s: %v", targetFileDir, err)
				return nil
			}
			if err := os.Chmod(targetFileDir, 0o777); err != nil {
				logrus.Errorf("Failed to chmod complete logs directory %s: %v", targetFileDir, err)
				return nil // Don't fail the whole process if we can't move the file
			}
		}
	}

	// Move the file
	if err := os.Rename(absoluteLogPathName, targetFilePath); err != nil {
		logrus.Errorf("Failed to move file from %s to %s: %v", absoluteLogPathName, targetFilePath, err)
	} else {
		logrus.Infof("Moved processed file from %s to %s", absoluteLogPathName, targetFilePath)
	}

	return nil
}

// Any session change triggers sessiondir updates on all head and worker nodes,
// so we only need to update from one node.
// for example:
//
//	my-cluster_abc123/
//		session_2024-12-15_10-30-45_123456    ← Empty file! The path itself is the information
//		session_2024-12-15_14-20-10_789012
func (r *RayLogHandler) WatchSessionLatestLoops() {
	sessionLatestDir := utils.GetTmpRayRoot()
	sessionLatestSymlink := filepath.Join(sessionLatestDir, "session_latest")
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Errorf("Failed to create fsnotify watcher for session_latest: %v", err)
		return
	}
	defer watcher.Close()

	// Add the session_latest directory to watcher
	if err := watcher.Add(sessionLatestDir); err != nil {
		logrus.Errorf("Failed to add %s to watcher: %v", sessionLatestDir, err)
		return
	}

	logrus.Infof("Started watching session_latest directory: %s", sessionLatestDir)
	for {
		select {
		case <-r.ShutdownChan:
			logrus.Info("Received shutdown signal, stopping session_latest watcher")
			return
		case event, ok := <-watcher.Events:
			if !ok {
				logrus.Warn("Session latest watcher events channel closed")
				return
			}

			logrus.Infof("File system event in session_latest: %s %s", event.Op, event.Name)
			if event.Name == sessionLatestSymlink {
				continue
			}
			rel, err := filepath.Rel(sessionLatestDir, event.Name)
			if err != nil {
				logrus.Errorf("Failed to get relative path for %s: %s", event.Name, err)
				continue
			}
			if !strings.Contains(rel, "session_") {
				logrus.Infof("Skip to process file: %s", event.Name)
				continue
			}

			// Handle changes to the symlink
			if event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
				sessionID := filepath.Base(event.Name)
				metafile := clustermetadata.EncodePath(
					utils.ClusterInfo{
						Name:      r.RayClusterName,
						Namespace: r.RayClusterNamespace,
						OwnerKind: r.OwnerKind,
						OwnerName: r.OwnerName},
					r.RootDir,
					sessionID,
				)
				if err := r.Writer.CreateDirectory(path.Dir(metafile)); err != nil {
					logrus.Errorf("Failed to create directory %s error %v", path.Dir(metafile), err)
					return
				}
				if err := r.Writer.WriteFile(metafile, strings.NewReader("")); err != nil {
					logrus.Errorf("Failed to write session file %s error %v", metafile, err)
					return
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				logrus.Warn("Session latest watcher errors channel closed")
				return
			}
			logrus.Errorf("Session latest watcher error: %v", err)
		}
	}
}

// Polls if the active session changes, when it does, it moves the old session logs to a prev-logs/ folder.
func (r *RayLogHandler) PollActiveSessionChanges() {
	tmpRayRoot := utils.GetTmpRayRoot()
	symlinkPath := filepath.Join(tmpRayRoot, sessionLatestLinkName)

	// Run has already started rotated protection for the session the handler was
	// configured with, under the node ID main.go discovered and validated for it before
	// the handler existed, so the transition starts life already handed off for it.
	//
	// The startup directory is resolved first, because it is compared against a
	// resolved symlink target on every observation. The configured value and that
	// target are routinely two spellings of one directory — a symlinked /tmp, or macOS
	// putting /var behind /private/var — and treating that as a session change would
	// discard a node ID that was verified for exactly this session and leave the
	// runtime pretending it has never seen one.
	startupDir := strings.TrimSpace(r.SessionDir)
	if resolved, err := filepath.EvalSymlinks(startupDir); err == nil && resolved != "" {
		startupDir = resolved
	}
	st := sessionTransition{
		dir:       startupDir,
		node:      strings.TrimSpace(r.GetRayNodeName()),
		handedOff: true,
	}

	// The first observation is not deferred by a tick. A session that changed between
	// the handler being configured and this goroutine starting has logs to relocate
	// now, which is what the startup block here has always done.
	//
	// The fallback is startupDir, not the raw SessionDir it was derived from, so that it
	// is the same string st.dir holds. advanceSession compares the two directly, and the
	// raw value is routinely a second spelling of the resolved one — utils.GetSessionDir
	// falls back to os.Readlink, which resolves only the session_latest link and leaves
	// any symlinked component above it in place. Passing that spelling here would look
	// like a session change and relocate the logs of the session that is still running.
	// Nothing is lost by declining to act: SessionDir names the session this poller
	// started with, so it can never be evidence of a new one, and the first tick that
	// resolves session_latest observes any real change.
	currentActiveDir, err := filepath.EvalSymlinks(symlinkPath)
	if err != nil || currentActiveDir == "" {
		logrus.Warnf("PollActiveSessionChanges: failed to resolve initial session_latest target: %v. Falling back to startup session directory %s", err, startupDir)
		currentActiveDir = startupDir
	}
	r.advanceSession(&st, currentActiveDir)

	interval := r.sessionPollInterval
	if interval <= 0 {
		interval = defaultSessionPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logrus.Infof("Started polling active session changes at: %s (initial target: %s)", symlinkPath, st.dir)
	for {
		select {
		case <-r.ShutdownChan:
			logrus.Info("PollActiveSessionChanges: stopping active session poller")
			return
		case <-ticker.C:
			newResolvedDir, err := filepath.EvalSymlinks(symlinkPath)
			if err != nil || newResolvedDir == "" {
				continue
			}
			r.advanceSession(&st, newResolvedDir)
		}
	}
}
