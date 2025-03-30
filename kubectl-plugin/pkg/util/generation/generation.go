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
	Context     *string           `yaml:"context,omitempty"`
	Namespace   *string           `yaml:"namespace,omitempty"`
	Name        *string           `yaml:"name,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`

	RayVersion *string `yaml:"ray-version,omitempty"`
	Image      *string `yaml:"image,omitempty"`

	HeadCPU              *string           `yaml:"head-cpu,omitempty"`
	HeadGPU              *string           `yaml:"head-gpu,omitempty"`
	HeadMemory           *string           `yaml:"head-memory,omitempty"`
	HeadEphemeralStorage *string           `yaml:"head-ephemeral-storage,omitempty"`
	HeadRayStartParams   map[string]string `yaml:"head-ray-start-params,omitempty"`
	HeadNodeSelectors    map[string]string `yaml:"head-node-selectors,omitempty"`

	WorkerGroups []WorkerGroupConfig `yaml:"worker-groups,omitempty"`
}

type WorkerGroupConfig struct {
	Name                   *string           `yaml:"name,omitempty"`
	WorkerCPU              *string           `yaml:"worker-cpu,omitempty"`
	WorkerGPU              *string           `yaml:"worker-gpu,omitempty"`
	WorkerTPU              *string           `yaml:"worker-tpu,omitempty"`
	WorkerMemory           *string           `yaml:"worker-memory,omitempty"`
	WorkerEphemeralStorage *string           `yaml:"worker-ephemeral-storage,omitempty"`
	WorkerReplicas         *int32            `yaml:"worker-replicas,omitempty"`
	WorkerRayStartParams   map[string]string `yaml:"worker-ray-start-params,omitempty"`
	WorkerNodeSelectors    map[string]string `yaml:"worker-node-selectors,omitempty"`
}

type RayJobYamlObject struct {
	RayJobName     string
	Namespace      string
	SubmissionMode string
	Entrypoint     string
	RayClusterSpecObject
}

func (rayClusterSpecObject *RayClusterSpecObject) GenerateRayClusterApplyConfig() *rayv1ac.RayClusterApplyConfiguration {
	rayClusterApplyConfig := rayv1ac.RayCluster(*rayClusterSpecObject.Name, *rayClusterSpecObject.Namespace).
		WithLabels(rayClusterSpecObject.Labels).
		WithAnnotations(rayClusterSpecObject.Annotations).
		WithSpec(rayClusterSpecObject.generateRayClusterSpec())

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

// generateRequestResources returns a corev1.ResourceList with the given CPU, memory, ephemeral storage, GPU, and TPU values for only resource requests
func generateRequestResources(cpu, memory, ephemeralStorage, gpu, tpu *string) corev1.ResourceList {
	resources := corev1.ResourceList{}

	if cpu != nil && *cpu != "" {
		cpuResource := resource.MustParse(*cpu)
		if !cpuResource.IsZero() {
			resources[corev1.ResourceCPU] = cpuResource
		}
	}

	if memory != nil && *memory != "" {
		memoryResource := resource.MustParse(*memory)
		if !memoryResource.IsZero() {
			resources[corev1.ResourceMemory] = memoryResource
		}
	}

	if ephemeralStorage != nil && *ephemeralStorage != "" {
		ephemeralStorageResource := resource.MustParse(*ephemeralStorage)
		if !ephemeralStorageResource.IsZero() {
			resources[corev1.ResourceEphemeralStorage] = ephemeralStorageResource
		}
	}

	if gpu != nil && *gpu != "" {
		gpuResource := resource.MustParse(*gpu)
		if !gpuResource.IsZero() {
			resources[corev1.ResourceName(util.ResourceNvidiaGPU)] = gpuResource
		}
	}

	if tpu != nil && *tpu != "" {
		tpuResource := resource.MustParse(*tpu)
		if !tpuResource.IsZero() {
			resources[corev1.ResourceName(util.ResourceGoogleTPU)] = tpuResource
		}
	}

	return resources
}

// generateLimitResources returns a corev1.ResourceList with the given memory, ephemeral storage, GPU, and TPU values for only resource limits
func generateLimitResources(memory, ephemeralStorage, gpu, tpu *string) corev1.ResourceList {
	resources := corev1.ResourceList{}

	if memory != nil && *memory != "" {
		memoryResource := resource.MustParse(*memory)
		if !memoryResource.IsZero() {
			resources[corev1.ResourceMemory] = memoryResource
		}
	}

	if ephemeralStorage != nil && *ephemeralStorage != "" {
		ephemeralStorageResource := resource.MustParse(*ephemeralStorage)
		if !ephemeralStorageResource.IsZero() {
			resources[corev1.ResourceEphemeralStorage] = ephemeralStorageResource
		}
	}

	if gpu != nil && *gpu != "" {
		gpuResource := resource.MustParse(*gpu)
		if !gpuResource.IsZero() {
			resources[corev1.ResourceName(util.ResourceNvidiaGPU)] = gpuResource
		}
	}

	if tpu != nil && *tpu != "" {
		tpuResource := resource.MustParse(*tpu)
		if !tpuResource.IsZero() {
			resources[corev1.ResourceName(util.ResourceGoogleTPU)] = tpuResource
		}
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
	maps.Copy(workerRayStartParams, rayClusterSpecObject.WorkerGroups[0].WorkerRayStartParams)

	headRequestResources := generateRequestResources(
		rayClusterSpecObject.HeadCPU,
		rayClusterSpecObject.HeadMemory,
		rayClusterSpecObject.HeadEphemeralStorage,
		rayClusterSpecObject.HeadGPU,
		// TPU is not used for head request resources
		nil,
	)
	headLimitResources := generateLimitResources(
		rayClusterSpecObject.HeadMemory,
		rayClusterSpecObject.HeadEphemeralStorage,
		rayClusterSpecObject.HeadGPU,
		// TPU is not used for head limit resources
		nil,
	)
	workerRequestResources := generateRequestResources(
		rayClusterSpecObject.WorkerGroups[0].WorkerCPU,
		rayClusterSpecObject.WorkerGroups[0].WorkerMemory,
		rayClusterSpecObject.WorkerGroups[0].WorkerEphemeralStorage,
		rayClusterSpecObject.WorkerGroups[0].WorkerGPU,
		rayClusterSpecObject.WorkerGroups[0].WorkerTPU,
	)
	workerLimitResources := generateLimitResources(
		rayClusterSpecObject.WorkerGroups[0].WorkerMemory,
		rayClusterSpecObject.WorkerGroups[0].WorkerEphemeralStorage,
		rayClusterSpecObject.WorkerGroups[0].WorkerGPU,
		rayClusterSpecObject.WorkerGroups[0].WorkerTPU,
	)

	workerGroupName := "default-group"
	if rayClusterSpecObject.WorkerGroups[0].Name != nil {
		workerGroupName = *rayClusterSpecObject.WorkerGroups[0].Name
	}

	rayClusterSpec := rayv1ac.RayClusterSpec().
		WithRayVersion(*rayClusterSpecObject.RayVersion).
		WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
			WithRayStartParams(headRayStartParams).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithSpec(corev1ac.PodSpec().
					WithNodeSelector(rayClusterSpecObject.HeadNodeSelectors).
					WithContainers(corev1ac.Container().
						WithName("ray-head").
						WithImage(*rayClusterSpecObject.Image).
						WithResources(corev1ac.ResourceRequirements().
							WithRequests(headRequestResources).
							WithLimits(headLimitResources)).
						WithPorts(corev1ac.ContainerPort().WithContainerPort(6379).WithName("gcs-server"),
							corev1ac.ContainerPort().WithContainerPort(8265).WithName("dashboard"),
							corev1ac.ContainerPort().WithContainerPort(10001).WithName("client")))))).
		WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
			WithRayStartParams(workerRayStartParams).
			WithGroupName(workerGroupName).
			WithReplicas(*rayClusterSpecObject.WorkerGroups[0].WorkerReplicas).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithSpec(corev1ac.PodSpec().
					WithNodeSelector(rayClusterSpecObject.WorkerGroups[0].WorkerNodeSelectors).
					WithContainers(corev1ac.Container().
						WithName("ray-worker").
						WithImage(*rayClusterSpecObject.Image).
						WithResources(corev1ac.ResourceRequirements().
							WithRequests(workerRequestResources).
							WithLimits(workerLimitResources))))))

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
