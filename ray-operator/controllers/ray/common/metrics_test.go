package common

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCreatedClustersCounterInc(t *testing.T) {
	clustersCreatedCount.Reset()

	CreatedClustersCounterInc("default", true, false)
	CreatedClustersCounterInc("default", false, true)
	CreatedClustersCounterInc("test", false, false)
	CreatedClustersCounterInc("test", false, false)

	expected := `
	# HELP ray_clusters_created_total The total number of ray clusters created
	# TYPE ray_clusters_created_total counter
	ray_clusters_created_total{created_by_ray_job="true",created_by_ray_service="false",namespace="default"} 1
	ray_clusters_created_total{created_by_ray_job="false",created_by_ray_service="true",namespace="default"} 1
	ray_clusters_created_total{created_by_ray_job="false",created_by_ray_service="false",namespace="test"} 2
	`
	if err := testutil.CollectAndCompare(clustersCreatedCount, strings.NewReader(expected)); err != nil {
		t.Errorf("unexpected collecting result:\n%s", err)
	}
}
