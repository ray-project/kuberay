package common

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Define all the prometheus counters for all clusters
var (
	rayClustersCreatedCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ray_clusters_created_total",
			Help: "The total number of RayClusters created",
		},
		[]string{"namespace", "created_by_ray_job", "created_by_ray_service"},
	)
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(rayClustersCreatedCounter)
}

func CreatedRayClustersCounterInc(namespace string, createdByRayJob bool, createdByRayService bool) {
	rayClustersCreatedCounter.WithLabelValues(namespace, strconv.FormatBool(createdByRayJob), strconv.FormatBool(createdByRayService)).Inc()
}
