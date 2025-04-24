package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// RayJobMetricsCollector implements the prometheus.Collector interface to collect ray job metrics.
type RayJobMetricsCollector struct {
	// Metrics
}

// NewRayJobMetricsCollector creates a new RayJobMetricsCollector instance.
func NewRayJobMetricsCollector() *RayJobMetricsCollector {
	collector := &RayJobMetricsCollector{}
	return collector
}

// Describe implements prometheus.Collector interface Describe method.
func (c *RayJobMetricsCollector) Describe(_ chan<- *prometheus.Desc) {
}

// Collect implements prometheus.Collector interface Collect method.
func (c *RayJobMetricsCollector) Collect(_ chan<- prometheus.Metric) {
}
