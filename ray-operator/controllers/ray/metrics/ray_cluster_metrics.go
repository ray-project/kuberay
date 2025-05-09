package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

//go:generate mockgen -destination=mocks/ray_cluster_metrics_mock.go -package=mocks github.com/ray-project/kuberay/ray-operator/controllers/ray/metrics RayClusterMetricsObserver
type RayClusterMetricsObserver interface {
	ObserveRayClusterProvisionedDuration(name, namespace string, duration float64)
	ObserveRayClusterHeadPodReady(name, namespace string, ready bool)
}

// RayClusterMetricsManager implements the prometheus.Collector and RayClusterMetricsObserver interface to collect ray cluster metrics.
type RayClusterMetricsManager struct {
	rayClusterProvisionedDurationSeconds *prometheus.GaugeVec
	rayClusterHeadPodReady               *prometheus.GaugeVec
}

// NewRayClusterMetricsManager creates a new RayClusterManager instance.
func NewRayClusterMetricsManager() *RayClusterMetricsManager {
	manager := &RayClusterMetricsManager{
		rayClusterProvisionedDurationSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kuberay_cluster_provisioned_duration_seconds",
				Help: "The time, in seconds, when a RayCluster's `RayClusterProvisioned` status transitions from false (or unset) to true",
			},
			[]string{"name", "namespace"},
		),
		rayClusterHeadPodReady: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kuberay_cluster_head_pod_ready",
				Help: "The time, in seconds, when a RayCluster's head pod is ready",
			},
			[]string{"name", "namespace", "condition"},
		),
	}
	return manager
}

// Describe implements prometheus.Collector interface Describe method.
func (c *RayClusterMetricsManager) Describe(ch chan<- *prometheus.Desc) {
	c.rayClusterProvisionedDurationSeconds.Describe(ch)
	c.rayClusterHeadPodReady.Describe(ch)
}

// Collect implements prometheus.Collector interface Collect method.
func (c *RayClusterMetricsManager) Collect(ch chan<- prometheus.Metric) {
	c.rayClusterProvisionedDurationSeconds.Collect(ch)
	c.rayClusterHeadPodReady.Collect(ch)
}

func (c *RayClusterMetricsManager) ObserveRayClusterProvisionedDuration(name, namespace string, duration float64) {
	c.rayClusterProvisionedDurationSeconds.WithLabelValues(name, namespace).Set(duration)
}

func (c *RayClusterMetricsManager) ObserveRayClusterHeadPodReady(name, namespace string, ready bool) {
	c.rayClusterHeadPodReady.DeleteLabelValues(name, namespace, strconv.FormatBool(!ready))
	c.rayClusterHeadPodReady.WithLabelValues(name, namespace, strconv.FormatBool(ready)).Set(1)
}
