package e2e

import (
	"embed"

	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

//go:embed *.py
var _files embed.FS

func ReadFile(t Test, fileName string) []byte {
	t.T().Helper()
	file, err := _files.ReadFile(fileName)
	t.Expect(err).NotTo(gomega.HaveOccurred())
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

func newConfigMap(namespace, name string, options ...option[corev1.ConfigMap]) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		BinaryData: map[string][]byte{},
		Immutable:  Ptr(true),
	}

	return configMapWith(cm, options...)
}

func configMapWith(configMap *corev1.ConfigMap, options ...option[corev1.ConfigMap]) *corev1.ConfigMap {
	return apply(configMap, options...)
}

func file(t Test, fileName string) option[corev1.ConfigMap] {
	return func(cm *corev1.ConfigMap) *corev1.ConfigMap {
		cm.BinaryData[fileName] = ReadFile(t, fileName)
		return cm
	}
}

func files(t Test, fileNames ...string) option[corev1.ConfigMap] {
	var files []option[corev1.ConfigMap]
	for _, fileName := range fileNames {
		files = append(files, file(t, fileName))
	}
	return options(files...)
}

func newRayClusterSpec(options ...option[rayv1.RayClusterSpec]) *rayv1.RayClusterSpec {
	return rayClusterSpecWith(rayClusterSpec(), options...)
}

func rayClusterSpecWith(spec *rayv1.RayClusterSpec, options ...option[rayv1.RayClusterSpec]) *rayv1.RayClusterSpec {
	return apply(spec, options...)
}

func mountConfigMap[T rayv1.RayClusterSpec | corev1ac.PodTemplateSpecApplyConfiguration](configMap *corev1.ConfigMap, mountPath string) option[T] {
	return func(t *T) *T {
		switch obj := (interface{})(t).(type) {
		case *rayv1.RayClusterSpec:
			mounts := obj.HeadGroupSpec.Template.Spec.Containers[0].VolumeMounts
			obj.HeadGroupSpec.Template.Spec.Containers[0].VolumeMounts = append(mounts, corev1.VolumeMount{
				Name:      configMap.Name,
				MountPath: mountPath,
			})
			obj.HeadGroupSpec.Template.Spec.Volumes = append(obj.HeadGroupSpec.Template.Spec.Volumes, corev1.Volume{
				Name: configMap.Name,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: configMap.Name,
						},
					},
				},
			})
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

func rayClusterSpec() *rayv1.RayClusterSpec {
	return &rayv1.RayClusterSpec{
		RayVersion: GetRayVersion(),
		HeadGroupSpec: rayv1.HeadGroupSpec{
			RayStartParams: map[string]string{
				"dashboard-host": "0.0.0.0",
			},
			Template: headPodTemplate(),
		},
		WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
			{
				Replicas:       Ptr(int32(1)),
				MinReplicas:    Ptr(int32(1)),
				MaxReplicas:    Ptr(int32(1)),
				GroupName:      "small-group",
				RayStartParams: map[string]string{},
				Template:       workerPodTemplate(),
			},
		},
	}
}

func podTemplateSpecApplyConfiguration(template *corev1ac.PodTemplateSpecApplyConfiguration, options ...option[corev1ac.PodTemplateSpecApplyConfiguration]) *corev1ac.PodTemplateSpecApplyConfiguration {
	return apply(template, options...)
}

func headPodTemplate() corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "ray-head",
					Image: GetRayImage(),
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 6379,
							Name:          "gcs",
						},
						{
							ContainerPort: 8265,
							Name:          "dashboard",
						},
						{
							ContainerPort: 10001,
							Name:          "client",
						},
					},
					Lifecycle: &corev1.Lifecycle{
						PreStop: &corev1.LifecycleHandler{
							Exec: &corev1.ExecAction{
								Command: []string{"/bin/sh", "-c", "ray stop"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("300m"),
							corev1.ResourceMemory: resource.MustParse("1G"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("2G"),
						},
					},
				},
			},
		},
	}
}

func headPodTemplateApplyConfiguration() *corev1ac.PodTemplateSpecApplyConfiguration {
	return corev1ac.PodTemplateSpec().
		WithSpec(corev1ac.PodSpec().
			WithContainers(corev1ac.Container().
				WithName("ray-head").
				WithImage(GetRayImage()).
				WithPorts(
					corev1ac.ContainerPort().WithName("gcs").WithContainerPort(6379),
					corev1ac.ContainerPort().WithName("dashboard").WithContainerPort(8265),
					corev1ac.ContainerPort().WithName("client").WithContainerPort(10001),
				).
				WithLifecycle(corev1ac.Lifecycle().
					WithPreStop(corev1ac.LifecycleHandler().
						WithExec(corev1ac.ExecAction().
							WithCommand("/bin/sh", "-c", "ray stop")))).
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

func workerPodTemplate() corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "ray-worker",
					Image: GetRayImage(),
					Lifecycle: &corev1.Lifecycle{
						PreStop: &corev1.LifecycleHandler{
							Exec: &corev1.ExecAction{
								Command: []string{"/bin/sh", "-c", "ray stop"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("300m"),
							corev1.ResourceMemory: resource.MustParse("1G"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("1G"),
						},
					},
				},
			},
		},
	}
}

func workerPodTemplateApplyConfiguration() *corev1ac.PodTemplateSpecApplyConfiguration {
	return corev1ac.PodTemplateSpec().
		WithSpec(corev1ac.PodSpec().
			WithContainers(corev1ac.Container().
				WithName("ray-worker").
				WithImage(GetRayImage()).
				WithLifecycle(corev1ac.Lifecycle().
					WithPreStop(corev1ac.LifecycleHandler().
						WithExec(corev1ac.ExecAction().
							WithCommand("/bin/sh", "-c", "ray stop")))).
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

func jobSubmitterPodTemplate() *corev1.PodTemplateSpec {
	return &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "ray-job-submitter",
					Image: GetRayImage(),
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("200Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("500Mi"),
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
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
