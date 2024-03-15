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

	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

func rayJobTemplate(name string, namespace string) *rayv1.RayJob {
	return &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: rayv1.RayJobSpec{
			Entrypoint:               "sleep 999",
			SubmissionMode:           rayv1.K8sJobMode,
			ShutdownAfterJobFinishes: true,
			RayClusterSpec: &rayv1.RayClusterSpec{
				RayVersion: "2.9.0",
				HeadGroupSpec: rayv1.HeadGroupSpec{
					RayStartParams: map[string]string{},
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "ray-head",
									Image: "rayproject/ray:2.9.0",
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
											Name:          "client",
											ContainerPort: 10001,
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
										Image: "rayproject/ray:2.9.0",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

var _ = Context("RayJob in K8sJobMode", func() {
	Describe("Successful RayJob in K8sJobMode", func() {
		ctx := context.Background()
		namespace := "default"
		rayJob := rayJobTemplate("rayjob-test", namespace)
		rayCluster := &rayv1.RayCluster{}

		It("Verify RayJob spec", func() {
			// This test case simulates the most common scenario in the RayJob code path.
			// (1) The submission mode is K8sJobMode.
			// (2) `shutdownAfterJobFinishes` is true.
			// In this test, RayJob passes through the following states: New -> Initializing -> Running -> Complete
			Expect(rayJob.Spec.SubmissionMode).To(Equal(rayv1.K8sJobMode))
			Expect(rayJob.Spec.ShutdownAfterJobFinishes).To(BeTrue())

			// This test assumes that there is only one worker group.
			Expect(len(rayJob.Spec.RayClusterSpec.WorkerGroupSpecs)).To(Equal(1))
		})

		It("Create a RayJob custom resource", func() {
			err := k8sClient.Create(ctx, rayJob)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayJob")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: namespace}, rayJob),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "Should be able to see RayJob: %v", rayJob.Name)
		})

		It("RayJobs's JobDeploymentStatus transitions from New to Initializing.", func() {
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusInitializing), "JobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)

			// In Initializing state, Status.RayClusterName, Status.JobId, and Status.StartTime must be set.
			Expect(rayJob.Status.RayClusterName).NotTo(BeEmpty())
			Expect(rayJob.Status.JobId).NotTo(BeEmpty())
			Expect(rayJob.Status.StartTime).NotTo(BeNil())
		})

		It("In Initializing state, the RayCluster should eventually be created.", func() {
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Status.RayClusterName, Namespace: namespace}, rayCluster),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "RayCluster %v not found", rayJob.Status.RayClusterName)

			// Check whether RayCluster is consistent with RayJob's RayClusterSpec.
			Expect(rayCluster.Spec.WorkerGroupSpecs[0].Replicas).To(Equal(rayJob.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas))
			Expect(rayCluster.Spec.RayVersion).To(Equal(rayJob.Spec.RayClusterSpec.RayVersion))

			// TODO (kevin85421): Check the RayCluster labels and annotations.
			Expect(rayCluster.Labels).Should(HaveKeyWithValue(utils.RayOriginatedFromCRNameLabelKey, rayJob.Name))
			Expect(rayCluster.Labels).Should(HaveKeyWithValue(utils.RayOriginatedFromCRDLabelKey, utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD)))
		})

		It("Make RayCluster.Status.State to be rayv1.Ready", func() {
			// The RayCluster is not 'Ready' yet because Pods are not running and ready.
			Expect(rayCluster.Status.State).NotTo(Equal(rayv1.Ready))
			allPods := []corev1.Pod{}

			// Check whether the number of worker Pods is consistent with RayCluster CR or not.
			numWorkerPods := int(*rayCluster.Spec.WorkerGroupSpecs[0].Replicas)
			workerFilterLabels := client.MatchingLabels{utils.RayClusterLabelKey: rayCluster.Name, utils.RayNodeGroupLabelKey: rayCluster.Spec.WorkerGroupSpecs[0].GroupName}
			workerPods := corev1.PodList{}
			Eventually(
				listResourceFunc(ctx, &workerPods, workerFilterLabels, &client.ListOptions{Namespace: namespace}),
				time.Second*3, time.Millisecond*500).Should(Equal(int(numWorkerPods)), fmt.Sprintf("workerGroup: %v", workerPods.Items))

			// The number of head Pods should be 1.
			headPods := corev1.PodList{}
			headFilterLabels := client.MatchingLabels{utils.RayClusterLabelKey: rayCluster.Name, utils.RayNodeGroupLabelKey: utils.RayNodeHeadGroupLabelValue}
			Eventually(
				listResourceFunc(ctx, &headPods, headFilterLabels, &client.ListOptions{Namespace: namespace}),
				time.Second*3, time.Millisecond*500).Should(Equal(1), fmt.Sprintf("head Pod: %v", headPods.Items))

			// Update all Pods, including head and worker Pods, to Running and PodReady.
			allPods = append(allPods, headPods.Items...)
			allPods = append(allPods, workerPods.Items...)

			for _, pod := range allPods {
				pod.Status.Phase = corev1.PodRunning
				// In envtest, if Pod.Status.Phase is set to running, the Pod's PodReady condition becomes true automatically.
				// Check https://github.com/ray-project/kuberay/issues/1736 for more details.
				Expect(k8sClient.Status().Update(ctx, &pod)).Should(BeNil())
			}

			// The RayCluster.Status.State should be Ready.
			Eventually(
				getClusterState(ctx, namespace, rayCluster.Name),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
		})

		It("RayJobs's JobDeploymentStatus transitions from Initializing to Running.", func() {
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusRunning), "JobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)

			// In Running state, the RayJob's Status.DashboardURL must be set.
			Expect(rayJob.Status.DashboardURL).NotTo(BeEmpty())

			// In Running state, the submitter Kubernetes Job must be created if this RayJob is in K8sJobMode.
			namespacedName := common.RayJobK8sJobNamespacedName(rayJob)
			job := &batchv1.Job{}
			err := k8sClient.Get(ctx, namespacedName, job)
			Expect(err).NotTo(HaveOccurred(), "failed to get Kubernetes Job")
		})

		It("RayJobs's JobDeploymentStatus transitions from Running to Complete.", func() {
			// Update fake dashboard client to return job info with "Succeeded" status.
			getJobInfo := func(context.Context, string) (*utils.RayJobInfo, error) {
				return &utils.RayJobInfo{JobStatus: rayv1.JobStatusSucceeded}, nil
			}
			fakeRayDashboardClient.GetJobInfoMock.Store(&getJobInfo)
			defer fakeRayDashboardClient.GetJobInfoMock.Store(nil)

			// RayJob transitions to Complete if and only if the corresponding submitter Kubernetes Job is Complete or Failed.
			Consistently(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusRunning), "JobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)

			// Update the submitter Kubernetes Job to Complete.
			namespacedName := common.RayJobK8sJobNamespacedName(rayJob)
			job := &batchv1.Job{}
			err := k8sClient.Get(ctx, namespacedName, job)
			Expect(err).NotTo(HaveOccurred(), "failed to get Kubernetes Job")

			// Update the submitter Kubernetes Job to Complete.
			conditions := []batchv1.JobCondition{
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			}
			job.Status.Conditions = conditions
			Expect(k8sClient.Status().Update(ctx, job)).Should(BeNil())

			// RayJob transitions to Complete.
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusComplete), "jobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)
		})

		It("If shutdownAfterJobFinishes is true, RayCluster should be deleted but not the submitter Job.", func() {
			Eventually(
				func() bool {
					return apierrors.IsNotFound(getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Status.RayClusterName, Namespace: namespace}, rayCluster)())
				},
				time.Second*3, time.Millisecond*500).Should(BeTrue())
			namespacedName := common.RayJobK8sJobNamespacedName(rayJob)
			job := &batchv1.Job{}
			Consistently(
				getResourceFunc(ctx, namespacedName, job),
				time.Second*3, time.Millisecond*500).Should(BeNil())
		})
	})

	Describe("RayJob has passed the ActiveDeadlineSeconds", func() {
		ctx := context.Background()
		namespace := "default"
		activeDeadlineSeconds := int32(3)
		rayJob := rayJobTemplate("rayjob-deadline", namespace)
		rayJob.Spec.ActiveDeadlineSeconds = pointer.Int32(activeDeadlineSeconds)

		It("Verify RayJob spec", func() {
			// In this test, RayJob passes through the following states: New -> Initializing -> Complete (because of ActiveDeadlineSeconds).
			Expect(rayJob.Spec.ActiveDeadlineSeconds).NotTo(BeNil())

			// This test assumes that there is only one worker group.
			Expect(len(rayJob.Spec.RayClusterSpec.WorkerGroupSpecs)).To(Equal(1))
		})

		It("Create a RayJob custom resource", func() {
			err := k8sClient.Create(ctx, rayJob)
			Expect(err).NotTo(HaveOccurred(), "Failed to create RayJob")
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: namespace}, rayJob),
				time.Second*3, time.Millisecond*500).Should(BeNil(), "Should be able to see RayJob: %v", rayJob.Name)
		})

		It("RayJobs's JobDeploymentStatus transitions from New to Initializing.", func() {
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*3, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusInitializing), "JobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)

			// In Initializing state, Status.RayClusterName, Status.JobId, and Status.StartTime must be set.
			Expect(rayJob.Status.RayClusterName).NotTo(BeEmpty())
			Expect(rayJob.Status.JobId).NotTo(BeEmpty())
			Expect(rayJob.Status.StartTime).NotTo(BeNil())
		})

		It("RayJobs has passed the activeDeadlineSeconds, and the JobDeploymentStatus transitions from Initializing to Complete.", func() {
			// RayJob transitions to Complete.
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusFailed), "jobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)
			Expect(rayJob.Status.Reason).To(Equal(rayv1.DeadlineExceeded))
		})
	})
})

func getRayClusterNameForRayJob(ctx context.Context, rayJob *rayv1.RayJob) func() (string, error) {
	return func() (string, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: "default"}, rayJob); err != nil {
			return "", err
		}
		return rayJob.Status.RayClusterName, nil
	}
}

func getDashboardURLForRayJob(ctx context.Context, rayJob *rayv1.RayJob) func() (string, error) {
	return func() (string, error) {
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: "default"}, rayJob); err != nil {
			return "", err
		}
		return rayJob.Status.DashboardURL, nil
	}
}
