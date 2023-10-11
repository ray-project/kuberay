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
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

var myRayJob = &rayv1alpha1.RayJob{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "rayjob-test",
		Namespace: "default",
	},
	Spec: rayv1alpha1.RayJobSpec{
		Entrypoint: "sleep 999",
		RayClusterSpec: &rayv1alpha1.RayClusterSpec{
			RayVersion: "1.12.1",
			HeadGroupSpec: rayv1alpha1.HeadGroupSpec{
				Replicas: pointer.Int32(1),
				RayStartParams: map[string]string{
					"port":                        "6379",
					"object-store-memory":         "100000000",
					"dashboard-host":              "0.0.0.0",
					"num-cpus":                    "1",
					"node-ip-address":             "127.0.0.1",
					"dashboard-agent-listen-port": "52365",
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"groupName": "headgroup",
						},
						Annotations: map[string]string{
							"key": "value",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Image: "rayproject/ray:2.7.0",
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
			WorkerGroupSpecs: []rayv1alpha1.WorkerGroupSpec{
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
								"groupName": "small-group",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:    "ray-worker",
									Image:   "rayproject/ray:2.7.0",
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

var _ = Context("Inside the default namespace", func() {
	ctx := context.TODO()
	myRayJob := myRayJob.DeepCopy()
	myRayJob.Name = "rayjob-test-default"

	Describe("When creating a rayjob", func() {
		It("should create a rayjob object", func() {
			err := k8sClient.Create(ctx, myRayJob)
			Expect(err).NotTo(HaveOccurred(), "failed to create test RayJob resource")
		})

		It("should see a rayjob object", func() {
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Name, Namespace: "default"}, myRayJob),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayJob  = %v", myRayJob.Name)
		})

		It("should create a raycluster object", func() {
			Eventually(
				getRayClusterNameForRayJob(ctx, myRayJob),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()), "My RayCluster name  = %v", myRayJob.Status.RayClusterName)
			myRayCluster := &rayv1alpha1.RayCluster{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Status.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)
		})

		It("Should create a number of workers equal to the replica setting", func() {
			filterLabels := client.MatchingLabels{common.RayClusterLabelKey: myRayJob.Status.RayClusterName, common.RayNodeGroupLabelKey: myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].GroupName}
			workerPods := corev1.PodList{}
			Eventually(
				listResourceFunc(ctx, &workerPods, filterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(int(*myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas)), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		// If in-tree autoscaling is disabled, the user should be able to update the replica settings.
		// This test verifies that the RayJob controller correctly updates the replica settings and increases the number of workers in this scenario.
		It("should increase number of worker pods by updating RayJob", func() {
			Eventually(
				getRayClusterNameForRayJob(ctx, myRayJob),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()), "My RayCluster name  = %v", myRayJob.Status.RayClusterName)

			myRayCluster := &rayv1alpha1.RayCluster{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Status.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)

			newReplicas := *myRayCluster.Spec.WorkerGroupSpecs[0].Replicas + 1

			// simulate updating the RayJob directly.
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				*myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas = newReplicas
				return k8sClient.Update(ctx, myRayJob)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update RayJob")

			// confirm the number of worker pods increased.
			filterLabels := client.MatchingLabels{common.RayClusterLabelKey: myRayJob.Status.RayClusterName, common.RayNodeGroupLabelKey: myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].GroupName}
			workerPods := corev1.PodList{}
			Eventually(
				listResourceFunc(ctx, &workerPods, filterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(int(newReplicas)), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		It("Dashboard URL should be set", func() {
			Eventually(
				getDashboardURLForRayJob(ctx, myRayJob),
				time.Second*3, time.Millisecond*500).Should(HavePrefix(myRayJob.Name), "Dashboard URL = %v", myRayJob.Status.DashboardURL)
		})

		It("test cluster selector", func() {
			Eventually(
				getRayClusterNameForRayJob(ctx, myRayJob),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()), "My RayCluster name  = %v", myRayJob.Status.RayClusterName)
			myRayJobWithClusterSelector := &rayv1alpha1.RayJob{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rayjob-test-default-2",
					Namespace: "default",
				},
				Spec: rayv1alpha1.RayJobSpec{
					Entrypoint:      "sleep 999",
					ClusterSelector: map[string]string{},
				},
			}
			myRayJobWithClusterSelector.Spec.ClusterSelector[RayJobDefaultClusterSelectorKey] = myRayJob.Status.RayClusterName

			err := k8sClient.Create(ctx, myRayJobWithClusterSelector)
			Expect(err).NotTo(HaveOccurred(), "failed to create RayJob resource")

			Eventually(
				getRayClusterNameForRayJob(ctx, myRayJobWithClusterSelector),
				time.Second*15, time.Millisecond*500).Should(Equal(myRayJob.Status.RayClusterName))
		})
	})
})

var _ = Context("Inside the default namespace with autoscaler", func() {
	ctx := context.TODO()
	myRayJob := myRayJob.DeepCopy()
	myRayJob.Name = "rayjob-test-with-autoscaler"
	upscalingMode := rayv1alpha1.UpscalingMode("Default")
	imagePullPolicy := corev1.PullPolicy("IfNotPresent")
	myRayJob.Spec.RayClusterSpec.EnableInTreeAutoscaling = pointer.BoolPtr(true)
	myRayJob.Spec.RayClusterSpec.AutoscalerOptions = &rayv1alpha1.AutoscalerOptions{
		UpscalingMode:      &upscalingMode,
		IdleTimeoutSeconds: pointer.Int32(1),
		ImagePullPolicy:    &imagePullPolicy,
	}

	Describe("When creating a rayjob", func() {
		It("should create a rayjob object", func() {
			err := k8sClient.Create(ctx, myRayJob)
			Expect(err).NotTo(HaveOccurred(), "failed to create test RayJob resource")
		})

		It("should see a rayjob object", func() {
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Name, Namespace: "default"}, myRayJob),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayJob  = %v", myRayJob.Name)
		})

		// if In-tree autoscaling is enabled, the autoscaler should adjust the number of replicas based on the workload.
		// This test emulates the behavior of the autoscaler by directly updating the RayCluster and verifying if the number of worker pods increases accordingly.
		It("should create new worker since autoscaler increases the replica", func() {
			Eventually(
				getRayClusterNameForRayJob(ctx, myRayJob),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()), "My RayCluster name  = %v", myRayJob.Status.RayClusterName)

			myRayCluster := &rayv1alpha1.RayCluster{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Status.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)

			// simulate autoscaler by updating the RayCluster directly. Note that the autoscaler
			// will not update the RayJob directly.
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Status.RayClusterName, Namespace: "default"}, myRayCluster),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "Active RayCluster = %v", myRayCluster.Name)
				*myRayCluster.Spec.WorkerGroupSpecs[0].Replicas++
				return k8sClient.Update(ctx, myRayCluster)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update RayCluster replica")

			// confirm a new worker pod is created.
			filterLabels := client.MatchingLabels{common.RayClusterLabelKey: myRayJob.Status.RayClusterName, common.RayNodeGroupLabelKey: myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].GroupName}
			workerPods := corev1.PodList{}
			Eventually(
				listResourceFunc(ctx, &workerPods, filterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*15, time.Millisecond*500).Should(Equal(int(*myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas)+1), fmt.Sprintf("workerGroup %v", workerPods.Items))
			// confirm RayJob controller does not revert the number of workers.
			Consistently(
				listResourceFunc(ctx, &workerPods, filterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*5, time.Millisecond*500).Should(Equal(int(*myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas)+1), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})

		// if In-tree autoscaling is enabled, only the autoscaler should update replicas to prevent race conditions
		// between user updates and autoscaler decisions. RayJob controller should not modify the replica. Consider this scenario:
		// 1. The autoscaler updates replicas to 10 based on the current workload.
		// 2. The user updates replicas to 15 in the RayJob YAML file.
		// 3. Both RayJob controller and the autoscaler attempt to update replicas, causing worker pods to be repeatedly created and terminated.
		// This test emulates a user attempting to update the replica and verifies that the number of worker pods remains unaffected by this change.
		It("should not increase number of workers by updating RayJob", func() {
			Eventually(
				getRayClusterNameForRayJob(ctx, myRayJob),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()), "My RayCluster name  = %v", myRayJob.Status.RayClusterName)

			myRayCluster := &rayv1alpha1.RayCluster{}
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: myRayJob.Status.RayClusterName, Namespace: "default"}, myRayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "My myRayCluster  = %v", myRayCluster.Name)

			oldReplicas := *myRayCluster.Spec.WorkerGroupSpecs[0].Replicas

			// simulate updating the RayJob directly.
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				*myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas = *myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas + 1
				return k8sClient.Update(ctx, myRayJob)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update RayJob")

			// confirm the number of worker pods is not changed.
			filterLabels := client.MatchingLabels{common.RayClusterLabelKey: myRayJob.Status.RayClusterName, common.RayNodeGroupLabelKey: myRayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].GroupName}
			workerPods := corev1.PodList{}
			Consistently(
				listResourceFunc(ctx, &workerPods, filterLabels, &client.ListOptions{Namespace: "default"}),
				time.Second*5, time.Millisecond*500).Should(Equal(int(oldReplicas)), fmt.Sprintf("workerGroup %v", workerPods.Items))
		})
	})
})

var _ = Context("With a delayed dashboard client", func() {
	ctx := context.TODO()
	myRayJob := myRayJob.DeepCopy()
	myRayJob.Name = "rayjob-delayed-dashbaord"

	mockedGetJobInfo := func(_ context.Context, jobId string) (*utils.RayJobInfo, error) {
		return nil, errors.New("dashboard is not ready")
	}

	Describe("When creating a rayjob", func() {
		It("should create a rayjob object", func() {
			// setup mock first
			utils.GetRayDashboardClientFunc().(*utils.FakeRayDashboardClient).GetJobInfoMock.Store(&mockedGetJobInfo)
			err := k8sClient.Create(ctx, myRayJob)
			Expect(err).NotTo(HaveOccurred(), "failed to create test RayJob resource")
		})

		It("should see a rayjob object with JobDeploymentStatusWaitForDashboardReady", func() {
			Eventually(
				getJobDeploymentStatusOfRayJob(ctx, myRayJob),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1alpha1.JobDeploymentStatusWaitForDashboardReady), "My myRayJob  = %v", myRayJob.Name)
		})

		It("Dashboard URL should be set and deployment status should leave the JobDeploymentStatusWaitForDashboardReady", func() {
			// clear mock to back to normal behavior
			utils.GetRayDashboardClientFunc().(*utils.FakeRayDashboardClient).GetJobInfoMock.Store(nil)
			Eventually(
				getDashboardURLForRayJob(ctx, myRayJob),
				time.Second*15, time.Millisecond*500).Should(HavePrefix(myRayJob.Name), "Dashboard URL = %v", myRayJob.Status.DashboardURL)
			Eventually(
				getJobDeploymentStatusOfRayJob(ctx, myRayJob),
				time.Second*3, time.Millisecond*500).Should(Not(Equal(rayv1alpha1.JobDeploymentStatusWaitForDashboardReady)), "My myRayJob  = %v", myRayJob.Name)
		})
	})
})

func getJobDeploymentStatusOfRayJob(ctx context.Context, rayJob *rayv1alpha1.RayJob) func() (rayv1alpha1.JobDeploymentStatus, error) {
	return func() (rayv1alpha1.JobDeploymentStatus, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: "default"}, rayJob); err != nil {
			return "", err
		}
		return rayJob.Status.JobDeploymentStatus, nil
	}
}

func getRayClusterNameForRayJob(ctx context.Context, rayJob *rayv1alpha1.RayJob) func() (string, error) {
	return func() (string, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: "default"}, rayJob); err != nil {
			return "", err
		}
		return rayJob.Status.RayClusterName, nil
	}
}

func getDashboardURLForRayJob(ctx context.Context, rayJob *rayv1alpha1.RayJob) func() (string, error) {
	return func() (string, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: "default"}, rayJob); err != nil {
			return "", err
		}
		return rayJob.Status.DashboardURL, nil
	}
}
