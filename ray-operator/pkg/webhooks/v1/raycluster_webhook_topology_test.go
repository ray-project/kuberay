package v1

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

var _ = Describe("RayCluster topology", func() {
	newTopologyRayCluster := func(mapping rayv1.TopologyLabelMapping) *rayv1.RayCluster {
		template := corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "ray"}}}}
		return &rayv1.RayCluster{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("topo-%d", rand.IntnRange(1000, 9000)), Namespace: "default"},
			Spec: rayv1.RayClusterSpec{
				HeadGroupSpec: rayv1.HeadGroupSpec{Template: template},
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{{
					GroupName:   "test",
					MinReplicas: new(int32(1)),
					MaxReplicas: new(int32(1)),
					Template:    template,
					Topology:    &rayv1.TopologySpec{LabelMappings: []rayv1.TopologyLabelMapping{mapping}},
				}},
			},
		}
	}

	It("accepts allowlisted mappings and rejects the rest", func() {
		accepted := newTopologyRayCluster(rayv1.TopologyLabelMapping{NodeLabel: testConfig.AllowedNodeLabels[0], MapTo: "ray.io/zone"})
		Expect(k8sClient.Create(ctx, accepted)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, accepted)

		err := k8sClient.Create(ctx, newTopologyRayCluster(rayv1.TopologyLabelMapping{NodeLabel: "cloud.google.com/gke-nodepool"}))
		Expect(err).To(MatchError(ContainSubstring(`node label "cloud.google.com/gke-nodepool" is not in the operator's allowedNodeLabels`)))
	})
})
