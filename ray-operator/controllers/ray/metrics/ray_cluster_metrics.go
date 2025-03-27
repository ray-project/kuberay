package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// TODO: Deprecate these metrics

// Define all the prometheus metrics for RayCluster
var (
	clustersCreatedCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ray_operator_clusters_created_total",
			Help: "Counts number of clusters created",
		},
		[]string{"namespace"},
	)
	clustersDeletedCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ray_operator_clusters_deleted_total",
			Help: "Counts number of clusters deleted",
		},
		[]string{"namespace"},
	)
	clustersSuccessfulCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ray_operator_clusters_successful_total",
			Help: "Counts number of clusters successful",
		},
		[]string{"namespace"},
	)
	clustersFailedCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ray_operator_clusters_failed_total",
			Help: "Counts number of clusters failed",
		},
		[]string{"namespace"},
	)
)

func CreatedClustersCounterInc(namespace string) {
	clustersCreatedCount.WithLabelValues(namespace).Inc()
}

// TODO: We don't handle the delete events in new reconciler mode, how to emit deletion metrics?
func DeletedClustersCounterInc(namespace string) {
	clustersDeletedCount.WithLabelValues(namespace).Inc()
}

func SuccessfulClustersCounterInc(namespace string) {
	clustersSuccessfulCount.WithLabelValues(namespace).Inc()
}

func FailedClustersCounterInc(namespace string) {
	clustersFailedCount.WithLabelValues(namespace).Inc()
}

func registerRayClusterMetrics() {
	metrics.Registry.MustRegister(clustersCreatedCount,
		clustersDeletedCount,
		clustersSuccessfulCount,
		clustersFailedCount)
}
