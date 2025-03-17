package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var rayClustersCreatedCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "ray_clusters_created_total",
		Help: "The total number of RayClusters created",
	},
	[]string{"namespace", "created_by_ray_job", "created_by_ray_service"},
)

// CreatedRayClustersCounterInc increments the counter for RayClusters created
func CreatedRayClustersCounterInc(namespace string, createdByRayJob bool, createdByRayService bool) {
	rayClustersCreatedCounter.WithLabelValues(namespace, strconv.FormatBool(createdByRayJob), strconv.FormatBool(createdByRayService)).Inc()
}

func registerRayClusterMetrics() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(rayClustersCreatedCounter)
}
