package generation

import (
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
)

const (
	resourceNvidiaGPU = "nvidia.com/gpu"
)

type RayClusterSpecObject struct {
	HeadNodeSelectors   map[string]string
	WorkerNodeSelectors map[string]string
	RayVersion          string
	Image               string
	HeadCPU             string
	HeadGPU             string
	HeadMemory          string
	WorkerCPU           string
	WorkerGPU           string
	WorkerMemory        string
	WorkerReplicas      int32
}

type RayClusterYamlObject struct {
	ClusterName string
	Namespace   string
	RayClusterSpecObject
}

type RayJobYamlObject struct {
	RayJobName     string
	Namespace      string
	SubmissionMode string
	Entrypoint     string
	RayClusterSpecObject
}

func (rayClusterObject *RayClusterYamlObject) GenerateRayClusterApplyConfig() *rayv1ac.RayClusterApplyConfiguration {
	rayClusterApplyConfig := rayv1ac.RayCluster(rayClusterObject.ClusterName, rayClusterObject.Namespace).
		WithSpec(rayClusterObject.generateRayClusterSpec())

	return rayClusterApplyConfig
}

func (rayJobObject *RayJobYamlObject) GenerateRayJobApplyConfig() *rayv1ac.RayJobApplyConfiguration {
	rayJobApplyConfig := rayv1ac.RayJob(rayJobObject.RayJobName, rayJobObject.Namespace).
		WithSpec(rayv1ac.RayJobSpec().
			WithSubmissionMode(rayv1.JobSubmissionMode(rayJobObject.SubmissionMode)).
			WithEntrypoint(rayJobObject.Entrypoint).
			WithRayClusterSpec(rayJobObject.generateRayClusterSpec()))

	return rayJobApplyConfig
}

func (rayClusterSpecObject *RayClusterSpecObject) generateRayClusterSpec() *rayv1ac.RayClusterSpecApplyConfiguration {
	// TODO: Look for better workaround/fixes for RayStartParams. Currently using `WithRayStartParams()` requires
	// a non-empty map with valid key value pairs and will not populate the field with empty/nil values. This
	// isn't ideal as it forces the generated RayCluster yamls to use those parameters.
	rayClusterSpec := rayv1ac.RayClusterSpec().
		WithRayVersion(rayClusterSpecObject.RayVersion).
		WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
			WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithSpec(corev1ac.PodSpec().
					WithNodeSelector(rayClusterSpecObject.HeadNodeSelectors).
					WithContainers(corev1ac.Container().
						WithName("ray-head").
						WithImage(rayClusterSpecObject.Image).
						WithResources(corev1ac.ResourceRequirements().
							WithRequests(corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(rayClusterSpecObject.HeadCPU),
								corev1.ResourceMemory: resource.MustParse(rayClusterSpecObject.HeadMemory),
							}).
							WithLimits(corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse(rayClusterSpecObject.HeadMemory),
							})).
						WithPorts(corev1ac.ContainerPort().WithContainerPort(6379).WithName("gcs-server"),
							corev1ac.ContainerPort().WithContainerPort(8265).WithName("dashboard"),
							corev1ac.ContainerPort().WithContainerPort(10001).WithName("client")))))).
		WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
			WithRayStartParams(map[string]string{"metrics-export-port": "8080"}).
			WithGroupName("default-group").
			WithReplicas(rayClusterSpecObject.WorkerReplicas).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithSpec(corev1ac.PodSpec().
					WithNodeSelector(rayClusterSpecObject.WorkerNodeSelectors).
					WithContainers(corev1ac.Container().
						WithName("ray-worker").
						WithImage(rayClusterSpecObject.Image).
						WithResources(corev1ac.ResourceRequirements().
							WithRequests(corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(rayClusterSpecObject.WorkerCPU),
								corev1.ResourceMemory: resource.MustParse(rayClusterSpecObject.WorkerMemory),
							}).
							WithLimits(corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse(rayClusterSpecObject.WorkerMemory),
							}))))))

	// If the HeadGPU resource is set with a value, then proceed with parsing.
	if rayClusterSpecObject.HeadGPU != "" {
		headGPUResource := resource.MustParse(rayClusterSpecObject.HeadGPU)
		if !headGPUResource.IsZero() {
			var requests, limits corev1.ResourceList
			requests = *rayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests
			limits = *rayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Limits
			requests[corev1.ResourceName(resourceNvidiaGPU)] = headGPUResource
			limits[corev1.ResourceName(resourceNvidiaGPU)] = headGPUResource

			rayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests = &requests
			rayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Limits = &limits
		}
	}

	// If the workerGPU resource is set with a value, then proceed with parsing.
	if rayClusterSpecObject.WorkerGPU != "" {
		workerGPUResource := resource.MustParse(rayClusterSpecObject.WorkerGPU)
		if !workerGPUResource.IsZero() {
			var requests, limits corev1.ResourceList
			requests = *rayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests
			limits = *rayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Limits
			requests[corev1.ResourceName(resourceNvidiaGPU)] = workerGPUResource
			limits[corev1.ResourceName(resourceNvidiaGPU)] = workerGPUResource

			rayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests = &requests
			rayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Limits = &limits
		}
	}

	return rayClusterSpec
}

// Converts RayClusterApplyConfiguration object into a yaml string
func ConvertRayClusterApplyConfigToYaml(rayClusterac *rayv1ac.RayClusterApplyConfiguration) (string, error) {
	resource, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rayClusterac)
	if err != nil {
		return "", err
	}

	podByte, err := yaml.Marshal(resource)
	if err != nil {
		return "", err
	}

	return string(podByte), nil
}

// Converts RayJobApplyConfiguration object into a yaml string
func ConvertRayJobApplyConfigToYaml(rayJobac *rayv1ac.RayJobApplyConfiguration) (string, error) {
	resource, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rayJobac)
	if err != nil {
		return "", err
	}

	podByte, err := yaml.Marshal(resource)
	if err != nil {
		return "", err
	}

	return string(podByte), nil
}
