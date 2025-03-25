package common

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestObserveRayClusterProvisionedDuration(t *testing.T) {
	ObserveRayClusterProvisionedDuration("default", 2*time.Minute)

	metric := `
		# HELP ray_cluster_provisioned_duration_seconds The time from RayClusters created to all ray pods are ready for the first time (RayClusterProvisioned) in seconds
		# TYPE ray_cluster_provisioned_duration_seconds histogram
		ray_cluster_provisioned_duration_seconds_bucket{namespace="default",le="30"} 0
		ray_cluster_provisioned_duration_seconds_bucket{namespace="default",le="60"} 0
		ray_cluster_provisioned_duration_seconds_bucket{namespace="default",le="120"} 1
		ray_cluster_provisioned_duration_seconds_bucket{namespace="default",le="180"} 1
		ray_cluster_provisioned_duration_seconds_bucket{namespace="default",le="240"} 1
		ray_cluster_provisioned_duration_seconds_bucket{namespace="default",le="300"} 1
		ray_cluster_provisioned_duration_seconds_bucket{namespace="default",le="600"} 1
		ray_cluster_provisioned_duration_seconds_bucket{namespace="default",le="900"} 1
		ray_cluster_provisioned_duration_seconds_bucket{namespace="default",le="1800"} 1
		ray_cluster_provisioned_duration_seconds_bucket{namespace="default",le="3600"} 1
		ray_cluster_provisioned_duration_seconds_sum{namespace="default"} 120
		ray_cluster_provisioned_duration_seconds_count{namespace="default"} 1
	`

	if err := testutil.CollectAndCompare(rayClusterProvisionedHistogram, strings.NewReader(metric), "ray_cluster_provisioned_duration_seconds"); err != nil {
		t.Errorf("unexpected collecting result:\n%s", err)
	}
}
