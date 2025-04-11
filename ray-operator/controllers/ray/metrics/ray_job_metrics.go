package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Define all the prometheus metrics for RayJob
var (
	rayJobsCreatedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ray_jobs_created_total",
			Help: "The total number of RayJob CRs created",
		},
		[]string{"namespace"},
	)
)

func RayJobsCreatedTotalInc(namespace string) {
	rayJobsCreatedTotal.WithLabelValues(namespace).Inc()
}

func registerRayJobMetrics() {
	metrics.Registry.MustRegister(rayJobsCreatedTotal)
}
