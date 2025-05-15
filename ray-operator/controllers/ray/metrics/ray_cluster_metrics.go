package metrics

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

//go:generate mockgen -destination=mocks/ray_cluster_metrics_mock.go -package=mocks github.com/ray-project/kuberay/ray-operator/controllers/ray/metrics RayClusterMetricsObserver
type RayClusterMetricsObserver interface {
	ObserveRayClusterProvisionedDuration(name, namespace string, duration float64)
}

// RayClusterMetricsManager implements the prometheus.Collector and RayClusterMetricsObserver interface to collect ray cluster metrics.
type RayClusterMetricsManager struct {
	rayClusterProvisionedDurationSeconds *prometheus.GaugeVec
	rayClusterInfo                       *prometheus.Desc
	client                               client.Client
	log                                  logr.Logger
}

// NewRayClusterMetricsManager creates a new RayClusterManager instance.
func NewRayClusterMetricsManager(client client.Client) *RayClusterMetricsManager {
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
		log:    logf.Log.WithName("raycluster-metrics"),
	}
	return manager
}

// Describe implements prometheus.Collector interface Describe method.
func (r *RayClusterMetricsManager) Describe(ch chan<- *prometheus.Desc) {
	r.rayClusterProvisionedDurationSeconds.Describe(ch)
	ch <- r.rayClusterInfo
}

// Collect implements prometheus.Collector interface Collect method.
func (r *RayClusterMetricsManager) Collect(ch chan<- prometheus.Metric) {
	r.rayClusterProvisionedDurationSeconds.Collect(ch)

	var rayClusterList rayv1.RayClusterList
	err := r.client.List(context.Background(), &rayClusterList)
	if err != nil {
		r.log.Error(err, "Failed to list RayClusters")
		return
	}

	for _, rayCluster := range rayClusterList.Items {
		r.collectRayClusterInfo(&rayCluster, ch)
	}
}

func (r *RayClusterMetricsManager) ObserveRayClusterProvisionedDuration(name, namespace string, duration float64) {
	r.rayClusterProvisionedDurationSeconds.WithLabelValues(name, namespace).Set(duration)
}

func (r *RayClusterMetricsManager) collectRayClusterInfo(cluster *rayv1.RayCluster, ch chan<- prometheus.Metric) {
	ownerKind := "None"
	if len(cluster.OwnerReferences) > 0 {
		ownerKind = cluster.OwnerReferences[0].Kind
	}

	ch <- prometheus.MustNewConstMetric(
		r.rayClusterInfo,
		prometheus.GaugeValue,
		1,
		cluster.Name,
		cluster.Namespace,
		ownerKind,
	)
}
