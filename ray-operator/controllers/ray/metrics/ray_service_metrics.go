package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

//go:generate mockgen -destination=mocks/ray_service_metrics_mock.go -package=mocks github.com/ray-project/kuberay/ray-operator/controllers/ray/metrics RayServiceMetricsObserver
type RayServiceMetricsObserver interface {
	ObserveRayServiceReady(name, namespace string, ready bool)
}

// RayServiceMetricsManager implements the prometheus.Collector and RayServiceMetricsObserver interface to collect ray service metrics.
type RayServiceMetricsManager struct {
	RayServiceReady *prometheus.GaugeVec
}

// NewRayServiceMetricsManager creates a new RayServiceMetricsManager instance.
func NewRayServiceMetricsManager() *RayServiceMetricsManager {
	collector := &RayServiceMetricsManager{
		RayServiceReady: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kuberay_service_ready",
				Help: "RayServiceReady means users can send requests to the underlying cluster and the number of serve endpoints is greater than 0.",
			},
			[]string{"name", "namespace", "condition"},
		),
	}
	return collector
}

// Describe implements prometheus.Collector interface Describe method.
func (c *RayServiceMetricsManager) Describe(ch chan<- *prometheus.Desc) {
	c.RayServiceReady.Describe(ch)
}

// Collect implements prometheus.Collector interface Collect method.
func (c *RayServiceMetricsManager) Collect(ch chan<- prometheus.Metric) {
	c.RayServiceReady.Collect(ch)
}

func (c *RayServiceMetricsManager) ObserveRayServiceReady(name, namespace string, ready bool) {
	c.RayServiceReady.WithLabelValues(name, namespace, strconv.FormatBool(!ready)).Set(0)
	c.RayServiceReady.WithLabelValues(name, namespace, strconv.FormatBool(ready)).Set(1)
}
