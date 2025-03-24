package common

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestObserveRayClusterHeadPodReadyDuration(t *testing.T) {
	ObserveRayClusterHeadPodReadyDuration("default", 2*time.Minute)

	metric := `
	# HELP ray_cluster_head_pod_ready_duration_seconds The time from RayClusters created to head pod ready in seconds
	# TYPE ray_cluster_head_pod_ready_duration_seconds histogram
	ray_cluster_head_pod_ready_duration_seconds_bucket{namespace="default",le="30"} 0
	ray_cluster_head_pod_ready_duration_seconds_bucket{namespace="default",le="60"} 0
	ray_cluster_head_pod_ready_duration_seconds_bucket{namespace="default",le="120"} 1
	ray_cluster_head_pod_ready_duration_seconds_bucket{namespace="default",le="180"} 1
	ray_cluster_head_pod_ready_duration_seconds_bucket{namespace="default",le="240"} 1
	ray_cluster_head_pod_ready_duration_seconds_bucket{namespace="default",le="300"} 1
	ray_cluster_head_pod_ready_duration_seconds_bucket{namespace="default",le="600"} 1
	ray_cluster_head_pod_ready_duration_seconds_bucket{namespace="default",le="900"} 1
	ray_cluster_head_pod_ready_duration_seconds_bucket{namespace="default",le="1800"} 1
	ray_cluster_head_pod_ready_duration_seconds_bucket{namespace="default",le="3600"} 1
	ray_cluster_head_pod_ready_duration_seconds_sum{namespace="default"} 120
	ray_cluster_head_pod_ready_duration_seconds_count{namespace="default"} 1
	`

	if err := testutil.CollectAndCompare(rayClusterHeadPodReadyHistogram, strings.NewReader(metric)); err != nil {
		t.Errorf("unexpected collecting result:\n%s", err)
	}
}
