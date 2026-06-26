package metrics

import (
	"context"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	clientmetrics "k8s.io/client-go/tools/metrics"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// This file defines client-go metrics to register with the controller-runtime
// registry. The metric definitions are sourced from
// k8s.io/component-base/metrics/prometheus/restclient/metrics.go.
// Normally controller-runtime registers a subset of these metrics and may
// eventually support registering the rest. See
// github.com/kubernetes-sigs/controller-runtime/issues/3202.
// Furthermore, the metrics registered here must work around sync.Once usage in
// k8s.io/client-go/tools/metrics.Register by assigning to adapters directly.
// See github.com/kubernetes/kubernetes/issues/127739.
// TODO: If both issues above are resolved, this may be simplified.

var (
	// requestLatency is a Prometheus Histogram metric type partitioned by
	// "verb", and "host" labels. It is used for the rest client latency metrics.
	requestLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rest_client_request_duration_seconds",
			Help:    "Request latency in seconds. Broken down by verb, and host.",
			Buckets: []float64{0.005, 0.025, 0.1, 0.25, 0.5, 1.0, 2.0, 4.0, 8.0, 15.0, 30.0, 60.0},
		},
		[]string{"verb", "host"},
	)

	// resolverLatency is a Prometheus Histogram metric type partitioned by
	// "host" labels. It is used for the rest client DNS resolver latency metrics.
	resolverLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rest_client_dns_resolution_duration_seconds",
			Help:    "DNS resolver latency in seconds. Broken down by host.",
			Buckets: []float64{0.005, 0.025, 0.1, 0.25, 0.5, 1.0, 2.0, 4.0, 8.0, 15.0, 30.0},
		},
		[]string{"host"},
	)

	requestSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "rest_client_request_size_bytes",
			Help: "Request size in bytes. Broken down by verb and host.",
			// 64 bytes to 16MB
			Buckets: []float64{64, 256, 512, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216},
		},
		[]string{"verb", "host"},
	)

	responseSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "rest_client_response_size_bytes",
			Help: "Response size in bytes. Broken down by verb and host.",
			// 64 bytes to 16MB
			Buckets: []float64{64, 256, 512, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216},
		},
		[]string{"verb", "host"},
	)

	rateLimiterLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rest_client_rate_limiter_duration_seconds",
			Help:    "Client side rate limiter latency in seconds. Broken down by verb, and host.",
			Buckets: []float64{0.005, 0.025, 0.1, 0.25, 0.5, 1.0, 2.0, 4.0, 8.0, 15.0, 30.0, 60.0},
		},
		[]string{"verb", "host"},
	)

	requestRetry = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rest_client_request_retries_total",
			Help: "Number of request retries, partitioned by status code, verb, and host.",
		},
		[]string{"code", "verb", "host"},
	)

	// rest_client_requests_total is omitted because controller-runtime sets it
	// up.
)

func init() {
	// Directly set the exported metric adapters in the client-go metrics
	// package. This bypasses the sync.Once in Register() that
	// controller-runtime called.
	clientmetrics.RequestLatency = &latencyAdapter{m: requestLatency}
	clientmetrics.ResolverLatency = &resolverLatencyAdapter{m: resolverLatency}
	clientmetrics.RequestSize = &sizeAdapter{m: requestSize}
	clientmetrics.ResponseSize = &sizeAdapter{m: responseSize}
	clientmetrics.RateLimiterLatency = &latencyAdapter{m: rateLimiterLatency}
	clientmetrics.RequestRetry = &retryAdapter{requestRetry}

	// Register the metrics with prometheus. If you are debugging a panic here,
	// it is likely that controller-runtime started managing some of these
	// metrics and they should be removed here. See the comment near the start
	// of the file for more info.
	ctrlmetrics.Registry.MustRegister(requestLatency)
	ctrlmetrics.Registry.MustRegister(requestSize)
	ctrlmetrics.Registry.MustRegister(responseSize)
	ctrlmetrics.Registry.MustRegister(rateLimiterLatency)
	ctrlmetrics.Registry.MustRegister(resolverLatency)
	ctrlmetrics.Registry.MustRegister(requestRetry)
}

type latencyAdapter struct {
	m *prometheus.HistogramVec
}

func (l *latencyAdapter) Observe(_ context.Context, verb string, u url.URL, latency time.Duration) {
	l.m.WithLabelValues(verb, u.Host).Observe(latency.Seconds())
}

type resolverLatencyAdapter struct {
	m *prometheus.HistogramVec
}

func (l *resolverLatencyAdapter) Observe(_ context.Context, host string, latency time.Duration) {
	l.m.WithLabelValues(host).Observe(latency.Seconds())
}

type sizeAdapter struct {
	m *prometheus.HistogramVec
}

func (s *sizeAdapter) Observe(_ context.Context, verb string, host string, size float64) {
	s.m.WithLabelValues(verb, host).Observe(size)
}

type retryAdapter struct {
	m *prometheus.CounterVec
}

func (r *retryAdapter) IncrementRetry(_ context.Context, code, method, host string) {
	r.m.WithLabelValues(code, method, host).Inc()
}
