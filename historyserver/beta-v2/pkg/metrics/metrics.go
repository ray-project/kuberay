// Package metrics centralizes Prometheus metric declarations for the History
// Server. All metrics register against prometheus.DefaultRegisterer at
// package init and are exposed via /metrics on the main HTTP listener.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ---- Processor metrics ------------------------------------------------------

var (
	// SessionsProcessed counts sessions whose Pipeline returned
	// SessionStatusProcessed (snapshot written). Live / already-snapped
	// outcomes go to SessionsSkipped instead.
	SessionsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "processor_sessions_processed_total",
		Help: "Sessions that completed ProcessSingleSession and had a snapshot written.",
	})

	// SessionsSkipped counts sessions the pipeline intentionally passed on.
	// reason="live"            -> RayCluster CR still present.
	// reason="already_snapped" -> snapshot object already exists in storage.
	SessionsSkipped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "processor_sessions_skipped_total",
		Help: "Sessions skipped by the pipeline, labeled by reason.",
	}, []string{"reason"})

	// SessionDuration records the wall time of a single ProcessSession call.
	// Exponential buckets (0.1s..~51s) cover both fast skips (~ms) and full
	// parse + S3 write (~seconds) without saturation.
	SessionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "processor_session_duration_seconds",
		Help:    "Wall time of ProcessSession.",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
	})

	// SessionErrors counts errors by pipeline stage. Stage names mirror the
	// SessionStatus error variants in processor/session.go.
	//
	// No "canceled" stage: client cancellation is expected and should not page
	// on-call; Pipeline returns SessionStatusCanceled without hitting this.
	SessionErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "processor_session_errors_total",
		Help: "Pipeline errors, labeled by pipeline stage (k8s_probe / events / snapshot_write).",
	}, []string{"stage"})
)

// ---- Server metrics ---------------------------------------------------------

var (
	// CacheHits counts snapshot-loader LRU hits.
	CacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "server_cache_hits_total",
		Help: "Snapshot loader cache hits.",
	})

	// CacheMisses counts snapshot-loader LRU misses (every miss triggers
	// exactly one storage fetch; see cache.go).
	CacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "server_cache_misses_total",
		Help: "Snapshot loader cache misses.",
	})

	// SnapshotFetchErrors counts storage fetch failures by kind:
	//   kind="not_found" -> ErrSnapshotNotFound; expected for dead-but-unsnapped
	//                       sessions, served as 503.
	//   kind="other"     -> read/decode failures; Supervisor treats these as
	//                       transient (no Pipeline retry), surfaced as 500.
	SnapshotFetchErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "server_snapshot_fetch_errors_total",
		Help: "Errors fetching snapshots from storage.",
	}, []string{"kind"})

	// MissingSnapshot503 counts 503 responses returned by handleMissingSnapshot.
	// Fires when a handler is hit without a prior /enter_cluster (e.g. a link
	// shared with stale cookies).
	MissingSnapshot503 = promauto.NewCounter(prometheus.CounterOpts{
		Name: "server_missing_snapshot_503_total",
		Help: "Responses returned as 503 due to missing snapshot.",
	})

	// EnterClusterTotal counts /enter_cluster outcomes for dead sessions:
	//   status="ok"    -> Supervisor.Ensure returned nil.
	//   status="error" -> Supervisor.Ensure returned an error (bubbled to 500).
	// The "live" sentinel and nil-supervisor fast paths are not counted.
	EnterClusterTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "server_enter_cluster_total",
		Help: "Outcomes of /enter_cluster calls that entered the Supervisor (dead sessions only).",
	}, []string{"status"})

	// EnterClusterDuration records the wall time of Supervisor.Ensure per
	// /enter_cluster request. Exponential buckets (0.01s..~10s) cover both fast
	// LRU hits (~ms) and cold-path Pipeline runs (~seconds) without saturation.
	EnterClusterDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "server_enter_cluster_duration_seconds",
		Help:    "Wall time of Supervisor.Ensure per /enter_cluster request.",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
	})

	// SingleflightDedupTotal counts callers in a coalesced singleflight group
	// inside Supervisor.Ensure (i.e. another caller was concurrently waiting on
	// the same session).
	//
	// singleflight.Result.Shared is true for EVERY participant including the
	// winner, so this counts participants — not followers. N coalesced callers
	// increment by N, which serves as a proxy for "Pipeline work saved".
	SingleflightDedupTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "server_singleflight_dedup_total",
		Help: "Callers whose Ensure result was coalesced with at least one other concurrent caller.",
	})
)

// Handler returns the /metrics HTTP handler backed by the default gatherer.
func Handler() http.Handler {
	return promhttp.Handler()
}
