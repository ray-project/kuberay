package e2e

import (
	"embed"
	"strings"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

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
	cmAC := corev1ac.ConfigMap("jobs", namespace).
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

func newRayClusterSpec(options ...option[rayv1ac.RayClusterSpecApplyConfiguration]) *rayv1ac.RayClusterSpecApplyConfiguration {
	return rayClusterSpecWith(rayClusterSpec(), options...)
}

func rayClusterSpecWith(spec *rayv1ac.RayClusterSpecApplyConfiguration, options ...option[rayv1ac.RayClusterSpecApplyConfiguration]) *rayv1ac.RayClusterSpecApplyConfiguration {
	return apply(spec, options...)
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

func rayClusterSpec() *rayv1ac.RayClusterSpecApplyConfiguration {
	return rayv1ac.RayClusterSpec().
		WithRayVersion(GetRayVersion()).
		WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
			WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
			WithTemplate(headPodTemplateApplyConfiguration())).
		WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
			WithReplicas(1).
			WithMinReplicas(1).
			WithMaxReplicas(1).
			WithGroupName("small-group").
			WithRayStartParams(map[string]string{"num-cpus": "1"}).
			WithTemplate(workerPodTemplateApplyConfiguration()))
}

func podTemplateSpecApplyConfiguration(template *corev1ac.PodTemplateSpecApplyConfiguration, options ...option[corev1ac.PodTemplateSpecApplyConfiguration]) *corev1ac.PodTemplateSpecApplyConfiguration {
	return apply(template, options...)
}

func headPodTemplateApplyConfiguration() *corev1ac.PodTemplateSpecApplyConfiguration {
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
						corev1.ResourceMemory: resource.MustParse("3G"),
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

func deployRedis(t Test, namespace string, password string) func() string {
	redisContainer := corev1ac.Container().WithName("redis").WithImage("redis:7.4").
		WithPorts(corev1ac.ContainerPort().WithContainerPort(6379))
	dbSizeCmd := []string{"redis-cli", "--no-auth-warning", "DBSIZE"}
	if password != "" {
		redisContainer.WithCommand("redis-server", "--requirepass", password)
		dbSizeCmd = []string{"redis-cli", "--no-auth-warning", "-a", password, "DBSIZE"}
	}

	pod, err := t.Client().Core().CoreV1().Pods(namespace).Apply(
		t.Ctx(),
		corev1ac.Pod("redis", namespace).
			WithLabels(map[string]string{"app": "redis"}).
			WithSpec(corev1ac.PodSpec().WithContainers(redisContainer)),
		TestApplyOptions,
	)
	require.NoError(t.T(), err)

	_, err = t.Client().Core().CoreV1().Services(namespace).Apply(
		t.Ctx(),
		corev1ac.Service("redis", namespace).
			WithSpec(corev1ac.ServiceSpec().
				WithSelector(map[string]string{"app": "redis"}).
				WithPorts(corev1ac.ServicePort().
					WithPort(6379),
				),
			),
		TestApplyOptions,
	)
	require.NoError(t.T(), err)

	return func() string {
		stdout, stderr := ExecPodCmd(t, pod, "redis", dbSizeCmd)
		return strings.TrimSpace(stdout.String() + stderr.String())
	}
}
