package metrics

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

// RayServiceMetricsManager implements the prometheus.Collector and RayServiceMetricsObserver interface to collect ray service metrics.
type RayServiceMetricsManager struct {
	rayServiceInfo *prometheus.Desc
	client         client.Client
	log            logr.Logger
}

// NewRayServiceMetricsManager creates a new RayServiceMetricsManager instance.
func NewRayServiceMetricsManager(ctx context.Context, client client.Client) *RayServiceMetricsManager {
	collector := &RayServiceMetricsManager{
		rayServiceInfo: prometheus.NewDesc(
			"kuberay_service_info",
			"Metadata information about RayService custom resources",
			[]string{"name", "namespace"},
			nil,
		),
		client: client,
		log:    ctrl.LoggerFrom(ctx),
	}
	return collector
}

// Describe implements prometheus.Collector interface Describe method.
func (c *RayServiceMetricsManager) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.rayServiceInfo
}

// Collect implements prometheus.Collector interface Collect method.
func (c *RayServiceMetricsManager) Collect(ch chan<- prometheus.Metric) {
	var rayServiceList rayv1.RayServiceList
	if err := c.client.List(context.Background(), &rayServiceList); err != nil {
		c.log.Error(err, "Failed to list RayServices")
		return
	}

	for _, rayService := range rayServiceList.Items {
		c.collectRayServiceInfo(&rayService, ch)
	}
}

func (c *RayServiceMetricsManager) collectRayServiceInfo(service *rayv1.RayService, ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(
		c.rayServiceInfo,
		prometheus.GaugeValue,
		1,
		service.Name,
		service.Namespace,
	)
}
