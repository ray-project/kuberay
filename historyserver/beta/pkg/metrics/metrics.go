// Package metrics centralizes Prometheus metric declarations for the v2 beta
// eventprocessor + historyserver. Both binaries register these metrics at
// startup and expose them via /metrics (HS on :8080, processor on a sidecar
// :9090). See implementation_plan.md §Phase 6 and §7 Open Risks #1 and #8 for
// the motivating scenarios — without these signals an operator cannot tell if
// the processor is stuck, if cache is saving object-store calls, or if orphan
// (dead-but-unsnapped) sessions are accumulating.
//
// Design notes:
//   - promauto registers every metric against prometheus.DefaultRegisterer at
//     package init. That is exactly what we want: /metrics handler uses the
//     same default gatherer, so the set is observable without a manual
//     Register call at each call-site. It also means every test run shares
//     process-wide counter state — tests that assert absolute values use
//     testutil.ToFloat64 against a baseline delta instead of comparing to 0.
//   - Names use the `processor_` / `server_` prefix (not `beta_*`) because
//     downstream scrape configs key off the binary identity, not the release
//     channel. When beta graduates to GA these names stay stable.
//   - Labels are intentionally tiny (reason / stage / kind) so cardinality
//     stays O(1) per metric. Plan §Phase 6 explicitly calls out not adding
//     per-session or per-endpoint labels.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ---- Processor metrics ------------------------------------------------------

var (
	// SessionsScanned counts every session the processor pulled from
	// storage.List on a tick, regardless of whether it was processed.
	SessionsScanned = promauto.NewCounter(prometheus.CounterOpts{
		Name: "processor_sessions_scanned_total",
		Help: "Total sessions listed by the processor across all ticks.",
	})

	// SessionsProcessed counts sessions that completed ProcessSingleSession
	// AND had a snapshot successfully written. Live sessions and
	// already-snapped sessions do not count here — see SessionsSkipped.
	SessionsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "processor_sessions_processed_total",
		Help: "Sessions that completed ProcessSingleSession and had a snapshot written.",
	})

	// SessionsSkipped counts sessions the processor intentionally passed on.
	// reason="live"           -> RayCluster CR still present.
	// reason="already_snapped" -> snapshot object already exists in storage.
	SessionsSkipped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "processor_sessions_skipped_total",
		Help: "Sessions skipped by the processor, labeled by reason.",
	}, []string{"reason"})

	// SessionDuration records the wall time a single processSession call
	// takes end-to-end. Exponential buckets (0.1s..~51s) cover the common
	// case (a fast skip is ~1ms, a full parse + S3 write is a few seconds)
	// without saturating the top bucket under normal operation.
	SessionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "processor_session_duration_seconds",
		Help:    "Wall time of processSession.",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
	})

	// SessionErrors counts errors by pipeline stage. Keep the stage set small
	// and bounded — matches the SessionStatus error variants in
	// processor/session.go so dashboards can key on the same names.
	SessionErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "processor_session_errors_total",
		Help: "Processor errors, labeled by pipeline stage.",
	}, []string{"stage"})

	// LastTickTimestamp is set to wall time at the end of every tick. An
	// operator can alert on (time() - processor_last_tick_timestamp_seconds)
	// to detect a stuck processor, which addresses plan §7 Open Risk #1.
	LastTickTimestamp = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "processor_last_tick_timestamp_seconds",
		Help: "Unix timestamp of the last completed processor tick.",
	})

	// OrphanSessions tracks sessions whose RayCluster CR is gone but whose
	// snapshot has not yet landed — i.e. the processor tried and failed this
	// tick. Addresses plan §7 Open Risk #8.
	OrphanSessions = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "processor_orphan_sessions",
		Help: "Sessions where the RayCluster CR is gone but no snapshot exists yet, as of the latest tick.",
	})
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
	//   kind="not_found" -> ErrSnapshotNotFound (expected for a dead-but-
	//                       unsnapped session; the 503 path).
	//   kind="other"     -> read / decode failures (unexpected).
	SnapshotFetchErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "server_snapshot_fetch_errors_total",
		Help: "Errors fetching snapshots from storage.",
	}, []string{"kind"})

	// MissingSnapshot503 counts 503 responses returned by
	// handleMissingSnapshot — a direct view of how often the frontend is
	// told "retry after 10 min".
	MissingSnapshot503 = promauto.NewCounter(prometheus.CounterOpts{
		Name: "server_missing_snapshot_503_total",
		Help: "Responses returned as 503 due to missing snapshot.",
	})
)

// Handler returns the /metrics HTTP handler backed by the default gatherer.
// Both the historyserver's restful router and the eventprocessor's standalone
// mux mount this handler at /metrics.
func Handler() http.Handler {
	return promhttp.Handler()
}
