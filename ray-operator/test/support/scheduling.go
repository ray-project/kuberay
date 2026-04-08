package support

import (
	"github.com/onsi/gomega"
	schedulingv1alpha2 "k8s.io/api/scheduling/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Workload(t Test, namespace, name string) func() (*schedulingv1alpha2.Workload, error) {
	return func() (*schedulingv1alpha2.Workload, error) {
		return GetWorkload(t, namespace, name)
	}
}

func GetWorkload(t Test, namespace, name string) (*schedulingv1alpha2.Workload, error) {
	return t.Client().Core().SchedulingV1alpha2().Workloads(namespace).Get(t.Ctx(), name, metav1.GetOptions{})
}

func PodGroup(t Test, namespace, name string) func() (*schedulingv1alpha2.PodGroup, error) {
	return func() (*schedulingv1alpha2.PodGroup, error) {
		return GetPodGroup(t, namespace, name)
	}
}

func GetPodGroup(t Test, namespace, name string) (*schedulingv1alpha2.PodGroup, error) {
	return t.Client().Core().SchedulingV1alpha2().PodGroups(namespace).Get(t.Ctx(), name, metav1.GetOptions{})
}

func Workloads(t Test, namespace string) func(g gomega.Gomega) []schedulingv1alpha2.Workload {
	return func(g gomega.Gomega) []schedulingv1alpha2.Workload {
		workloads, err := t.Client().Core().SchedulingV1alpha2().Workloads(namespace).List(t.Ctx(), metav1.ListOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return workloads.Items
	}
}

func PodGroups(t Test, namespace string) func(g gomega.Gomega) []schedulingv1alpha2.PodGroup {
	return func(g gomega.Gomega) []schedulingv1alpha2.PodGroup {
		podGroups, err := t.Client().Core().SchedulingV1alpha2().PodGroups(namespace).List(t.Ctx(), metav1.ListOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		return podGroups.Items
	}
}

func GetEvents(t Test, namespace string, objectName string, reason string) func() ([]string, error) {
	return func() ([]string, error) {
		events, err := t.Client().Core().EventsV1().Events(namespace).List(t.Ctx(), metav1.ListOptions{
			FieldSelector: "regarding.name=" + objectName + ",reason=" + reason,
		})
		if err != nil {
			return nil, err
		}
		messages := make([]string, 0, len(events.Items))
		for _, e := range events.Items {
			messages = append(messages, e.Note)
		}
		return messages, nil
	}
}
