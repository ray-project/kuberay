package metrics

func Register() {
	registerRayClusterMetrics()
	registerRayJobMetrics()
}
