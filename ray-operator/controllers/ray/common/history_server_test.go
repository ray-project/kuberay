package common

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func makeTestPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "ray-head",
					Image: "rayproject/ray:2.44.0",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			},
		},
	}
}

func TestInjectHistoryServerCollector_Disabled(t *testing.T) {
	pod := makeTestPod()

	InjectHistoryServerCollector(context.Background(), nil, &pod)

	assert.Len(t, pod.Spec.Containers, 1, "should not inject when options is nil")
}

func TestInjectHistoryServerCollector_Enabled(t *testing.T) {
	pod := makeTestPod()
	opts := &rayv1.HistoryServerCollectorOptions{}

	InjectHistoryServerCollector(context.Background(), opts, &pod)

	// Should have 2 containers: ray-head + collector
	assert.Len(t, pod.Spec.Containers, 2)
	assert.Equal(t, "collector", pod.Spec.Containers[1].Name)
	assert.Equal(t, historyServerCollectorDefaultImage, pod.Spec.Containers[1].Image)

	// Ray container should have postStart hook
	rayContainer := pod.Spec.Containers[0]
	assert.NotNil(t, rayContainer.Lifecycle)
	assert.NotNil(t, rayContainer.Lifecycle.PostStart)
	assert.NotNil(t, rayContainer.Lifecycle.PostStart.Exec)

	// Ray container should have event env vars
	envMap := make(map[string]string)
	for _, env := range rayContainer.Env {
		envMap[env.Name] = env.Value
	}
	assert.Equal(t, "true", envMap["RAY_enable_ray_event"])
	assert.Equal(t, "true", envMap["RAY_enable_core_worker_ray_event_to_aggregator"])
	assert.Equal(t, historyServerEventsExportAddr, envMap["RAY_DASHBOARD_AGGREGATOR_AGENT_EVENTS_EXPORT_ADDR"])

	// Both containers should have /tmp/ray volume mount
	assert.True(t, hasVolumeMount(rayContainer.VolumeMounts, historyServerVolumeMountPath))
	assert.True(t, hasVolumeMount(pod.Spec.Containers[1].VolumeMounts, historyServerVolumeMountPath))

	// Pod should have the shared volume
	assert.True(t, hasVolume(pod.Spec.Volumes, historyServerVolumeName))
}

func TestInjectHistoryServerCollector_CustomImage(t *testing.T) {
	pod := makeTestPod()
	opts := &rayv1.HistoryServerCollectorOptions{
		Image: ptr.To("my-registry/collector:v1.0"),
	}

	InjectHistoryServerCollector(context.Background(), opts, &pod)

	assert.Len(t, pod.Spec.Containers, 2)
	assert.Equal(t, "my-registry/collector:v1.0", pod.Spec.Containers[1].Image)
}

func TestInjectHistoryServerCollector_CustomRuntimeClass(t *testing.T) {
	pod := makeTestPod()
	opts := &rayv1.HistoryServerCollectorOptions{
		RuntimeClassName: ptr.To("gcs"),
	}

	InjectHistoryServerCollector(context.Background(), opts, &pod)

	collector := pod.Spec.Containers[1]
	foundRuntimeClass := false
	for _, arg := range collector.Args {
		if arg == "--runtime-class-name=gcs" {
			foundRuntimeClass = true
		}
	}
	assert.True(t, foundRuntimeClass, "collector should have --runtime-class-name=gcs")
}

func TestInjectHistoryServerCollector_EnvFromSecret(t *testing.T) {
	pod := makeTestPod()
	opts := &rayv1.HistoryServerCollectorOptions{
		EnvFrom: []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "s3-credentials"},
				},
			},
		},
	}

	InjectHistoryServerCollector(context.Background(), opts, &pod)

	collector := pod.Spec.Containers[1]
	assert.Len(t, collector.EnvFrom, 1)
	assert.NotNil(t, collector.EnvFrom[0].SecretRef)
	assert.Equal(t, "s3-credentials", collector.EnvFrom[0].SecretRef.Name)
}

func TestInjectHistoryServerCollector_ExplicitEnv(t *testing.T) {
	pod := makeTestPod()
	opts := &rayv1.HistoryServerCollectorOptions{
		Env: []corev1.EnvVar{
			{Name: "S3_BUCKET", Value: "my-bucket"},
			{Name: "S3_REGION", Value: "us-west-2"},
		},
	}

	InjectHistoryServerCollector(context.Background(), opts, &pod)

	collector := pod.Spec.Containers[1]
	envMap := make(map[string]string)
	for _, env := range collector.Env {
		envMap[env.Name] = env.Value
	}
	assert.Equal(t, "my-bucket", envMap["S3_BUCKET"])
	assert.Equal(t, "us-west-2", envMap["S3_REGION"])
}

func TestInjectHistoryServerCollector_CustomResources(t *testing.T) {
	pod := makeTestPod()
	opts := &rayv1.HistoryServerCollectorOptions{
		Resources: &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
	}

	InjectHistoryServerCollector(context.Background(), opts, &pod)

	collector := pod.Spec.Containers[1]
	assert.Equal(t, resource.MustParse("500m"), collector.Resources.Requests[corev1.ResourceCPU])
	assert.Equal(t, resource.MustParse("512Mi"), collector.Resources.Requests[corev1.ResourceMemory])
}

func TestInjectHistoryServerCollector_PreservesExistingPostStart(t *testing.T) {
	pod := makeTestPod()
	pod.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{
		PostStart: &corev1.LifecycleHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/bin/sh", "-c", "echo existing-hook"},
			},
		},
	}
	opts := &rayv1.HistoryServerCollectorOptions{}

	InjectHistoryServerCollector(context.Background(), opts, &pod)

	// Should NOT overwrite existing postStart hook
	assert.Equal(t, "echo existing-hook", pod.Spec.Containers[0].Lifecycle.PostStart.Exec.Command[2])
	// But should still inject collector sidecar
	assert.Len(t, pod.Spec.Containers, 2)
}

func TestInjectHistoryServerCollector_SkipsExistingEnvVars(t *testing.T) {
	pod := makeTestPod()
	pod.Spec.Containers[0].Env = []corev1.EnvVar{
		{Name: "RAY_enable_ray_event", Value: "false"},
	}
	opts := &rayv1.HistoryServerCollectorOptions{}

	InjectHistoryServerCollector(context.Background(), opts, &pod)

	// Should NOT overwrite existing env var
	envMap := make(map[string]string)
	for _, env := range pod.Spec.Containers[0].Env {
		envMap[env.Name] = env.Value
	}
	assert.Equal(t, "false", envMap["RAY_enable_ray_event"])
}

func hasVolumeMount(mounts []corev1.VolumeMount, path string) bool {
	for _, m := range mounts {
		if m.MountPath == path {
			return true
		}
	}
	return false
}

func hasVolume(volumes []corev1.Volume, name string) bool {
	for _, v := range volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}
