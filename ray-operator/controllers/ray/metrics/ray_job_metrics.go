package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

//go:generate mockgen -destination=mocks/ray_job_metrics_mock.go -package=mocks github.com/ray-project/kuberay/ray-operator/controllers/ray/metrics RayJobMetricsObserver
type RayJobMetricsObserver interface {
	ObserveRayJobExecutionDuration(name, namespace string, jobDeploymentStatus rayv1.JobDeploymentStatus, retryCount int, duration float64)
}

// RayJobMetricsManager implements the prometheus.Collector and RayJobMetricsObserver interface to collect ray job metrics.
type RayJobMetricsManager struct {
	rayJobExecutionDurationSeconds *prometheus.GaugeVec
}

// NewRayJobMetricsManager creates a new RayJobMetricsManager instance.
func NewRayJobMetricsManager() *RayJobMetricsManager {
	collector := &RayJobMetricsManager{
		rayJobExecutionDurationSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kuberay_job_execution_duration_seconds",
				Help: "Duration from when the RayJob CR’s JobDeploymentStatus transitions from Initializing to either the Retrying state or a terminal state, such as Complete or Failed. The Retrying state indicates that the CR previously failed and that spec.backoffLimit is enabled.",
			},
			[]string{"name", "namespace", "job_deployment_status", "retry_count"},
		),
	}
	return collector
}

// Describe implements prometheus.Collector interface Describe method.
func (c *RayJobMetricsManager) Describe(ch chan<- *prometheus.Desc) {
	c.rayJobExecutionDurationSeconds.Describe(ch)
}

// Collect implements prometheus.Collector interface Collect method.
func (c *RayJobMetricsManager) Collect(ch chan<- prometheus.Metric) {
	c.rayJobExecutionDurationSeconds.Collect(ch)
}

func (c *RayJobMetricsManager) ObserveRayJobExecutionDuration(name, namespace string, jobDeploymentStatus rayv1.JobDeploymentStatus, retryCount int, duration float64) {
	c.rayJobExecutionDurationSeconds.WithLabelValues(name, namespace, string(jobDeploymentStatus), strconv.Itoa(retryCount)).Set(duration)
}
