package common

import (
	"encoding/json"
	"fmt"
	"reflect"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	v1 "k8s.io/api/core/v1"
)

func buildRayResourcePatch(instance rayiov1alpha1.RayCluster) ([]byte, error) {
	// Build a JSON Patch as a slice of maps, one entry per groupSpec.
	var patch_slice []utils.PatchOperation
	headGroupSpec := instance.Spec.HeadGroupSpec
	headDetectedRayResources := getDetectedRayResources(
		headGroupSpec.RayStartParams,
		headGroupSpec.Template,
		headGroupSpec.RayResources,
	)
	previousHeadDetectedRayResources := instance.Status.HeadStatus.DetectedRayResources
	// If the detected Ray resources we just computed don't match the status,
	// add a patch operation to update the detected Ray resources.
	if !reflect.DeepEqual(headDetectedRayResources, previousHeadDetectedRayResources) {
		patch_slice = append(
			patch_slice,
			utils.PatchOperation{
				Op:    "add",
				Path:  "/status/headGroupStatus/detectedRayResources",
				Value: headDetectedRayResources,
			},
		)
	}
	workerGroupSpecs := instance.Spec.WorkerGroupSpecs
	for i, workerGroupSpec := range workerGroupSpecs {
		workerDetectedRayResources := getDetectedRayResources(
			workerGroupSpec.RayStartParams,
			workerGroupSpec.Template,
			workerGroupSpec.RayResources,
		)
		previousWorkerDetectedRayResources := instance.Status.WorkerGroupStatuses[i].DetectedRayResources
		if !reflect.DeepEqual(workerDetectedRayResources, previousWorkerDetectedRayResources) {
			patch_slice = append(
				patch_slice,
				utils.PatchOperation{
					Op:    "add",
					Path:  fmt.Sprintf("/spec/workerGroupStatuses/%v/rayResources", i),
					Value: workerDetectedRayResources,
				},
			)
		}

		if workerGroupSpec.GroupName != instance.Status.WorkerGroupStatuses[i].GroupName {
			return 0
		}
	}
	patch_bytes, err := json.Marshal(patch_slice)
	return patch_bytes, err
}

//Determines the Ray resources of a Ray head or worker group, based on the rayStartParams,
//the podTemplate and the user-specifed rayResources spec.
//Data from rayResources overrides data from rayStartParams.
//Data from rayStartParams overrides data from the podTemplate.
func getDetectedRayResources(
	rayStartParams map[string]string, template v1.PodTemplateSpec, rayResources map[string]int32,
) map[string]int32 {
	return map[string]int32{"MOOO": 123, "HOOOOOH": 248}
}
