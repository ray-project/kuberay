package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -destination=mocks/ray_cluster_metrics_mock.go -package=mocks github.com/ray-project/kuberay/ray-operator/controllers/ray/metrics RayClusterMetricsObserver
type RayClusterMetricsObserver interface {
	ObserveRayClusterProvisionedDuration(name, namespace string, duration float64)
}

type RayClusterMetricsManager struct {
	rayClusterProvisionedDurationSeconds *prometheus.GaugeVec
	rayClusterInfo                       *prometheus.Desc
	client                               ctrlclient.Client
}

// NewRayClusterMetricsManager creates a new RayClusterManager instance.
func NewRayClusterMetricsManager(client ctrlclient.Client) *RayClusterMetricsManager {
	manager := &RayClusterMetricsManager{
		rayClusterProvisionedDurationSeconds: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kuberay_cluster_provisioned_duration_seconds",
				Help: "The time, in seconds, when a RayCluster's `RayClusterProvisioned` status transitions from false (or unset) to true",
			},
			[]string{"name", "namespace"},
		),
		rayClusterInfo: prometheus.NewDesc(
			"kuberay_cluster_info",
			"Metadata information about Ray clusters",
			[]string{"name", "namespace", "owner_kind"},
			nil,
		),
		client: client,
	}
	return manager
}

// Describe implements prometheus.Collector interface Describe method.
func (c *RayClusterMetricsManager) Describe(ch chan<- *prometheus.Desc) {
	c.rayClusterProvisionedDurationSeconds.Describe(ch)
	ch <- c.rayClusterInfo
}

// Collect implements prometheus.Collector interface Collect method.
func (c *RayClusterMetricsManager) Collect(ch chan<- prometheus.Metric) {
	c.rayClusterProvisionedDurationSeconds.Collect(ch)

	var rayClusterList rayv1.RayClusterList
	err := c.client.List(context.TODO(), &rayClusterList)
	if err != nil {
		return
	}

	for _, rayCluster := range rayClusterList.Items {
		c.collectRayClusterInfo(&rayCluster, ch)
	}
}

func (c *RayClusterMetricsManager) ObserveRayClusterProvisionedDuration(name, namespace string, duration float64) {
	c.rayClusterProvisionedDurationSeconds.WithLabelValues(name, namespace).Set(duration)
}

func (c *RayClusterMetricsManager) collectRayClusterInfo(cluster *rayv1.RayCluster, ch chan<- prometheus.Metric) {
	ownerKind := "none"
	if len(cluster.OwnerReferences) > 0 {
		ownerKind = cluster.OwnerReferences[0].Kind
	}

	ch <- prometheus.MustNewConstMetric(
		c.rayClusterInfo,
		prometheus.GaugeValue,
		1,
		cluster.Name,
		cluster.Namespace,
		ownerKind,
	)
}
