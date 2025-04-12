package e2eautoscaler

import (
	"embed"

	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

//go:embed *.py
var _files embed.FS

func ReadFile(t Test, fileName string) []byte {
	t.T().Helper()
	file, err := _files.ReadFile(fileName)
	require.NoError(t.T(), err)
	return file
}

type option[T any] func(t *T) *T

func apply[T any](t *T, options ...option[T]) *T {
	for _, opt := range options {
		t = opt(t)
	}
	return t
}

func options[T any](options ...option[T]) option[T] {
	return func(t *T) *T {
		for _, opt := range options {
			t = opt(t)
		}
		return t
	}
}

func newConfigMap(namespace string, options ...option[corev1ac.ConfigMapApplyConfiguration]) *corev1ac.ConfigMapApplyConfiguration {
	cmAC := corev1ac.ConfigMap("scripts", namespace).
		WithBinaryData(map[string][]byte{}).
		WithImmutable(true)

	return configMapWith(cmAC, options...)
}

func configMapWith(configMapAC *corev1ac.ConfigMapApplyConfiguration, options ...option[corev1ac.ConfigMapApplyConfiguration]) *corev1ac.ConfigMapApplyConfiguration {
	return apply(configMapAC, options...)
}

func file(t Test, fileName string) option[corev1ac.ConfigMapApplyConfiguration] {
	return func(cmAC *corev1ac.ConfigMapApplyConfiguration) *corev1ac.ConfigMapApplyConfiguration {
		cmAC.WithBinaryData(map[string][]byte{fileName: ReadFile(t, fileName)})
		return cmAC
	}
}

func files(t Test, fileNames ...string) option[corev1ac.ConfigMapApplyConfiguration] {
	var files []option[corev1ac.ConfigMapApplyConfiguration]
	for _, fileName := range fileNames {
		files = append(files, file(t, fileName))
	}
	return options(files...)
}

func mountConfigMap[T rayv1ac.RayClusterSpecApplyConfiguration | corev1ac.PodTemplateSpecApplyConfiguration](configMap *corev1.ConfigMap, mountPath string) option[T] {
	return func(t *T) *T {
		switch obj := (interface{})(t).(type) {
		case *rayv1ac.RayClusterSpecApplyConfiguration:
			obj.HeadGroupSpec.Template.Spec.Containers[0].WithVolumeMounts(corev1ac.VolumeMount().
				WithName(configMap.Name).
				WithMountPath(mountPath))
			obj.HeadGroupSpec.Template.Spec.WithVolumes(corev1ac.Volume().
				WithName(configMap.Name).
				WithConfigMap(corev1ac.ConfigMapVolumeSource().WithName(configMap.Name)))

		case *corev1ac.PodTemplateSpecApplyConfiguration:
			obj.Spec.Containers[0].WithVolumeMounts(corev1ac.VolumeMount().
				WithName(configMap.Name).
				WithMountPath(mountPath))
			obj.Spec.WithVolumes(corev1ac.Volume().
				WithName(configMap.Name).
				WithConfigMap(corev1ac.ConfigMapVolumeSource().WithName(configMap.Name)))
		}
		return t
	}
}

func headPodTemplateApplyConfiguration() *corev1ac.PodTemplateSpecApplyConfiguration {
	return corev1ac.PodTemplateSpec().
		WithSpec(corev1ac.PodSpec().
			WithContainers(corev1ac.Container().
				WithName("ray-head").
				WithImage(GetRayImage()).
				WithPorts(
					corev1ac.ContainerPort().WithName(utils.GcsServerPortName).WithContainerPort(utils.DefaultGcsServerPort),
					corev1ac.ContainerPort().WithName(utils.ServingPortName).WithContainerPort(utils.DefaultServingPort),
					corev1ac.ContainerPort().WithName(utils.DashboardPortName).WithContainerPort(utils.DefaultDashboardPort),
					corev1ac.ContainerPort().WithName(utils.ClientPortName).WithContainerPort(utils.DefaultClientPort),
				).
				WithResources(corev1ac.ResourceRequirements().
					WithRequests(corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("300m"),
						corev1.ResourceMemory: resource.MustParse("1G"),
					}).
					WithLimits(corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("2G"),
					}))))
}

func headPodTemplateApplyConfigurationV2() *corev1ac.PodTemplateSpecApplyConfiguration {
	return corev1ac.PodTemplateSpec().
		WithSpec(corev1ac.PodSpec().
			WithRestartPolicy(corev1.RestartPolicyNever).
			WithContainers(corev1ac.Container().
				WithName("ray-head").
				WithImage(GetRayImage()).
				WithPorts(
					corev1ac.ContainerPort().WithName(utils.GcsServerPortName).WithContainerPort(utils.DefaultGcsServerPort),
					corev1ac.ContainerPort().WithName(utils.ServingPortName).WithContainerPort(utils.DefaultServingPort),
					corev1ac.ContainerPort().WithName(utils.DashboardPortName).WithContainerPort(utils.DefaultDashboardPort),
					corev1ac.ContainerPort().WithName(utils.ClientPortName).WithContainerPort(utils.DefaultClientPort),
				).
				WithEnv(corev1ac.EnvVar().WithName("RAY_enable_autoscaler_v2").WithValue("1")).
				WithResources(corev1ac.ResourceRequirements().
					WithRequests(corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("300m"),
						corev1.ResourceMemory: resource.MustParse("1G"),
					}).
					WithLimits(corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("2G"),
					}))))
}

func workerPodTemplateApplyConfiguration() *corev1ac.PodTemplateSpecApplyConfiguration {
	return corev1ac.PodTemplateSpec().
		WithSpec(corev1ac.PodSpec().
			WithContainers(corev1ac.Container().
				WithName("ray-worker").
				WithImage(GetRayImage()).
				WithResources(corev1ac.ResourceRequirements().
					WithRequests(corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("300m"),
						corev1.ResourceMemory: resource.MustParse("1G"),
					}).
					WithLimits(corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1G"),
					}))))
}

func workerPodTemplateApplyConfigurationV2() *corev1ac.PodTemplateSpecApplyConfiguration {
	return corev1ac.PodTemplateSpec().
		WithSpec(corev1ac.PodSpec().
			WithRestartPolicy(corev1.RestartPolicyNever).
			WithContainers(corev1ac.Container().
				WithName("ray-worker").
				WithImage(GetRayImage()).
				WithResources(corev1ac.ResourceRequirements().
					WithRequests(corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("300m"),
						corev1.ResourceMemory: resource.MustParse("1G"),
					}).
					WithLimits(corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1G"),
					}))))
}
