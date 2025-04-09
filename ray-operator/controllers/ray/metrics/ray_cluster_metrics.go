package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

type RayClusterMetricCollector struct {
	// Metrics
	rayClustersCreatedCounter *prometheus.CounterVec
}

func NewRayClusterMetricCollector() *RayClusterMetricCollector {
	collector := &RayClusterMetricCollector{
		rayClustersCreatedCounter: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ray_clusters_created_total",
				Help: "The total number of RayClusters created",
			},
			[]string{"namespace", "created_by_ray_job", "created_by_ray_service"},
		),
	}
	return collector
}

// Describe implements prometheus.Collector interface Describe method.
func (c *RayClusterMetricCollector) Describe(ch chan<- *prometheus.Desc) {
	c.rayClustersCreatedCounter.Describe(ch)
}

// Collect implements prometheus.Collector interface Collect method.
func (c *RayClusterMetricCollector) Collect(ch chan<- prometheus.Metric) {
	c.rayClustersCreatedCounter.Collect(ch)
}

// IncRayClustersCreatedCounter increments the counter for RayClusters created
func (c *RayClusterMetricCollector) IncRayClustersCreatedCounter(namespace string, createdByRayJob bool, createdByRayService bool) {
	c.rayClustersCreatedCounter.WithLabelValues(namespace, strconv.FormatBool(createdByRayJob), strconv.FormatBool(createdByRayService)).Inc()
}
