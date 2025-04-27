package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// RayClusterCollector implements the prometheus.Collector and ray.RayClusterMetricsCollector interface to collect ray cluster metrics.
type RayClusterCollector struct {
	// Metrics
	rayClusterProvisionedDuration  *prometheus.GaugeVec
	rayClusterHeadPodReadyDuration *prometheus.GaugeVec
}

// NewRayClusterCollector creates a new RayClusterCollector instance.
func NewRayClusterCollector() *RayClusterCollector {
	collector := &RayClusterCollector{
		rayClusterProvisionedDuration: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kuberay_cluster_provisioned_duration_seconds",
				Help: "The time, in seconds, when a RayCluster's `RayClusterProvisioned` status transitions from false (or unset) to true",
			},
			[]string{"name", "namespace"},
		),
		rayClusterHeadPodReadyDuration: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kuberay_cluster_head_pod_ready_duration_seconds",
				Help: "The time, in seconds, from RayClusters created to head pod ready",
			},
			[]string{"name", "namespace"},
		),
	}
	return collector
}

// Describe implements prometheus.Collector interface Describe method.
func (c *RayClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	c.rayClusterProvisionedDuration.Describe(ch)
	c.rayClusterHeadPodReadyDuration.Describe(ch)
}

// Collect implements prometheus.Collector interface Collect method.
func (c *RayClusterCollector) Collect(ch chan<- prometheus.Metric) {
	c.rayClusterProvisionedDuration.Collect(ch)
	c.rayClusterHeadPodReadyDuration.Collect(ch)
}

// ObserveRayClusterProvisionedDuration observes the duration of RayCluster's status transition from false (or unset) to true
func (c *RayClusterCollector) ObserveRayClusterProvisionedDuration(name, namespace string, duration float64) {
	c.rayClusterProvisionedDuration.WithLabelValues(name, namespace).Set(duration)
}

// ObserveRayClusterHeadPodReadyDuration observes the duration of RayCluster from creation to head pod ready
func (c *RayClusterCollector) ObserveRayClusterHeadPodReadyDuration(name, namespace string, duration float64) {
	c.rayClusterHeadPodReadyDuration.WithLabelValues(name, namespace).Set(duration)
}

type RayClusterNoopCollector struct{}

func NewRayClusterNoopCollector() *RayClusterNoopCollector {
	return &RayClusterNoopCollector{}
}

func (c *RayClusterNoopCollector) ObserveRayClusterProvisionedDuration(_ string, _ string, _ float64) {
}

func (c *RayClusterNoopCollector) ObserveRayClusterHeadPodReadyDuration(_ string, _ string, _ float64) {
}
