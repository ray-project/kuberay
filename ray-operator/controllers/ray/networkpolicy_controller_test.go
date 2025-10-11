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
			HeadGroupSpec: rayv1.HeadGroupSpec{
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

		It("Check Head NetworkPolicy is created", func() {
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			expectedHeadName := rayCluster.Name + "-head"
			headNamespacedName := types.NamespacedName{Namespace: namespace, Name: expectedHeadName}

			Eventually(
				getResourceFunc(ctx, headNamespacedName, headNetworkPolicy),
				time.Second*10, time.Millisecond*500).Should(Succeed(), "Head NetworkPolicy should be created: %v", expectedHeadName)
		})

		It("Check Worker NetworkPolicy is created", func() {
			workerNetworkPolicy := &networkingv1.NetworkPolicy{}
			expectedWorkerName := rayCluster.Name + "-workers"
			workerNamespacedName := types.NamespacedName{Namespace: namespace, Name: expectedWorkerName}

			Eventually(
				getResourceFunc(ctx, workerNamespacedName, workerNetworkPolicy),
				time.Second*10, time.Millisecond*500).Should(Succeed(), "Worker NetworkPolicy should be created: %v", expectedWorkerName)
		})

		It("Verify Head NetworkPolicy has correct structure", func() {
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			expectedHeadName := rayCluster.Name + "-head"
			headNamespacedName := types.NamespacedName{Namespace: namespace, Name: expectedHeadName}

			err := k8sClient.Get(ctx, headNamespacedName, headNetworkPolicy)
			Expect(err).NotTo(HaveOccurred(), "Failed to get Head NetworkPolicy")

			// Verify basic properties
			Expect(headNetworkPolicy.Name).To(Equal(expectedHeadName))
			Expect(headNetworkPolicy.Namespace).To(Equal(namespace))

			// Verify labels
			expectedLabels := map[string]string{
				utils.RayClusterLabelKey:                rayCluster.Name,
				utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
				utils.KubernetesCreatedByLabelKey:       utils.ComponentName,
			}
			Expect(headNetworkPolicy.Labels).To(Equal(expectedLabels))

			// Verify owner reference is set
			Expect(headNetworkPolicy.OwnerReferences).To(HaveLen(1))
			Expect(headNetworkPolicy.OwnerReferences[0].Name).To(Equal(rayCluster.Name))
			Expect(headNetworkPolicy.OwnerReferences[0].Kind).To(Equal("RayCluster"))

			// Verify policy type
			Expect(headNetworkPolicy.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress}))

			// Verify pod selector targets head pods only
			expectedPodSelector := metav1.LabelSelector{
				MatchLabels: map[string]string{
					utils.RayClusterLabelKey:  rayCluster.Name,
					utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
				},
			}
			Expect(headNetworkPolicy.Spec.PodSelector).To(Equal(expectedPodSelector))

			// Verify ingress rules - CodeFlare 5-rule pattern
			Expect(len(headNetworkPolicy.Spec.Ingress)).To(BeNumerically(">=", 5), "Should have at least 5 ingress rules")
			Expect(len(headNetworkPolicy.Spec.Ingress)).To(BeNumerically("<=", 6), "Should have at most 6 ingress rules (including optional RayJob)")

			// Verify Rule 1: Intra-cluster communication - NO PORTS (allows all)
			intraClusterRule := headNetworkPolicy.Spec.Ingress[0]
			Expect(intraClusterRule.From).To(HaveLen(1))
			Expect(intraClusterRule.Ports).To(BeEmpty(), "Intra-cluster rule should have NO ports (allows all)")

			expectedIntraClusterPeer := networkingv1.NetworkPolicyPeer{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						utils.RayClusterLabelKey: rayCluster.Name,
					},
				},
			}
			Expect(intraClusterRule.From[0]).To(Equal(expectedIntraClusterPeer))

			// Verify Rule 2: External access from any pod in namespace
			externalRule := headNetworkPolicy.Spec.Ingress[1]
			Expect(externalRule.From).To(HaveLen(1))
			Expect(externalRule.Ports).To(HaveLen(2), "External rule should have 2 ports (10001, 8265)")

			// Verify empty pod selector (any pod in namespace)
			expectedAnyPodPeer := networkingv1.NetworkPolicyPeer{
				PodSelector: &metav1.LabelSelector{},
			}
			Expect(externalRule.From[0]).To(Equal(expectedAnyPodPeer))

			// Verify Rule 5: Secured ports - NO FROM (allows all)
			securedRule := headNetworkPolicy.Spec.Ingress[4]
			Expect(securedRule.From).To(BeEmpty(), "Secured ports rule should have NO from (allows all)")
			Expect(securedRule.Ports).ToNot(BeEmpty(), "Secured ports rule should have at least 1 port (8443)")

			// Check for mTLS port 8443 (always present)
			portFound8443 := false
			for _, port := range securedRule.Ports {
				if port.Port.IntVal == 8443 {
					portFound8443 = true
				}
			}
			Expect(portFound8443).To(BeTrue(), "Should include mTLS port 8443")
		})

		It("Verify Worker NetworkPolicy has correct structure", func() {
			workerNetworkPolicy := &networkingv1.NetworkPolicy{}
			expectedWorkerName := rayCluster.Name + "-workers"
			workerNamespacedName := types.NamespacedName{Namespace: namespace, Name: expectedWorkerName}

			err := k8sClient.Get(ctx, workerNamespacedName, workerNetworkPolicy)
			Expect(err).NotTo(HaveOccurred(), "Failed to get Worker NetworkPolicy")

			// Verify basic properties
			Expect(workerNetworkPolicy.Name).To(Equal(expectedWorkerName))
			Expect(workerNetworkPolicy.Namespace).To(Equal(namespace))

			// Verify pod selector targets worker pods only
			expectedPodSelector := metav1.LabelSelector{
				MatchLabels: map[string]string{
					utils.RayClusterLabelKey:  rayCluster.Name,
					utils.RayNodeTypeLabelKey: string(rayv1.WorkerNode),
				},
			}
			Expect(workerNetworkPolicy.Spec.PodSelector).To(Equal(expectedPodSelector))

			// Verify ingress rules - workers only allow intra-cluster communication
			Expect(workerNetworkPolicy.Spec.Ingress).To(HaveLen(1))
			Expect(workerNetworkPolicy.Spec.Ingress[0].From).To(HaveLen(1))

			// Verify intra-cluster peer
			intraClusterPeer := workerNetworkPolicy.Spec.Ingress[0].From[0]
			expectedIntraClusterPeer := networkingv1.NetworkPolicyPeer{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						utils.RayClusterLabelKey: rayCluster.Name,
					},
				},
			}
			Expect(intraClusterPeer).To(Equal(expectedIntraClusterPeer))
		})

		It("Delete RayCluster should delete NetworkPolicies", func() {
			// Delete the RayCluster
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")

			// Note: envtest doesn't run garbage collection automatically like a real cluster
			// In a real cluster, the NetworkPolicies would be automatically deleted due to owner reference
			// For testing, we manually delete them to simulate garbage collection

			// Clean up head NetworkPolicy
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			expectedHeadName := rayCluster.Name + "-head"
			headNamespacedName := types.NamespacedName{Namespace: namespace, Name: expectedHeadName}

			err = k8sClient.Get(ctx, headNamespacedName, headNetworkPolicy)
			if err == nil {
				err = k8sClient.Delete(ctx, headNetworkPolicy)
				Expect(err).NotTo(HaveOccurred(), "Failed to manually delete Head NetworkPolicy")
			}

			// Clean up worker NetworkPolicy
			workerNetworkPolicy := &networkingv1.NetworkPolicy{}
			expectedWorkerName := rayCluster.Name + "-workers"
			workerNamespacedName := types.NamespacedName{Namespace: namespace, Name: expectedWorkerName}

			err = k8sClient.Get(ctx, workerNamespacedName, workerNetworkPolicy)
			if err == nil {
				err = k8sClient.Delete(ctx, workerNetworkPolicy)
				Expect(err).NotTo(HaveOccurred(), "Failed to manually delete Worker NetworkPolicy")
			}

			// Verify both NetworkPolicies are deleted
			Eventually(
				func() bool {
					headErr := k8sClient.Get(ctx, headNamespacedName, headNetworkPolicy)
					workerErr := k8sClient.Get(ctx, workerNamespacedName, workerNetworkPolicy)
					return (headErr != nil && client.IgnoreNotFound(headErr) == nil) &&
						(workerErr != nil && client.IgnoreNotFound(workerErr) == nil)
				},
				time.Second*5, time.Millisecond*500).Should(BeTrue(), "Both NetworkPolicies should be deleted")
		})
	})

	Describe("RayCluster owned by RayJob", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplateForNetworkPolicy("raycluster-rayjob", namespace)

		// Add RayJob owner reference
		rayCluster.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: "ray.io/v1",
				Kind:       "RayJob",
				Name:       "test-rayjob",
				UID:        "12345",
			},
		}

		It("Create a RayCluster with RayJob owner", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster with RayJob owner")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
		})

		It("Check Head NetworkPolicy includes RayJob peer", func() {
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			expectedHeadName := rayCluster.Name + "-head"
			headNamespacedName := types.NamespacedName{Namespace: namespace, Name: expectedHeadName}

			Eventually(
				getResourceFunc(ctx, headNamespacedName, headNetworkPolicy),
				time.Second*10, time.Millisecond*500).Should(Succeed(), "Head NetworkPolicy should be created")

			// Should have additional RayJob rule (last rule)
			Expect(len(headNetworkPolicy.Spec.Ingress)).To(BeNumerically(">=", 5), "Should have additional RayJob ingress rule")

			// Find the RayJob rule (should be the last rule)
			rayJobRule := headNetworkPolicy.Spec.Ingress[len(headNetworkPolicy.Spec.Ingress)-1]
			Expect(rayJobRule.From).To(HaveLen(1), "RayJob rule should have one peer")

			// Verify RayJob peer
			rayJobPeer := rayJobRule.From[0]
			expectedRayJobPeer := networkingv1.NetworkPolicyPeer{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"batch.kubernetes.io/job-name": "test-rayjob",
					},
				},
			}
			Expect(rayJobPeer).To(Equal(expectedRayJobPeer))
		})

		It("Clean up RayCluster with RayJob owner", func() {
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")
		})
	})

	Describe("NetworkPolicy Already Exists", Ordered, func() {
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

		It("Create Head NetworkPolicy before RayCluster", func() {
			err := k8sClient.Create(ctx, existingHeadNetworkPolicy)
			Expect(err).NotTo(HaveOccurred(), "Failed to create existing Head NetworkPolicy")
		})

		It("Create RayCluster should handle existing NetworkPolicy gracefully", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)

			// Head NetworkPolicy should be updated by the controller
			headNetworkPolicy := &networkingv1.NetworkPolicy{}
			headNamespacedName := types.NamespacedName{Namespace: namespace, Name: existingHeadNetworkPolicy.Name}
			err = k8sClient.Get(ctx, headNamespacedName, headNetworkPolicy)
			Expect(err).NotTo(HaveOccurred(), "Head NetworkPolicy should exist")

			// Worker NetworkPolicy should be created
			workerNetworkPolicy := &networkingv1.NetworkPolicy{}
			expectedWorkerName := rayCluster.Name + "-workers"
			workerNamespacedName := types.NamespacedName{Namespace: namespace, Name: expectedWorkerName}
			Eventually(
				getResourceFunc(ctx, workerNamespacedName, workerNetworkPolicy),
				time.Second*10, time.Millisecond*500).Should(Succeed(), "Worker NetworkPolicy should be created")
		})

		It("Clean up resources", func() {
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")

			err = k8sClient.Delete(ctx, existingHeadNetworkPolicy)
			// Policy might have been updated by controller, ignore delete errors
			_ = err

			// Clean up worker policy if it exists
			workerNetworkPolicy := &networkingv1.NetworkPolicy{}
			expectedWorkerName := rayCluster.Name + "-workers"
			workerNamespacedName := types.NamespacedName{Namespace: namespace, Name: expectedWorkerName}
			err = k8sClient.Get(ctx, workerNamespacedName, workerNetworkPolicy)
			if err == nil {
				err = k8sClient.Delete(ctx, workerNetworkPolicy)
				Expect(err).NotTo(HaveOccurred(), "Failed to delete worker NetworkPolicy")
			}
		})
	})

	Describe("RayCluster Deletion", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplateForNetworkPolicy("raycluster-deletion", namespace)

		It("Create and immediately delete RayCluster", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")

			// Add deletion timestamp by deleting
			err = k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to delete RayCluster")

			// Verify RayCluster is being deleted or deleted
			Eventually(
				func() bool {
					err := k8sClient.Get(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster)
					return err != nil && client.IgnoreNotFound(err) == nil
				},
				time.Second*10, time.Millisecond*500).Should(BeTrue(), "RayCluster should be deleted")
		})
	})
})
