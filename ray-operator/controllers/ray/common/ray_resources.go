package common

import (
	"encoding/json"
	"fmt"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	v1 "k8s.io/api/core/v1"
)

func buildRayResourcePatch(instance rayiov1alpha1.RayCluster) ([]byte, error) {
	// Build a JSON Patch as a slice of maps, one entry per groupSpec.
	var patch_slice []PatchOperation
	headRayResources := getRayResources(instance.Spec.HeadGroupSpec)
	if true {
		patch_slice = append(
			patch_slice,
			PatchOperation{
				Op:    "add",
				Path:  "/status/headGroupStatus/rayResources",
				Value: headRayResources,
			},
		)
	}
	for i := 0; i < 1; i++ {
		workerRayResources := getDetectedRayResources()
		if true {
			patch_slice = append(
				patch_slice,
				PatchOperation{
					Op:    "add",
					Path:  fmt.Sprintf("/spec/workerGroupStatuses/%v/rayResources", i),
					Value: workerRayResources,
				},
			)
		}
	}
	patch_bytes, err := json.Marshal(patch_slice)
	return patch_bytes, err
}

func getDetectedRayResources(
	curRayResources map[string]int32, rayStartParams map[string]string, podTemplate v1.PodTemplateSpec,
) map[string]int32 {
	return map[string]int32{"MOOO": 123, "HOOOOOH": 248}
}
