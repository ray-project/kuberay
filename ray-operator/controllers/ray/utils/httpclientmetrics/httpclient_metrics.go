package httpclientmetrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Mode describes how the operator reaches the Ray component.
type Mode string

const (
	// ModeDirect means the operator connects directly to a pod IP or in-cluster Service.
	ModeDirect Mode = "direct"
	// ModeProxy means the operator proxies through the Kubernetes API server.
	ModeProxy Mode = "proxy"
)

// Endpoint label values used to keep metric cardinality low.
const (
	endpointServeApplications = "serve_applications"
	endpointJobs              = "jobs"
	endpointServeProxyHealth  = "serve_proxy_health"
	endpointUnknown           = "unknown"
)

// requestDurationBuckets are the histogram buckets (in seconds) for Ray HTTP
// client response times. The largest bucket matches the longest client timeout
// (10s when proxying through the Kubernetes apiserver; direct requests time out
// at 2s), so buckets above that ceiling would never receive observations.
var requestDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0}

var (
	dashboardClientRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kuberay_dashboard_client_request_duration_seconds",
			Help:    "Response time of HTTP requests from the KubeRay operator to the Ray dashboard API, in seconds.",
			Buckets: requestDurationBuckets,
		},
		[]string{"method", "ray_endpoint", "code", "mode"},
	)

	serveClientRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kuberay_serve_client_request_duration_seconds",
			Help:    "Response time of HTTP requests from the KubeRay operator to the Ray Serve proxy actor, in seconds.",
			Buckets: requestDurationBuckets,
		},
		[]string{"method", "ray_endpoint", "code", "mode"},
	)
)

// RegisterMetrics registers all HTTP client metrics with the given registerer.
// This should be called from main.go when metrics are enabled, rather than via init().
// The histogram's _count series (with the same labels) already provides the total
// request count, so no separate request counter is needed.
func RegisterMetrics(reg prometheus.Registerer) {
	reg.MustRegister(dashboardClientRequestDuration)
	reg.MustRegister(serveClientRequestDuration)
}

// DashboardClientMetrics returns the histogram used for Ray dashboard client requests.
func DashboardClientMetrics() *prometheus.HistogramVec {
	return dashboardClientRequestDuration
}

// ServeClientMetrics returns the histogram used for Ray Serve proxy client requests.
func ServeClientMetrics() *prometheus.HistogramVec {
	return serveClientRequestDuration
}

// instrumentedRoundTripper wraps an http.RoundTripper to record request
// duration metrics for outbound HTTP calls to Ray components.
type instrumentedRoundTripper struct {
	inner     http.RoundTripper
	histogram *prometheus.HistogramVec
	mode      string
}

// NewInstrumentedRoundTripper returns a new http.RoundTripper that records
// response time metrics (the histogram's _count also yields the request count).
func NewInstrumentedRoundTripper(inner http.RoundTripper, histogram *prometheus.HistogramVec, mode Mode) http.RoundTripper {
	if inner == nil {
		inner = http.DefaultTransport
	}
	if histogram == nil {
		return inner
	}
	return &instrumentedRoundTripper{
		inner:     inner,
		histogram: histogram,
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

	return resp, err
}

// normalizeEndpoint maps a raw URL path to a low-cardinality label value.
// It strips dynamic segments (e.g., job IDs) and Kubernetes proxy prefixes.
// When adding new Ray API endpoints, add a corresponding mapping here.
func normalizeEndpoint(urlPath string) string {
	// Strip Kubernetes API server proxy prefix if present.
	// e.g., /api/v1/namespaces/default/services/svc:dashboard/proxy/api/jobs/id
	const kubernetesProxyPathSegment = "/proxy/"
	if idx := strings.LastIndex(urlPath, kubernetesProxyPathSegment); idx != -1 {
		urlPath = urlPath[idx+len(kubernetesProxyPathSegment)-1:]
	}

	if strings.HasPrefix(urlPath, "/api/serve/") {
		return endpointServeApplications
	}
	if strings.HasPrefix(urlPath, "/api/jobs/") || urlPath == "/api/jobs" {
		return endpointJobs
	}
	if strings.HasPrefix(urlPath, "/-/") {
		return endpointServeProxyHealth
	}
	return endpointUnknown
}
