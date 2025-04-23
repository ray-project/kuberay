package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

type RayClusterMetricCollector struct {
	// Metrics
	rayClusterProvisionedDuration *prometheus.GaugeVec
}

func NewRayClusterMetricCollector() *RayClusterMetricCollector {
	collector := &RayClusterMetricCollector{
		rayClusterProvisionedDuration: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kuberay_cluster_provisioned_duration_seconds",
				Help: "The time, in seconds, when a RayCluster's `RayClusterProvisioned` status transitions from false (or unset) to true",
			},
			[]string{"name", "namespace"},
		),
	}
	return collector
}

// Describe implements prometheus.Collector interface Describe method.
func (c *RayClusterMetricCollector) Describe(ch chan<- *prometheus.Desc) {
	c.rayClusterProvisionedDuration.Describe(ch)
}

// Collect implements prometheus.Collector interface Collect method.
func (c *RayClusterMetricCollector) Collect(ch chan<- prometheus.Metric) {
	c.rayClusterProvisionedDuration.Collect(ch)
}

// ObserveRayClusterProvisionedDuration observes the duration of RayCluster from creation to provisioned
func (c *RayClusterMetricCollector) ObserveRayClusterProvisionedDuration(name, namespace string, duration float64) {
	c.rayClusterProvisionedDuration.WithLabelValues(name, namespace).Set(duration)
}
