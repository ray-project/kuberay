package common

import (
	"strings"
	"testing"
	"time"

	"k8s.io/component-base/metrics/testutil"
)

func TestObserveRayServicesReadyDuration(t *testing.T) {
	ObserveRayServicesReadyDuration("default", 2*time.Minute)

	metric := `
	# HELP ray_services_ready_duration_seconds The time between RayServices created to ready
	# TYPE ray_services_ready_duration_seconds histogram
	ray_services_ready_duration_seconds_bucket{namespace="default",le="30"} 0
	ray_services_ready_duration_seconds_bucket{namespace="default",le="60"} 0
	ray_services_ready_duration_seconds_bucket{namespace="default",le="120"} 1
	ray_services_ready_duration_seconds_bucket{namespace="default",le="180"} 1
	ray_services_ready_duration_seconds_bucket{namespace="default",le="240"} 1
	ray_services_ready_duration_seconds_bucket{namespace="default",le="300"} 1
	ray_services_ready_duration_seconds_bucket{namespace="default",le="600"} 1
	ray_services_ready_duration_seconds_bucket{namespace="default",le="900"} 1
	ray_services_ready_duration_seconds_bucket{namespace="default",le="1800"} 1
	ray_services_ready_duration_seconds_bucket{namespace="default",le="3600"} 1
	ray_services_ready_duration_seconds_count{namespace="default"} 1
	ray_services_ready_duration_seconds_sum{namespace="default"} 120
	`

	if err := testutil.CollectAndCompare(rayServicesReadyHistogram, strings.NewReader(metric)); err != nil {
		t.Errorf("unexpected collecting result:\n%s", err)
	}
}
