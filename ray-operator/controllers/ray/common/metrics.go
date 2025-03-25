package common

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Define all the prometheus counters for all clusters
var (
	rayServicesCreatedCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ray_services_created_total",
			Help: "The total number of RayServices created",
		},
		[]string{"namespace"},
	)
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(rayServicesCreatedCounter)
}

func CreatedRayServicesCounterInc(namespace string) {
	rayServicesCreatedCounter.WithLabelValues(namespace).Inc()
}
