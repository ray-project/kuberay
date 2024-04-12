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

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	batchv1 "k8s.io/api/batch/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Context("Inside the default namespace", func() {
	ctx := context.TODO()
	var workerPods corev1.PodList
	var headPods corev1.PodList

	mySuspendedRayCluster := &rayv1.RayCluster{}

	mySuspendedRayJob := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayjob-test-suspend",
			Namespace: "default",
		},
		Spec: rayv1.RayJobSpec{
			ShutdownAfterJobFinishes: true,
			Suspend:                  true,
			Entrypoint:               "sleep 999",
			RayClusterSpec: &rayv1.RayClusterSpec{
				RayVersion: "2.9.0",
				HeadGroupSpec: rayv1.HeadGroupSpec{
					RayStartParams: map[string]string{},
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "ray-head",
									Image: "rayproject/ray:2.8.0",
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
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						Replicas:       pointer.Int32(3),
						MinReplicas:    pointer.Int32(0),
						MaxReplicas:    pointer.Int32(10000),
						GroupName:      "small-group",
						RayStartParams: map[string]string{},
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "ray-worker",
										Image: "rayproject/ray:2.8.0",
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
				time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusSuspended))
		})

		It("should NOT create a raycluster object", func() {
			Consistently(
				getRayClusterNameForRayJob(ctx, mySuspendedRayJob),
				time.Second*3, time.Millisecond*500).Should(BeEmpty())
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
				listResourceFunc(ctx, &workerPods, common.RayClusterGroupPodsAssociationOptions(mySuspendedRayCluster, "small-group").ToListOptions()...),
				time.Second*15, time.Millisecond*500).Should(Equal(3), fmt.Sprintf("workerGroup %v", workerPods.Items))
			if len(workerPods.Items) > 0 {
				Expect(workerPods.Items[0].Status.Phase).Should(Or(Equal(corev1.PodRunning), Equal(corev1.PodPending)))
			}
		})

		It("should create a head pod resource", func() {
			err := k8sClient.List(ctx, &headPods,
				common.RayClusterGroupPodsAssociationOptions(mySuspendedRayCluster, utils.RayNodeHeadGroupLabelValue).ToListOptions()...)

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

		It("should NOT create the underlying K8s job yet because the cluster is not ready", func() {
			underlyingK8sJob := &batchv1.Job{}
			Eventually(
				// k8sClient client throws error if resource not found
				func() bool {
					err := getResourceFunc(ctx, client.ObjectKey{Name: mySuspendedRayJob.Name, Namespace: "default"}, underlyingK8sJob)()
					return errors.IsNotFound(err)
				},
				time.Second*10, time.Millisecond*500).Should(BeTrue())
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
				isAllPodsRunningByFilters(ctx, headPods, common.RayClusterGroupPodsAssociationOptions(mySuspendedRayCluster, utils.RayNodeHeadGroupLabelValue).ToListOptions()...),
				time.Second*15, time.Millisecond*500).Should(Equal(true), "Head Pod should be running.")

			for _, workerPod := range workerPods.Items {
				workerPod.Status.Phase = corev1.PodRunning
				Expect(k8sClient.Status().Update(ctx, &workerPod)).Should(BeNil())
			}

			Eventually(
				isAllPodsRunningByFilters(ctx, workerPods, common.RayClusterGroupPodsAssociationOptions(mySuspendedRayCluster, "small-group").ToListOptions()...),
				time.Second*15, time.Millisecond*500).Should(Equal(true), "All worker Pods should be running.")
		})

		It("Dashboard URL should be set", func() {
			Eventually(
				getDashboardURLForRayJob(ctx, mySuspendedRayJob),
				time.Second*3, time.Millisecond*500).Should(HavePrefix(mySuspendedRayJob.Name), "Dashboard URL = %v", mySuspendedRayJob.Status.DashboardURL)
		})

		It("should create the underlying Kubernetes Job object", func() {
			underlyingK8sJob := &batchv1.Job{}
			// The underlying Kubernetes Job should be created when the RayJob is created
			Eventually(
				// k8sClient does not throw error if Job is found
				func() error {
					return getResourceFunc(ctx, client.ObjectKey{Name: mySuspendedRayJob.Name, Namespace: "default"}, underlyingK8sJob)()
				},
				time.Second*15, time.Millisecond*500).Should(BeNil(), "Expected Kubernetes job to be present")
		})
	})
})
