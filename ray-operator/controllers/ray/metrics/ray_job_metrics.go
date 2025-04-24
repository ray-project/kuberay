package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

// RayJobMetricsCollector implements the prometheus.Collector interface to collect ray job metrics.
type RayJobMetricsCollector struct {
	// Metrics
	rayJobExecutionDurationSeconds *prometheus.GaugeVec
}

// NewRayJobMetricsCollector creates a new RayJobMetricsCollector instance.
func NewRayJobMetricsCollector() *RayJobMetricsCollector {
	collector := &RayJobMetricsCollector{
		rayJobExecutionDurationSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kuberay_job_execution_duration_seconds",
				Help: "The time between RayJob CRs created to termination",
			},
			[]string{"name", "namespace", "result", "retry_count"},
		),
	}
	return collector
}

// Describe implements prometheus.Collector interface Describe method.
func (c *RayJobMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	c.rayJobExecutionDurationSeconds.Describe(ch)
}

// Collect implements prometheus.Collector interface Collect method.
func (c *RayJobMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	c.rayJobExecutionDurationSeconds.Collect(ch)
}

func (c *RayJobMetricsCollector) ObserveRayJobExecutionDuration(name, namespace, result string, retryCount int, duration float64) {
	c.rayJobExecutionDurationSeconds.WithLabelValues(name, namespace, result, strconv.Itoa(retryCount)).Set(duration)
}
