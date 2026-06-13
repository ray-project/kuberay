package httpclientmetrics

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ClientType identifies which Ray HTTP client is being instrumented.
type ClientType string

const (
	// ClientTypeDashboard instruments the Ray dashboard API client.
	ClientTypeDashboard ClientType = "dashboard"
	// ClientTypeProxy instruments the Ray Serve proxy health-check client.
	ClientTypeProxy ClientType = "proxy"
)

// Mode describes how the operator reaches the Ray component.
type Mode string

const (
	// ModeDirect means the operator connects directly to a pod IP or in-cluster Service.
	ModeDirect Mode = "direct"
	// ModeProxy means the operator proxies through the Kubernetes API server.
	ModeProxy Mode = "proxy"
)

var (
	dashboardClientRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kuberay_dashboard_client_request_duration_seconds",
			Help:    "Latency of HTTP requests from the KubeRay operator to the Ray dashboard API, in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0, 15.0},
		},
		[]string{"method", "ray_endpoint", "code", "mode"},
	)

	dashboardClientRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kuberay_dashboard_client_requests_total",
			Help: "Total number of HTTP requests from the KubeRay operator to the Ray dashboard API.",
		},
		[]string{"method", "ray_endpoint", "code", "mode"},
	)

	proxyClientRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kuberay_proxy_client_request_duration_seconds",
			Help:    "Latency of HTTP requests from the KubeRay operator to the Ray Serve proxy actor, in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0, 15.0},
		},
		[]string{"method", "ray_endpoint", "code", "mode"},
	)

	proxyClientRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kuberay_proxy_client_requests_total",
			Help: "Total number of HTTP requests from the KubeRay operator to the Ray Serve proxy actor.",
		},
		[]string{"method", "ray_endpoint", "code", "mode"},
	)
)

// RegisterMetrics registers all HTTP client metrics with the given registerer.
// This should be called from main.go when metrics are enabled, rather than via init().
func RegisterMetrics(reg prometheus.Registerer) {
	reg.MustRegister(dashboardClientRequestDuration)
	reg.MustRegister(dashboardClientRequestsTotal)
	reg.MustRegister(proxyClientRequestDuration)
	reg.MustRegister(proxyClientRequestsTotal)
}

// instrumentedRoundTripper wraps an http.RoundTripper to record request
// duration and count metrics for outbound HTTP calls to Ray components.
type instrumentedRoundTripper struct {
	inner     http.RoundTripper
	histogram *prometheus.HistogramVec
	counter   *prometheus.CounterVec
	mode      string
}

// NewInstrumentedRoundTripper returns a new http.RoundTripper that records
// latency and request count metrics.
func NewInstrumentedRoundTripper(inner http.RoundTripper, clientType ClientType, mode Mode) http.RoundTripper {
	if inner == nil {
		inner = http.DefaultTransport
	}
	var histogram *prometheus.HistogramVec
	var counter *prometheus.CounterVec
	switch clientType {
	case ClientTypeDashboard:
		histogram = dashboardClientRequestDuration
		counter = dashboardClientRequestsTotal
	case ClientTypeProxy:
		histogram = proxyClientRequestDuration
		counter = proxyClientRequestsTotal
	default:
		panic(fmt.Sprintf("unknown clientType: %q", clientType))
	}
	return &instrumentedRoundTripper{
		inner:     inner,
		histogram: histogram,
		counter:   counter,
		mode:      string(mode),
	}
}

func (t *instrumentedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.inner.RoundTrip(req)
	duration := time.Since(start).Seconds()

	code := "error"
	if err == nil && resp != nil {
		code = strconv.Itoa(resp.StatusCode)
	}

	endpoint := normalizeEndpoint(req.URL.Path)
	method := req.Method

	t.histogram.WithLabelValues(method, endpoint, code, t.mode).Observe(duration)
	t.counter.WithLabelValues(method, endpoint, code, t.mode).Inc()

	return resp, err
}

// normalizeEndpoint maps a raw URL path to a low-cardinality label value.
// It strips dynamic segments (e.g., job IDs) and Kubernetes proxy prefixes.
// When adding new Ray API endpoints, add a corresponding mapping here.
func normalizeEndpoint(urlPath string) string {
	// Strip Kubernetes API server proxy prefix if present.
	// e.g., /api/v1/namespaces/default/services/svc:dashboard/proxy/api/jobs/id
	if idx := strings.Index(urlPath, "/proxy/"); idx != -1 {
		urlPath = urlPath[idx+len("/proxy"):]
	}

	if strings.HasPrefix(urlPath, "/api/serve/") {
		return "serve_applications"
	}
	if strings.HasPrefix(urlPath, "/api/jobs/") || urlPath == "/api/jobs" {
		return "jobs"
	}
	if strings.HasPrefix(urlPath, "/-/") {
		return "proxy_health"
	}
	return "unknown"
}
