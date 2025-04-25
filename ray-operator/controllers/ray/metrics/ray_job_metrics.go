package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

// RayJobCollector implements the prometheus.Collector and ray.RayJobMetricsCollector interface to collect ray job metrics.
type RayJobCollector struct {
	rayJobExecutionDurationSeconds *prometheus.GaugeVec
}

// NewRayJobCollector creates a new RayJobCollector instance.
func NewRayJobCollector() *RayJobCollector {
	collector := &RayJobCollector{
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
func (c *RayJobCollector) Describe(ch chan<- *prometheus.Desc) {
	c.rayJobExecutionDurationSeconds.Describe(ch)
}

// Collect implements prometheus.Collector interface Collect method.
func (c *RayJobCollector) Collect(ch chan<- prometheus.Metric) {
	c.rayJobExecutionDurationSeconds.Collect(ch)
}

func (c *RayJobCollector) ObserveRayJobExecutionDuration(name, namespace, result string, retryCount int, duration float64) {
	c.rayJobExecutionDurationSeconds.WithLabelValues(name, namespace, result, strconv.Itoa(retryCount)).Set(duration)
}

type RayJobNoopCollector struct{}

func NewRayJobNoopCollector() *RayJobNoopCollector {
	return &RayJobNoopCollector{}
}

func (c *RayJobNoopCollector) ObserveRayJobExecutionDuration(_ string, _ string, _ string, _ int, _ float64) {
}
