package support

import (
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	schedulingv1alpha2 "github.com/ray-project/kuberay/ray-operator/internal/scheduling/v1alpha2"
)

// The scheduling.k8s.io/v1alpha2 types were removed from k8s.io/api in v0.37
// (Kubernetes 1.37), but KubeRay still supports the functionality on 1.36
// clusters, so the types are vendored locally. Because they are no longer part
// of the typed client-go clientset, these helpers access them via the dynamic
// client and convert the results into the vendored types.
var (
	workloadGVR = schema.GroupVersionResource{Group: schedulingv1alpha2.GroupName, Version: "v1alpha2", Resource: "workloads"}
	podGroupGVR = schema.GroupVersionResource{Group: schedulingv1alpha2.GroupName, Version: "v1alpha2", Resource: "podgroups"}
)

func Workload(t Test, namespace, name string) func() (*schedulingv1alpha2.Workload, error) {
	return func() (*schedulingv1alpha2.Workload, error) {
		return GetWorkload(t, namespace, name)
	}
}

func GetWorkload(t Test, namespace, name string) (*schedulingv1alpha2.Workload, error) {
	u, err := t.Client().Dynamic().Resource(workloadGVR).Namespace(namespace).Get(t.Ctx(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	workload := &schedulingv1alpha2.Workload{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, workload); err != nil {
		return nil, err
	}
	return workload, nil
}

func PodGroup(t Test, namespace, name string) func() (*schedulingv1alpha2.PodGroup, error) {
	return func() (*schedulingv1alpha2.PodGroup, error) {
		return GetPodGroup(t, namespace, name)
	}
}

func GetPodGroup(t Test, namespace, name string) (*schedulingv1alpha2.PodGroup, error) {
	u, err := t.Client().Dynamic().Resource(podGroupGVR).Namespace(namespace).Get(t.Ctx(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	podGroup := &schedulingv1alpha2.PodGroup{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, podGroup); err != nil {
		return nil, err
	}
	return podGroup, nil
}

func Workloads(t Test, namespace string) func(g gomega.Gomega) []schedulingv1alpha2.Workload {
	return func(g gomega.Gomega) []schedulingv1alpha2.Workload {
		list, err := t.Client().Dynamic().Resource(workloadGVR).Namespace(namespace).List(t.Ctx(), metav1.ListOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		workloads := make([]schedulingv1alpha2.Workload, len(list.Items))
		for i := range list.Items {
			g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(list.Items[i].Object, &workloads[i])).To(gomega.Succeed())
		}
		return workloads
	}
}

func PodGroups(t Test, namespace string) func(g gomega.Gomega) []schedulingv1alpha2.PodGroup {
	return func(g gomega.Gomega) []schedulingv1alpha2.PodGroup {
		list, err := t.Client().Dynamic().Resource(podGroupGVR).Namespace(namespace).List(t.Ctx(), metav1.ListOptions{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		podGroups := make([]schedulingv1alpha2.PodGroup, len(list.Items))
		for i := range list.Items {
			g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(list.Items[i].Object, &podGroups[i])).To(gomega.Succeed())
		}
		return podGroups
	}
}
