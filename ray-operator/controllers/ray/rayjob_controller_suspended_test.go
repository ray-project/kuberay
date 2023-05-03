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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Context("Inside the default namespace", func() {
	ctx := context.TODO()
	var workerPods corev1.PodList
	var headPods corev1.PodList
	mySuspendedRayCluster := &rayiov1alpha1.RayCluster{}

	mySuspendedRayJob := &rayiov1alpha1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayjob-test-suspend",
			Namespace: "default",
		},
		Spec: rayiov1alpha1.RayJobSpec{
			Suspend:    true,
			Entrypoint: "sleep 999",
			RayClusterSpec: &rayiov1alpha1.RayClusterSpec{
				RayVersion: "2.4.0",
				HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
					ServiceType: corev1.ServiceTypeClusterIP,
					Replicas:    pointer.Int32(1),
					RayStartParams: map[string]string{
						"port":                        "6379",
						"object-store-memory":         "100000000",
						"dashboard-host":              "0.0.0.0",
						"num-cpus":                    "1",
						"node-ip-address":             "127.0.0.1",
						"block":                       "true",
						"dashboard-agent-listen-port": "52365",
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"rayCluster": "raycluster-sample",
								"groupName":  "headgroup",
							},
							Annotations: map[string]string{
								"key": "value",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "ray-head",
									Image: "rayproject/ray:2.2.0",
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
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("1"),
											corev1.ResourceMemory: resource.MustParse("2Gi"),
										},
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("1"),
											corev1.ResourceMemory: resource.MustParse("2Gi"),
										},
									},
									Ports: []corev1.ContainerPort{
										{
											Name:          "gcs-server",
											ContainerPort: 6379,
										},
										{
											Name:          "dashboard",
											ContainerPort: 8265,
										},
										{
											Name:          "head",
											ContainerPort: 10001,
										},
										{
											Name:          "dashboard-agent",
											ContainerPort: 52365,
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
						MaxReplicas: pointer.Int32(10000),
						GroupName:   "small-group",
						RayStartParams: map[string]string{
							"port":                        "6379",
							"num-cpus":                    "1",
							"dashboard-agent-listen-port": "52365",
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "default",
								Labels: map[string]string{
									"rayCluster": "raycluster-sample",
									"groupName":  "small-group",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:    "ray-worker",
										Image:   "rayproject/ray:2.2.0",
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
										Ports: []corev1.ContainerPort{
											{
												Name:          "client",
												ContainerPort: 80,
											},
											{
												Name:          "dashboard-agent",
												ContainerPort: 52365,
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

	Describe("When creating a rayjob with suspend == true", func() {
		It("should create a rayjob object", func() {
			err := k8sClient.Create(ctx, mySuspendedRayJob)
			Expect(err).NotTo(HaveOccurred(), "failed to create test RayJob resource")
		})

		It("should see a rayjob object", func() {
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: mySuspendedRayJob.Name, Namespace: "default"}, mySuspendedRayJob),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayJob  = %v", mySuspendedRayJob.Name)
		})

		It("should have deployment status suspended", func() {
			Eventually(
				getRayJobDeploymentStatus(ctx, mySuspendedRayJob),
				time.Second*5, time.Millisecond*500).Should(Equal(rayiov1alpha1.JobDeploymentStatusSuspended))
		})

		It("should NOT create a raycluster object", func() {
			// Ray Cluster name can be present on RayJob's CRD
			Eventually(
				getRayClusterNameForRayJob(ctx, mySuspendedRayJob),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()))
			// However the actual cluster instance and underlying resources should not be created while suspend == true
			Eventually(
				// k8sClient client throws error if resource not found
				func() bool {
					err := getResourceFunc(ctx, client.ObjectKey{Name: mySuspendedRayJob.Status.RayClusterName, Namespace: "default"}, mySuspendedRayCluster)()
					return errors.IsNotFound(err)
				},
				time.Second*10, time.Millisecond*500).Should(BeTrue())
		})

		It("should unsuspend a rayjob object", func() {
			mySuspendedRayJob.Spec.Suspend = false
			err := k8sClient.Update(ctx, mySuspendedRayJob)
			Expect(err).NotTo(HaveOccurred(), "failed to update test RayJob resource")
		})

		It("should create a raycluster object", func() {
			// Ray Cluster name can be present on RayJob's CRD
			Eventually(
				getRayClusterNameForRayJob(ctx, mySuspendedRayJob),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()))
			// The actual cluster instance and underlying resources SHOULD be created when suspend == false
			Eventually(
				// k8sClient client does not throw error if cluster IS found
				getResourceFunc(ctx, client.ObjectKey{Name: mySuspendedRayJob.Status.RayClusterName, Namespace: "default"}, mySuspendedRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil())
		})

		It("should create 3 workers", func() {
			Eventually(
				listResourceFunc(ctx, &workerPods, client.MatchingLabels{
					common.RayClusterLabelKey:   mySuspendedRayCluster.Name,
					common.RayNodeGroupLabelKey: "small-group",
				},
					&client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(3), fmt.Sprintf("workerGroup %v", workerPods.Items))
			if len(workerPods.Items) > 0 {
				Expect(workerPods.Items[0].Status.Phase).Should(Or(Equal(corev1.PodRunning), Equal(corev1.PodPending)))
			}
		})

		It("should create a head pod resource", func() {
			err := k8sClient.List(ctx, &headPods,
				client.MatchingLabels{
					common.RayClusterLabelKey:   mySuspendedRayCluster.Name,
					common.RayNodeGroupLabelKey: "headgroup",
				},
				&client.ListOptions{Namespace: "default"},
				client.InNamespace(mySuspendedRayCluster.Namespace))

			Expect(err).NotTo(HaveOccurred(), "failed list head pods")
			Expect(len(headPods.Items)).Should(BeNumerically("==", 1), "My head pod list= %v", headPods.Items)

			pod := &corev1.Pod{}
			if len(headPods.Items) > 0 {
				pod = &headPods.Items[0]
			}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: pod.Name, Namespace: "default"}, pod),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My head pod = %v", pod)
			Expect(pod.Status.Phase).Should(Or(Equal(corev1.PodPending)))
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
				isAllPodsRunning(ctx, headPods, client.MatchingLabels{
					common.RayClusterLabelKey:   mySuspendedRayCluster.Name,
					common.RayNodeGroupLabelKey: "headgroup",
				}, "default"),
				time.Second*15, time.Millisecond*500).Should(Equal(true), "Head Pod should be running.")

			for _, workerPod := range workerPods.Items {
				workerPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(BeNil())
			}

			Eventually(
				isAllPodsRunning(ctx, workerPods, client.MatchingLabels{common.RayClusterLabelKey: mySuspendedRayCluster.Name, common.RayNodeGroupLabelKey: "small-group"}, "default"),
				time.Second*15, time.Millisecond*500).Should(Equal(true), "All worker Pods should be running.")
		})

		It("Dashboard URL should be set", func() {
			Eventually(
				getDashboardURLForRayJob(ctx, mySuspendedRayJob),
				time.Second*3, time.Millisecond*500).Should(HavePrefix(mySuspendedRayJob.Name), "Dashboard URL = %v", mySuspendedRayJob.Status.DashboardURL)
		})
	})
})

func getRayJobDeploymentStatus(ctx context.Context, rayJob *rayiov1alpha1.RayJob) func() (rayiov1alpha1.JobDeploymentStatus, error) {
	return func() (rayiov1alpha1.JobDeploymentStatus, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: "default"}, rayJob); err != nil {
			return "", err
		}
		return rayJob.Status.JobDeploymentStatus, nil
	}
}
