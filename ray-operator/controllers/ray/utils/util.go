package utils

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/sirupsen/logrus"
	"github.com/tebeka/selenium/log"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var rayClusterLog = logf.Log.WithName("RayCluster-Controller")

// IsCreated returns true if pod has been created and is maintained by the API server
func IsCreated(pod *corev1.Pod) bool {
	return pod.Status.Phase != ""
}

// CheckName makes sure the name does not start with a numeric value and the total length is < 63 char
func CheckName(s string) string {
	maxLength := 50 // 63 - (max(8,6) + 5 ) // 6 to 8 char are consumed at the end with "-head-" or -worker- + 5 generated.

	if len(s) > maxLength {
		// shorten the name
		offset := int(math.Abs(float64(maxLength) - float64(len(s))))
		fmt.Printf("pod name is too long: len = %v, we will shorten it by offset = %v\n", len(s), offset)
		s = s[offset:]
	}

	// cannot start with a numeric value
	if unicode.IsDigit(rune(s[0])) {
		s = "r" + s[1:]
	}

	// cannot start with a punctuation
	if unicode.IsPunct(rune(s[0])) {
		fmt.Println(s)
		s = "r" + s[1:]
	}

	return s
}

// CheckLabel makes sure the label value does not start with a punctuation and the total length is < 63 char
func CheckLabel(s string) string {
	maxLenght := 63

	if len(s) > maxLenght {
		// shorten the name
		offset := int(math.Abs(float64(maxLenght) - float64(len(s))))
		fmt.Printf("label value is too long: len = %v, we will shorten it by offset = %v\n", len(s), offset)
		s = s[offset:]
	}

	// cannot start with a punctuation
	if unicode.IsPunct(rune(s[0])) {
		fmt.Println(s)
		s = "r" + s[1:]
	}

	return s
}

// Before Get substring before a string.
func Before(value string, a string) string {
	pos := strings.Index(value, a)
	if pos == -1 {
		return ""
	}
	return value[0:pos]
}

// FormatInt returns the string representation of i in the given base,
// for 2 <= base <= 36. The result uses the lower-case letters 'a' to 'z'
// for digit values >= 10.
func FormatInt32(n int32) string {
	return strconv.FormatInt(int64(n), 10)
}

// GetNamespace return namespace
func GetNamespace(metaData metav1.ObjectMeta) string {
	if metaData.Namespace == "" {
		return "default"
	}
	return metaData.Namespace
}

// GenerateServiceName generates a ray head service name from cluster name
func GenerateServiceName(clusterName string) string {
	return fmt.Sprintf("%s-%s-%s", clusterName, rayiov1alpha1.HeadNode, "svc")
}

// GenerateIdentifier generates identifier of same group pods
func GenerateIdentifier(clusterName string, nodeType rayiov1alpha1.RayNodeType) string {
	return fmt.Sprintf("%s-%s", clusterName, nodeType)
}

// TODO: find target container through name instead of using index 0.
// FindRayContainerIndex finds the ray head/worker container's index in the pod
func FindRayContainerIndex(spec corev1.PodSpec) (index int) {
	// We only support one container at this moment. We definitely need a better way to filter out sidecar containers.
	if len(spec.Containers) > 1 {
		logrus.Warnf("Pod has multiple containers, we choose index=0 as Ray container")
	}
	return 0
}

// CalculateDesiredReplicas calculate desired worker replicas at the cluster level
func CalculateDesiredReplicas(cluster *rayiov1alpha1.RayCluster) int32 {
	count := int32(0)
	for _, nodeGroup := range cluster.Spec.WorkerGroupSpecs {
		count += *nodeGroup.Replicas
	}

	return count
}

// CalculateDesiredReplicas calculate desired worker replicas at the cluster level
func CalculateMinReplicas(cluster *rayiov1alpha1.RayCluster) int32 {
	count := int32(0)
	for _, nodeGroup := range cluster.Spec.WorkerGroupSpecs {
		count += *nodeGroup.MinReplicas
	}

	return count
}

// CalculateDesiredReplicas calculate desired worker replicas at the cluster level
func CalculateMaxReplicas(cluster *rayiov1alpha1.RayCluster) int32 {
	count := int32(0)
	for _, nodeGroup := range cluster.Spec.WorkerGroupSpecs {
		count += *nodeGroup.MaxReplicas
	}

	return count
}

// CalculateDesiredReplicas calculate desired worker replicas at the cluster level
func CalculateAvailableReplicas(pods corev1.PodList) int32 {
	count := int32(0)
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning {
			count++
		}
	}

	return count
}

func Contains(s []string, searchTerm string) bool {
	i := sort.SearchStrings(s, searchTerm)
	return i < len(s) && s[i] == searchTerm
}

func FilterContainerByName(containers []corev1.Container, name string) (corev1.Container, error) {
	for _, container := range containers {
		if strings.Compare(container.Name, name) == 0 {
			return container, nil
		}
	}

	return corev1.Container{}, fmt.Errorf("can not find container %s", name)
}

// GetHeadGroupServiceAccountName returns the head group service account if it exists.
// Otherwise, it returns the name of the cluster itself.
func GetHeadGroupServiceAccountName(cluster *rayiov1alpha1.RayCluster) string {
	headGroupServiceAccountName := cluster.Spec.HeadGroupSpec.Template.Spec.ServiceAccountName
	if headGroupServiceAccountName != "" {
		return headGroupServiceAccountName
	}
	return cluster.Name
}

func PodNotMatchingTemplate(pod corev1.Pod, template corev1.PodTemplateSpec) bool {
	if pod.Status.Phase == corev1.PodRunning && pod.ObjectMeta.DeletionTimestamp == nil {
		if len(template.Spec.Containers) != len(pod.Spec.Containers) {
			return true
		}
		cmap := map[string]*corev1.Container{}
		for _, container := range pod.Spec.Containers {
			cmap[container.Name] = &container
		}
		for _, container1 := range template.Spec.Containers {
			if container2, ok := cmap[container1.Name]; ok {
				if container1.Image != container2.Image {
					// image name do not match
					return true
				}
				if len(container1.Resources.Requests) != len(container2.Resources.Requests) ||
					len(container1.Resources.Limits) != len(container2.Resources.Limits) {
					// resource entries do not match
					return true
				}

				resources1 := []corev1.ResourceList{
					container1.Resources.Requests,
					container1.Resources.Limits,
				}
				resources2 := []corev1.ResourceList{
					container2.Resources.Requests,
					container2.Resources.Limits,
				}
				for i := range resources1 {
					// we need to make sure all fields match
					for name, quantity1 := range resources1[i] {
						if quantity2, ok := resources2[i][name]; ok {
							if quantity1.Cmp(quantity2) != 0 {
								// request amount does not match
								return true
							}
						} else {
							// no such request
							return true
						}
					}
				}

				// now we consider them equal
				delete(cmap, container1.Name)
			} else {
				// container name do not match
				return true
			}
		}
		if len(cmap) != 0 {
			// one or more containers do not match
			return true
		}
	}
	return false
}

// Update the rayResources field based on user-provided rayStartParams and ray container resources
func ComputePodStatuses(instance *rayiov1alpha1.RayCluster) (
	headStatus rayiov1alpha1.GroupStatus, workerGroupStatuses []rayiov1alpha1.GroupStatus,
) {
	headGroupSpec := instance.Spec.HeadGroupSpec
	detectedRayResources := computeRayResources(
		headGroupSpec.RayResources,
		headGroupSpec.RayStartParams,
		headGroupSpec.Template,
	)
	headStatus = rayiov1alpha1.GroupStatus{
		DetectedRayResources: detectedRayResources,
	}
	for _, workerGroupSpec := range instance.Spec.WorkerGroupSpecs {
		detectedRayResources = computeRayResources(
			workerGroupSpec.RayResources,
			workerGroupSpec.RayStartParams,
			workerGroupSpec.Template,
		)
		workerGroupStatus := rayiov1alpha1.GroupStatus{
			GroupName:            workerGroupSpec.GroupName,
			DetectedRayResources: detectedRayResources,
		}
		workerGroupStatuses = append(workerGroupStatuses, workerGroupStatus)
	}
	return headStatus, workerGroupStatuses
}

// Updates the user-specified rayResource spec with data from the rayStartParams and the pod template.
// Data from rayResources overrides data from rayStartParams.
// Data from rayStartParams overrides data from the podTemplate.
// If CPU, GPU, or memory is not present in the returned resource map, Ray will attempt to
// infer these upon "ray start".
func computeRayResources(
	rayResourceSpec rayiov1alpha1.RayResources,
	rayStartParams map[string]string,
	podTemplate v1.PodTemplateSpec,
) (updatedRayResources rayiov1alpha1.RayResources) {

	updatedRayResources = make(rayiov1alpha1.RayResources)

	rayContainerIndex := GetRayContainerIndex(podTemplate.Spec)
	rayContainerResources := podTemplate.Spec.Containers[rayContainerIndex].Resources

	// Compute CPU
	if cpu_spec, ok := rayResourceSpec["CPU"]; ok {
		// Prioritize user-specified override.
		updatedRayResources["CPU"] = cpu_spec
	} else {
		// If override is not specified, look at rayStartParams and the the Ray container's resources.
		num_cpu, err := computeCPU(rayStartParams, rayContainerResources)
		if err != nil {
			rayClusterLog.Error(err, "Failed to compute CPU.")
		} else if num_cpu > 0 {
			updatedRayResources["CPU"] = num_cpu
		}
	}

	// Compute GPU
	if gpu_spec, ok := rayResourceSpec["GPU"]; ok {
		// Prioritize user-specified override.
		updatedRayResources["GPU"] = gpu_spec
	} else {
		// If override is not specified, look at rayStartParams and the the Ray container's resources.
		num_gpu, err := computeGPU(rayStartParams, rayContainerResources)
		if err != nil {
			rayClusterLog.Error(err, "Failed to compute CPU.")
		} else if num_gpu > 0 {
			updatedRayResources["GPU"] = num_gpu
		}
	}

	// Compute memory
	if memory_spec, ok := rayResourceSpec["memory"]; ok {
		// Prioritize user-specified override.
		updatedRayResources["memory"] = memory_spec
	} else {
		// If override is not specified, look at rayStartParams and the the Ray container's resources.
		memory, err := computeMemory(rayStartParams, rayContainerResources)
		if err != nil {
			rayClusterLog.Error(err, "Failed to compute CPU.")
		} else if memory > 0 {
			updatedRayResources["memory"] = memory
		}
	}
	return updatedRayResources
}

func computeCPU(rayStartParams map[string]string, rayContainerResources v1.ResourceRequirements) (int64, error) {
	// Try using the num-cpus rayStartParam.
	if cpuParam, ok := rayStartParams["num-cpus"]; ok {
		cpuInt, err := strconv.ParseInt(cpuParam, 10, 64)
		if err != nil {
			rayClusterLog.Error(err, "Failed to parse num-cpus rayStartParam.")
		} else {
			return cpuInt, nil
		}
	}

	// If we couldn't read CPU from the rayStartParams, trying getting the CPU count from the
	// container resources.
	cpuQuantity := rayContainerResources.Limits[v1.ResourceCPU]
	if !cpuQuantity.IsZero() {
		return cpuQuantity.Value(), nil
	}

	// The user might not have set CPU limits for the Ray container.
	// That's not adviseable, but we don't consider it an error.
	// Return a 0 value, which will be ignored by the caller of this function.
	return 0, nil
}

func computeGPU(rayStartParams map[string]string, rayContainerResources v1.ResourceRequirements) (int64, error) {
	// Try using the num-gpus rayStartParam.
	if gpuParam, ok := rayStartParams["num-gpus"]; ok {
		gpuInt, err := strconv.ParseInt(gpuParam, 10, 64)
		if err != nil {
			rayClusterLog.Error(err, "Failed to parse num-cpus rayStartParam.")
		} else {
			return int64(gpuInt), nil
		}
	}

	// If we couldn't read GPU from the rayStartParams, trying getting the GPU count from the
	// container resources.

	// Scan for resource names containing gpu, e.g. "nvidia.com/gpu".
	for resourceName, resourceQuantity := range rayContainerResources.Limits {
		if strings.Contains(string(resourceName), "gpu") {
			// For now, we only support one GPU type.
			// Return the first match for a GPU quantity.
			return int64(resourceQuantity.Value()), nil
		}
	}

	// No GPUs specified. Return a 0 value, which will be ignored by the caller of this function.
	return 0, nil
}

func computeMemory(rayStartParams map[string]string, rayContainerResources v1.ResourceRequirements) (int64, error) {
	// First look in the memory rayStartParam.
	if memoryParam, ok := rayStartParams["memory"]; ok {
		memoryInt, err := strconv.ParseInt(memoryParam, 10, 64)
		if err != nil {
			rayClusterLog.Error(err, "Failed to parse memory rayStartParam.")
		} else {
			return memoryInt, nil
		}
	}

	// If we couldn't read CPU from the rayStartParams, trying getting the CPU count from the
	// container resources.
	memoryQuantity := rayContainerResources.Limits[v1.ResourceMemory]
	if !memoryQuantity.IsZero() {
		return memoryQuantity.Value(), nil
	}

	// The user might not have set memory limits for the Ray container.
	// That's very inadviseable, but we don't consider it an error.
	// Return a 0 value, which will be ignored by the caller of this function.
	return 0, nil
}

func GetRayContainerIndex(podSpec v1.PodSpec) (rayContainerIndex int) {
	// a ray pod can have multiple containers.
	// we identify the ray container based on env var: RAY=true
	// if the env var is missing, we choose containers[0].
	for i, container := range podSpec.Containers {
		for _, env := range container.Env {
			if env.Name == strings.ToLower("ray") && env.Value == strings.ToLower("true") {
				log.Info("Head pod container with index " + strconv.Itoa(i) + " identified as Ray container based on env RAY=true.")
				return i
			}
		}
	}
	// not found, use first container
	log.Info("Head pod container with index 0 identified as Ray container.")
	return 0
}
