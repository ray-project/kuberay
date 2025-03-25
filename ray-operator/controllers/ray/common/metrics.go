package common

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Define all the prometheus counters for all clusters
var (
	rayClusterProvisionedHistogram = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "ray_cluster_provisioned_duration_seconds",
			Help: "The time from RayClusters created to all ray pods are ready for the first time (RayClusterProvisioned) in seconds",
			// It may not be applicable to all users, but default buckets cannot be used either.
			// For reference, see: https://github.com/prometheus/client_golang/blob/331dfab0cc853dca0242a0d96a80184087a80c1d/prometheus/histogram.go#L271
			Buckets: []float64{30, 60, 120, 180, 240, 300, 600, 900, 1800, 3600},
		},
		[]string{"namespace"},
	)
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(rayClusterProvisionedHistogram)
}

// ObserveRayClusterProvisionedDuration observes the duration of RayCluster from creation to provisioned
func ObserveRayClusterProvisionedDuration(namespace string, duration time.Duration) {
	rayClusterProvisionedHistogram.WithLabelValues(namespace).Observe(duration.Seconds())
}
