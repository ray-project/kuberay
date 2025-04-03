package common

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCreatedRayServicesCounterInc(t *testing.T) {
	CreatedRayServicesCounterInc("default")
	CreatedRayServicesCounterInc("default")
	CreatedRayServicesCounterInc("test")
	CreatedRayServicesCounterInc("test")

	expected := `
	# HELP ray_services_created_total The total number of RayServices created
	# TYPE ray_services_created_total counter
	ray_services_created_total{namespace="default"} 2
	ray_services_created_total{namespace="test"} 2
	`
	if err := testutil.CollectAndCompare(rayServicesCreatedCounter, strings.NewReader(expected)); err != nil {
		t.Errorf("unexpected collecting result:\n%s", err)
	}
}
