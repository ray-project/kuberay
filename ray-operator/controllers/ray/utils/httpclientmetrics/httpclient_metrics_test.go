package httpclientmetrics

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		urlPath  string
		expected string
	}{
		{"serve applications", "/api/serve/applications/", endpointServeApplications},
		{"jobs list", "/api/jobs/", endpointJobs},
		{"job info with ID", "/api/jobs/raysubmit_abc123", endpointJobs},
		{"job logs", "/api/jobs/raysubmit_abc123/logs", endpointJobs},
		{"job stop", "/api/jobs/raysubmit_abc123/stop", endpointJobs},
		{"job delete", "/api/jobs/raysubmit_abc123", endpointJobs},
		{"proxy health", "/-/healthz", endpointServeProxyHealth},
		{"unknown path", "/unknown/path", endpointUnknown},
		{"empty path", "", endpointUnknown},
		{
			"k8s proxy prefix with jobs",
			"/api/v1/namespaces/default/services/svc:dashboard/proxy/api/jobs/raysubmit_abc123",
			endpointJobs,
		},
		{
			"k8s proxy prefix with namespace named proxy",
			"/api/v1/namespaces/proxy/services/svc:dashboard/proxy/api/jobs/raysubmit_abc123",
			endpointJobs,
		},
		{
			"k8s proxy prefix with serve",
			"/api/v1/namespaces/default/services/svc:dashboard/proxy/api/serve/applications/",
			endpointServeApplications,
		},
		{
			"k8s pod proxy prefix with healthz",
			"/api/v1/namespaces/default/pods/pod-name:8000/proxy/-/healthz",
			endpointServeProxyHealth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeEndpoint(tt.urlPath)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// mockRoundTripper is a configurable http.RoundTripper for testing.
type mockRoundTripper struct {
	statusCode int
	err        error
}

func (m *mockRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &http.Response{StatusCode: m.statusCode}, nil
}

func newTestMetrics(t *testing.T) *prometheus.HistogramVec {
	t.Helper()
	return prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    t.Name() + "_request_duration_seconds",
			Help:    "Test histogram.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0},
		},
		[]string{"method", "ray_endpoint", "code", "mode"},
	)
}

func newTestInstrumentedRT(t *testing.T, inner http.RoundTripper, mode string) (http.RoundTripper, *prometheus.HistogramVec) {
	t.Helper()
	histogram := newTestMetrics(t)
	rt := NewInstrumentedRoundTripper(inner, histogram, Mode(mode))
	return rt, histogram
}

// getHistogramSampleCount retrieves the sample count from a histogram observer.
func getHistogramSampleCount(t *testing.T, histogram *prometheus.HistogramVec, labels ...string) uint64 {
	t.Helper()
	observer, err := histogram.GetMetricWithLabelValues(labels...)
	require.NoError(t, err)
	metric := &dto.Metric{}
	h := observer.(prometheus.Metric)
	err = h.Write(metric)
	require.NoError(t, err)
	return metric.GetHistogram().GetSampleCount()
}

func TestInstrumentedRoundTripper_RecordsHistogram(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		statusCode     int
		transportErr   error
		expectedCode   string
		expectedEP     string
		expectedMethod string
	}{
		{
			name:           "GET serve details 200",
			method:         "GET",
			path:           "/api/serve/applications/",
			statusCode:     200,
			expectedCode:   "200",
			expectedEP:     endpointServeApplications,
			expectedMethod: "GET",
		},
		{
			name:           "PUT deploy 200",
			method:         "PUT",
			path:           "/api/serve/applications/",
			statusCode:     200,
			expectedCode:   "200",
			expectedEP:     endpointServeApplications,
			expectedMethod: "PUT",
		},
		{
			name:           "GET job info 200",
			method:         "GET",
			path:           "/api/jobs/raysubmit_abc",
			statusCode:     200,
			expectedCode:   "200",
			expectedEP:     endpointJobs,
			expectedMethod: "GET",
		},
		{
			name:           "POST submit job 200",
			method:         "POST",
			path:           "/api/jobs/",
			statusCode:     200,
			expectedCode:   "200",
			expectedEP:     endpointJobs,
			expectedMethod: "POST",
		},
		{
			name:           "DELETE job 200",
			method:         "DELETE",
			path:           "/api/jobs/raysubmit_abc",
			statusCode:     200,
			expectedCode:   "200",
			expectedEP:     endpointJobs,
			expectedMethod: "DELETE",
		},
		{
			name:           "GET job info 500",
			method:         "GET",
			path:           "/api/jobs/raysubmit_abc",
			statusCode:     500,
			expectedCode:   "500",
			expectedEP:     endpointJobs,
			expectedMethod: "GET",
		},
		{
			name:           "GET job info 404",
			method:         "GET",
			path:           "/api/jobs/raysubmit_abc",
			statusCode:     404,
			expectedCode:   "404",
			expectedEP:     endpointJobs,
			expectedMethod: "GET",
		},
		{
			name:           "GET proxy health 200",
			method:         "GET",
			path:           "/-/healthz",
			statusCode:     200,
			expectedCode:   "200",
			expectedEP:     endpointServeProxyHealth,
			expectedMethod: "GET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRoundTripper{statusCode: tt.statusCode, err: tt.transportErr}
			rt, histogram := newTestInstrumentedRT(t, mock, "direct")

			req, err := http.NewRequestWithContext(context.Background(), tt.method, "http://localhost:8265"+tt.path, nil)
			require.NoError(t, err)

			resp, rtErr := rt.RoundTrip(req)
			if tt.transportErr != nil {
				require.Error(t, rtErr)
			} else {
				require.NoError(t, rtErr)
				assert.Equal(t, tt.statusCode, resp.StatusCode)
			}

			count := getHistogramSampleCount(t, histogram, tt.expectedMethod, tt.expectedEP, tt.expectedCode, "direct")
			assert.Equal(t, uint64(1), count, "histogram should have recorded exactly 1 observation")
		})
	}
}

func TestInstrumentedRoundTripper_Modes(t *testing.T) {
	for _, mode := range []string{"direct", "proxy"} {
		t.Run("mode_"+mode, func(t *testing.T) {
			mock := &mockRoundTripper{statusCode: 200}
			rt, histogram := newTestInstrumentedRT(t, mock, mode)

			req, err := http.NewRequestWithContext(context.Background(), "GET", "http://localhost:8265/api/serve/applications/", nil)
			require.NoError(t, err)

			_, err = rt.RoundTrip(req)
			require.NoError(t, err)

			count := getHistogramSampleCount(t, histogram, "GET", endpointServeApplications, "200", mode)
			assert.Equal(t, uint64(1), count)
		})
	}
}

func TestInstrumentedRoundTripper_TransportError(t *testing.T) {
	mock := &mockRoundTripper{err: errors.New("context deadline exceeded")}
	rt, histogram := newTestInstrumentedRT(t, mock, "proxy")

	req, err := http.NewRequestWithContext(context.Background(), "GET", "http://localhost:8265/api/serve/applications/", nil)
	require.NoError(t, err)

	_, rtErr := rt.RoundTrip(req)
	require.Error(t, rtErr)

	// The histogram must record the failed request with code="error" so that its
	// _count series accounts for transport failures in error-rate calculations.
	count := getHistogramSampleCount(t, histogram, "GET", endpointServeApplications, "error", "proxy")
	assert.Equal(t, uint64(1), count)
}

func TestInstrumentedRoundTripper_RequestCount(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		calls          uint64
		expectedEP     string
		expectedCode   string
		expectedMethod string
	}{
		{
			name:           "serve applications",
			url:            "http://localhost:8265/api/serve/applications/",
			calls:          3,
			expectedEP:     endpointServeApplications,
			expectedCode:   "200",
			expectedMethod: "GET",
		},
		{
			name:           "proxy health",
			url:            "http://10.0.0.1:8000/-/healthz",
			calls:          5,
			expectedEP:     endpointServeProxyHealth,
			expectedCode:   "200",
			expectedMethod: "GET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRoundTripper{statusCode: 200}
			rt, histogram := newTestInstrumentedRT(t, mock, "direct")

			req, err := http.NewRequestWithContext(context.Background(), tt.expectedMethod, tt.url, nil)
			require.NoError(t, err)

			for range tt.calls {
				_, err = rt.RoundTrip(req)
				require.NoError(t, err)
			}

			// The histogram's _count series yields the total request count.
			count := getHistogramSampleCount(t, histogram, tt.expectedMethod, tt.expectedEP, tt.expectedCode, "direct")
			assert.Equal(t, tt.calls, count)
		})
	}
}

func TestNewInstrumentedRoundTripperNilInner(t *testing.T) {
	histogram := newTestMetrics(t)
	rt := NewInstrumentedRoundTripper(nil, histogram, ModeDirect)
	assert.NotNil(t, rt)
}

func TestNewInstrumentedRoundTripperNoopsWithoutCollectors(t *testing.T) {
	inner := &mockRoundTripper{statusCode: 200}
	rt := NewInstrumentedRoundTripper(inner, nil, ModeDirect)
	assert.Same(t, inner, rt)
}

func TestRegisterMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	require.NotPanics(t, func() { RegisterMetrics(reg) })
	assertHistogramsRegistered(t, reg)
}

// TestRegisterMetricsWithDefaultRegisterer mirrors the API server wiring, which
// registers with prometheus.DefaultRegisterer and exposes metrics via
// promhttp.Handler() (backed by prometheus.DefaultGatherer).
func TestRegisterMetricsWithDefaultRegisterer(t *testing.T) {
	require.NotPanics(t, func() { RegisterMetrics(prometheus.DefaultRegisterer) })
	assertHistogramsRegistered(t, prometheus.DefaultGatherer)
}

// assertHistogramsRegistered records one observation on each histogram and
// verifies both metric families are emitted by the given gatherer.
func assertHistogramsRegistered(t *testing.T, gatherer prometheus.Gatherer) {
	t.Helper()
	dashboardClientRequestDuration.WithLabelValues("GET", endpointJobs, "200", string(ModeDirect)).Observe(0.01)
	serveClientRequestDuration.WithLabelValues("GET", endpointServeProxyHealth, "200", string(ModeDirect)).Observe(0.01)

	families, err := gatherer.Gather()
	require.NoError(t, err)

	registered := make(map[string]bool)
	for _, mf := range families {
		registered[mf.GetName()] = true
	}

	for _, name := range []string{
		"kuberay_dashboard_client_request_duration_seconds",
		"kuberay_serve_client_request_duration_seconds",
	} {
		assert.True(t, registered[name], "expected metric %q to be registered", name)
	}
}
