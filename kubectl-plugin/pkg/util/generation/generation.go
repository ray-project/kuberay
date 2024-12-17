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

type RayClusterSpecObject struct {
	RayVersion                       string
	Image                            string
	HeadCPU                          string
	HeadMemory                       string
	WorkerGrpName                    string
	WorkerCPU                        string
	WorkerMemory                     string
	HeadLifecyclePrestopExecCommand  []string
	WorkerLifecyclePrestopExecComand []string
	WorkerReplicas                   int32
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
					WithContainers(corev1ac.Container().
						WithName("ray-head").
						WithImage(rayClusterSpecObject.Image).
						WithResources(corev1ac.ResourceRequirements().
							WithRequests(corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(rayClusterSpecObject.HeadCPU),
								corev1.ResourceMemory: resource.MustParse(rayClusterSpecObject.HeadMemory),
							}).
							WithLimits(corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(rayClusterSpecObject.HeadCPU),
								corev1.ResourceMemory: resource.MustParse(rayClusterSpecObject.HeadMemory),
							})).
						WithPorts(corev1ac.ContainerPort().WithContainerPort(6379).WithName("gcs-server"),
							corev1ac.ContainerPort().WithContainerPort(8265).WithName("dashboard"),
							corev1ac.ContainerPort().WithContainerPort(10001).WithName("client")))))).
		WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
			WithRayStartParams(map[string]string{"metrics-export-port": "8080"}).
			WithGroupName(rayClusterSpecObject.WorkerGrpName).
			WithReplicas(rayClusterSpecObject.WorkerReplicas).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithSpec(corev1ac.PodSpec().
					WithContainers(corev1ac.Container().
						WithName("ray-worker").
						WithImage(rayClusterSpecObject.Image).
						WithResources(corev1ac.ResourceRequirements().
							WithRequests(corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(rayClusterSpecObject.HeadCPU),
								corev1.ResourceMemory: resource.MustParse(rayClusterSpecObject.HeadMemory),
							}).
							WithLimits(corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(rayClusterSpecObject.HeadCPU),
								corev1.ResourceMemory: resource.MustParse(rayClusterSpecObject.HeadMemory),
							}))))))

	// Lifecycle cannot be empty, an empty lifecycle will stop pod startup so this will add lifecycle if its not empty
	if len(rayClusterSpecObject.WorkerLifecyclePrestopExecComand) > 0 {
		rayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Lifecycle = corev1ac.Lifecycle().
			WithPreStop(corev1ac.LifecycleHandler().
				WithExec(corev1ac.ExecAction().
					WithCommand(rayClusterSpecObject.WorkerLifecyclePrestopExecComand...)))
	}
	if len(rayClusterSpecObject.HeadLifecyclePrestopExecCommand) > 0 {
		rayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Lifecycle = corev1ac.Lifecycle().
			WithPreStop(corev1ac.LifecycleHandler().
				WithExec(corev1ac.ExecAction().
					WithCommand(rayClusterSpecObject.HeadLifecyclePrestopExecCommand...)))
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
