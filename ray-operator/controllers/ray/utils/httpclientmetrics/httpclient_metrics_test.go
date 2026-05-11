package httpclientmetrics

import (
	"errors"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dto "github.com/prometheus/client_model/go"
)

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		urlPath  string
		expected string
	}{
		{"serve applications", "/api/serve/applications/", "serve_applications"},
		{"serve applications with query", "/api/serve/applications/?api_type=declarative", "serve_applications"},
		{"jobs list", "/api/jobs/", "jobs"},
		{"job info with ID", "/api/jobs/raysubmit_abc123", "jobs"},
		{"job logs", "/api/jobs/raysubmit_abc123/logs", "jobs"},
		{"job stop", "/api/jobs/raysubmit_abc123/stop", "jobs"},
		{"job delete", "/api/jobs/raysubmit_abc123", "jobs"},
		{"proxy health", "/-/healthz", "proxy_health"},
		{"unknown path", "/unknown/path", "unknown"},
		{"empty path", "", "unknown"},
		{
			"k8s proxy prefix with jobs",
			"/api/v1/namespaces/default/services/svc:dashboard/proxy/api/jobs/raysubmit_abc123",
			"jobs",
		},
		{
			"k8s proxy prefix with serve",
			"/api/v1/namespaces/default/services/svc:dashboard/proxy/api/serve/applications/",
			"serve_applications",
		},
		{
			"k8s pod proxy prefix with healthz",
			"/api/v1/namespaces/default/pods/pod-name:8000/proxy/-/healthz",
			"proxy_health",
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

func newTestMetrics(t *testing.T) (histogram *prometheus.HistogramVec, counter *prometheus.CounterVec) {
	t.Helper()
	histogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    t.Name() + "_request_duration_seconds",
			Help:    "Test histogram.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0, 15.0},
		},
		[]string{"method", "ray_endpoint", "code", "mode"},
	)
	counter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: t.Name() + "_requests_total",
			Help: "Test counter.",
		},
		[]string{"method", "ray_endpoint", "code", "mode"},
	)
	return histogram, counter
}

func newTestInstrumentedRT(t *testing.T, inner http.RoundTripper, mode string) (http.RoundTripper, *prometheus.HistogramVec, *prometheus.CounterVec) {
	t.Helper()
	histogram, counter := newTestMetrics(t)
	if inner == nil {
		inner = http.DefaultTransport
	}
	rt := &instrumentedRoundTripper{
		inner:     inner,
		histogram: histogram,
		counter:   counter,
		mode:      mode,
	}
	return rt, histogram, counter
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

func TestDashboardClientHistogram(t *testing.T) {
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
			expectedEP:     "serve_applications",
			expectedMethod: "GET",
		},
		{
			name:           "PUT deploy 200",
			method:         "PUT",
			path:           "/api/serve/applications/",
			statusCode:     200,
			expectedCode:   "200",
			expectedEP:     "serve_applications",
			expectedMethod: "PUT",
		},
		{
			name:           "GET job info 200",
			method:         "GET",
			path:           "/api/jobs/raysubmit_abc",
			statusCode:     200,
			expectedCode:   "200",
			expectedEP:     "jobs",
			expectedMethod: "GET",
		},
		{
			name:           "POST submit job 200",
			method:         "POST",
			path:           "/api/jobs/",
			statusCode:     200,
			expectedCode:   "200",
			expectedEP:     "jobs",
			expectedMethod: "POST",
		},
		{
			name:           "DELETE job 200",
			method:         "DELETE",
			path:           "/api/jobs/raysubmit_abc",
			statusCode:     200,
			expectedCode:   "200",
			expectedEP:     "jobs",
			expectedMethod: "DELETE",
		},
		{
			name:           "GET job info 500",
			method:         "GET",
			path:           "/api/jobs/raysubmit_abc",
			statusCode:     500,
			expectedCode:   "500",
			expectedEP:     "jobs",
			expectedMethod: "GET",
		},
		{
			name:           "GET job info 404",
			method:         "GET",
			path:           "/api/jobs/raysubmit_abc",
			statusCode:     404,
			expectedCode:   "404",
			expectedEP:     "jobs",
			expectedMethod: "GET",
		},
		{
			name:           "transport error (timeout)",
			method:         "GET",
			path:           "/api/serve/applications/",
			transportErr:   errors.New("context deadline exceeded"),
			expectedCode:   "error",
			expectedEP:     "serve_applications",
			expectedMethod: "GET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRoundTripper{statusCode: tt.statusCode, err: tt.transportErr}
			rt, histogram, _ := newTestInstrumentedRT(t, mock, "direct")

			req, err := http.NewRequest(tt.method, "http://localhost:8265"+tt.path, nil)
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

func TestDashboardClientHistogramModes(t *testing.T) {
	for _, mode := range []string{"direct", "proxy"} {
		t.Run("mode_"+mode, func(t *testing.T) {
			mock := &mockRoundTripper{statusCode: 200}
			rt, histogram, _ := newTestInstrumentedRT(t, mock, mode)

			req, err := http.NewRequest("GET", "http://localhost:8265/api/serve/applications/", nil)
			require.NoError(t, err)

			_, err = rt.RoundTrip(req)
			require.NoError(t, err)

			count := getHistogramSampleCount(t, histogram, "GET", "serve_applications", "200", mode)
			assert.Equal(t, uint64(1), count)
		})
	}
}

func TestProxyClientHistogram(t *testing.T) {
	mock := &mockRoundTripper{statusCode: 200}
	rt, histogram, _ := newTestInstrumentedRT(t, mock, "direct")

	req, err := http.NewRequest("GET", "http://10.0.0.1:8000/-/healthz", nil)
	require.NoError(t, err)

	_, err = rt.RoundTrip(req)
	require.NoError(t, err)

	count := getHistogramSampleCount(t, histogram, "GET", "proxy_health", "200", "direct")
	assert.Equal(t, uint64(1), count)
}

func TestProxyClientHistogramError(t *testing.T) {
	mock := &mockRoundTripper{err: errors.New("connection refused")}
	rt, histogram, _ := newTestInstrumentedRT(t, mock, "proxy")

	req, err := http.NewRequest("GET", "http://10.0.0.1:8000/-/healthz", nil)
	require.NoError(t, err)

	_, rtErr := rt.RoundTrip(req)
	require.Error(t, rtErr)

	count := getHistogramSampleCount(t, histogram, "GET", "proxy_health", "error", "proxy")
	assert.Equal(t, uint64(1), count)
}

func TestDashboardClientCounter(t *testing.T) {
	mock := &mockRoundTripper{statusCode: 200}
	rt, _, counter := newTestInstrumentedRT(t, mock, "direct")

	req, err := http.NewRequest("GET", "http://localhost:8265/api/serve/applications/", nil)
	require.NoError(t, err)

	// Call 3 times
	for range 3 {
		_, err = rt.RoundTrip(req)
		require.NoError(t, err)
	}

	c, err := counter.GetMetricWithLabelValues("GET", "serve_applications", "200", "direct")
	require.NoError(t, err)
	assert.Equal(t, float64(3), testutil.ToFloat64(c))
}

func TestDashboardClientCounterError(t *testing.T) {
	mock := &mockRoundTripper{err: errors.New("timeout")}
	rt, _, counter := newTestInstrumentedRT(t, mock, "direct")

	req, err := http.NewRequest("GET", "http://localhost:8265/api/jobs/raysubmit_abc", nil)
	require.NoError(t, err)

	_, _ = rt.RoundTrip(req)

	c, err := counter.GetMetricWithLabelValues("GET", "jobs", "error", "direct")
	require.NoError(t, err)
	assert.Equal(t, float64(1), testutil.ToFloat64(c))
}

func TestProxyClientCounter(t *testing.T) {
	mock := &mockRoundTripper{statusCode: 200}
	rt, _, counter := newTestInstrumentedRT(t, mock, "direct")

	req, err := http.NewRequest("GET", "http://10.0.0.1:8000/-/healthz", nil)
	require.NoError(t, err)

	for range 5 {
		_, err = rt.RoundTrip(req)
		require.NoError(t, err)
	}

	c, err := counter.GetMetricWithLabelValues("GET", "proxy_health", "200", "direct")
	require.NoError(t, err)
	assert.Equal(t, float64(5), testutil.ToFloat64(c))
}

func TestNewInstrumentedRoundTripperNilInner(t *testing.T) {
	rt := NewInstrumentedRoundTripper(nil, ClientTypeDashboard, ModeDirect)
	assert.NotNil(t, rt)
}

func TestNewInstrumentedRoundTripperClientTypes(t *testing.T) {
	for _, clientType := range []ClientType{ClientTypeDashboard, ClientTypeProxy} {
		t.Run(string(clientType), func(t *testing.T) {
			rt := NewInstrumentedRoundTripper(http.DefaultTransport, clientType, ModeDirect)
			assert.NotNil(t, rt)
		})
	}
}

func TestNewInstrumentedRoundTripperPanicsOnUnknownClientType(t *testing.T) {
	assert.Panics(t, func() {
		NewInstrumentedRoundTripper(http.DefaultTransport, "invalid", ModeDirect)
	})
}
