package generation

import (
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"

	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
)

type RayClusterYamlObject struct {
	ClusterName    string
	Namespace      string
	RayVersion     string
	Image          string
	HeadCPU        string
	HeadMemory     string
	WorkerGrpName  string
	WorkerCPU      string
	WorkerMemory   string
	WorkerReplicas int32
}

func (rayClusterObject *RayClusterYamlObject) GenerateRayClusterApplyConfig() (*rayv1ac.RayClusterApplyConfiguration, error) {
	// TODO: Look for better workaround/fixes for RayStartParams. Currently using `WithRayStartParams()` requires
	// a non-empty map with valid key value pairs and will not populate the field with empty/nil values. This
	// isn't ideal as it forces the generated RayCluster yamls to use those parameters.
	rayClusterApplyConfig := rayv1ac.RayCluster(rayClusterObject.ClusterName, rayClusterObject.Namespace).
		WithName(rayClusterObject.ClusterName).
		WithSpec(rayv1ac.RayClusterSpec().
			WithRayVersion(rayClusterObject.RayVersion).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
				WithTemplate(corev1ac.PodTemplateSpec().
					WithSpec(corev1ac.PodSpec().
						WithContainers(corev1ac.Container().
							WithName("ray-head").
							WithImage(rayClusterObject.Image).
							WithResources(corev1ac.ResourceRequirements().
								WithRequests(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse(rayClusterObject.HeadCPU),
									corev1.ResourceMemory: resource.MustParse(rayClusterObject.HeadMemory),
								}).
								WithLimits(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse(rayClusterObject.HeadCPU),
									corev1.ResourceMemory: resource.MustParse(rayClusterObject.HeadMemory),
								})).
							WithPorts(corev1ac.ContainerPort().WithContainerPort(6379).WithName("gcs-server"),
								corev1ac.ContainerPort().WithContainerPort(8265).WithName("dashboard"),
								corev1ac.ContainerPort().WithContainerPort(10001).WithName("client")))))).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithRayStartParams(map[string]string{"metrics-export-port": "8080"}).
				WithGroupName(rayClusterObject.WorkerGrpName).
				WithReplicas(rayClusterObject.WorkerReplicas).
				WithTemplate(corev1ac.PodTemplateSpec().
					WithSpec(corev1ac.PodSpec().
						WithContainers(corev1ac.Container().
							WithName("ray-worker").
							WithImage(rayClusterObject.Image).
							WithResources(corev1ac.ResourceRequirements().
								WithRequests(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse(rayClusterObject.HeadCPU),
									corev1.ResourceMemory: resource.MustParse(rayClusterObject.HeadMemory),
								}).
								WithLimits(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse(rayClusterObject.HeadCPU),
									corev1.ResourceMemory: resource.MustParse(rayClusterObject.HeadMemory),
								})))))))

	return rayClusterApplyConfig, nil
}

// Converts RayClusterApplyConfiguration object into a yaml string
func ConvertRayClusterApplyConfigToYaml(rayClusterac *rayv1ac.RayClusterApplyConfiguration) (string, error) {
	var resource map[string]interface{}
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
