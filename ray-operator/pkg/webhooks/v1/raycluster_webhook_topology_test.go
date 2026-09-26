package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

var _ = Describe("RayCluster topology", func() {
	It("accepts allowlisted mappings and rejects the rest", func() {
		accepted := newTopologyRayCluster(rayv1.TopologyLabelMapping{NodeLabel: testConfig.AllowedNodeLabels[0], MapTo: "ray.io/zone"})
		Expect(k8sClient.Create(ctx, accepted)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, accepted)

		err := k8sClient.Create(ctx, newTopologyRayCluster(rayv1.TopologyLabelMapping{NodeLabel: "cloud.google.com/gke-nodepool"}))
		Expect(err).To(MatchError(ContainSubstring(`node label "cloud.google.com/gke-nodepool" is not in the operator's allowedNodeLabels`)))
	})
})
