// Package processor implements the per-session pipeline that classifies a
// session as live or dead and, when dead, parses raw events into a
// SessionSnapshot persisted to object storage.
//
// Pipeline.ProcessSession is driven on-demand by the HTTP Supervisor when
// /enter_cluster hits a dead session.
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

// SessionStatus is ProcessSession's outcome classification. Success statuses
// (Live, AlreadySnapped, Processed) return a nil error; all others pair with
// a non-nil error.
type SessionStatus int

const (
	// SessionStatusLive means the RayCluster CR is still present and the
	// session is intentionally skipped.
	SessionStatusLive SessionStatus = iota
	// SessionStatusAlreadySnapped means a snapshot already exists in storage.
	SessionStatusAlreadySnapped
	// SessionStatusProcessed means events were parsed and the snapshot was
	// written. The returned *SessionSnapshot is non-nil only for this status.
	SessionStatusProcessed
	// SessionStatusK8sProbeErr means the K8s Get returned a non-NotFound
	// error and the cluster state is unknown.
	SessionStatusK8sProbeErr
	// SessionStatusEventsErr means event parsing failed.
	SessionStatusEventsErr
	// SessionStatusSnapshotWriteErr means the object-store PUT failed.
	SessionStatusSnapshotWriteErr
	// SessionStatusCanceled means ctx was canceled mid-pipeline; not an *Err
	// status.
	SessionStatusCanceled
)

// Pipeline processes a single Ray session end-to-end: dead detection,
// skip-if-exists, event parsing, and snapshot write. It is stateless across
// sessions and safe for concurrent use.
type Pipeline struct {
	reader    storage.StorageReader
	writer    storage.StorageWriter
	k8sClient client.Client

	// rootDir is the storage prefix prepended on writes to mirror reader layout.
	rootDir string
}

// NewPipeline constructs a Pipeline. All collaborators must be non-nil; an
// empty rootDir means no prefix.
func NewPipeline(reader storage.StorageReader, writer storage.StorageWriter, k8sClient client.Client, rootDir string) *Pipeline {
	return &Pipeline{
		reader:    reader,
		writer:    writer,
		k8sClient: k8sClient,
		rootDir:   rootDir,
	}
}

// ProcessSession processes one session end-to-end and returns the outcome
// classification, the built snapshot (when applicable), and an error.
//
//   - (Live, nil, nil): no-op; caller moves on.
//   - (AlreadySnapped, nil, nil): skip-if-exists fired; caller moves on.
//   - (Processed, snap, nil): snapshot built and written; snap is the exact
//     object persisted and is never nil for this status.
//   - (K8sProbeErr | EventsErr | SnapshotWriteErr, nil, err): real failure.
//   - (Canceled, nil, ctx.Err()): ctx was canceled between steps.
//
// ctx is polled at each step boundary; cancellation surfaces as Canceled.
// Wall time is always recorded to metrics.SessionDuration regardless of outcome.
func (p *Pipeline) ProcessSession(ctx context.Context, session utils.ClusterInfo) (SessionStatus, *snapshot.SessionSnapshot, error) {
	start := time.Now()
	defer func() { metrics.SessionDuration.Observe(time.Since(start).Seconds()) }()

	clusterNameID := session.Name + "_" + session.Namespace

	// Fast-fail if the request was canceled before we started.
	if err := ctx.Err(); err != nil {
		return SessionStatusCanceled, nil, err
	}

	// Step 1: dead detection. NotFound means dead; other errors propagate.
	// Treating unknown state as dead would snapshot a live cluster.
	dead, err := p.isDead(ctx, session)
	if err != nil {
		// Distinguish ctx cancellation from real API errors to keep alerting noise-free.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return SessionStatusCanceled, nil, ctxErr
		}
		return SessionStatusK8sProbeErr, nil, fmt.Errorf("k8s probe for %s/%s: %w", session.Namespace, session.Name, err)
	}
	if !dead {
		return SessionStatusLive, nil, nil
	}

	// Step 2: skip-if-exists. Snapshots are immutable.
	if err := ctx.Err(); err != nil {
		return SessionStatusCanceled, nil, err
	}
	if p.snapshotExists(clusterNameID, session.SessionName) {
		return SessionStatusAlreadySnapped, nil, nil
	}

	// Step 3: parse events with a fresh handler per call (no cross-session state).
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

	// Step 4: serialize and PUT. The returned *SessionSnapshot lets the caller
	// prime the LRU.
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
//   - (true,  nil): CR is absent; the cluster is dead.
//   - (false, nil): CR exists; the cluster is live.
//   - (false, err): unknown state; caller should skip and retry later.
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

// snapshotExists reports whether the session's snapshot already exists in
// storage.
func (p *Pipeline) snapshotExists(clusterNameID, sessionName string) bool {
	return p.reader.GetContent(clusterNameID, snapshot.SnapshotPath(sessionName)) != nil
}

// writeSnapshot marshals snap and performs a single atomic PUT.
func (p *Pipeline) writeSnapshot(clusterNameID, sessionName string, snap *snapshot.SessionSnapshot) error {
	body, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot for %s/%s: %w", clusterNameID, sessionName, err)
	}
	dst := path.Join(p.rootDir, clusterNameID, snapshot.SnapshotPath(sessionName))
	if err := p.writer.WriteFile(dst, bytes.NewReader(body)); err != nil {
		return fmt.Errorf("write snapshot %s: %w", dst, err)
	}
	logrus.Infof("wrote snapshot for %s/%s (%d bytes)", clusterNameID, sessionName, len(body))
	return nil
}

// buildSnapshotFromHandler flattens handler state into a SessionSnapshot. It
// uses the handler's getters, which lock and return deep copies, so callers
// may still hold a reference to the handler.
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
