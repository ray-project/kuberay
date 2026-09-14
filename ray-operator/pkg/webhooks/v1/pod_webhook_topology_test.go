package v1

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

var _ = Describe("Pod topology", func() {
	It("prepares a topology worker at CREATE", func() {
		clusterName := fmt.Sprintf("topo-%d", rand.IntnRange(1000, 9000))
		template := corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ray"}}}}
		cluster := &rayv1.RayCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: "default"},
			Spec: rayv1.RayClusterSpec{
				HeadGroupSpec: rayv1.HeadGroupSpec{Template: template},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{{
					GroupName:   "test",
					MinReplicas: new(int32(1)),
					MaxReplicas: new(int32(1)),
					Template:    template,
					Topology:    &rayv1.TopologySpec{LabelMappings: []rayv1.TopologyLabelMapping{{NodeLabel: testZoneLabel}}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, cluster)

		pod := newWorkerPod("test")
		pod.Name = clusterName + "-test-worker-" + rand.String(5)
		pod.Labels[utils.RayClusterLabelKey] = clusterName
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, pod)

		// the unit tests cover the mutation itself; this proves the real admission path applied it
		Expect(nodeLabelsEnv(pod.Spec.Containers[utils.RayContainerIndex].Env)).NotTo(BeNil())
	})
})
