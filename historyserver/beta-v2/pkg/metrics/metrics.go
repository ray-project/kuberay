// Package metrics centralizes Prometheus metric declarations for the v2
// beta-v2 (lazy mode) History Server. All metrics are registered against
// prometheus.DefaultRegisterer at package init and exposed via /metrics on
// :8080 (there is no sidecar metrics port — beta-v2 is a single binary; see
// beta_poc.md §3). Operators use these signals to monitor the snapshot
// cache, the /enter_cluster blocking path, and per-session pipeline
// failures.
//
// Relative to beta (ticker mode) this file removes three ticker-only
// metrics and adds three lazy-mode metrics; see the inline "deleted" vs
// "added" commentary below for the exact list and why each change was
// made.
//
// Design notes:
//   - promauto registers every metric against prometheus.DefaultRegisterer at
//     package init. That is exactly what we want: the /metrics handler uses
//     the same default gatherer, so the set is observable without a manual
//     Register call at each call-site. It also means every test run shares
//     process-wide counter state — tests that assert absolute values use
//     testutil.ToFloat64 against a baseline delta instead of comparing to 0.
//   - Names use the `processor_` / `server_` prefix (not `beta_v2_*`) because
//     downstream scrape configs key off the binary identity, not the release
//     channel. When beta-v2 graduates to GA these names stay stable.
//   - Labels are intentionally tiny (reason / stage / kind / status) so
//     cardinality stays O(1) per metric. No per-session or per-endpoint
//     labels.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ---- Processor metrics ------------------------------------------------------
//
// The ticker-only metrics from beta are INTENTIONALLY removed in beta-v2:
//
//   - processor_sessions_scanned_total
//   - processor_last_tick_timestamp_seconds
//   - processor_orphan_sessions
//
// WHY: in lazy mode there is no periodic scan — work is driven by HTTP
// requests, so "sessions scanned per tick" and "time since last tick" no
// longer have meaning. Exposing zero-valued gauges under the same name
// would be worse than deleting: operators would write alerts expecting
// fresh signal and see a permanent outage.
//
// The per-session success / skip / error counters are KEPT because they
// still describe real per-session outcomes — beta-v2 just drives them from
// Supervisor.runOnce instead of Processor.runOnce.

var (
	// SessionsProcessed counts sessions whose Pipeline completed with
	// SessionStatusProcessed AND had a snapshot successfully written. Live
	// and already-snapped sessions do not count here — see SessionsSkipped.
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

	// SessionDuration records the wall time a single ProcessSession call
	// takes end-to-end. Exponential buckets (0.1s..~51s) cover the common
	// case (a fast skip is ~1ms, a full parse + S3 write is a few seconds)
	// without saturating the top bucket under normal operation.
	SessionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "processor_session_duration_seconds",
		Help:    "Wall time of ProcessSession.",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
	})

	// SessionErrors counts errors by pipeline stage. Keep the stage set small
	// and bounded — matches the SessionStatus error variants in
	// processor/session.go so dashboards can key on the same names. WHY no
	// "canceled" stage: client cancellation is expected in request-driven
	// mode and should NOT page on-call; Pipeline returns SessionStatusCanceled
	// without hitting this counter.
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
	//   kind="not_found" -> ErrSnapshotNotFound (expected for a dead-but-
	//                       unsnapped session; the 503 path for dead-snapshot
	//                       endpoints).
	//   kind="other"     -> read / decode failures (unexpected — Supervisor
	//                       treats these as transient and refuses to run
	//                       Pipeline, so they surface to the client as 500).
	SnapshotFetchErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "server_snapshot_fetch_errors_total",
		Help: "Errors fetching snapshots from storage.",
	}, []string{"kind"})

	// MissingSnapshot503 counts 503 responses returned by
	// handleMissingSnapshot — a direct view of how often the frontend is
	// told "retry after 10 min". In beta-v2 this triggers far less often
	// because /enter_cluster BLOCKS until the snapshot exists; it only
	// fires when a handler is hit with no prior /enter_cluster (e.g. a
	// link shared with stale cookies).
	MissingSnapshot503 = promauto.NewCounter(prometheus.CounterOpts{
		Name: "server_missing_snapshot_503_total",
		Help: "Responses returned as 503 due to missing snapshot.",
	})

	// EnterClusterTotal counts /enter_cluster outcomes for dead sessions.
	// status="ok"    -> Supervisor.Ensure returned nil.
	// status="error" -> Supervisor.Ensure returned an error (bubbled to 500).
	// The "live" sentinel path and the nil-supervisor fast path are NOT
	// counted here — they do no meaningful work and conflate the signal.
	EnterClusterTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "server_enter_cluster_total",
		Help: "Outcomes of /enter_cluster calls that entered the Supervisor (dead sessions only).",
	}, []string{"status"})

	// EnterClusterDuration records the wall time of Supervisor.Ensure per
	// /enter_cluster request. This is the single most important latency
	// signal for the lazy-mode UX: a fast LRU hit is ~ms, a cold-path
	// Pipeline run can be seconds. Exponential buckets 0.01s..~10s cover
	// both regimes without saturation.
	EnterClusterDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "server_enter_cluster_duration_seconds",
		Help:    "Wall time of Supervisor.Ensure per /enter_cluster request.",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
	})

	// SingleflightDedupTotal counts callers who participated in a coalesced
	// singleflight group inside Supervisor.Ensure — i.e. at least one other
	// caller was waiting for the same session at the same time.
	//
	// Semantic note: singleflight.Result.Shared is true for EVERY caller
	// in a coalesced group, including the winner. So this counter reports
	// "participants in a shared group", not "followers only". If N callers
	// share a single execution, this increments by N — useful as a proxy
	// for "how much Pipeline work did we save" without having to wire a
	// winner-vs-follower distinction into singleflight itself.
	SingleflightDedupTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "server_singleflight_dedup_total",
		Help: "Callers whose Ensure result was coalesced with at least one other concurrent caller.",
	})
)

// Handler returns the /metrics HTTP handler backed by the default gatherer.
// The historyserver restful router mounts this handler at /metrics on the
// main :8080 listener — beta-v2 has no sidecar metrics port.
func Handler() http.Handler {
	return promhttp.Handler()
}
