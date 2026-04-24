// Package processor implements the per-session pipeline used by the History
// Server v2 beta-v2 (lazy mode). Each session is first classified (live vs
// dead) by querying the Kubernetes API for the owning RayCluster CR, then —
// if dead and not already snapshotted — its raw events are parsed into a
// SessionSnapshot and persisted to object storage.
//
// Relative to beta (ticker mode), beta-v2 drops the standalone Processor
// loop: Pipeline.ProcessSession is driven on-demand by the HTTP Supervisor
// (pkg/server/enter_cluster.go) when a user visits /enter_cluster on a dead
// session. The per-session body is otherwise unchanged — same state machine,
// same skip-if-exists write, same K8s dead-detection. See beta_poc.md §1/§2
// for the lazy-mode sequence diagram.
//
// Design references:
//   - historyserver/beta_poc.md §4 (exported ProcessSession signature)
//   - historyserver/beta_poc.md §8 risk #1 (why ctx.Err() between steps)
//   - historyserver/beta/implementation_plan.md §5 (state machine, unchanged)
//   - historyserver/beta/implementation_plan.md §9 decision #5 (why K8s API,
//     not marker file)
package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"time"

	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/metrics"
	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/snapshot"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// SessionStatus is the outcome classification returned by ProcessSession. The
// Supervisor consumes this to drive per-status metric labels (see
// enter_cluster.go) without peeking into error strings. Callers should treat
// "error" and "not processed this call" as independent concerns: every
// non-Live / non-AlreadySnapped / non-Processed status pairs with a non-nil
// error, and vice-versa the three terminal success states always return nil
// error.
type SessionStatus int

const (
	// SessionStatusLive — RayCluster CR is still present; session intentionally
	// skipped. Not an error. Supervisor will not PUT a snapshot.
	SessionStatusLive SessionStatus = iota
	// SessionStatusAlreadySnapped — snapshot already exists in storage; the
	// skip-if-exists guard fired. Not an error.
	SessionStatusAlreadySnapped
	// SessionStatusProcessed — events parsed and snapshot written successfully.
	// The returned *SessionSnapshot is non-nil only for this status so the
	// caller (Supervisor) can Prime the loader's LRU without an extra S3 GET.
	SessionStatusProcessed
	// SessionStatusK8sProbeErr — K8s API Get on the RayCluster CR returned a
	// non-NotFound error (e.g. API server outage). State unknown; retry later.
	SessionStatusK8sProbeErr
	// SessionStatusEventsErr — event parsing failed. Most likely a malformed
	// event file; surfaces for operator attention.
	SessionStatusEventsErr
	// SessionStatusSnapshotWriteErr — object-store PUT failed. A subsequent
	// /enter_cluster call re-drives the pipeline; skip-if-exists still permits
	// the write since a failed PUT leaves no object behind.
	SessionStatusSnapshotWriteErr
	// SessionStatusCanceled — ctx was canceled mid-pipeline (e.g. HTTP client
	// disconnected or Supervisor followers hung up while waiting on the
	// singleflight winner). WHY a dedicated status: lazy mode is request-
	// driven, so client cancellation is both common and benign — operators
	// should NOT be paged for it. Having a distinct status keeps it out of the
	// "real failure" stage labels (k8s_probe / events / snapshot_write) that
	// feed the SessionErrors alerting signal.
	SessionStatusCanceled
)

// Pipeline processes a single Ray session end-to-end: dead detection,
// skip-if-exists, event parsing, snapshot write.
//
// A Pipeline is stateless across sessions; it is safe to share one instance
// across many ProcessSession calls (concurrently or serially). All mutable
// state lives in per-call EventHandler instances that do not escape
// ProcessSession. In beta-v2 the Supervisor serializes concurrent callers
// for the same session via singleflight, but Pipeline itself makes no such
// assumption — parallel calls for distinct sessions are always safe.
type Pipeline struct {
	reader    storage.StorageReader
	writer    storage.StorageWriter
	k8sClient client.Client

	// rootDir is the object-store prefix (e.g. "log") that v1's StorageReader
	// auto-prepends in GetContent but StorageWriter.WriteFile does NOT. We
	// prepend it manually in writeSnapshot so the written key matches what
	// the reader will later look up. See pkg/storage/s3/s3.go: WriteFile vs
	// GetContent for the asymmetry.
	rootDir string
}

// NewPipeline constructs a Pipeline. Any of the three collaborators must be
// non-nil; nil inputs are treated as programmer errors and will panic on first
// use rather than silently skipping sessions. rootDir is the configured
// storage prefix (same value passed to the reader/writer factory as RootDir);
// empty string means "no prefix" which matches backends that have no rootDir.
func NewPipeline(reader storage.StorageReader, writer storage.StorageWriter, k8sClient client.Client, rootDir string) *Pipeline {
	return &Pipeline{
		reader:    reader,
		writer:    writer,
		k8sClient: k8sClient,
		rootDir:   rootDir,
	}
}

// ProcessSession processes one session end-to-end. Returns
// (SessionStatus, *SessionSnapshot, error):
//
//   - (Live,            nil,  nil): intentional no-op; caller moves on.
//   - (AlreadySnapped,  nil,  nil): skip-if-exists fired; caller moves on.
//   - (Processed,       snap, nil): snapshot built AND written. snap is the
//     pointer callers may Prime into the LRU; it is the exact object
//     persisted to S3. Never nil when status == Processed.
//   - (K8sProbeErr / EventsErr / SnapshotWriteErr, nil, non-nil err):
//     a real failure; Supervisor bubbles this to HTTP 500.
//   - (Canceled,        nil,  ctx.Err()): ctx was canceled between steps
//     (e.g. HTTP client disconnected).
//
// WHY ctx.Err() checks between steps and not a context-aware storage layer:
// v1 StorageReader / StorageWriter / EventHandler.ProcessSingleSession do
// NOT take context. Instead of forcing a v1 API change we poll ctx at each
// pipeline boundary. Granularity is coarse (one step ≈ a few seconds), which
// is fine for lazy mode's goal of releasing the HTTP goroutine promptly when
// the client disconnects. See beta_poc.md §8 risk #1.
//
// Wall time is always recorded to metrics.SessionDuration regardless of
// outcome — a stuck session with a slow K8s probe is still signal.
func (p *Pipeline) ProcessSession(ctx context.Context, session utils.ClusterInfo) (SessionStatus, *snapshot.SessionSnapshot, error) {
	start := time.Now()
	defer func() { metrics.SessionDuration.Observe(time.Since(start).Seconds()) }()

	clusterNameID := session.Name + "_" + session.Namespace

	// Early ctx check: a request that was already canceled before we even
	// started (e.g. Supervisor follower whose client hung up while waiting
	// on the singleflight winner) must not spend a K8s API call.
	if err := ctx.Err(); err != nil {
		return SessionStatusCanceled, nil, err
	}

	// Step 1: Dead detection via K8s API.
	// NotFound == dead (CR deleted). Other errors are propagated so the
	// caller logs them and retries on a subsequent request; we never treat
	// an unknown K8s state as "dead" because that would incorrectly
	// snapshot a live cluster mid-outage.
	dead, err := p.isDead(ctx, session)
	if err != nil {
		// Distinguish ctx cancellation from real API errors. controller-
		// runtime's Get returns a wrapped ctx.Err() when the caller's ctx
		// is done; surfacing that as Canceled (not K8sProbeErr) keeps
		// operator alerting noise-free.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return SessionStatusCanceled, nil, ctxErr
		}
		return SessionStatusK8sProbeErr, nil, fmt.Errorf("k8s probe for %s/%s: %w", session.Namespace, session.Name, err)
	}
	if !dead {
		return SessionStatusLive, nil, nil
	}

	// Step 2: Skip-if-exists. Snapshots are immutable; once written, we
	// never overwrite. A nil reader from GetContent means the object is
	// absent.
	if err := ctx.Err(); err != nil {
		return SessionStatusCanceled, nil, err
	}
	if p.snapshotExists(clusterNameID, session.SessionName) {
		return SessionStatusAlreadySnapped, nil, nil
	}

	// Step 3: Parse raw events into an in-memory handler. We create a fresh
	// EventHandler per call to keep memory bounded and so that a Supervisor
	// handling multiple sessions concurrently does not share handler state
	// across them.
	//
	// NOTE: eventserver.EventHandler.ProcessSingleSession does NOT accept a
	// context; once we call it, ctx cancellation cannot interrupt the read
	// loop mid-file. We accept this granularity — a single session's event
	// files are bounded in size, so worst case the goroutine drains within
	// a few seconds after the client disconnects.
	if err := ctx.Err(); err != nil {
		return SessionStatusCanceled, nil, err
	}
	h := eventserver.NewEventHandler(p.reader)
	if err := h.ProcessSingleSession(session); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return SessionStatusCanceled, nil, ctxErr
		}
		return SessionStatusEventsErr, nil, fmt.Errorf("process events for %s/%s: %w", session.Namespace, session.Name, err)
	}

	// Step 4: Serialize handler state into the snapshot schema and PUT.
	// We return the built *SessionSnapshot so Supervisor can Prime the LRU,
	// avoiding a redundant S3 GET right after a successful write.
	if err := ctx.Err(); err != nil {
		return SessionStatusCanceled, nil, err
	}
	snap := buildSnapshotFromHandler(h, session)
	if err := p.writeSnapshot(clusterNameID, session.SessionName, snap); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return SessionStatusCanceled, nil, ctxErr
		}
		return SessionStatusSnapshotWriteErr, nil, err
	}
	return SessionStatusProcessed, snap, nil
}

// isDead queries the Kubernetes API for the owning RayCluster CR.
//
// Return semantics:
//   - (true,  nil): CR is absent -> the cluster is dead.
//   - (false, nil): CR exists    -> the cluster is live.
//   - (false, err): other error  -> state is unknown; caller must skip
//     this session for this call and retry later.
//
// Why K8s API and not a collector-written marker file: collectors run in
// every Ray pod (head + workers). Any pod SIGTERM (worker rolling update,
// autoscaler scale-down, node drain) would write a marker and falsely mark a
// live cluster dead. A RayCluster CR is deleted only by the operator at true
// end-of-life, giving us an authoritative dead signal. See
// implementation_plan.md §9 decision #5.
func (p *Pipeline) isDead(ctx context.Context, session utils.ClusterInfo) (bool, error) {
	rc := &rayv1.RayCluster{}
	err := p.k8sClient.Get(ctx, k8stypes.NamespacedName{
		Namespace: session.Namespace,
		Name:      session.Name,
	}, rc)
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// snapshotExists returns true if object storage already holds the session's
// processed snapshot. A nil io.Reader from GetContent is the storage layer's
// way of saying "absent".
func (p *Pipeline) snapshotExists(clusterNameID, sessionName string) bool {
	return p.reader.GetContent(clusterNameID, snapshot.SnapshotPath(sessionName)) != nil
}

// writeSnapshot marshals the SessionSnapshot and performs a single PUT.
// Object-store PUTs (S3, GCS, Azure blob) are atomic at the object level, so
// a crash mid-PUT cannot leave a partially-written object; a subsequent
// /enter_cluster call's skip-if-exists will then re-drive the write.
func (p *Pipeline) writeSnapshot(clusterNameID, sessionName string, snap *snapshot.SessionSnapshot) error {
	body, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot for %s/%s: %w", clusterNameID, sessionName, err)
	}
	// v1 StorageWriter.WriteFile takes the full absolute key (no auto-prepend),
	// while StorageReader.GetContent internally prepends rootDir + clusterId.
	// Mirror GetContent's layout here so a subsequent Load finds the object:
	//   key = {rootDir}/{clusterNameID}/{snapshot.SnapshotPath(sessionName)}
	dst := path.Join(p.rootDir, clusterNameID, snapshot.SnapshotPath(sessionName))
	if err := p.writer.WriteFile(dst, bytes.NewReader(body)); err != nil {
		return fmt.Errorf("write snapshot %s: %w", dst, err)
	}
	logrus.Infof("wrote snapshot for %s/%s (%d bytes)", clusterNameID, sessionName, len(body))
	return nil
}

// buildSnapshotFromHandler flattens the handler's per-map state into the
// SessionSnapshot schema. We use the handler's public getters (which
// internally take the appropriate locks and return deep copies), so this
// function is safe to call even if other goroutines still hold a reference
// to the handler.
func buildSnapshotFromHandler(h *eventserver.EventHandler, session utils.ClusterInfo) *snapshot.SessionSnapshot {
	key := utils.BuildClusterSessionKey(session.Name, session.Namespace, session.SessionName)

	return &snapshot.SessionSnapshot{
		SessionKey:  key,
		GeneratedAt: time.Now().UTC(),
		Tasks:       groupTasksByID(h.GetTasks(key)),
		Actors:      h.GetActorsMap(key),
		Jobs:        h.GetJobsMap(key),
		Nodes:       h.GetNodeMap(key),
		LogEvents: snapshot.LogEventPayload{
			ByJobID: h.ClusterLogEventMap.GetRawEventsByJobID(key),
		},
	}
}

// groupTasksByID re-nests the flat []Task returned by EventHandler.GetTasks
// into the map[taskID][]attempt shape expected by SessionSnapshot.Tasks.
//
// GetTasks already returns one entry per (taskID, attempt) pair — we just
// group by TaskID so the output matches v1's conceptual "TaskMap" layout
// without us having to reach into the handler's private locks.
func groupTasksByID(tasks []types.Task) map[string][]types.Task {
	if len(tasks) == 0 {
		return map[string][]types.Task{}
	}
	out := make(map[string][]types.Task, len(tasks))
	for _, t := range tasks {
		out[t.TaskID] = append(out[t.TaskID], t)
	}
	return out
}
