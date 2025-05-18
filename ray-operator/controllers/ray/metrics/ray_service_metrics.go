package metrics

import (
	"context"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

//go:generate mockgen -destination=mocks/ray_service_metrics_mock.go -package=mocks github.com/ray-project/kuberay/ray-operator/controllers/ray/metrics RayServiceMetricsObserver
type RayServiceMetricsObserver interface {
	ObserveRayServiceReady(name, namespace string, ready bool)
}

// RayServiceMetricsManager implements the prometheus.Collector and RayServiceMetricsObserver interface to collect ray service metrics.
type RayServiceMetricsManager struct {
	rayServiceInfo  *prometheus.Desc
	RayServiceReady *prometheus.GaugeVec
	client          client.Client
	log             logr.Logger
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
		RayServiceReady: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kuberay_service_ready",
				Help: "RayServiceReady means users can send requests to the underlying cluster and the number of serve endpoints is greater than 0.",
			},
			[]string{"name", "namespace", "condition"},
		),
		client: client,
		log:    ctrl.LoggerFrom(ctx),
	}
	return collector
}

// Describe implements prometheus.Collector interface Describe method.
func (c *RayServiceMetricsManager) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.rayServiceInfo
	c.RayServiceReady.Describe(ch)
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
	c.RayServiceReady.Collect(ch)
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

func (c *RayServiceMetricsManager) ObserveRayServiceReady(name, namespace string, ready bool) {
	c.RayServiceReady.WithLabelValues(name, namespace, strconv.FormatBool(!ready)).Set(0)
	c.RayServiceReady.WithLabelValues(name, namespace, strconv.FormatBool(ready)).Set(1)
}
