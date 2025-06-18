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
	"errors"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	"github.com/ray-project/kuberay/ray-operator/test/support"
)

func rayClusterTemplate(name string, namespace string) *rayv1.RayCluster {
	const (
		minReplicas int32 = 0
		maxReplicas int32 = 4
		replicas    int32 = 3
	)
	return &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: rayv1.RayClusterSpec{
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
					Replicas:    ptr.To(replicas),
					MinReplicas: ptr.To(minReplicas),
					MaxReplicas: ptr.To(maxReplicas),
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

var _ = Context("Inside the default namespace", func() {
	Describe("Static RayCluster", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplate("raycluster-static", namespace)
		headPod := corev1.Pod{}
		headPods := corev1.PodList{}
		workerPods := corev1.PodList{}
		workerFilters := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()
		headFilters := common.RayClusterHeadPodsAssociationOptions(rayCluster).ToListOptions()

		It("Verify RayCluster spec", func() {
			// These test are designed based on the following assumptions:
			// (1) Ray Autoscaler is disabled.
			// (2) There is only one worker group, and its `replicas` is set to 3, and `maxReplicas` is set to 4, and `workersToDelete` is empty.
			Expect(rayCluster.Spec.EnableInTreeAutoscaling).To(BeNil())
			Expect(rayCluster.Spec.WorkerGroupSpecs).To(HaveLen(1))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Replicas).To(Equal(ptr.To[int32](3)))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].MaxReplicas).To(Equal(ptr.To[int32](4)))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete).To(BeEmpty())
		})

		It("Create a RayCluster custom resource", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
		})

		It("Check head service", func() {
			// TODO (kevin85421): Create a function to associate the RayCluster with the head service.
			svc := &corev1.Service{}
			headSvcName, err := utils.GenerateHeadServiceName(utils.RayClusterCRD, rayCluster.Spec, rayCluster.Name)
			Expect(err).NotTo(HaveOccurred())
			namespacedName := types.NamespacedName{Namespace: namespace, Name: headSvcName}

			Eventually(
				getResourceFunc(ctx, namespacedName, svc),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Head service: %v", svc)
		})

		It("Check the number of worker Pods", func() {
			numWorkerPods := 3
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("Create a head Pod resource with default sidecars", func() {
			err := k8sClient.List(ctx, &headPods, headFilters...)
			Expect(err).NotTo(HaveOccurred(), "Failed to list head Pods")
			Expect(headPods.Items).Should(HaveLen(1), "headPods: %v", headPods.Items)

			headPod = headPods.Items[0]
			Expect(headPod.Spec.Containers[len(headPod.Spec.Containers)-1].Name).Should(Equal("fluentbit"), "fluentbit sidecar exists")
			Expect(headPod.Spec.Containers).Should(HaveLen(2), "Because we disable autoscaling and inject a FluentBit sidecar, the head Pod should have 2 containers")
		})

		It("Update all Pods to Running", func() {
			// We need to manually update Pod statuses otherwise they'll always be Pending.
			// envtest doesn't create a full K8s cluster. It's only the control plane.
			// There's no container runtime or any other K8s controllers.
			// So Pods are created, but no controller updates them from Pending to Running.
			// See https://book.kubebuilder.io/reference/envtest.html

			// Note that this test assumes that headPods and workerPods are up-to-date.
			for _, headPod := range headPods.Items {
				headPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &headPod)).Should(Succeed())
			}

			Eventually(
				isAllPodsRunningByFilters).WithContext(ctx).WithArguments(headPods, headFilters).WithTimeout(time.Second*3).WithPolling(time.Millisecond*500).Should(BeTrue(), "Head Pod should be running.")

			for _, workerPod := range workerPods.Items {
				workerPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(Succeed())
			}

			Eventually(
				isAllPodsRunningByFilters).WithContext(ctx).WithArguments(workerPods, workerFilters).WithTimeout(time.Second*3).WithPolling(time.Millisecond*500).Should(BeTrue(), "All worker Pods should be running.")
		})

		It("RayCluster's .status.state should be updated to 'ready' shortly after all Pods are Running", func() {
			// Note that RayCluster is `ready` when all Pods are Running and their PodReady conditions are true.
			// However, in envtest, PodReady conditions are automatically set to true when Pod.Status.Phase is set to Running.
			// We need to figure out the behavior. See https://github.com/ray-project/kuberay/issues/1736 for more details.
			Eventually(
				getClusterState(ctx, namespace, rayCluster.Name),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
			// Check that the StateTransitionTimes are set.
			Eventually(
				func() *metav1.Time {
					status := getClusterStatus(ctx, namespace, rayCluster.Name)()
					return status.StateTransitionTimes[rayv1.Ready]
				},
				time.Second*3, time.Millisecond*500).Should(Not(BeNil()))
		})

		// The following tests focus on checking whether KubeRay creates the correct number of Pods.
		It("Delete a worker Pod, and KubeRay should create a new one", func() {
			numWorkerPods := 3
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))

			pod := workerPods.Items[0]
			err := k8sClient.Delete(ctx, &pod, &client.DeleteOptions{GracePeriodSeconds: ptr.To[int64](0)})
			Expect(err).NotTo(HaveOccurred(), "Failed to delete a Pod")
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("Increase replicas past maxReplicas", func() {
			// increasing replicas to 5, which is greater than maxReplicas (4)
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
					time.Second*3, time.Millisecond*500).Should(Succeed(), "rayCluster: %v", rayCluster)
				rayCluster.Spec.WorkerGroupSpecs[0].Replicas = ptr.To[int32](5)

				// Operator may update revision after we get cluster earlier. Update may result in 409 conflict error.
				// We need to handle conflict error and retry the update.
				return k8sClient.Update(ctx, rayCluster)
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster")

			// The `maxReplicas` is set to 4, so the number of worker Pods should be 4.
			numWorkerPods := 4
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
			Consistently(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*2, time.Millisecond*200).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})
	})

	Describe("RayCluster with overridden app.kubernetes.io labels", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplate("raycluster-overridden-k8s-labels", namespace)
		rayCluster.Spec.HeadGroupSpec.Template.Labels = map[string]string{
			utils.KubernetesApplicationNameLabelKey: "myapp",
		}
		headPods := corev1.PodList{}
		workerPods := corev1.PodList{}
		workerFilters := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()
		headFilters := common.RayClusterHeadPodsAssociationOptions(rayCluster).ToListOptions()

		It("Verify RayCluster spec", func() {
			// These test are designed based on the following assumptions:
			// (1) The app.kubernetes.io/name label of the HeadGroupSpec is overridden.
			// (2) There is only one worker group, and its `replicas` is set to 3.
			Expect(rayCluster.Spec.HeadGroupSpec.Template.Labels[utils.KubernetesApplicationNameLabelKey]).NotTo(BeEmpty())
			Expect(rayCluster.Spec.WorkerGroupSpecs).To(HaveLen(1))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Replicas).To(Equal(ptr.To[int32](3)))
		})

		It("Create a RayCluster custom resource", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
		})

		It("Check the number of head Pods", func() {
			numHeadPods := 1
			Eventually(
				listResourceFunc(ctx, &headPods, headFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numHeadPods), fmt.Sprintf("headGroup %v", headPods.Items))
			for _, head := range headPods.Items {
				Expect(head.Labels[utils.KubernetesApplicationNameLabelKey]).To(Equal("myapp"))
			}
		})

		It("Check the number of worker Pods", func() {
			numWorkerPods := 3
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("Update all Pods to Running", func() {
			// We need to manually update Pod statuses otherwise they'll always be Pending.
			// envtest doesn't create a full K8s cluster. It's only the control plane.
			// There's no container runtime or any other K8s controllers.
			// So Pods are created, but no controller updates them from Pending to Running.
			// See https://book.kubebuilder.io/reference/envtest.html

			// Note that this test assumes that headPods and workerPods are up-to-date.
			for _, headPod := range headPods.Items {
				headPod.Status.Phase = corev1.PodRunning
				headPod.Status.PodIP = "1.1.1.1" // This should be carried to rayCluster.Status.Head.ServiceIP. We check it later.
				Expect(k8sClient.Status().Update(ctx, &headPod)).Should(Succeed())
			}

			Eventually(
				isAllPodsRunningByFilters).WithContext(ctx).WithArguments(headPods, headFilters).WithTimeout(time.Second*3).WithPolling(time.Millisecond*500).Should(BeTrue(), "Head Pod should be running.")

			for _, workerPod := range workerPods.Items {
				workerPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(Succeed())
			}

			Eventually(
				isAllPodsRunningByFilters).WithContext(ctx).WithArguments(workerPods, workerFilters).WithTimeout(time.Second*3).WithPolling(time.Millisecond*500).Should(BeTrue(), "All worker Pods should be running.")
		})

		It("RayCluster's .status.state and .status.head.ServiceIP should be updated shortly after all Pods are Running", func() {
			// Note that RayCluster is `ready` when all Pods are Running and their PodReady conditions are true.
			// However, in envtest, PodReady conditions are automatically set to true when Pod.Status.Phase is set to Running.
			// We need to figure out the behavior. See https://github.com/ray-project/kuberay/issues/1736 for more details.
			Eventually(
				getClusterState(ctx, namespace, rayCluster.Name),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
			// Check that the StateTransitionTimes are set.
			Eventually(
				func() *metav1.Time {
					status := getClusterStatus(ctx, namespace, rayCluster.Name)()
					return status.StateTransitionTimes[rayv1.Ready]
				},
				time.Second*3, time.Millisecond*500).Should(Not(BeNil()))

			Eventually(func() (string, error) {
				err := getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster)()
				return rayCluster.Status.Head.ServiceIP, err
			}, time.Second*3, time.Millisecond*500).Should(Equal("1.1.1.1"), "Should be able to see the rayCluster.Status.Head.ServiceIP: %v", rayCluster.Status.Head.ServiceIP)
		})
	})

	Describe("RayCluster with invalid overridden ray.io/cluster labels", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplate("raycluster-overridden-cluster-label", namespace)
		rayCluster.Spec.HeadGroupSpec.Template.Labels = map[string]string{
			utils.RayClusterLabelKey: "invalid-cluster-name",
		}
		headPods := corev1.PodList{}
		workerPods := corev1.PodList{}
		workerFilters := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()
		headFilters := common.RayClusterHeadPodsAssociationOptions(rayCluster).ToListOptions()

		It("Verify RayCluster spec", func() {
			// These test are designed based on the following assumptions:
			// (1) The ray.io/cluster label of the HeadGroupSpec is overridden.
			// (2) There is only one worker group, and its `replicas` is set to 3.
			Expect(rayCluster.Spec.HeadGroupSpec.Template.Labels[utils.RayClusterLabelKey]).NotTo(BeEmpty())
			Expect(rayCluster.Spec.WorkerGroupSpecs).To(HaveLen(1))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Replicas).To(Equal(ptr.To[int32](3)))
		})

		It("Create a RayCluster custom resource", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
		})

		It("Check the number of head Pods", func() {
			numHeadPods := 1
			Eventually(
				listResourceFunc(ctx, &headPods, headFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numHeadPods), fmt.Sprintf("headGroup %v, headFilters: %v", headPods.Items, headFilters))
			for _, head := range headPods.Items {
				Expect(head.Labels[utils.RayClusterLabelKey]).To(Equal("raycluster-overridden-cluster-label"))
			}
		})

		It("Check the number of worker Pods", func() {
			numWorkerPods := 3
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("Update all Pods to Running", func() {
			// We need to manually update Pod statuses otherwise they'll always be Pending.
			// envtest doesn't create a full K8s cluster. It's only the control plane.
			// There's no container runtime or any other K8s controllers.
			// So Pods are created, but no controller updates them from Pending to Running.
			// See https://book.kubebuilder.io/reference/envtest.html

			// Note that this test assumes that headPods and workerPods are up-to-date.
			for _, headPod := range headPods.Items {
				headPod.Status.Phase = corev1.PodRunning
				headPod.Status.PodIP = "1.1.1.1" // This should be carried to rayCluster.Status.Head.ServiceIP. We check it later.
				Expect(k8sClient.Status().Update(ctx, &headPod)).Should(Succeed())
			}

			Eventually(
				isAllPodsRunningByFilters).WithContext(ctx).WithArguments(headPods, headFilters).WithTimeout(time.Second*3).WithPolling(time.Millisecond*500).Should(BeTrue(), "Head Pod should be running.")

			for _, workerPod := range workerPods.Items {
				workerPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(Succeed())
			}

			Eventually(
				isAllPodsRunningByFilters).WithContext(ctx).WithArguments(workerPods, workerFilters).WithTimeout(time.Second*3).WithPolling(time.Millisecond*500).Should(BeTrue(), "All worker Pods should be running.")
		})

		It("RayCluster's .status.state and .status.head.ServiceIP should be updated shortly after all Pods are Running", func() {
			// Note that RayCluster is `ready` when all Pods are Running and their PodReady conditions are true.
			// However, in envtest, PodReady conditions are automatically set to true when Pod.Status.Phase is set to Running.
			// We need to figure out the behavior. See https://github.com/ray-project/kuberay/issues/1736 for more details.
			Eventually(
				getClusterState(ctx, namespace, rayCluster.Name),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
			// Check that the StateTransitionTimes are set.
			Eventually(
				func() *metav1.Time {
					status := getClusterStatus(ctx, namespace, rayCluster.Name)()
					return status.StateTransitionTimes[rayv1.Ready]
				},
				time.Second*3, time.Millisecond*500).Should(Not(BeNil()))

			Eventually(func() (string, error) {
				err := getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster)()
				return rayCluster.Status.Head.ServiceIP, err
			}, time.Second*3, time.Millisecond*500).Should(Equal("1.1.1.1"), "Should be able to see the rayCluster.Status.Head.ServiceIP: %v", rayCluster.Status.Head.ServiceIP)
		})
	})

	Describe("RayCluster with autoscaling enabled", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplate("raycluster-autoscaler", namespace)
		rayCluster.Spec.EnableInTreeAutoscaling = ptr.To(true)
		workerPods := corev1.PodList{}
		workerFilter := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()

		It("Verify RayCluster spec", func() {
			// These test are designed based on the following assumptions:
			// (1) Ray Autoscaler is enabled.
			// (2) There is only one worker group, and its `replicas` is set to 3, and `maxReplicas` is set to 4, and `workersToDelete` is empty.
			Expect(*rayCluster.Spec.EnableInTreeAutoscaling).To(BeTrue())
			Expect(rayCluster.Spec.WorkerGroupSpecs).To(HaveLen(1))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Replicas).To(Equal(ptr.To[int32](3)))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].MaxReplicas).To(Equal(ptr.To[int32](4)))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete).To(BeEmpty())
		})

		It("Create a RayCluster custom resource", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
		})

		It("Check RoleBinding / Role / ServiceAccount", func() {
			rb := &rbacv1.RoleBinding{}
			namespacedName := common.RayClusterAutoscalerRoleBindingNamespacedName(rayCluster)
			Eventually(
				getResourceFunc(ctx, namespacedName, rb),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Autoscaler RoleBinding: %v", namespacedName)

			role := &rbacv1.Role{}
			namespacedName = common.RayClusterAutoscalerRoleNamespacedName(rayCluster)
			Eventually(
				getResourceFunc(ctx, namespacedName, role),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Autoscaler Role: %v", namespacedName)

			sa := &corev1.ServiceAccount{}
			namespacedName = common.RayClusterAutoscalerServiceAccountNamespacedName(rayCluster)
			Eventually(
				getResourceFunc(ctx, namespacedName, sa),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Autoscaler ServiceAccount: %v", namespacedName)
		})

		It("Check the number of worker Pods", func() {
			numWorkerPods := 3
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilter...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("Simulate Ray Autoscaler scales down", func() {
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
					time.Second*3, time.Millisecond*500).Should(Succeed())
				podToDelete := workerPods.Items[0]
				rayCluster.Spec.WorkerGroupSpecs[0].Replicas = ptr.To[int32](2)
				rayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{podToDelete.Name}
				return k8sClient.Update(ctx, rayCluster)
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster custom resource")

			numWorkerPods := 2
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilter...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))

			// Ray Autoscaler should clean up WorkersToDelete after scaling process has finished.
			// Call cleanUpWorkersToDelete to simulate the behavior of the Ray Autoscaler.
			cleanUpWorkersToDelete(ctx, rayCluster)
		})

		It("Simulate Ray Autoscaler scales up", func() {
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
					time.Second*3, time.Millisecond*500).Should(Succeed())
				rayCluster.Spec.WorkerGroupSpecs[0].Replicas = ptr.To[int32](4)
				return k8sClient.Update(ctx, rayCluster)
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster custom resource")

			numWorkerPods := 4
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilter...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})
	})

	updateRayClusterSuspendField := func(ctx context.Context, rayCluster *rayv1.RayCluster, suspend bool) error {
		return retry.RetryOnConflict(retry.DefaultRetry, func() error {
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: rayCluster.Namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "rayCluster = %v", rayCluster)
			rayCluster.Spec.Suspend = &suspend
			return k8sClient.Update(ctx, rayCluster)
		})
	}

	updateRayClusterWorkerGroupSuspendField := func(ctx context.Context, rayCluster *rayv1.RayCluster, suspend bool) error {
		return retry.RetryOnConflict(retry.DefaultRetry, func() error {
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: rayCluster.Namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "rayCluster = %v", rayCluster)
			rayCluster.Spec.WorkerGroupSpecs[0].Suspend = &suspend
			return k8sClient.Update(ctx, rayCluster)
		})
	}

	findRayClusterSuspendStatus := func(ctx context.Context, rayCluster *rayv1.RayCluster) (rayv1.RayClusterConditionType, error) {
		if err := getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: rayCluster.Namespace}, rayCluster)(); err != nil {
			return "", err
		}
		suspending := meta.IsStatusConditionTrue(rayCluster.Status.Conditions, string(rayv1.RayClusterSuspending))
		suspended := meta.IsStatusConditionTrue(rayCluster.Status.Conditions, string(rayv1.RayClusterSuspended))
		if suspending && suspended {
			return "invalid", errors.New("invalid: rayv1.RayClusterSuspending and rayv1.RayClusterSuspended should not be both true")
		} else if suspending {
			return rayv1.RayClusterSuspending, nil
		} else if suspended {
			return rayv1.RayClusterSuspended, nil
		}
		return "", nil
	}

	testSuspendRayCluster := func(withConditionDisabled bool) {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplate("raycluster-suspend", namespace)
		headPods := corev1.PodList{}
		workerPods := corev1.PodList{}
		allPods := corev1.PodList{}
		workerFilters := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()
		headFilters := common.RayClusterHeadPodsAssociationOptions(rayCluster).ToListOptions()
		allFilters := common.RayClusterAllPodsAssociationOptions(rayCluster).ToListOptions()

		if withConditionDisabled {
			features.SetFeatureGateDuringTest(GinkgoTB(), features.RayClusterStatusConditions, false)
		}

		By("Verify RayCluster spec", func() {
			// These test are designed based on the following assumptions:
			// (1) Ray Autoscaler is disabled.
			// (2) There is only one worker group, and its `replicas` is set to 3, and `maxReplicas` is set to 4, and `workersToDelete` is empty.
			Expect(rayCluster.Spec.EnableInTreeAutoscaling).To(BeNil())
			Expect(rayCluster.Spec.WorkerGroupSpecs).To(HaveLen(1))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Replicas).To(Equal(ptr.To[int32](3)))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].MaxReplicas).To(Equal(ptr.To[int32](4)))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete).To(BeEmpty())
		})

		By("Create a RayCluster custom resource", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
		})

		By("Check the number of worker Pods", func() {
			numWorkerPods := 3
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		By("Should delete all head and worker Pods if suspended", func() {
			// suspend a Raycluster and check that all Pods are deleted.
			err := updateRayClusterSuspendField(ctx, rayCluster, true)
			Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster")

			// Check that all Pods are deleted
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(0), fmt.Sprintf("workerGroup %v", workerPods.Items))
			Eventually(
				listResourceFunc(ctx, &headPods, headFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(0), fmt.Sprintf("head %v", headPods.Items))
			Eventually(
				listResourceFunc(ctx, &allPods, allFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(0), fmt.Sprintf("all pods %v", allPods.Items))
		})

		By("RayCluster's .status.state should be updated to 'suspended' shortly after all Pods are terminated", func() {
			Eventually(
				getClusterState(ctx, namespace, rayCluster.Name),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Suspended))
			if !withConditionDisabled {
				Eventually(findRayClusterSuspendStatus, time.Second*3, time.Millisecond*500).
					WithArguments(ctx, rayCluster).Should(Equal(rayv1.RayClusterSuspended))
				Expect(meta.IsStatusConditionTrue(rayCluster.Status.Conditions, string(rayv1.RayClusterProvisioned))).To(BeFalse())
				// rayCluster.Status.Head.PodName will be cleared.
				// rayCluster.Status.Head.PodIP will also be cleared, but we don't test it here since we don't have IPs in tests.
				Expect(rayCluster.Status.Head.PodName).To(BeEmpty())
			}
		})

		By("Set suspend to false and then revert it to true before all Pods are running", func() {
			// set suspend to false
			err := updateRayClusterSuspendField(ctx, rayCluster, false)
			Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster")

			// check that all Pods are created
			Eventually(
				listResourceFunc(ctx, &headPods, headFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(1), fmt.Sprintf("head %v", headPods.Items))
			numWorkerPods := 3
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))

			// only update worker Pod statuses so that the head Pod status is still Pending.
			for _, workerPod := range workerPods.Items {
				workerPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(Succeed())
			}

			// change suspend to true before all Pods are Running.
			err = updateRayClusterSuspendField(ctx, rayCluster, true)
			Expect(err).NotTo(HaveOccurred(), "Failed to update test RayCluster resource")

			// check that all Pods are deleted
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(0), fmt.Sprintf("workerGroup %v", workerPods.Items))
			Eventually(
				listResourceFunc(ctx, &headPods, headFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(0), fmt.Sprintf("head %v", headPods.Items))
			Eventually(
				listResourceFunc(ctx, &allPods, allFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(0), fmt.Sprintf("all pods %v", headPods.Items))

			// RayCluster should be in Suspended state.
			Eventually(
				getClusterState(ctx, namespace, rayCluster.Name),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Suspended))

			if !withConditionDisabled {
				Eventually(findRayClusterSuspendStatus, time.Second*3, time.Millisecond*500).
					WithArguments(ctx, rayCluster).Should(Equal(rayv1.RayClusterSuspended))
			}
		})

		By("Should run all head and worker pods if un-suspended", func() {
			// Resume the suspended RayCluster
			err := updateRayClusterSuspendField(ctx, rayCluster, false)
			Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster")

			// check that all pods are created
			Eventually(
				listResourceFunc(ctx, &headPods, headFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(1), fmt.Sprintf("head %v", headPods.Items))
			numWorkerPods := 3
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))

			// We need to also manually update Pod statuses back to "Running" or else they will always stay as Pending.
			// This is because we don't run kubelets in the unit tests to update the status subresource.
			for _, headPod := range headPods.Items {
				headPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &headPod)).Should(Succeed())
			}
			for _, workerPod := range workerPods.Items {
				workerPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(Succeed())
			}
		})

		By("RayCluster's .status.state should be updated back to 'ready' after being un-suspended", func() {
			Eventually(
				getClusterState(ctx, namespace, rayCluster.Name),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
			if !withConditionDisabled {
				Eventually(findRayClusterSuspendStatus, time.Second*3, time.Millisecond*500).
					WithArguments(ctx, rayCluster).Should(BeEmpty())
				Expect(meta.IsStatusConditionTrue(rayCluster.Status.Conditions, string(rayv1.RayClusterProvisioned))).To(BeTrue())
				// rayCluster.Status.Head.PodName should have a value now.
				// rayCluster.Status.Head.PodIP should also have a value now, but we don't test it here since we don't have IPs in tests.
				Expect(rayCluster.Status.Head.PodName).NotTo(BeEmpty())
			}
		})

		By("Delete the cluster", func() {
			err := k8sClient.Delete(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred())
		})
	}

	Describe("Suspend RayCluster", Ordered, func() {
		It("without Condition", func() {
			testSuspendRayCluster(true)
		})

		It("with Condition", func() {
			testSuspendRayCluster(false)
		})

		It("atomically with Condition", func() {
			ctx := context.Background()
			namespace := "default"
			rayCluster := rayClusterTemplate("raycluster-suspend-atomically", namespace)
			allPods := corev1.PodList{}
			allFilters := common.RayClusterAllPodsAssociationOptions(rayCluster).ToListOptions()
			numPods := 4 // 1 Head + 3 Workers
			Expect(features.Enabled(features.RayClusterStatusConditions)).To(BeTrue())

			By("Create a RayCluster custom resource", func() {
				err := k8sClient.Create(ctx, rayCluster)
				Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
				Eventually(getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
					time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
			})

			By("Check the number of Pods and add finalizers", func() {
				Eventually(listResourceFunc(ctx, &allPods, allFilters...), time.Second*3, time.Millisecond*500).
					Should(Equal(numPods), fmt.Sprintf("all pods %v", allPods.Items))
				// Add finalizers to worker Pods to prevent it from being deleted so that we can test if the status condition makes the suspending process atomic.
				for _, pod := range allPods.Items {
					err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
						pod.Finalizers = append(pod.Finalizers, "ray.io/deletion-blocker")
						return k8sClient.Update(ctx, &pod)
					})
					Expect(err).NotTo(HaveOccurred(), "Failed to update Pods")
				}
			})

			By("Should turn on the RayClusterSuspending if we set `.Spec.Suspend` back to true", func() {
				// suspend a Raycluster.
				err := updateRayClusterSuspendField(ctx, rayCluster, true)
				Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster")

				Eventually(findRayClusterSuspendStatus, time.Second*3, time.Millisecond*500).
					WithArguments(ctx, rayCluster).Should(Equal(rayv1.RayClusterSuspending))
			})

			By("Should keep RayClusterSuspending consistently if we set `.Spec.Suspend` back to false", func() {
				err := updateRayClusterSuspendField(ctx, rayCluster, false)
				Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster")

				Consistently(findRayClusterSuspendStatus, time.Second*3, time.Millisecond*500).
					WithArguments(ctx, rayCluster).Should(Equal(rayv1.RayClusterSuspending))
			})

			By("Pods should be deleted and new Pods should created back after we remove those finalizers", func() {
				Eventually(listResourceFunc(ctx, &allPods, allFilters...), time.Second*3, time.Millisecond*500).
					Should(Equal(numPods), fmt.Sprintf("all pods %v", allPods.Items))

				var oldNames []string
				for _, pod := range allPods.Items {
					oldNames = append(oldNames, pod.Name)
					err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
						pod.Finalizers = nil
						return k8sClient.Update(ctx, &pod)
					})
					Expect(err).NotTo(HaveOccurred(), "Failed to update Pods")
				}
				// RayClusterSuspending and RayClusterSuspended should be both false.
				Eventually(findRayClusterSuspendStatus, time.Second*3, time.Millisecond*500).
					WithArguments(ctx, rayCluster).Should(BeEmpty())
				Consistently(findRayClusterSuspendStatus, time.Second*3, time.Millisecond*500).
					WithArguments(ctx, rayCluster).Should(BeEmpty())

				// New Pods should be created.
				Eventually(listResourceFunc(ctx, &allPods, allFilters...), time.Second*3, time.Millisecond*500).
					Should(Equal(numPods), fmt.Sprintf("all pods %v", allPods.Items))

				var newNames []string
				for _, pod := range allPods.Items {
					newNames = append(newNames, pod.Name)
				}
				Expect(newNames).NotTo(ConsistOf(oldNames))
			})

			By("Set suspend to true and all Pods should be deleted again", func() {
				err := updateRayClusterSuspendField(ctx, rayCluster, true)
				Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster")

				Eventually(listResourceFunc(ctx, &allPods, allFilters...), time.Second*3, time.Millisecond*500).
					Should(Equal(0), fmt.Sprintf("all pods %v", allPods.Items))

				Eventually(findRayClusterSuspendStatus, time.Second*3, time.Millisecond*500).
					WithArguments(ctx, rayCluster).Should(Equal(rayv1.RayClusterSuspended))
				Consistently(findRayClusterSuspendStatus, time.Second*3, time.Millisecond*500).
					WithArguments(ctx, rayCluster).Should(Equal(rayv1.RayClusterSuspended))
			})

			By("Delete the cluster", func() {
				err := k8sClient.Delete(ctx, rayCluster)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		It("worker group", func() {
			ctx := context.Background()
			namespace := "default"
			rayCluster := rayClusterTemplate("raycluster-suspend-workergroup", namespace)
			allPods := corev1.PodList{}
			allFilters := common.RayClusterAllPodsAssociationOptions(rayCluster).ToListOptions()
			workerFilters := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()
			headFilters := common.RayClusterHeadPodsAssociationOptions(rayCluster).ToListOptions()

			features.SetFeatureGateDuringTest(GinkgoTB(), features.RayJobDeletionPolicy, true)

			By("Create a RayCluster custom resource", func() {
				err := k8sClient.Create(ctx, rayCluster)
				Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
				Eventually(getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
					time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
			})

			By("Check the number of Pods and add finalizers", func() {
				Eventually(listResourceFunc(ctx, &allPods, allFilters...), time.Second*3, time.Millisecond*500).
					Should(Equal(4), fmt.Sprintf("all pods %v", allPods.Items))
			})

			By("Setting suspend=true in first worker group should not fail", func() {
				// suspend the Raycluster worker group
				err := updateRayClusterWorkerGroupSuspendField(ctx, rayCluster, true)
				Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster")
			})

			By("Worker pods should be deleted but head pod should still be running", func() {
				Eventually(listResourceFunc(ctx, &allPods, workerFilters...), time.Second*5, time.Millisecond*500).
					Should(Equal(0), fmt.Sprintf("all pods %v", allPods.Items))
				Eventually(listResourceFunc(ctx, &allPods, headFilters...), time.Second*5, time.Millisecond*500).
					Should(Equal(1), fmt.Sprintf("all pods %v", allPods.Items))
				Eventually(listResourceFunc(ctx, &allPods, allFilters...), time.Second*5, time.Millisecond*500).
					Should(Equal(1), fmt.Sprintf("all pods %v", allPods.Items))
			})

			By("Delete the cluster", func() {
				err := k8sClient.Delete(ctx, rayCluster)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		It("worker group with Autoscaler enabled", func() {
			ctx := context.Background()
			namespace := "default"
			rayCluster := rayClusterTemplate("raycluster-suspend-workergroup-autoscaler", namespace)
			rayCluster.Spec.EnableInTreeAutoscaling = ptr.To(true)
			allPods := corev1.PodList{}
			allFilters := common.RayClusterAllPodsAssociationOptions(rayCluster).ToListOptions()
			workerFilters := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()
			headFilters := common.RayClusterHeadPodsAssociationOptions(rayCluster).ToListOptions()

			features.SetFeatureGateDuringTest(GinkgoTB(), features.RayJobDeletionPolicy, true)

			By("Create a RayCluster custom resource", func() {
				err := k8sClient.Create(ctx, rayCluster)
				Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
				Eventually(getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
					time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
			})

			By("Check the number of Pods", func() {
				Eventually(listResourceFunc(ctx, &allPods, allFilters...), time.Second*3, time.Millisecond*500).
					Should(Equal(4), fmt.Sprintf("all pods %v", allPods.Items))
			})

			By("Setting suspend=true in first worker group should not fail", func() {
				// suspend the Raycluster worker group
				err := updateRayClusterWorkerGroupSuspendField(ctx, rayCluster, true)
				Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster")
			})

			By("Worker pods should not be deleted and head pod should still be running", func() {
				Consistently(listResourceFunc(ctx, &allPods, workerFilters...), time.Second*5, time.Millisecond*500).
					Should(Equal(3), fmt.Sprintf("all pods %v", allPods.Items))
				Consistently(listResourceFunc(ctx, &allPods, headFilters...), time.Second*5, time.Millisecond*500).
					Should(Equal(1), fmt.Sprintf("all pods %v", allPods.Items))
			})

			By("Delete the cluster", func() {
				err := k8sClient.Delete(ctx, rayCluster)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("RayCluster with a multi-host worker group", Ordered, func() {
		const (
			minReplicas int32 = 0
			maxReplicas int32 = 4
			replicas    int32 = 3
		)
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplate("raycluster-multihost", namespace)
		numOfHosts := int32(4)
		rayCluster.Spec.WorkerGroupSpecs[0].NumOfHosts = numOfHosts
		rayCluster.Spec.EnableInTreeAutoscaling = ptr.To(true)
		headPods := corev1.PodList{}
		headFilters := common.RayClusterHeadPodsAssociationOptions(rayCluster).ToListOptions()
		numHeadPods := 1

		workerPods := corev1.PodList{}
		numWorkerPods := 3 * int(numOfHosts)
		workerFilters := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()

		It("Verify RayCluster spec", func() {
			// These test are designed based on the following assumptions:
			// (1) Ray Autoscaler is enabled.
			// (2) There is only one worker group, and its `replicas` is set to 3, and `workersToDelete` is empty.
			// (3) The worker group is a multi-host TPU PodSlice consisting of 4 hosts.
			Expect(*rayCluster.Spec.EnableInTreeAutoscaling).To(BeTrue())
			Expect(rayCluster.Spec.WorkerGroupSpecs).To(HaveLen(1))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].NumOfHosts).To(Equal(numOfHosts))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Replicas).To(Equal(ptr.To[int32](3)))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete).To(BeEmpty())
		})

		It("Create a RayCluster custom resource", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
		})

		It("Update all Pods to Running", func() {
			// Note that this test assumes that headPods and workerPods are up-to-date.
			Eventually(listResourceFunc(ctx, &headPods, headFilters...), time.Second*3, time.Millisecond*500).Should(Equal(numHeadPods), "headPods: %v", headPods.Items)
			for _, headPod := range headPods.Items {
				headPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &headPod)).Should(Succeed())
			}
			Eventually(
				isAllPodsRunningByFilters).WithContext(ctx).WithArguments(headPods, headFilters).WithTimeout(time.Second*3).WithPolling(time.Millisecond*500).Should(BeTrue(), "Head Pod should be running.")

			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))

			for _, workerPod := range workerPods.Items {
				workerPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(Succeed())
			}

			Eventually(
				isAllPodsRunningByFilters).WithContext(ctx).WithArguments(workerPods, workerFilters).WithTimeout(time.Second*3).WithPolling(time.Millisecond*500).Should(BeTrue(), "All worker Pods should be running.")
		})

		It("RayCluster's .status.state transitions to 'ready' when all worker Pods are Running and check pod counts are correct", func() {
			desiredWorkerPods := replicas * numOfHosts
			minWorkerPods := minReplicas * numOfHosts
			maxWorkerPods := maxReplicas * numOfHosts

			Eventually(
				getClusterState(ctx, namespace, rayCluster.Name),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))

			Eventually(func() bool {
				// "Replica" in status fields (e.g., DesiredWorkerReplicas) refers to the number of pods.
				// The name is preserved for compatibility, but it semantically means pod count.
				status := getClusterStatus(ctx, namespace, rayCluster.Name)()
				if status.DesiredWorkerReplicas != desiredWorkerPods ||
					status.MinWorkerReplicas != minWorkerPods ||
					status.MaxWorkerReplicas != maxWorkerPods {
					return false
				}
				return true
			}, time.Second*5, time.Millisecond*500).Should(BeTrue())
		})

		It("Simulate Ray Autoscaler scales down", func() {
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
					time.Second*3, time.Millisecond*500).Should(Succeed())
				rayCluster.Spec.WorkerGroupSpecs[0].Replicas = ptr.To[int32](2)
				rayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{
					workerPods.Items[0].Name, workerPods.Items[1].Name, workerPods.Items[2].Name, workerPods.Items[3].Name,
				}
				return k8sClient.Update(ctx, rayCluster)
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster custom resource")

			numWorkerPods := 2 * int(numOfHosts)
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))

			// Ray Autoscaler should clean up WorkersToDelete after scaling process has finished.
			// Call cleanUpWorkersToDelete to simulate the behavior of the Ray Autoscaler.
			cleanUpWorkersToDelete(ctx, rayCluster)
		})

		It("Simulate Ray Autoscaler scales up", func() {
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
					time.Second*3, time.Millisecond*500).Should(Succeed())
				rayCluster.Spec.WorkerGroupSpecs[0].Replicas = ptr.To[int32](4)
				return k8sClient.Update(ctx, rayCluster)
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to update RayCluster custom resource")

			numWorkerPods := 4 * int(numOfHosts)
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("Delete a worker Pod, and KubeRay should create a new one", func() {
			numWorkerPods := 4 * int(numOfHosts)
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))

			pod := workerPods.Items[0]
			err := k8sClient.Delete(ctx, &pod, &client.DeleteOptions{GracePeriodSeconds: ptr.To[int64](0)})
			Expect(err).NotTo(HaveOccurred(), "Failed to delete a Pod")
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
			Consistently(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*2, time.Millisecond*200).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})
	})

	Describe("RayCluster with PodTemplate referencing a different namespace", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplate("raycluster-podtemplate-namespace", namespace)
		headPods := corev1.PodList{}
		workerPods := corev1.PodList{}
		workerFilters := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()
		headFilters := common.RayClusterHeadPodsAssociationOptions(rayCluster).ToListOptions()

		It("Create a RayCluster with PodTemplate referencing a different namespace.", func() {
			rayCluster.Spec.HeadGroupSpec.Template.ObjectMeta.Namespace = "not-default"
			rayCluster.Spec.WorkerGroupSpecs[0].Template.ObjectMeta.Namespace = "not-default"
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
		})

		It("Check workers are in the same namespace as RayCluster", func() {
			numWorkerPods := 3
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("Create a head Pod is in the same namespace as RayCluster", func() {
			// In suite_test.go, we set `RayClusterReconcilerOptions.HeadSidecarContainers` to include a FluentBit sidecar.
			err := k8sClient.List(ctx, &headPods, headFilters...)
			Expect(err).NotTo(HaveOccurred(), "Failed to list head Pods")
			Expect(headPods.Items).Should(HaveLen(1), "headPods: %v", headPods.Items)
		})
	})

	Describe("RayCluster without resource request", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplate("no-resource-req", namespace)
		rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		}
		rayCluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		}
		headPods := corev1.PodList{}
		workerPods := corev1.PodList{}
		workerFilters := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()
		headFilters := common.RayClusterHeadPodsAssociationOptions(rayCluster).ToListOptions()

		It("Verify RayCluster spec", func() {
			// These test are designed based on the following assumptions:
			// (1) Both head and worker Pods do not have resource requests, but they have resource limits.
			// (2) There is only one worker group, and its `replicas` is set to 3.
			Expect(rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests).To(BeNil())
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests).To(BeNil())
			Expect(rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Limits).NotTo(BeNil())
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Limits).NotTo(BeNil())
			Expect(rayCluster.Spec.WorkerGroupSpecs).To(HaveLen(1))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Replicas).To(Equal(ptr.To[int32](3)))
		})

		It("Create a RayCluster custom resource", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
		})

		It("Check the number of worker Pods", func() {
			numWorkerPods := 3
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("Create a head Pod", func() {
			err := k8sClient.List(ctx, &headPods, headFilters...)
			Expect(err).NotTo(HaveOccurred(), "Failed to list head Pods")
			Expect(headPods.Items).Should(HaveLen(1), "headPods: %v", headPods.Items)
		})

		It("Update all Pods to Running", func() {
			for _, headPod := range headPods.Items {
				headPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &headPod)).Should(Succeed())
			}

			Eventually(
				isAllPodsRunningByFilters).WithContext(ctx).WithArguments(headPods, headFilters).WithTimeout(time.Second*3).WithPolling(time.Millisecond*500).Should(BeTrue(), "Head Pod should be running.")

			for _, workerPod := range workerPods.Items {
				workerPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(Succeed())
			}

			Eventually(
				isAllPodsRunningByFilters).WithContext(ctx).WithArguments(workerPods, workerFilters).WithTimeout(time.Second*3).WithPolling(time.Millisecond*500).Should(BeTrue(), "All worker Pods should be running.")
		})

		It("RayCluster's .status.state should be updated to 'ready' shortly after all Pods are Running", func() {
			Eventually(
				getClusterState(ctx, namespace, rayCluster.Name),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
		})

		It("Check DesiredMemory and DesiredCPU", func() {
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
			desiredMemory := resource.MustParse("4Gi")
			desiredCPU := resource.MustParse("4")
			Expect(rayCluster.Status.DesiredMemory).To(Equal(desiredMemory))
			Expect(rayCluster.Status.DesiredCPU).To(Equal(desiredCPU))
		})
	})

	Describe("RayCluster with invalid NumOfHosts", Ordered, func() {
		// Some users only upgrade the KubeRay image without upgrading the CRD. For example, when a
		// user upgrades the KubeRay operator from v1.0.0 to v1.1.0 without upgrading the CRD, the
		// KubeRay operator will use the zero value of `NumOfHosts` in the CRD. Hence, all worker
		// Pods will be deleted. This test case is designed to prevent Pods from being deleted.
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplate("raycluster-invalid-numofhosts", namespace)
		numOfHosts := int32(0)
		rayCluster.Spec.WorkerGroupSpecs[0].NumOfHosts = numOfHosts
		workerPods := corev1.PodList{}
		workerFilters := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()

		It("Verify RayCluster spec", func() {
			// These test are designed based on the following assumptions:
			// (1) There is only one worker group, and its `replicas` is set to 3, and `workersToDelete` is empty.
			// (2) The worker group has an invalid `numOfHosts` value of 0.
			Expect(rayCluster.Spec.WorkerGroupSpecs).To(HaveLen(1))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].NumOfHosts).To(Equal(numOfHosts))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Replicas).To(Equal(ptr.To[int32](3)))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete).To(BeEmpty())
		})

		It("Create a RayCluster custom resource", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
		})

		It("Check the number of worker Pods", func() {
			numWorkerPods := 3 * int(numOfHosts)
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})
	})

	Describe("RayCluster with RayClusterStatusConditions feature gate enabled", func() {
		BeforeEach(func() {
			Expect(features.Enabled(features.RayClusterStatusConditions)).To(BeTrue())
		})

		It("Should handle HeadPodReady and RayClusterProvisioned conditions correctly", func(ctx SpecContext) {
			namespace := "default"
			rayCluster := rayClusterTemplate("raycluster-status-conditions-enabled", namespace)
			rayCluster.Spec.WorkerGroupSpecs[0].Replicas = ptr.To[int32](1)
			rayCluster.Spec.WorkerGroupSpecs[0].MaxReplicas = ptr.To[int32](1)
			var headPod corev1.Pod
			var workerPod corev1.Pod
			headPods := corev1.PodList{}
			workerPods := corev1.PodList{}
			workerFilters := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()
			headFilters := common.RayClusterHeadPodsAssociationOptions(rayCluster).ToListOptions()

			By("Verify RayCluster spec")
			Expect(rayCluster.Spec.EnableInTreeAutoscaling).To(BeNil())
			Expect(rayCluster.Spec.WorkerGroupSpecs).To(HaveLen(1))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Replicas).To(Equal(ptr.To[int32](1)))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].MaxReplicas).To(Equal(ptr.To[int32](1)))
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete).To(BeEmpty())

			By("Create a RayCluster custom resource")
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)

			By("Check the number of worker Pods")
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(1), fmt.Sprintf("workerGroup %v", workerPods.Items))
			workerPod = workerPods.Items[0]

			By("Get the head pod")
			err = k8sClient.List(ctx, &headPods, headFilters...)
			Expect(err).NotTo(HaveOccurred(), "Failed to list head Pods")
			Expect(headPods.Items).Should(HaveLen(1), "headPods: %v", headPods.Items)
			headPod = headPods.Items[0]

			By("Check RayCluster conditions empty initially")
			// Initially, neither head Pod nor worker Pod are ready. The RayClusterProvisioned condition should not be present.
			Expect(testRayCluster.Status.Conditions).To(BeEmpty())

			By("Update the head pod to Running and Ready")
			headPod.Status.Phase = corev1.PodRunning
			headPod.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, &headPod)).Should(Succeed())

			By("Check RayCluster HeadPodReady condition is true")
			// The head pod is ready, so HeadPodReady condition should be added and set to True.
			Eventually(
				func() bool {
					if err := getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster)(); err != nil {
						return false
					}
					return meta.IsStatusConditionPresentAndEqual(rayCluster.Status.Conditions, string(rayv1.HeadPodReady), metav1.ConditionTrue)
				},
				time.Second*3, time.Millisecond*500).Should(BeTrue())

			By("Check RayCluster RayClusterProvisioned condition is false")
			// But the worker pod is not ready yet, RayClusterProvisioned condition should be false.
			Consistently(
				func() bool {
					if err := getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster)(); err != nil {
						return false
					}
					return meta.IsStatusConditionFalse(rayCluster.Status.Conditions, string(rayv1.RayClusterProvisioned))
				},
				time.Second*3, time.Millisecond*500).Should(BeTrue())

			By("Update the worker pod to Running")
			workerPod.Status.Phase = corev1.PodRunning
			workerPod.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(Succeed())

			By("Check RayCluster RayClusterProvisioned condition is true")
			// All Ray Pods are ready for the first time, RayClusterProvisioned condition should be added and set to True.
			Eventually(
				func() bool {
					if err := getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster)(); err != nil {
						return false
					}
					return meta.IsStatusConditionPresentAndEqual(rayCluster.Status.Conditions, string(rayv1.RayClusterProvisioned), metav1.ConditionTrue)
				},
				time.Second*3, time.Millisecond*500).Should(BeTrue())

			By("Update the worker pod to NotReady")
			workerPod.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			}
			Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(Succeed())

			By("Check RayCluster RayClusterProvisioned condition is true")
			// The worker pod fails readiness, but since RayClusterProvisioned focuses solely on whether all Ray Pods are ready for the first time,
			// RayClusterProvisioned condition should still be True.
			Consistently(
				func() bool {
					if err := getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster)(); err != nil {
						return false
					}
					return meta.IsStatusConditionPresentAndEqual(rayCluster.Status.Conditions, string(rayv1.RayClusterProvisioned), metav1.ConditionTrue)
				},
				time.Second*3, time.Millisecond*500).Should(BeTrue())

			By("Update the head pod to NotReady")
			headPod.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			}
			Expect(k8sClient.Status().Update(ctx, &headPod)).Should(Succeed())

			By("Check RayCluster HeadPodReady condition is false")
			// The head pod is not ready, so HeadPodReady should be false.
			Eventually(
				func() bool {
					if err := getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster)(); err != nil {
						return false
					}
					return meta.IsStatusConditionPresentAndEqual(rayCluster.Status.Conditions, string(rayv1.HeadPodReady), metav1.ConditionFalse)
				},
				time.Second*3, time.Millisecond*500).Should(BeTrue())

			By("Check RayCluster RayClusterProvisioned condition is still true")
			// The head pod also fails readiness, RayClusterProvisioned condition not changed.
			Eventually(
				func() bool {
					if err := getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster)(); err != nil {
						return false
					}
					return meta.IsStatusConditionPresentAndEqual(rayCluster.Status.Conditions, string(rayv1.RayClusterProvisioned), metav1.ConditionTrue)
				},
				time.Second*3, time.Millisecond*500).Should(BeTrue())
		})

		It("Should handle RayClusterReplicaFailure condition correctly", func(ctx SpecContext) {
			namespace := "default"
			rayCluster := rayClusterTemplate("raycluster-status-conditions-enabled-invalid", namespace)
			rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].ImagePullPolicy = "!invalid!"

			By("Create an invalid RayCluster custom resource")
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)

			By("Check RayCluster RayClusterReplicaFailure condition is true")
			Eventually(
				func() bool {
					if err := getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster)(); err != nil {
						return false
					}
					return meta.IsStatusConditionPresentAndEqual(rayCluster.Status.Conditions, string(rayv1.RayClusterReplicaFailure), metav1.ConditionTrue)
				},
				time.Second*3, time.Millisecond*500).Should(BeTrue())
		})
	})

	Describe("RayCluster with a maximum name", func() {
		ctx := context.Background()
		namespace := "default"
		rayCluster := rayClusterTemplate(strings.Repeat("r", utils.MaxRayClusterNameLength), namespace)
		numOfHosts := int32(4)
		rayCluster.Spec.WorkerGroupSpecs[0].NumOfHosts = numOfHosts // This creates the -headless service.
		rayCluster.Annotations = make(map[string]string)
		rayCluster.Annotations[utils.EnableServeServiceKey] = "true" // This creates the -serve-svc service, which is the longest svc suffix.
		workerPods := corev1.PodList{}
		workerFilters := common.RayClusterGroupPodsAssociationOptions(rayCluster, rayCluster.Spec.WorkerGroupSpecs[0].GroupName).ToListOptions()

		It("Create a RayCluster custom resource", func() {
			err := k8sClient.Create(ctx, rayCluster)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Should be able to see RayCluster: %v", rayCluster.Name)
		})

		It("Check the number of worker Pods", func() {
			numWorkerPods := 3 * int(numOfHosts)
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilters...),
				time.Second*3, time.Millisecond*500).Should(Equal(numWorkerPods), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("Check RayCluster service names", func() {
			var svc corev1.Service
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name + "-head-svc", Namespace: namespace}, &svc),
				time.Second*3, time.Millisecond*500).Should(Succeed())
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name + "-serve-svc", Namespace: namespace}, &svc),
				time.Second*3, time.Millisecond*500).Should(Succeed())
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayCluster.Name + "-headless", Namespace: namespace}, &svc),
				time.Second*3, time.Millisecond*500).Should(Succeed())
		})
	})
})
