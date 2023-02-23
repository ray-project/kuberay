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
	"fmt"
	"log"
	"reflect"
	"time"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/utils/pointer"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

var _ = Context("Inside the default namespace", func() {
	ctx := context.TODO()
	var workerPods corev1.PodList
	var headPods corev1.PodList
	enableInTreeAutoscaling := true

	myRayCluster := &rayiov1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-sample",
			Namespace: "default",
		},
		Spec: rayiov1alpha1.RayClusterSpec{
			RayVersion:              "1.0",
			EnableInTreeAutoscaling: &enableInTreeAutoscaling,
			HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
				ServiceType: "ClusterIP",
				RayStartParams: map[string]string{
					"port":                "6379",
					"object-manager-port": "12345",
					"node-manager-port":   "12346",
					"num-cpus":            "1",
				},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: "head-service-account",
						Containers: []corev1.Container{
							{
								Name:    "ray-head",
								Image:   "rayproject/ray:2.3.0",
								Command: []string{"python"},
								Args:    []string{"/opt/code.py"},
								Env: []corev1.EnvVar{
									{
										Name: "MY_POD_IP",
										ValueFrom: &corev1.EnvVarSource{
											FieldRef: &corev1.ObjectFieldSelector{
												FieldPath: "status.podIP",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			WorkerGroupSpecs: []rayiov1alpha1.WorkerGroupSpec{
				{
					Replicas:    pointer.Int32(3),
					MinReplicas: pointer.Int32(0),
					MaxReplicas: pointer.Int32(4),
					GroupName:   "small-group",
					RayStartParams: map[string]string{
						"port":     "6379",
						"num-cpus": "1",
					},
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:    "ray-worker",
									Image:   "rayproject/ray:2.3.0",
									Command: []string{"echo"},
									Args:    []string{"Hello Ray"},
									Env: []corev1.EnvVar{
										{
											Name: "MY_POD_IP",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													FieldPath: "status.podIP",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	headFilterLabels := client.MatchingLabels{common.RayClusterLabelKey: myRayCluster.Name, common.RayNodeGroupLabelKey: "headgroup"}
	workerFilterLabels := client.MatchingLabels{common.RayClusterLabelKey: myRayCluster.Name, common.RayNodeGroupLabelKey: "small-group"}

	Describe("When creating a raycluster", func() {
		It("should create a raycluster object", func() {
			err := k8sClient.Create(ctx, myRayCluster)
			Expect(err).NotTo(HaveOccurred(), "failed to create test RayCluster resource")
		})

		It("should see a raycluster object", func() {
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayCluster.Name, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)
		})

		It("should create a new head service resource", func() {
			svc := &corev1.Service{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: "raycluster-sample-head-svc", Namespace: "default"}, svc),
				time.Second*15, time.Millisecond*500).Should(BeNil(), "My head service = %v", svc)
			Expect(svc.Spec.Selector[common.RayIDLabelKey]).Should(Equal(utils.GenerateIdentifier(myRayCluster.Name, rayiov1alpha1.HeadNode)))
		})

		It("should create 3 workers", func() {
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(3), fmt.Sprintf("workerGroup %v", workerPods.Items))
			if len(workerPods.Items) > 0 {
				Expect(workerPods.Items[0].Status.Phase).Should(Or(Equal(corev1.PodRunning), Equal(corev1.PodPending)))
			}
		})

		It("should create a head pod resource", func() {
			err := k8sClient.List(ctx, &headPods, headFilterLabels, &client.ListOptions{Namespace: "default"}, client.InNamespace(myRayCluster.Namespace))
			Expect(err).NotTo(HaveOccurred(), "failed list head pods")
			Expect(len(headPods.Items)).Should(BeNumerically("==", 1), "My head pod list= %v", headPods.Items)

			pod := &corev1.Pod{}
			if len(headPods.Items) > 0 {
				pod = &headPods.Items[0]
			}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: pod.Name, Namespace: "default"}, pod),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My head pod = %v", pod)
			Expect(pod.Status.Phase).Should(Or(Equal(corev1.PodPending), Equal(corev1.PodRunning)))
		})

		It("should create the head group's specified K8s ServiceAccount if it doesn't exist", func() {
			saName := utils.GetHeadGroupServiceAccountName(myRayCluster)
			sa := &corev1.ServiceAccount{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: saName, Namespace: "default"}, sa),
				time.Second*15, time.Millisecond*500).Should(BeNil(), "My head group ServiceAccount = %v", saName)
		})

		It("should create the autoscaler K8s RoleBinding if it doesn't exist", func() {
			rbName := myRayCluster.Name
			rb := &rbacv1.RoleBinding{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rbName, Namespace: myRayCluster.Namespace}, rb),
				time.Second*15, time.Millisecond*500).Should(BeNil(), "autoscaler RoleBinding = %v", rbName)
		})

		It("should be able to update all Pods to Running", func() {
			// We need to manually update Pod statuses otherwise they'll always be Pending.
			// envtest doesn't create a full K8s cluster. It's only the control plane.
			// There's no container runtime or any other K8s controllers.
			// So Pods are created, but no controller updates them from Pending to Running.
			// See https://book.kubebuilder.io/reference/envtest.html

			for _, headPod := range headPods.Items {
				headPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &headPod)).Should(BeNil())
			}

			Eventually(
				isAllPodsRunning(ctx, headPods, headFilterLabels, "default"),
				time.Second*15, time.Millisecond*500).Should(Equal(true), "Head Pod should be running.")

			for _, workerPod := range workerPods.Items {
				workerPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(BeNil())
			}

			Eventually(
				isAllPodsRunning(ctx, workerPods, workerFilterLabels, "default"),
				time.Second*15, time.Millisecond*500).Should(Equal(true), "All worker Pods should be running.")
		})

		It("cluster's .status.state should be updated to 'ready' shortly after all Pods are Running", func() {
			Eventually(
				getClusterState(ctx, "default", myRayCluster.Name),
				time.Second*(common.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS+5), time.Millisecond*500).Should(Equal(rayiov1alpha1.Ready))
		})

		It("should re-create a deleted worker", func() {
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(3), fmt.Sprintf("workerGroup %v", workerPods.Items))

			pod := workerPods.Items[0]
			err := k8sClient.Delete(ctx, &pod,
				&client.DeleteOptions{GracePeriodSeconds: pointer.Int64(0)})

			Expect(err).NotTo(HaveOccurred(), "failed delete a pod")

			// at least 3 pods should be in none-failed phase
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(3), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("should update a raycluster object deleting a random pod", func() {
			// adding a scale down
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayCluster.Name, Namespace: "default"}, myRayCluster),
					time.Second*9, time.Millisecond*500).Should(BeNil(), "My raycluster = %v", myRayCluster)
				myRayCluster.Spec.WorkerGroupSpecs[0].Replicas = pointer.Int32(2)

				// Operator may update revision after we get cluster earlier. Update may result in 409 conflict error.
				// We need to handle conflict error and retry the update.
				return k8sClient.Update(ctx, myRayCluster)
			})

			Expect(err).NotTo(HaveOccurred(), "failed to update test RayCluster resource")
		})

		It("should have only 2 running worker", func() {
			// retry listing pods, given that last update may not immediately happen.
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(2), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("should update a raycluster object", func() {
			// adding a scale strategy
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayCluster.Name, Namespace: "default"}, myRayCluster),
					time.Second*9, time.Millisecond*500).Should(BeNil(), "My raycluster = %v", myRayCluster)
				podToDelete := workerPods.Items[0]
				myRayCluster.Spec.WorkerGroupSpecs[0].Replicas = pointer.Int32(1)
				myRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{podToDelete.Name}
				return k8sClient.Update(ctx, myRayCluster)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayCluster resource")
		})

		It("should have only 1 running worker", func() {
			// retry listing pods, given that last update may not immediately happen.
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(1), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("should increase replicas past maxReplicas", func() {
			// increasing replicas to 5, which is greater than maxReplicas (4)
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayCluster.Name, Namespace: "default"}, myRayCluster),
					time.Second*9, time.Millisecond*500).Should(BeNil(), "My raycluster = %v", myRayCluster)
				myRayCluster.Spec.WorkerGroupSpecs[0].Replicas = pointer.Int32(5)

				// Operator may update revision after we get cluster earlier. Update may result in 409 conflict error.
				// We need to handle conflict error and retry the update.
				return k8sClient.Update(ctx, myRayCluster)
			})

			Expect(err).NotTo(HaveOccurred(), "failed to update test RayCluster resource")
		})

		It("should scale to maxReplicas (4) workers", func() {
			// retry listing pods, given that last update may not immediately happen.
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(4), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("should countinue to have only maxReplicas (4) workers", func() {
			// check that pod count stays at 4 for two seconds.
			Consistently(
				listResourceFunc(ctx, &workerPods, workerFilterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*2, time.Millisecond*200).Should(Equal(4), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})
	})
})

func getResourceFunc(ctx context.Context, key client.ObjectKey, obj client.Object) func() error {
	return func() error {
		return k8sClient.Get(ctx, key, obj)
	}
}

func listResourceFunc(ctx context.Context, workerPods *corev1.PodList, opt ...client.ListOption) func() (int, error) {
	return func() (int, error) {
		if err := k8sClient.List(ctx, workerPods, opt...); err != nil {
			return -1, err
		}

		count := 0
		for _, aPod := range workerPods.Items {
			if (reflect.DeepEqual(aPod.Status.Phase, corev1.PodRunning) || reflect.DeepEqual(aPod.Status.Phase, corev1.PodPending)) && aPod.DeletionTimestamp == nil {
				count++
			}
		}

		return count, nil
	}
}

func getClusterState(ctx context.Context, namespace string, clusterName string) func() rayiov1alpha1.ClusterState {
	return func() rayiov1alpha1.ClusterState {
		var cluster rayiov1alpha1.RayCluster
		if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: clusterName}, &cluster); err != nil {
			log.Fatal(err)
		}
		return cluster.Status.State
	}
}

func isAllPodsRunning(ctx context.Context, podlist corev1.PodList, filterLabels client.MatchingLabels, namespace string) bool {
	err := k8sClient.List(ctx, &podlist, filterLabels, &client.ListOptions{Namespace: namespace})
	Expect(err).ShouldNot(HaveOccurred(), "failed to list Pods")
	for _, pod := range podlist.Items {
		if pod.Status.Phase != corev1.PodRunning {
			return false
		}
	}
	return true
}
