package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

//go:generate mockgen -destination=mocks/ray_job_metrics_mock.go -package=mocks github.com/ray-project/kuberay/ray-operator/controllers/ray/metrics RayJobMetricsCollector
type RayJobMetricsCollector interface {
	ObserveRayJobExecutionDuration(name, namespace, result string, retryCount int, duration float64)
}

// RayJobCollector implements the prometheus.Collector and RayJobMetricsCollector interface to collect ray job metrics.
type RayJobCollector struct {
	rayJobExecutionDurationSeconds *prometheus.GaugeVec
}

// NewRayJobCollector creates a new RayJobCollector instance.
func NewRayJobCollector() *RayJobCollector {
	collector := &RayJobCollector{
		rayJobExecutionDurationSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kuberay_job_execution_duration_seconds",
				Help: "Duration from when the RayJob CR’s JobDeploymentStatus transitions from Initializing to either the Retrying state or a terminal state, such as Complete or Failed. The Retrying state indicates that the CR previously failed and that spec.backoffLimit is enabled.",
			},
			[]string{"name", "namespace", "job_deployment_result", "retry_count"},
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

func (c *RayJobCollector) ObserveRayJobExecutionDuration(name, namespace, jobDeploymentResult string, retryCount int, duration float64) {
	c.rayJobExecutionDurationSeconds.WithLabelValues(name, namespace, jobDeploymentResult, strconv.Itoa(retryCount)).Set(duration)
}
