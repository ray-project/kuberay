package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Tests in this package intentionally take read-baseline/delta style
// assertions (before := testutil.ToFloat64(...)) rather than comparing against
// zero. Rationale: promauto registers metrics at package init against the
// default registerer, so test ordering within this binary would otherwise be
// load-bearing — a safer baseline-delta pattern survives re-runs and
// side-effects from other packages importing metrics.

// TestCounter_Inc verifies a plain Counter increments by exactly 1.
func TestCounter_Inc(t *testing.T) {
	before := testutil.ToFloat64(SessionsProcessed)
	SessionsProcessed.Inc()
	after := testutil.ToFloat64(SessionsProcessed)
	if got, want := after-before, 1.0; got != want {
		t.Fatalf("SessionsProcessed delta: got %v, want %v", got, want)
	}
}

// TestCounterVec_WithLabelValues verifies CounterVec increments are scoped to
// the label set used at WithLabelValues time.
func TestCounterVec_WithLabelValues(t *testing.T) {
	beforeK8s := testutil.ToFloat64(SessionErrors.WithLabelValues("k8s_probe"))
	beforeEvents := testutil.ToFloat64(SessionErrors.WithLabelValues("events"))

	SessionErrors.WithLabelValues("k8s_probe").Inc()
	SessionErrors.WithLabelValues("k8s_probe").Inc()

	afterK8s := testutil.ToFloat64(SessionErrors.WithLabelValues("k8s_probe"))
	afterEvents := testutil.ToFloat64(SessionErrors.WithLabelValues("events"))

	if got, want := afterK8s-beforeK8s, 2.0; got != want {
		t.Fatalf("SessionErrors{stage=k8s_probe} delta: got %v, want %v", got, want)
	}
	if got, want := afterEvents-beforeEvents, 0.0; got != want {
		t.Fatalf("SessionErrors{stage=events} should be untouched; got delta %v", got)
	}
}

// TestEnterClusterMetrics covers the two lazy-mode-specific metrics. WHY
// assertion here and not just in TestHandler_Serves: these metrics are the
// headline observability for /enter_cluster; a regression that silently
// stops incrementing them would hide Supervisor failures from operators.
func TestEnterClusterMetrics(t *testing.T) {
	beforeOK := testutil.ToFloat64(EnterClusterTotal.WithLabelValues("ok"))
	beforeErr := testutil.ToFloat64(EnterClusterTotal.WithLabelValues("error"))
	beforeDedup := testutil.ToFloat64(SingleflightDedupTotal)

	EnterClusterTotal.WithLabelValues("ok").Inc()
	EnterClusterTotal.WithLabelValues("error").Inc()
	EnterClusterTotal.WithLabelValues("error").Inc()
	SingleflightDedupTotal.Inc()
	EnterClusterDuration.Observe(0.42)

	if got, want := testutil.ToFloat64(EnterClusterTotal.WithLabelValues("ok"))-beforeOK, 1.0; got != want {
		t.Fatalf("EnterClusterTotal{status=ok} delta: got %v, want %v", got, want)
	}
	if got, want := testutil.ToFloat64(EnterClusterTotal.WithLabelValues("error"))-beforeErr, 2.0; got != want {
		t.Fatalf("EnterClusterTotal{status=error} delta: got %v, want %v", got, want)
	}
	if got, want := testutil.ToFloat64(SingleflightDedupTotal)-beforeDedup, 1.0; got != want {
		t.Fatalf("SingleflightDedupTotal delta: got %v, want %v", got, want)
	}
}

// TestHandler_Serves verifies /metrics returns 200 and the scrape exposition
// includes our declared metric names. This catches two regressions at once:
// (1) a metric silently unregistered, (2) promhttp handler misconfigured.
func TestHandler_Serves(t *testing.T) {
	// Increment every counter / counterVec / gauge at least once so the
	// exposition includes the family — Prometheus omits never-observed
	// CounterVec / HistogramVec children.
	SessionsProcessed.Inc()
	SessionsSkipped.WithLabelValues("live").Inc()
	SessionsSkipped.WithLabelValues("already_snapped").Inc()
	SessionDuration.Observe(0.25)
	SessionErrors.WithLabelValues("k8s_probe").Inc()
	SessionErrors.WithLabelValues("events").Inc()
	SessionErrors.WithLabelValues("snapshot_write").Inc()
	CacheHits.Inc()
	CacheMisses.Inc()
	SnapshotFetchErrors.WithLabelValues("not_found").Inc()
	SnapshotFetchErrors.WithLabelValues("other").Inc()
	MissingSnapshot503.Inc()
	EnterClusterTotal.WithLabelValues("ok").Inc()
	EnterClusterTotal.WithLabelValues("error").Inc()
	EnterClusterDuration.Observe(0.12)
	SingleflightDedupTotal.Inc()

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("/metrics status: got %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	wantNames := []string{
		"processor_sessions_processed_total",
		"processor_sessions_skipped_total",
		"processor_session_duration_seconds",
		"processor_session_errors_total",
		"server_cache_hits_total",
		"server_cache_misses_total",
		"server_snapshot_fetch_errors_total",
		"server_missing_snapshot_503_total",
		"server_enter_cluster_total",
		"server_enter_cluster_duration_seconds",
		"server_singleflight_dedup_total",
	}
	for _, name := range wantNames {
		if !strings.Contains(body, name) {
			t.Errorf("/metrics body missing %q", name)
		}
	}

	// Negative assertions: the three ticker-only metrics must NOT appear.
	// WHY this matters: if someone copies a metric back without updating
	// the semantic, dashboards built on the old name would silently show
	// bogus data.
	removed := []string{
		"processor_sessions_scanned_total",
		"processor_last_tick_timestamp_seconds",
		"processor_orphan_sessions",
	}
	for _, name := range removed {
		if strings.Contains(body, name) {
			t.Errorf("/metrics body unexpectedly contains retired metric %q", name)
		}
	}
}
