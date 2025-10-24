package utils

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAddSchedulerNameToObject(t *testing.T) {
	schedulerName := "test-scheduler"

	t.Run("Pod object should have schedulerName set", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
			},
			Spec: corev1.PodSpec{},
		}

		AddSchedulerNameToObject(pod, schedulerName)

		if pod.Spec.SchedulerName != schedulerName {
			t.Errorf("expected schedulerName to be %q, got %q", schedulerName, pod.Spec.SchedulerName)
		}
	})

	t.Run("PodTemplateSpec object should have schedulerName set", func(t *testing.T) {
		podTemplate := &corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-template",
				Namespace: "default",
			},
			Spec: corev1.PodSpec{},
		}

		AddSchedulerNameToObject(podTemplate, schedulerName)

		if podTemplate.Spec.SchedulerName != schedulerName {
			t.Errorf("expected schedulerName to be %q, got %q", schedulerName, podTemplate.Spec.SchedulerName)
		}
	})
}
