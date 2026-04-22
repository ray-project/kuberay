package processor

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/beta/pkg/metrics"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// DefaultInterval is the default scan interval. Plan §9 decision #11 = 10 minutes.
const DefaultInterval = 10 * time.Minute

// sessionProcessor is the subset of Pipeline that Processor needs.
// Defined as a package-private interface so tests can inject a fake.
// *Pipeline (from session.go) satisfies it automatically.
//
// processSession returns both a status (for metric labeling) and an error
// (for logging). The two are intentionally decoupled: status is a closed
// enum and is always set, error is nil for the three terminal states
// (Live, AlreadySnapped, Processed) and non-nil for the *Err variants.
type sessionProcessor interface {
	processSession(ctx context.Context, session utils.ClusterInfo) (SessionStatus, error)
}

// Processor drives periodic scans of all sessions in storage and delegates
// per-session work to the injected sessionProcessor.
//
// Lifecycle:
//   - Run(stop) starts a ticker loop, runs one iteration immediately,
//     then on every tick. Returns when stop is closed.
//   - Errors from processSession are logged but never abort the outer loop;
//     a bad session should not prevent good sessions from being processed.
type Processor struct {
	pipeline sessionProcessor
	reader   storage.StorageReader // used for List()
	interval time.Duration
}

// NewProcessor wires a Processor. interval <= 0 uses DefaultInterval.
func NewProcessor(pipeline sessionProcessor, reader storage.StorageReader, interval time.Duration) *Processor {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Processor{pipeline: pipeline, reader: reader, interval: interval}
}

// Run blocks until stop is closed. One iteration runs immediately at start
// (useful for fresh deployments — no need to wait for the first tick).
func (pr *Processor) Run(stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logrus.Infof("processor starting: interval=%s", pr.interval)
	pr.runOnce(ctx)

	ticker := time.NewTicker(pr.interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			logrus.Info("processor stop signal received, exiting")
			return
		case <-ticker.C:
			pr.runOnce(ctx)
		}
	}
}

// runOnce scans the storage once and dispatches processSession per session.
// Per-session errors are logged and skipped — never abort the whole pass.
//
// Metric recording happens at the tick boundary (SessionsScanned,
// LastTickTimestamp, OrphanSessions) and per-session (SessionsProcessed,
// SessionsSkipped, SessionErrors). OrphanSessions is defined here as "sessions
// that SHOULD have been snapped this tick but weren't due to an error" — i.e.
// the three error variants. Live / already-snapped sessions are not orphans;
// they are the steady-state outcome. See plan §7 Open Risk #8 for why we
// surface this gauge rather than a counter.
func (pr *Processor) runOnce(ctx context.Context) {
	sessions := pr.reader.List()
	logrus.Infof("processor tick: scanning %d sessions", len(sessions))
	metrics.SessionsScanned.Add(float64(len(sessions)))

	errored := 0
	orphansThisTick := 0
	for _, s := range sessions {
		status, err := pr.pipeline.processSession(ctx, s)
		if err != nil {
			logrus.Errorf("processSession %s/%s/%s: %v", s.Namespace, s.Name, s.SessionName, err)
			errored++
		}
		switch status {
		case SessionStatusLive:
			metrics.SessionsSkipped.WithLabelValues("live").Inc()
		case SessionStatusAlreadySnapped:
			metrics.SessionsSkipped.WithLabelValues("already_snapped").Inc()
		case SessionStatusProcessed:
			metrics.SessionsProcessed.Inc()
		case SessionStatusK8sProbeErr:
			metrics.SessionErrors.WithLabelValues("k8s_probe").Inc()
			orphansThisTick++
		case SessionStatusEventsErr:
			metrics.SessionErrors.WithLabelValues("events").Inc()
			orphansThisTick++
		case SessionStatusSnapshotWriteErr:
			metrics.SessionErrors.WithLabelValues("snapshot_write").Inc()
			orphansThisTick++
		}
	}

	metrics.OrphanSessions.Set(float64(orphansThisTick))
	metrics.LastTickTimestamp.SetToCurrentTime()

	logrus.Infof("processor tick complete: scanned=%d errored=%d orphans=%d",
		len(sessions), errored, orphansThisTick)
}
