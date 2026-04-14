/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ray

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/test/support"
)

func rayClusterTemplateForNetworkPolicy(name string, namespace string) *rayv1.RayCluster {
	return &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: rayv1.RayClusterSpec{
			RayVersion: support.GetRayVersion(),
			NetworkIsolation: &rayv1.NetworkIsolationConfig{
				Mode: ptr.To(rayv1.NetworkIsolationDenyAll),
			},
			HeadGroupSpec: rayv1.HeadGroupSpec{
				RayStartParams: map[string]string{},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Image: support.GetRayImage(),
							},
						},
					},
				},
			},
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{
					Replicas:    ptr.To[int32](1),
					MinReplicas: ptr.To[int32](0),
					MaxReplicas: ptr.To[int32](2),
					GroupName:   "small-group",
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "ray-worker",
									Image: support.GetRayImage(),
								},
							},
						},
					},
				},
			},
		},
	}
}

var _ = Context("NetworkPolicy Controller Integration Tests", func() {
	Describe("Basic NetworkPolicy Creation", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplateForNetworkPolicy("raycluster-networkpolicy", namespace)

		It("Verify RayCluster spec", func() {
			Expect(rayCluster.Spec.WorkerGroupSpecs).To(HaveLen(1))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Replicas).To(Equal(ptr.To[int32](1)))
		})

		It("Create a RayCluster custom resource", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
		})

		It("Head NetworkPolicy is created", func() {
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			headKey := types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-head"}

			Eventually(
				getResourceFunc(ctx, headKey, headNetworkPolicy),
				time.Second*10, time.Millisecond*500).Should(Succeed(), "Head NetworkPolicy should be created")
		})

		It("Worker NetworkPolicy is created", func() {
			workerNetworkPolicy := &networkingv1.NetworkPolicy{}
			workerKey := types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-workers"}

			Eventually(
				getResourceFunc(ctx, workerKey, workerNetworkPolicy),
				time.Second*10, time.Millisecond*500).Should(Succeed(), "Worker NetworkPolicy should be created")
		})

		It("Head NetworkPolicy has correct labels and owner reference", func() {
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			headKey := types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-head"}

			err := k8sClient.Get(ctx, headKey, headNetworkPolicy)
			Expect(err).NotTo(HaveOccurred(), "Failed to get Head NetworkPolicy")

			expectedLabels := map[string]string{
				utils.RayClusterLabelKey:                rayCluster.Name,
				utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
				utils.KubernetesCreatedByLabelKey:       utils.ComponentName,
			}
			Expect(headNetworkPolicy.Labels).To(Equal(expectedLabels))

			Expect(headNetworkPolicy.OwnerReferences).To(HaveLen(1))
			Expect(headNetworkPolicy.OwnerReferences[0].Name).To(Equal(rayCluster.Name))
			Expect(headNetworkPolicy.OwnerReferences[0].Kind).To(Equal("RayCluster"))
		})

		It("Head NetworkPolicy targets head pods with correct policy types and ingress rules", func() {
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			headKey := types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-head"}

			err := k8sClient.Get(ctx, headKey, headNetworkPolicy)
			Expect(err).NotTo(HaveOccurred(), "Failed to get Head NetworkPolicy")

			// denyAll mode — both Ingress and Egress policy types must be present.
			Expect(headNetworkPolicy.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeIngress))
			Expect(headNetworkPolicy.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress))

			// PodSelector must target head pods.
			expectedPodSelector := metav1.LabelSelector{
				MatchLabels: map[string]string{
					utils.RayClusterLabelKey:  rayCluster.Name,
					utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
				},
			}
			Expect(headNetworkPolicy.Spec.PodSelector).To(Equal(expectedPodSelector))

			// 3 base ingress rules: intra-cluster, operator, and RayJob submitter access.
			Expect(headNetworkPolicy.Spec.Ingress).To(HaveLen(3), "Should have 3 base ingress rules")

			// Rule 0: intra-cluster — no ports (allows all).
			intraClusterRule := headNetworkPolicy.Spec.Ingress[0]
			Expect(intraClusterRule.From).To(HaveLen(1))
			Expect(intraClusterRule.Ports).To(BeEmpty(), "Intra-cluster rule should allow all ports")
			Expect(intraClusterRule.From[0]).To(Equal(networkingv1.NetworkPolicyPeer{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{utils.RayClusterLabelKey: rayCluster.Name},
				},
			}))

			// 2 base egress rules: intra-cluster and DNS.
			Expect(headNetworkPolicy.Spec.Egress).To(HaveLen(2), "Should have 2 base egress rules")

			// Egress Rule 0: intra-cluster — no ports (allows all).
			intraClusterEgress := headNetworkPolicy.Spec.Egress[0]
			Expect(intraClusterEgress.To).To(HaveLen(1))
			Expect(intraClusterEgress.Ports).To(BeEmpty(), "Intra-cluster egress should allow all ports")
			Expect(intraClusterEgress.To[0]).To(Equal(networkingv1.NetworkPolicyPeer{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{utils.RayClusterLabelKey: rayCluster.Name},
				},
			}))

			// Egress Rule 1: DNS — UDP+TCP port 53, no destination restriction.
			dnsEgress := headNetworkPolicy.Spec.Egress[1]
			Expect(dnsEgress.To).To(BeEmpty(), "DNS rule should not restrict destinations")
			Expect(dnsEgress.Ports).To(HaveLen(2))
			dnsPort := intstr.FromInt(53)
			Expect(dnsEgress.Ports[0].Port).To(Equal(&dnsPort))
			Expect(dnsEgress.Ports[1].Port).To(Equal(&dnsPort))
		})

		It("Worker NetworkPolicy targets worker pods with intra-cluster ingress", func() {
			workerNetworkPolicy := &networkingv1.NetworkPolicy{}
			workerKey := types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-workers"}

			err := k8sClient.Get(ctx, workerKey, workerNetworkPolicy)
			Expect(err).NotTo(HaveOccurred(), "Failed to get Worker NetworkPolicy")

			expectedPodSelector := metav1.LabelSelector{
				MatchLabels: map[string]string{
					utils.RayClusterLabelKey:  rayCluster.Name,
					utils.RayNodeTypeLabelKey: string(rayv1.WorkerNode),
				},
			}
			Expect(workerNetworkPolicy.Spec.PodSelector).To(Equal(expectedPodSelector))

			// Workers allow only intra-cluster ingress (no Ports = all ports).
			Expect(workerNetworkPolicy.Spec.Ingress).To(HaveLen(1))
			Expect(workerNetworkPolicy.Spec.Ingress[0].From).To(HaveLen(1))
			Expect(workerNetworkPolicy.Spec.Ingress[0].Ports).To(BeEmpty())
			Expect(workerNetworkPolicy.Spec.Ingress[0].From[0]).To(Equal(networkingv1.NetworkPolicyPeer{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{utils.RayClusterLabelKey: rayCluster.Name},
				},
			}))
		})

		It("Delete RayCluster", func() {
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")
		})
	})

	Describe("No NetworkPolicy when NetworkIsolation is absent", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplateForNetworkPolicy("raycluster-no-isolation", namespace)
		rayCluster.Spec.NetworkIsolation = nil

		It("Create a RayCluster without NetworkIsolation", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed())
		})

		It("Head NetworkPolicy should NOT be created", func() {
			headKey := client.ObjectKey{Namespace: namespace, Name: rayCluster.Name + "-head"}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, headKey, &networkingv1.NetworkPolicy{})
				return err != nil && client.IgnoreNotFound(err) == nil
			}, time.Second*5, time.Millisecond*500).Should(BeTrue(), "Head NetworkPolicy must not be created")
		})

		It("Worker NetworkPolicy should NOT be created", func() {
			workerKey := client.ObjectKey{Namespace: namespace, Name: rayCluster.Name + "-workers"}
			Consistently(func() bool {
				err := k8sClient.Get(ctx, workerKey, &networkingv1.NetworkPolicy{})
				return err != nil && client.IgnoreNotFound(err) == nil
			}, time.Second*5, time.Millisecond*500).Should(BeTrue(), "Worker NetworkPolicy must not be created")
		})

		It("Delete RayCluster", func() {
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")
		})
	})

	Describe("NetworkPolicies deleted when NetworkIsolation is removed", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplateForNetworkPolicy("raycluster-cleanup", namespace)

		It("Create a RayCluster with NetworkIsolation", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed())
		})

		It("Both NetworkPolicies are created", func() {
			Eventually(
				getResourceFunc(ctx, types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-head"}, &networkingv1.NetworkPolicy{}),
				time.Second*10, time.Millisecond*500).Should(Succeed(), "Head NetworkPolicy should be created")
			Eventually(
				getResourceFunc(ctx, types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-workers"}, &networkingv1.NetworkPolicy{}),
				time.Second*10, time.Millisecond*500).Should(Succeed(), "Worker NetworkPolicy should be created")
		})

		It("Remove NetworkIsolation from spec", func() {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster)
			Expect(err).NotTo(HaveOccurred())
			rayCluster.Spec.NetworkIsolation = nil
			err = k8sClient.Update(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster")
		})

		It("Head NetworkPolicy is deleted by the controller", func() {
			headKey := client.ObjectKey{Namespace: namespace, Name: rayCluster.Name + "-head"}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, headKey, &networkingv1.NetworkPolicy{})
				return err != nil && client.IgnoreNotFound(err) == nil
			}, time.Second*10, time.Millisecond*500).Should(BeTrue(), "Head NetworkPolicy should be deleted")
		})

		It("Worker NetworkPolicy is deleted by the controller", func() {
			workerKey := client.ObjectKey{Namespace: namespace, Name: rayCluster.Name + "-workers"}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, workerKey, &networkingv1.NetworkPolicy{})
				return err != nil && client.IgnoreNotFound(err) == nil
			}, time.Second*10, time.Millisecond*500).Should(BeTrue(), "Worker NetworkPolicy should be deleted")
		})

		It("Delete RayCluster", func() {
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")
		})
	})

	Describe("Mode denyAllIngress creates Ingress-only policies", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplateForNetworkPolicy("raycluster-mode-ingress", namespace)
		rayCluster.Spec.NetworkIsolation.Mode = ptr.To(rayv1.NetworkIsolationDenyAllIngress)

		It("Create a RayCluster with denyAllIngress mode", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed())
		})

		It("Head NetworkPolicy has Ingress only policy type", func() {
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			headKey := types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-head"}
			Eventually(
				getResourceFunc(ctx, headKey, headNetworkPolicy),
				time.Second*10, time.Millisecond*500).Should(Succeed())

			Expect(headNetworkPolicy.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeIngress))
			Expect(headNetworkPolicy.Spec.PolicyTypes).NotTo(ContainElement(networkingv1.PolicyTypeEgress))
			Expect(headNetworkPolicy.Spec.Egress).To(BeEmpty())
		})

		It("Worker NetworkPolicy has Ingress only policy type", func() {
			workerNetworkPolicy := &networkingv1.NetworkPolicy{}
			workerKey := types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-workers"}
			Eventually(
				getResourceFunc(ctx, workerKey, workerNetworkPolicy),
				time.Second*10, time.Millisecond*500).Should(Succeed())

			Expect(workerNetworkPolicy.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeIngress))
			Expect(workerNetworkPolicy.Spec.PolicyTypes).NotTo(ContainElement(networkingv1.PolicyTypeEgress))
			Expect(workerNetworkPolicy.Spec.Egress).To(BeEmpty())
		})

		It("Delete RayCluster", func() {
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")
		})
	})

	Describe("Mode denyAllEgress creates Egress-only policies", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplateForNetworkPolicy("raycluster-mode-egress", namespace)
		rayCluster.Spec.NetworkIsolation.Mode = ptr.To(rayv1.NetworkIsolationDenyAllEgress)

		It("Create a RayCluster with denyAllEgress mode", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed())
		})

		It("Head NetworkPolicy has Egress only policy type", func() {
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			headKey := types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-head"}
			Eventually(
				getResourceFunc(ctx, headKey, headNetworkPolicy),
				time.Second*10, time.Millisecond*500).Should(Succeed())

			Expect(headNetworkPolicy.Spec.PolicyTypes).NotTo(ContainElement(networkingv1.PolicyTypeIngress))
			Expect(headNetworkPolicy.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress))
			Expect(headNetworkPolicy.Spec.Ingress).To(BeEmpty())
			Expect(headNetworkPolicy.Spec.Egress).NotTo(BeEmpty())
		})

		It("Worker NetworkPolicy has Egress only policy type", func() {
			workerNetworkPolicy := &networkingv1.NetworkPolicy{}
			workerKey := types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-workers"}
			Eventually(
				getResourceFunc(ctx, workerKey, workerNetworkPolicy),
				time.Second*10, time.Millisecond*500).Should(Succeed())

			Expect(workerNetworkPolicy.Spec.PolicyTypes).NotTo(ContainElement(networkingv1.PolicyTypeIngress))
			Expect(workerNetworkPolicy.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress))
			Expect(workerNetworkPolicy.Spec.Ingress).To(BeEmpty())
			Expect(workerNetworkPolicy.Spec.Egress).NotTo(BeEmpty())
		})

		It("Delete RayCluster", func() {
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")
		})
	})

	Describe("Custom IngressRules are appended to head NetworkPolicy", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		tcpProto := corev1.ProtocolTCP
		customPort := int32(9999)
		rayCluster := rayClusterTemplateForNetworkPolicy("raycluster-custom-ingress", namespace)
		rayCluster.Spec.NetworkIsolation.IngressRules = []networkingv1.NetworkPolicyIngressRule{
			{
				Ports: []networkingv1.NetworkPolicyPort{
					{
						Protocol: &tcpProto,
						Port:     portPtr(customPort),
					},
				},
			},
		}

		It("Create a RayCluster with custom IngressRules", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed())
		})

		It("Head NetworkPolicy has 4 ingress rules (3 base + 1 custom)", func() {
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			headKey := types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-head"}
			Eventually(
				getResourceFunc(ctx, headKey, headNetworkPolicy),
				time.Second*10, time.Millisecond*500).Should(Succeed())

			Expect(headNetworkPolicy.Spec.Ingress).To(HaveLen(4), "Should have 3 base + 1 custom ingress rules")
			Expect(headNetworkPolicy.Spec.Ingress[3].Ports[0].Port.IntVal).To(Equal(customPort))
		})

		It("Delete RayCluster", func() {
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")
		})
	})

	Describe("RayJob submitter ingress rule is present on all clusters", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplateForNetworkPolicy("raycluster-submitter-rule", namespace)

		It("Create a standalone RayCluster", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed())
		})

		It("Head NetworkPolicy includes RayJob submitter ingress rule on dashboard port", func() {
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			headKey := types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-head"}

			Eventually(
				getResourceFunc(ctx, headKey, headNetworkPolicy),
				time.Second*10, time.Millisecond*500).Should(Succeed(), "Head NetworkPolicy should be created")

			Expect(headNetworkPolicy.Spec.Ingress).To(HaveLen(3), "Should have 3 base ingress rules")

			submitterRule := headNetworkPolicy.Spec.Ingress[2]
			Expect(submitterRule.From).To(HaveLen(1))
			Expect(submitterRule.From[0]).To(Equal(networkingv1.NetworkPolicyPeer{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						utils.RayOriginatedFromCRDLabelKey: utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD),
					},
				},
			}))
			Expect(submitterRule.Ports).To(HaveLen(1), "Submitter rule should only allow dashboard port")
			Expect(submitterRule.Ports[0].Port.IntVal).To(Equal(int32(8265)))
		})

		It("Clean up RayCluster", func() {
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")
		})
	})

	Describe("Pre-existing NetworkPolicy is not modified by the controller", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplateForNetworkPolicy("raycluster-existing-np", namespace)
		existingHeadNetworkPolicy := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rayCluster.Name + "-head",
				Namespace: namespace,
				Labels: map[string]string{
					"test": "existing",
				},
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			},
		}

		It("Create Head NetworkPolicy before the RayCluster exists", func() {
			err := k8sClient.Create(ctx, existingHeadNetworkPolicy)
			Expect(err).NotTo(HaveOccurred(), "Failed to create pre-existing Head NetworkPolicy")
		})

		It("Create RayCluster — controller should skip the pre-existing NetworkPolicy", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed())
		})

		It("Pre-existing Head NetworkPolicy is not adopted by the controller", func() {
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			headKey := types.NamespacedName{Namespace: namespace, Name: existingHeadNetworkPolicy.Name}

			// Give the controller time to reconcile, then verify the policy was not modified.
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, headKey, headNetworkPolicy)).To(Succeed())
				g.Expect(headNetworkPolicy.OwnerReferences).To(BeEmpty(), "Controller must not adopt a pre-existing NetworkPolicy")
				g.Expect(headNetworkPolicy.Labels).To(Equal(map[string]string{"test": "existing"}), "Labels must remain unchanged")
			}, time.Second*5, time.Millisecond*500).Should(Succeed())
		})

		It("Worker NetworkPolicy is created", func() {
			workerKey := types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-workers"}
			Eventually(
				getResourceFunc(ctx, workerKey, &networkingv1.NetworkPolicy{}),
				time.Second*10, time.Millisecond*500).Should(Succeed(), "Worker NetworkPolicy should be created")
		})

		It("Clean up resources", func() {
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")

			// Clean up the pre-existing policy (not garbage collected since it has no owner)
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			headKey := types.NamespacedName{Namespace: namespace, Name: existingHeadNetworkPolicy.Name}
			err = k8sClient.Get(ctx, headKey, headNetworkPolicy)
			if err == nil {
				Expect(k8sClient.Delete(ctx, headNetworkPolicy)).To(Succeed())
			}
		})
	})

	Describe("RayCluster Deletion", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplateForNetworkPolicy("raycluster-deletion", namespace)

		It("Create a RayCluster and verify NetworkPolicies exist", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")

			Eventually(
				getResourceFunc(ctx, types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-head"}, &networkingv1.NetworkPolicy{}),
				time.Second*10, time.Millisecond*500).Should(Succeed(), "Head NetworkPolicy should be created")

			Eventually(
				getResourceFunc(ctx, types.NamespacedName{Namespace: namespace, Name: rayCluster.Name + "-workers"}, &networkingv1.NetworkPolicy{}),
				time.Second*10, time.Millisecond*500).Should(Succeed(), "Worker NetworkPolicy should be created")
		})

		It("Delete RayCluster", func() {
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")

			Eventually(
				func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster)
					return err != nil && client.IgnoreNotFound(err) == nil
				},
				time.Second*10, time.Millisecond*500).Should(BeTrue(), "RayCluster should be deleted")
		})
	})
})

// portPtr converts an int32 port number into the *intstr.IntOrString required by NetworkPolicyPort.Port.
func portPtr(port int32) *intstr.IntOrString {
	v := intstr.FromInt32(port)
	return &v
}
