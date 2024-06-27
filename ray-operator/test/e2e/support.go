package e2e

import (
	"embed"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

//go:embed *.py
var _files embed.FS

func ReadFile(t Test, fs embed.FS, fileName string) []byte {
	t.T().Helper()
	file, err := fs.ReadFile(fileName)
	t.Expect(err).NotTo(gomega.HaveOccurred())
	return file
}

type ApplyOption[T any] func(t *T) *T

func Apply[T any](t *T, options ...ApplyOption[T]) *T {
	for _, opt := range options {
		t = opt(t)
	}
	return t
}

func Options[T any](options ...ApplyOption[T]) ApplyOption[T] {
	return func(t *T) *T {
		for _, opt := range options {
			t = opt(t)
		}
		return t
	}
}

func NewConfigMap(namespace, name string, options ...ApplyOption[corev1ac.ConfigMapApplyConfiguration]) *corev1ac.ConfigMapApplyConfiguration {
	cmAC := corev1ac.ConfigMap(name, namespace).
		WithBinaryData(map[string][]byte{}).
		WithImmutable(true)

	return ConfigMapWith(cmAC, options...)
}

func ConfigMapWith(configMapAC *corev1ac.ConfigMapApplyConfiguration, options ...ApplyOption[corev1ac.ConfigMapApplyConfiguration]) *corev1ac.ConfigMapApplyConfiguration {
	return Apply(configMapAC, options...)
}

func File(t Test, fs embed.FS, fileName string) ApplyOption[corev1ac.ConfigMapApplyConfiguration] {
	return func(cmAC *corev1ac.ConfigMapApplyConfiguration) *corev1ac.ConfigMapApplyConfiguration {
		cmAC.WithBinaryData(map[string][]byte{fileName: ReadFile(t, fs, fileName)})
		return cmAC
	}
}

func Files(t Test, fs embed.FS, fileNames ...string) ApplyOption[corev1ac.ConfigMapApplyConfiguration] {
	var files []ApplyOption[corev1ac.ConfigMapApplyConfiguration]
	for _, fileName := range fileNames {
		files = append(files, File(t, fs, fileName))
	}
	return Options(files...)
}

func NewRayClusterSpec(options ...ApplyOption[rayv1ac.RayClusterSpecApplyConfiguration]) *rayv1ac.RayClusterSpecApplyConfiguration {
	return RayClusterSpecWith(rayClusterSpec(), options...)
}

func RayClusterSpecWith(spec *rayv1ac.RayClusterSpecApplyConfiguration, options ...ApplyOption[rayv1ac.RayClusterSpecApplyConfiguration]) *rayv1ac.RayClusterSpecApplyConfiguration {
	return Apply(spec, options...)
}

func MountConfigMap[T rayv1ac.RayClusterSpecApplyConfiguration | corev1ac.PodTemplateSpecApplyConfiguration](configMap *corev1.ConfigMap, mountPath string) ApplyOption[T] {
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

func rayClusterSpec() *rayv1ac.RayClusterSpecApplyConfiguration {
	return rayv1ac.RayClusterSpec().
		WithRayVersion(GetRayVersion()).
		WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
			WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
			WithTemplate(HeadPodTemplateApplyConfiguration())).
		WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
			WithReplicas(1).
			WithMinReplicas(1).
			WithMaxReplicas(1).
			WithGroupName("small-group").
			WithRayStartParams(map[string]string{"num-cpus": "1"}).
			WithTemplate(WorkerPodTemplateApplyConfiguration()))
}

func podTemplateSpecApplyConfiguration(template *corev1ac.PodTemplateSpecApplyConfiguration, options ...ApplyOption[corev1ac.PodTemplateSpecApplyConfiguration]) *corev1ac.PodTemplateSpecApplyConfiguration {
	return Apply(template, options...)
}

func HeadPodTemplateApplyConfiguration() *corev1ac.PodTemplateSpecApplyConfiguration {
	return corev1ac.PodTemplateSpec().
		WithSpec(corev1ac.PodSpec().
			WithContainers(corev1ac.Container().
				WithName("ray-head").
				WithImage(GetRayImage()).
				WithPorts(
					corev1ac.ContainerPort().WithName("gcs").WithContainerPort(6379),
					corev1ac.ContainerPort().WithName("serve").WithContainerPort(8000),
					corev1ac.ContainerPort().WithName("dashboard").WithContainerPort(8265),
					corev1ac.ContainerPort().WithName("client").WithContainerPort(10001),
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

func WorkerPodTemplateApplyConfiguration() *corev1ac.PodTemplateSpecApplyConfiguration {
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

func jobSubmitterPodTemplateApplyConfiguration() *corev1ac.PodTemplateSpecApplyConfiguration {
	return corev1ac.PodTemplateSpec().
		WithSpec(corev1ac.PodSpec().
			WithRestartPolicy(corev1.RestartPolicyNever).
			WithContainers(corev1ac.Container().
				WithName("ray-job-submitter").
				WithImage(GetRayImage()).
				WithResources(corev1ac.ResourceRequirements().
					WithRequests(corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("200Mi"),
					}).
					WithLimits(corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("500Mi"),
					}))))
}
