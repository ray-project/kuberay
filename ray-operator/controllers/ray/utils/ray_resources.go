package utils

import (
	"strconv"
	"strings"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"

	v1 "k8s.io/api/core/v1"
)

// Updates the user-specified rayResource spec with CPU, GPU, and memory data from the rayStartParams and the pod template.
// CPU, GPU, and memory data from user-specified rayResourceSpec overrides data from rayStartParams.
// CPU, GPU, and memory data from rayStartParams overrides data from the podTemplate.
// If CPU, GPU, or memory is not present in the resource map returned by this method, Ray will attempt to
// infer these upon "ray start".
// The map returned by this method will be written to the RayCluster CR's Status field, which allows it to be
// read by the Ray Autoscaler.
func ComputeRayResources(
	rayResourceSpec rayiov1alpha1.RayResources,
	rayStartParams map[string]string,
	rayContainerResources v1.ResourceRequirements,
) (updatedRayResources rayiov1alpha1.RayResources) {

	updatedRayResources = make(rayiov1alpha1.RayResources)
	// Copy user CPU, GPU, memory overrides and custom resources.
	for key, value := range rayResourceSpec {
		updatedRayResources[key] = value
	}

	// Compute CPU unless user has already provided an override.
	if _, ok := updatedRayResources["CPU"]; !ok {
		// If override is not specified, look at rayStartParams and the the Ray container's resources.
		num_cpu := computeCPU(rayStartParams, rayContainerResources)
		if num_cpu > 0 {
			updatedRayResources["CPU"] = num_cpu
		}
	}

	// Compute GPU unless user has already provided an override.
	if _, ok := updatedRayResources["GPU"]; !ok {
		// If override is not specified, look at rayStartParams and the the Ray container's resources.
		num_gpu := computeGPU(rayStartParams, rayContainerResources)
		if num_gpu > 0 {
			updatedRayResources["GPU"] = num_gpu
		}
	}

	// Compute memory unless user has already provided an override.
	if _, ok := updatedRayResources["memory"]; !ok {
		// If override is not specified, look at rayStartParams and the the Ray container's resources.
		memory := computeMemory(rayStartParams, rayContainerResources)
		if memory > 0 {
			updatedRayResources["memory"] = memory
		}
	}
	return updatedRayResources
}

func computeCPU(rayStartParams map[string]string, rayContainerResources v1.ResourceRequirements) int64 {
	// Try using the num-cpus rayStartParam.
	if cpuParam, ok := rayStartParams["num-cpus"]; ok {
		cpuInt, err := strconv.ParseInt(cpuParam, 10, 64)
		if err != nil {
			rayClusterLog.Error(err, "Failed to parse num-cpus rayStartParam.")
		} else {
			return cpuInt
		}
	}

	// If we couldn't read CPU from the rayStartParams, trying getting the CPU count from the
	// container resources.
	cpuQuantity := rayContainerResources.Limits[v1.ResourceCPU]
	if !cpuQuantity.IsZero() {
		return cpuQuantity.Value()
	}

	// The user might not have set CPU limits for the Ray container.
	// That's not adviseable, but we don't consider it an error.
	// Return a 0 value, which will be ignored by the caller of this function.
	return 0
}

func computeGPU(rayStartParams map[string]string, rayContainerResources v1.ResourceRequirements) int64 {
	// Try using the num-gpus rayStartParam.
	if gpuParam, ok := rayStartParams["num-gpus"]; ok {
		gpuInt, err := strconv.ParseInt(gpuParam, 10, 64)
		if err != nil {
			rayClusterLog.Error(err, "Failed to parse num-cpus rayStartParam.")
		} else {
			return gpuInt
		}
	}

	// If we couldn't read GPU from the rayStartParams, trying getting the GPU count from the
	// container resources.

	// Scan for a resource name containing gpu, e.g. "nvidia.com/gpu".
	for resourceName, resourceQuantity := range rayContainerResources.Limits {
		if strings.Contains(string(resourceName), "gpu") {
			// For now, we only support one GPU type.
			// Return the first match for a GPU quantity.
			return resourceQuantity.Value()
		}
	}

	// No GPUs specified. Return a 0 value, which will be ignored by the caller of this function.
	return 0
}

func computeMemory(rayStartParams map[string]string, rayContainerResources v1.ResourceRequirements) int64 {
	// First look in the memory rayStartParam.
	if memoryParam, ok := rayStartParams["memory"]; ok {
		memoryInt, err := strconv.ParseInt(memoryParam, 10, 64)
		if err != nil {
			rayClusterLog.Error(err, "Failed to parse memory rayStartParam.")
		} else {
			return memoryInt
		}
	}

	// If we couldn't read CPU from the rayStartParams, trying getting the CPU count from the
	// container resources.
	memoryQuantity := rayContainerResources.Limits[v1.ResourceMemory]
	if !memoryQuantity.IsZero() {
		return memoryQuantity.Value()
	}

	// The user might not have set memory limits for the Ray container.
	// That's very inadvisable, but we don't consider it an error.
	// Return a 0 value, which will be ignored by the caller of this function.
	return 0
}
