// Package processor implements the per-session pipeline used by the History
// Server v2 beta event processor. Each session is first classified (live vs
// dead) by querying the Kubernetes API for the owning RayCluster CR, then —
// if dead and not already snapshotted — its raw events are parsed into a
// SessionSnapshot and persisted to object storage.
//
// Design references:
//   - implementation_plan.md §Phase 3.1 (full spec for this file)
//   - implementation_plan.md §5 (state machine: LIVE -> DEAD,UN-SNAPPED -> DEAD,SNAPPED)
//   - implementation_plan.md §9 decision #5 (why K8s API, not marker file)
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

	"github.com/ray-project/kuberay/historyserver/beta/pkg/metrics"
	"github.com/ray-project/kuberay/historyserver/beta/pkg/snapshot"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// SessionStatus is the outcome classification returned by processSession. The
// Processor loop consumes this to drive per-status metric labels (see
// processor.go runOnce) without peeking into error strings. Callers should
// treat "error" and "not processed this tick" as independent concerns: every
// non-Live / non-AlreadySnapped / non-Processed status pairs with a non-nil
// error, and vice-versa the three terminal states always return nil error.
type SessionStatus int

const (
	// SessionStatusLive — RayCluster CR is still present; session intentionally
	// skipped. Not an error.
	SessionStatusLive SessionStatus = iota
	// SessionStatusAlreadySnapped — snapshot already exists in storage; the
	// skip-if-exists guard fired. Not an error.
	SessionStatusAlreadySnapped
	// SessionStatusProcessed — events parsed and snapshot written successfully.
	SessionStatusProcessed
	// SessionStatusK8sProbeErr — K8s API Get on the RayCluster CR returned a
	// non-NotFound error (e.g. API server outage). State unknown; retry later.
	SessionStatusK8sProbeErr
	// SessionStatusEventsErr — event parsing failed. Most likely a malformed
	// event file; surfaces for operator attention.
	SessionStatusEventsErr
	// SessionStatusSnapshotWriteErr — object-store PUT failed. The next tick
	// will retry thanks to skip-if-exists still allowing a write.
	SessionStatusSnapshotWriteErr
)

// Pipeline processes a single Ray session end-to-end: dead detection,
// skip-if-exists, event parsing, snapshot write.
//
// A Pipeline is stateless across sessions; it is safe to share one instance
// across many processSession calls (concurrently or serially). All mutable
// state lives in per-call EventHandler instances that do not escape
// processSession.
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

// processSession processes one session end-to-end. Returns a (SessionStatus,
// error) pair:
//
//   - (Live, nil) / (AlreadySnapped, nil): intentional no-op; caller moves on.
//   - (Processed, nil): snapshot written.
//   - (*Err, non-nil): a real failure; caller logs. The processor loop treats
//     this as non-fatal and continues with other sessions so a single bad
//     session does not block an entire tick.
//
// Wall time is always recorded to metrics.SessionDuration regardless of
// outcome — a stuck session with a slow K8s probe is still signal.
func (p *Pipeline) processSession(ctx context.Context, session utils.ClusterInfo) (SessionStatus, error) {
	start := time.Now()
	defer func() { metrics.SessionDuration.Observe(time.Since(start).Seconds()) }()

	clusterNameID := session.Name + "_" + session.Namespace

	// Step 1: Dead detection via K8s API.
	// NotFound == dead (CR deleted). Other errors are propagated so the
	// caller logs them and retries next tick; we never treat an unknown
	// K8s state as "dead" because that would incorrectly snapshot a live
	// cluster mid-outage.
	dead, err := p.isDead(ctx, session)
	if err != nil {
		return SessionStatusK8sProbeErr, fmt.Errorf("k8s probe for %s/%s: %w", session.Namespace, session.Name, err)
	}
	if !dead {
		return SessionStatusLive, nil
	}

	// Step 2: Skip-if-exists. Snapshots are immutable; once written, we
	// never overwrite. A nil reader here means the object is absent.
	if p.snapshotExists(clusterNameID, session.SessionName) {
		return SessionStatusAlreadySnapped, nil
	}

	// Step 3: Parse raw events into an in-memory handler. We create a
	// fresh EventHandler per session to keep memory bounded; this is also
	// what makes the method safe to call concurrently in future.
	h := eventserver.NewEventHandler(p.reader)
	if err := h.ProcessSingleSession(session); err != nil {
		return SessionStatusEventsErr, fmt.Errorf("process events for %s/%s: %w", session.Namespace, session.Name, err)
	}

	// Step 4: Serialize handler state into the snapshot schema and PUT.
	snap := buildSnapshotFromHandler(h, session)
	if err := p.writeSnapshot(clusterNameID, session.SessionName, snap); err != nil {
		return SessionStatusSnapshotWriteErr, err
	}
	return SessionStatusProcessed, nil
}

// isDead queries the Kubernetes API for the owning RayCluster CR.
//
// Return semantics:
//   - (true,  nil): CR is absent -> the cluster is dead.
//   - (false, nil): CR exists    -> the cluster is live.
//   - (false, err): other error  -> state is unknown; caller must skip
//     this session for this tick and retry later.
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
// a crash mid-PUT cannot leave a partially-written object; the next tick's
// skip-if-exists will then re-drive the write.
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
