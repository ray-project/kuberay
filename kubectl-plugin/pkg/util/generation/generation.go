package generation

import (
	"maps"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
)

type RayClusterSpecObject struct {
	HeadRayStartParams     map[string]string
	WorkerRayStartParams   map[string]string
	NodeSelectors          map[string]string
	RayVersion             string
	Image                  string
	HeadCPU                string
	HeadGPU                string
	HeadMemory             string
	HeadEphemeralStorage   string
	WorkerCPU              string
	WorkerGPU              string
	WorkerMemory           string
	WorkerEphemeralStorage string
	WorkerReplicas         int32
}

type RayClusterYamlObject struct {
	ClusterName string
	Namespace   string
	Labels      map[string]string
	Annotations map[string]string
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
		WithLabels(rayClusterObject.Labels).
		WithAnnotations(rayClusterObject.Annotations).
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

// generateResources returns a corev1.ResourceList with the given CPU, memory, ephemeral storage, and GPU values for both requests and limits
func generateResources(cpu, memory, ephemeralStorage, gpu string) corev1.ResourceList {
	resources := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse(cpu),
		corev1.ResourceMemory: resource.MustParse(memory),
	}
	if ephemeralStorage != "" {
		resources[corev1.ResourceEphemeralStorage] = resource.MustParse(ephemeralStorage)
	}

	gpuResource := resource.MustParse(gpu)
	if !gpuResource.IsZero() {
		resources[corev1.ResourceName(util.ResourceNvidiaGPU)] = gpuResource
	}

	return resources
}

func (rayClusterSpecObject *RayClusterSpecObject) generateRayClusterSpec() *rayv1ac.RayClusterSpecApplyConfiguration {
	// TODO: Look for better workaround/fixes for RayStartParams. Currently using `WithRayStartParams()` requires
	// a non-empty map with valid key value pairs and will not populate the field with empty/nil values. This
	// isn't ideal as it forces the generated RayCluster yamls to use those parameters.
	headRayStartParams := map[string]string{
		"dashboard-host": "0.0.0.0",
	}
	workerRayStartParams := map[string]string{
		"metrics-export-port": "8080",
	}
	maps.Copy(headRayStartParams, rayClusterSpecObject.HeadRayStartParams)
	maps.Copy(workerRayStartParams, rayClusterSpecObject.WorkerRayStartParams)

	headResources := generateResources(rayClusterSpecObject.HeadCPU, rayClusterSpecObject.HeadMemory, rayClusterSpecObject.HeadEphemeralStorage, rayClusterSpecObject.HeadGPU)
	workerResources := generateResources(rayClusterSpecObject.WorkerCPU, rayClusterSpecObject.WorkerMemory, rayClusterSpecObject.WorkerEphemeralStorage, rayClusterSpecObject.WorkerGPU)

	rayClusterSpec := rayv1ac.RayClusterSpec().
		WithRayVersion(rayClusterSpecObject.RayVersion).
		WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
			WithRayStartParams(headRayStartParams).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithSpec(corev1ac.PodSpec().
					WithNodeSelector(rayClusterSpecObject.NodeSelectors).
					WithContainers(corev1ac.Container().
						WithName("ray-head").
						WithImage(rayClusterSpecObject.Image).
						WithResources(corev1ac.ResourceRequirements().
							WithRequests(headResources).
							WithLimits(headResources)).
						WithPorts(corev1ac.ContainerPort().WithContainerPort(6379).WithName("gcs-server"),
							corev1ac.ContainerPort().WithContainerPort(8265).WithName("dashboard"),
							corev1ac.ContainerPort().WithContainerPort(10001).WithName("client")))))).
		WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
			WithRayStartParams(workerRayStartParams).
			WithGroupName("default-group").
			WithReplicas(rayClusterSpecObject.WorkerReplicas).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithSpec(corev1ac.PodSpec().
					WithNodeSelector(rayClusterSpecObject.NodeSelectors).
					WithContainers(corev1ac.Container().
						WithName("ray-worker").
						WithImage(rayClusterSpecObject.Image).
						WithResources(corev1ac.ResourceRequirements().
							WithRequests(workerResources).
							WithLimits(workerResources))))))

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
