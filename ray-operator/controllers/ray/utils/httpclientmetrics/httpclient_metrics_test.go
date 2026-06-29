package httpclientmetrics

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
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
		{"serve applications", "/api/serve/applications/", "serve_applications"},
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
			"k8s proxy prefix with namespace named proxy",
			"/api/v1/namespaces/proxy/services/svc:dashboard/proxy/api/jobs/raysubmit_abc123",
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
	rt := NewInstrumentedRoundTripper(inner, histogram, counter, Mode(mode))
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
			name:           "GET proxy health 200",
			method:         "GET",
			path:           "/-/healthz",
			statusCode:     200,
			expectedCode:   "200",
			expectedEP:     "proxy_health",
			expectedMethod: "GET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRoundTripper{statusCode: tt.statusCode, err: tt.transportErr}
			rt, histogram, _ := newTestInstrumentedRT(t, mock, "direct")

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
			rt, histogram, _ := newTestInstrumentedRT(t, mock, mode)

			req, err := http.NewRequestWithContext(context.Background(), "GET", "http://localhost:8265/api/serve/applications/", nil)
			require.NoError(t, err)

			_, err = rt.RoundTrip(req)
			require.NoError(t, err)

			count := getHistogramSampleCount(t, histogram, "GET", "serve_applications", "200", mode)
			assert.Equal(t, uint64(1), count)
		})
	}
}

func TestInstrumentedRoundTripper_TransportError(t *testing.T) {
	mock := &mockRoundTripper{err: errors.New("context deadline exceeded")}
	rt, histogram, _ := newTestInstrumentedRT(t, mock, "proxy")

	req, err := http.NewRequestWithContext(context.Background(), "GET", "http://localhost:8265/api/serve/applications/", nil)
	require.NoError(t, err)

	_, rtErr := rt.RoundTrip(req)
	require.Error(t, rtErr)

	count := getHistogramSampleCount(t, histogram, "GET", "serve_applications", "error", "proxy")
	assert.Equal(t, uint64(1), count)
}

func TestInstrumentedRoundTripper_CounterIncrement(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		calls          int
		expectedEP     string
		expectedCode   string
		expectedMethod string
	}{
		{
			name:           "serve applications",
			url:            "http://localhost:8265/api/serve/applications/",
			calls:          3,
			expectedEP:     "serve_applications",
			expectedCode:   "200",
			expectedMethod: "GET",
		},
		{
			name:           "proxy health",
			url:            "http://10.0.0.1:8000/-/healthz",
			calls:          5,
			expectedEP:     "proxy_health",
			expectedCode:   "200",
			expectedMethod: "GET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRoundTripper{statusCode: 200}
			rt, _, counter := newTestInstrumentedRT(t, mock, "direct")

			req, err := http.NewRequestWithContext(context.Background(), tt.expectedMethod, tt.url, nil)
			require.NoError(t, err)

			for range tt.calls {
				_, err = rt.RoundTrip(req)
				require.NoError(t, err)
			}

			c, err := counter.GetMetricWithLabelValues(tt.expectedMethod, tt.expectedEP, tt.expectedCode, "direct")
			require.NoError(t, err)
			assert.InDelta(t, float64(tt.calls), testutil.ToFloat64(c), 0)
		})
	}
}

func TestNewInstrumentedRoundTripperNilInner(t *testing.T) {
	histogram, counter := newTestMetrics(t)
	rt := NewInstrumentedRoundTripper(nil, histogram, counter, ModeDirect)
	assert.NotNil(t, rt)
}

func TestNewInstrumentedRoundTripperNoopsWithoutCollectors(t *testing.T) {
	inner := &mockRoundTripper{statusCode: 200}
	rt := NewInstrumentedRoundTripper(inner, nil, nil, ModeDirect)
	assert.Same(t, inner, rt)
}
