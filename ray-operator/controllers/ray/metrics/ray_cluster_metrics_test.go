package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCreatedRayClustersCounterInc(t *testing.T) {
	CreatedRayClustersCounterInc("default", true, false)
	CreatedRayClustersCounterInc("default", false, true)
	CreatedRayClustersCounterInc("test", false, false)
	CreatedRayClustersCounterInc("test", false, false)

	expected := `
	# HELP ray_clusters_created_total The total number of RayClusters created
	# TYPE ray_clusters_created_total counter
	ray_clusters_created_total{created_by_ray_job="true",created_by_ray_service="false",namespace="default"} 1
	ray_clusters_created_total{created_by_ray_job="false",created_by_ray_service="true",namespace="default"} 1
	ray_clusters_created_total{created_by_ray_job="false",created_by_ray_service="false",namespace="test"} 2
	`
	if err := testutil.CollectAndCompare(rayClustersCreatedCounter, strings.NewReader(expected)); err != nil {
		t.Errorf("unexpected collecting result:\n%s", err)
	}
}
