package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

type RayClusterMetricCollector struct {
	// Metrics
}

func NewRayClusterMetricCollector() *RayClusterMetricCollector {
	collector := &RayClusterMetricCollector{}
	return collector
}

// Describe implements prometheus.Collector interface Describe method.
func (c *RayClusterMetricCollector) Describe(_ chan<- *prometheus.Desc) {
}

// Collect implements prometheus.Collector interface Collect method.
func (c *RayClusterMetricCollector) Collect(_ chan<- prometheus.Metric) {
}
