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
	"os"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	"github.com/ray-project/kuberay/ray-operator/test/support"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

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
				RayVersion: support.GetRayVersion(),
				HeadGroupSpec: rayv1.HeadGroupSpec{
					RayStartParams: map[string]string{},
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "ray-head",
									Image: support.GetRayImage(),
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
						Replicas:       ptr.To[int32](3),
						MinReplicas:    ptr.To[int32](0),
						MaxReplicas:    ptr.To[int32](10000),
						GroupName:      "small-group",
						RayStartParams: map[string]string{},
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
		},
	}
}

var _ = Context("RayJob with different submission modes", func() {
	Context("RayJob in K8sJobMode", func() {
		Describe("RayJob SubmitterConfig BackoffLimit", Ordered, func() {
			ctx := context.Background()
			namespace := "default"
			rayJobWithDefaultSubmitterConfigBackoffLimit := rayJobTemplate("rayjob-default", namespace)
			rayJobWithNonDefaultSubmitterConfigBackoffLimit := rayJobTemplate("rayjob-non-default", namespace)
			rayJobWithNonDefaultSubmitterConfigBackoffLimit.Spec.SubmitterConfig = &rayv1.SubmitterConfig{
				BackoffLimit: ptr.To[int32](88),
			}
			rayJobs := make(map[*rayv1.RayJob]int32)
			rayJobs[rayJobWithDefaultSubmitterConfigBackoffLimit] = int32(2)
			rayJobs[rayJobWithNonDefaultSubmitterConfigBackoffLimit] = int32(88)

			It("Verify RayJob spec", func() {
				for rayJob := range rayJobs {
					// Make sure the submission mode is K8sJobMode.
					Expect(rayJob.Spec.SubmissionMode).To(Equal(rayv1.K8sJobMode))
				}
			})

			It("Create RayJob custom resources", func() {
				for rayJob := range rayJobs {
					err := k8sClient.Create(ctx, rayJob)
					Expect(err).NotTo(HaveOccurred(), "Failed to create RayJob: %v", rayJob.Name)
					Eventually(
						getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: namespace}, rayJob),
						time.Second*3, time.Millisecond*500).Should(BeNil(), "Should be able to see RayJob: %v", rayJob.Name)
				}
			})

			It("RayJobs's JobDeploymentStatus transitions from New to Initializing.", func() {
				for rayJob := range rayJobs {
					Eventually(
						getRayJobDeploymentStatus(ctx, rayJob),
						time.Second*3, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusInitializing), "JobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)
				}
			})

			It("RayJobs's JobDeploymentStatus transitions from Initializing to Running.", func() {
				for rayJob := range rayJobs {
					rayCluster := &rayv1.RayCluster{}
					Eventually(
						getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Status.RayClusterName, Namespace: namespace}, rayCluster),
						time.Second*3, time.Millisecond*500).Should(BeNil(), "RayCluster %v not found", rayJob.Status.RayClusterName)

					// Make RayCluster.Status.State to be rayv1.Ready.
					updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
					updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)

					// The RayCluster.Status.State should be Ready.
					Eventually(
						getClusterState(ctx, namespace, rayCluster.Name),
						time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))

					// RayJobs's JobDeploymentStatus transitions to Running.
					Eventually(
						getRayJobDeploymentStatus(ctx, rayJob),
						time.Second*3, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusRunning), "JobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)

				}
			})

			It("Verify K8s Job BackoffLimit", func() {
				for rayJob, backoffLimit := range rayJobs {
					// In Running state, the submitter Kubernetes Job must be created if this RayJob is in K8sJobMode.
					namespacedName := common.RayJobK8sJobNamespacedName(rayJob)
					job := &batchv1.Job{}
					err := k8sClient.Get(ctx, namespacedName, job)
					Expect(err).NotTo(HaveOccurred(), "failed to get Kubernetes Job")
					Expect(*(job.Spec.BackoffLimit)).To(Equal(backoffLimit))
				}
			})
		})

		Describe("Successful RayJob in K8sJobMode", Ordered, func() {
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
				Expect(rayJob.Spec.RayClusterSpec.WorkerGroupSpecs).To(HaveLen(1))
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
				Expect(rayCluster.Status.State).NotTo(Equal(rayv1.Ready)) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288

				updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
				updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)

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
				getJobInfo := func(context.Context, string) (*utils.RayJobInfo, error) { //nolint:unparam // This is a mock function so parameters are required
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
				Expect(k8sClient.Status().Update(ctx, job)).Should(Succeed())

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

		Describe("Invalid RayJob in K8sJobMode", Ordered, func() {
			ctx := context.Background()
			namespace := "default"
			rayJob := rayJobTemplate("rayjob-invalid-test", namespace)
			rayCluster := &rayv1.RayCluster{Spec: *rayJob.Spec.RayClusterSpec}
			template := common.GetDefaultSubmitterTemplate(rayCluster)
			template.Spec.RestartPolicy = "" // Make it invalid to create a submitter. Ref: https://github.com/ray-project/kuberay/pull/2389#issuecomment-2359564334
			rayJob.Spec.SubmitterPodTemplate = &template

			It("Verify RayJob spec", func() {
				Expect(rayJob.Spec.SubmissionMode).To(Equal(rayv1.K8sJobMode))
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
			})

			It("In Initializing state, the RayCluster should eventually be created.", func() {
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Status.RayClusterName, Namespace: namespace}, rayCluster),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "RayCluster %v not found", rayJob.Status.RayClusterName)
			})

			It("Make RayCluster.Status.State to be rayv1.Ready", func() {
				// The RayCluster is not 'Ready' yet because Pods are not running and ready.
				Expect(rayCluster.Status.State).NotTo(Equal(rayv1.Ready)) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288

				updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
				updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)

				// The RayCluster.Status.State should be Ready.
				Eventually(
					getClusterState(ctx, namespace, rayCluster.Name),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
			})

			It("RayJobs's JobDeploymentStatus transitions from Initializing to Running.", func() {
				Eventually(
					func() ([]corev1.Event, error) {
						events := &corev1.EventList{}
						if err := k8sClient.List(ctx, events, client.InNamespace(rayJob.Namespace)); err != nil {
							return nil, err
						}
						return events.Items, nil
					},
					time.Second*3, time.Millisecond*500).Should(ContainElement(HaveField("Message", ContainSubstring("Failed to create new Kubernetes Job default/rayjob-invalid-test"))))

				_ = k8sClient.Delete(ctx, rayJob)
			})
		})

		Describe("Successful RayJob in K8sjobMode with DELETE_RAYJOB_CR_AFTER_JOB_FINISHES", Ordered, func() {
			ctx := context.Background()
			namespace := "default"
			rayJob := rayJobTemplate("rayjob-test-delete", namespace)
			rayCluster := &rayv1.RayCluster{}

			It("Verify RayJob spec", func() {
				// This test case simulates the most common scenario in the RayJob code path.
				// (1) The submission mode is K8sJobMode.
				// (2) `shutdownAfterJobFinishes` is true.
				// In this test, RayJob passes through the following states: New -> Initializing -> Running -> Complete
				Expect(rayJob.Spec.SubmissionMode).To(Equal(rayv1.K8sJobMode))
				Expect(rayJob.Spec.ShutdownAfterJobFinishes).To(BeTrue())

				// This test assumes that there is only one worker group.
				Expect(rayJob.Spec.RayClusterSpec.WorkerGroupSpecs).To(HaveLen(1))
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
				Expect(rayCluster.Status.State).NotTo(Equal(rayv1.Ready)) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288

				updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
				updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)

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
				getJobInfo := func(context.Context, string) (*utils.RayJobInfo, error) { //nolint:unparam // This is a mock function so parameters are required
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
				Expect(k8sClient.Status().Update(ctx, job)).Should(Succeed())

				// RayJob transitions to Complete.
				Eventually(
					getRayJobDeploymentStatus(ctx, rayJob),
					time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusComplete), "jobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)
			})

			It("If DELETE_RAYJOB_CR_AFTER_JOB_FINISHES environement variable is set, RayJob should be deleted.", func() {
				os.Setenv(utils.DELETE_RAYJOB_CR_AFTER_JOB_FINISHES, "true")
				defer os.Unsetenv(utils.DELETE_RAYJOB_CR_AFTER_JOB_FINISHES)
				Eventually(
					func() bool {
						return apierrors.IsNotFound(getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: namespace}, rayJob)())
					}, time.Second*3, time.Millisecond*500).Should(BeTrue())
			})
		})

		Describe("RayJob has passed the ActiveDeadlineSeconds", Ordered, func() {
			ctx := context.Background()
			namespace := "default"
			activeDeadlineSeconds := int32(3)
			rayJob := rayJobTemplate("rayjob-deadline", namespace)
			rayJob.Spec.ActiveDeadlineSeconds = ptr.To[int32](activeDeadlineSeconds)

			It("Verify RayJob spec", func() {
				// In this test, RayJob passes through the following states: New -> Initializing -> Complete (because of ActiveDeadlineSeconds).
				Expect(rayJob.Spec.ActiveDeadlineSeconds).NotTo(BeNil())

				// This test assumes that there is only one worker group.
				Expect(rayJob.Spec.RayClusterSpec.WorkerGroupSpecs).To(HaveLen(1))
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

		Describe("Retrying RayJob in K8sJobMode", Ordered, func() {
			ctx := context.Background()
			namespace := "default"
			rayJob := rayJobTemplate("rayjob-retry-test", namespace)
			rayJob.Spec.BackoffLimit = ptr.To[int32](1)
			rayCluster := &rayv1.RayCluster{}

			It("Verify RayJob spec", func() {
				// This test case simulates a retry scenario in the RayJob when:
				// (1) The submission mode is K8sJobMode.
				// (2) backoffLimit > 0
				// In this test, RayJob passes through the following states: New -> Initializing -> Running -> Retrying -> New
				Expect(rayJob.Spec.SubmissionMode).To(Equal(rayv1.K8sJobMode))
				Expect(*rayJob.Spec.BackoffLimit).To(Equal(int32(1)))
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
				Expect(rayCluster.Status.State).NotTo(Equal(rayv1.Ready)) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288

				updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
				updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)

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

			It("RayJobs's JobDeploymentStatus transitions from Running -> Retrying -> New -> Initializing", func() {
				// Update fake dashboard client to return job info with "Failed" status.
				//nolint:unparam // this is a mock and the function signature cannot change
				getJobInfo := func(context.Context, string) (*utils.RayJobInfo, error) {
					return &utils.RayJobInfo{JobStatus: rayv1.JobStatusFailed}, nil
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
				Expect(k8sClient.Status().Update(ctx, job)).Should(Succeed())

				// record the current cluster name
				oldClusterName := rayJob.Status.RayClusterName

				// RayJob transitions from Running -> Retrying -> New -> Initializing
				// We only check the final state "Initializing" because it's difficult to test transient states like "Retrying" and "New"
				Eventually(
					getRayJobDeploymentStatus(ctx, rayJob),
					time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusInitializing), "jobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)

				// validate the RayCluster is deleted on retry
				Eventually(
					func() bool {
						return apierrors.IsNotFound(getResourceFunc(ctx, client.ObjectKey{Name: oldClusterName, Namespace: namespace}, rayCluster)())
					},
					time.Second*3, time.Millisecond*500).Should(BeTrue())

				// validate the submitter Job is deleted on retry
				Eventually(
					func() bool {
						return apierrors.IsNotFound(getResourceFunc(ctx, common.RayJobK8sJobNamespacedName(rayJob), job)())
					},
					time.Second*3, time.Millisecond*500).Should(BeTrue())
			})

			It("In Initializing state, the RayCluster should eventually be created (attempt 2)", func() {
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

			It("Make RayCluster.Status.State to be rayv1.Ready (attempt 2)", func() {
				// The RayCluster is not 'Ready' yet because Pods are not running and ready.
				Expect(rayCluster.Status.State).NotTo(Equal(rayv1.Ready)) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288

				updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
				updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)

				// The RayCluster.Status.State should be Ready.
				Eventually(
					getClusterState(ctx, namespace, rayCluster.Name),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
			})

			It("RayJobs's JobDeploymentStatus transitions from Initializing to Running (attempt 2)", func() {
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

			It("RayJobs's JobDeploymentStatus transitions from Running -> Complete (attempt 2)", func() {
				// Update fake dashboard client to return job info with "Failed" status.
				//nolint:unparam // this is a mock and the function signature cannot change
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
				Expect(k8sClient.Status().Update(ctx, job)).Should(Succeed())

				// RayJob transitions from Running -> Complete
				Eventually(
					getRayJobDeploymentStatus(ctx, rayJob),
					time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusComplete), "jobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)
			})

			It("Validate RayJob succeeded and failed status", func() {
				Eventually(
					getRayJobSucceededStatus(ctx, rayJob),
					time.Second*5, time.Millisecond*500).Should(Equal(int32(1)), "succeeded = %v", rayJob.Status.Succeeded)

				Eventually(
					getRayJobFailedStatus(ctx, rayJob),
					time.Second*5, time.Millisecond*500).Should(Equal(int32(1)), "failed = %v", rayJob.Status.Failed)
			})
		})
	})

	Context("RayJob in InteractiveMode", func() {
		Describe("Successful RayJob", Ordered, func() {
			ctx := context.Background()
			namespace := "default"
			rayJob := rayJobTemplate("rayjob-test-none-mode", namespace)
			rayJob.Spec.SubmissionMode = rayv1.InteractiveMode
			rayCluster := &rayv1.RayCluster{}
			testRayJobId := "fake-id"

			It("Verify RayJob spec", func() {
				Expect(rayJob.Spec.SubmissionMode).To(Equal(rayv1.InteractiveMode))
				Expect(rayJob.Spec.ShutdownAfterJobFinishes).To(BeTrue())
				Expect(rayJob.Spec.RayClusterSpec.WorkerGroupSpecs).To(HaveLen(1))
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

				Expect(rayJob.Status.RayClusterName).NotTo(BeEmpty())
				Expect(rayJob.Status.JobId).To(BeEmpty())
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
				Expect(rayCluster.Status.State).NotTo(Equal(rayv1.Ready)) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288

				updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
				updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)

				// The RayCluster.Status.State should be Ready.
				Eventually(
					getClusterState(ctx, namespace, rayCluster.Name),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
			})

			It("RayJobs's JobDeploymentStatus transitions from Initializing to Waiting.", func() {
				Eventually(
					getRayJobDeploymentStatus(ctx, rayJob),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusWaiting), "JobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)
			})

			It("sets jobId in RayJob", func() {
				err := setJobIdOnRayJob(ctx, rayJob, testRayJobId)
				Expect(err).NotTo(HaveOccurred())
			})

			It("RayJobs's JobDeploymentStatus transitions from Waiting to Running if annotation is set.", func() {
				Eventually(
					getRayJobDeploymentStatus(ctx, rayJob),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusRunning), "JobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)
			})

			It("should set RayJob's JobId to the value of the annotation", func() {
				Expect(rayJob.Status.JobId).To(Equal(testRayJobId))
			})
		})
	})

	Describe("RayJob with DeletionPolicy", Ordered, func() {
		JustBeforeEach(func() {
			features.SetFeatureGateDuringTest(GinkgoTB(), features.RayJobDeletionPolicy, true)
		})

		It("DeletionPolicy=DeleteCluster", func() {
			ctx := context.Background()
			namespace := "default"
			rayJob := rayJobTemplate("rayjob-test-deletionpolicy-deletecluster", namespace)
			deletionPolicy := rayv1.DeleteClusterDeletionPolicy
			rayJob.Spec.DeletionPolicy = &deletionPolicy
			rayJob.Spec.ShutdownAfterJobFinishes = false
			rayCluster := &rayv1.RayCluster{}

			By("Verify RayJob spec", func() {
				Expect(*rayJob.Spec.DeletionPolicy).To(Equal(rayv1.DeleteClusterDeletionPolicy))
			})

			By("Create a RayJob custom resource", func() {
				err := k8sClient.Create(ctx, rayJob)
				Expect(err).NotTo(HaveOccurred(), "Failed to create RayJob")
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: namespace}, rayJob),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "Should be able to see RayJob: %v", rayJob.Name)
			})

			By("RayJobs's JobDeploymentStatus transitions from New to Initializing.", func() {
				Eventually(
					getRayJobDeploymentStatus(ctx, rayJob),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusInitializing), "JobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)

				// In Initializing state, Status.RayClusterName, Status.JobId, and Status.StartTime must be set.
				Expect(rayJob.Status.RayClusterName).NotTo(BeEmpty())
				Expect(rayJob.Status.JobId).NotTo(BeEmpty())
				Expect(rayJob.Status.StartTime).NotTo(BeNil())
			})

			By("In Initializing state, the RayCluster should eventually be created.", func() {
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

			By("Make RayCluster.Status.State to be rayv1.Ready", func() {
				// The RayCluster is not 'Ready' yet because Pods are not running and ready.
				Expect(rayCluster.Status.State).NotTo(Equal(rayv1.Ready)) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288

				updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
				updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)

				// The RayCluster.Status.State should be Ready.
				Eventually(
					getClusterState(ctx, namespace, rayCluster.Name),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
			})

			By("RayJobs's JobDeploymentStatus transitions from Initializing to Running.", func() {
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

			By("RayJobs's JobDeploymentStatus transitions from Running to Complete.", func() {
				// Update fake dashboard client to return job info with "Succeeded" status.
				getJobInfo := func(context.Context, string) (*utils.RayJobInfo, error) { //nolint:unparam // This is a mock function so parameters are required
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
				Expect(k8sClient.Status().Update(ctx, job)).Should(Succeed())

				// RayJob transitions to Complete.
				Eventually(
					getRayJobDeploymentStatus(ctx, rayJob),
					time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusComplete), "jobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)
			})

			By("If DeletionPolicy=DeleteCluster, RayCluster should be deleted, but not the submitter Job.", func() {
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

		It("DeletionPolicy=DeleteWorkers", func() {
			ctx := context.Background()
			namespace := "default"
			rayJob := rayJobTemplate("rayjob-test-deletionpolicy-deleteworkers", namespace)
			deletionPolicy := rayv1.DeleteWorkersDeletionPolicy
			rayJob.Spec.DeletionPolicy = &deletionPolicy
			rayJob.Spec.ShutdownAfterJobFinishes = false
			rayCluster := &rayv1.RayCluster{}

			By("Verify RayJob spec", func() {
				Expect(*rayJob.Spec.DeletionPolicy).To(Equal(rayv1.DeleteWorkersDeletionPolicy))
			})

			By("Create a RayJob custom resource", func() {
				err := k8sClient.Create(ctx, rayJob)
				Expect(err).NotTo(HaveOccurred(), "Failed to create RayJob")
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: namespace}, rayJob),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "Should be able to see RayJob: %v", rayJob.Name)
			})

			By("RayJobs's JobDeploymentStatus transitions from New to Initializing.", func() {
				Eventually(
					getRayJobDeploymentStatus(ctx, rayJob),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusInitializing), "JobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)

				// In Initializing state, Status.RayClusterName, Status.JobId, and Status.StartTime must be set.
				Expect(rayJob.Status.RayClusterName).NotTo(BeEmpty())
				Expect(rayJob.Status.JobId).NotTo(BeEmpty())
				Expect(rayJob.Status.StartTime).NotTo(BeNil())
			})

			By("In Initializing state, the RayCluster should eventually be created.", func() {
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

			By("Make RayCluster.Status.State to be rayv1.Ready", func() {
				// The RayCluster is not 'Ready' yet because Pods are not running and ready.
				Expect(rayCluster.Status.State).NotTo(Equal(rayv1.Ready)) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288

				updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
				updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)

				// The RayCluster.Status.State should be Ready.
				Eventually(
					getClusterState(ctx, namespace, rayCluster.Name),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
			})

			By("RayJobs's JobDeploymentStatus transitions from Initializing to Running.", func() {
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

			By("RayJobs's JobDeploymentStatus transitions from Running to Complete.", func() {
				// Update fake dashboard client to return job info with "Succeeded" status.
				getJobInfo := func(context.Context, string) (*utils.RayJobInfo, error) { //nolint:unparam // This is a mock function so parameters are required
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
				Expect(k8sClient.Status().Update(ctx, job)).Should(Succeed())

				// RayJob transitions to Complete.
				Eventually(
					getRayJobDeploymentStatus(ctx, rayJob),
					time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusComplete), "jobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)
			})

			By("If DeletionPolicy=DeleteWorkers, all workers should be deleted, but not the Head pod and submitter Job", func() {
				// RayCluster exists
				Consistently(
					getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Status.RayClusterName, Namespace: namespace}, rayCluster),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "RayCluster %v not found", rayJob.Status.RayClusterName)

				// Check worker group is suspended
				Expect(*rayCluster.Spec.WorkerGroupSpecs[0].Suspend).To(BeTrue())

				// 0 worker Pods exist
				workerPods := corev1.PodList{}
				workerLabels := common.RayClusterWorkerPodsAssociationOptions(rayCluster).ToListOptions()
				Eventually(
					listResourceFunc(ctx, &workerPods, workerLabels...),
					time.Second*3, time.Millisecond*500).Should(Equal(0), "expected 0 workers")

				// Head Pod is still running
				headPods := corev1.PodList{}
				headLabels := common.RayClusterHeadPodsAssociationOptions(rayCluster).ToListOptions()
				Consistently(
					listResourceFunc(ctx, &headPods, headLabels...),
					time.Second*3, time.Millisecond*500).Should(Equal(1), "Head pod list should have only 1 Pod = %v", headPods.Items)

				namespacedName := common.RayJobK8sJobNamespacedName(rayJob)
				job := &batchv1.Job{}
				Consistently(
					getResourceFunc(ctx, namespacedName, job),
					time.Second*3, time.Millisecond*500).Should(BeNil())
			})
		})

		It("DeletionPolicy=DeleteSelf", func() {
			ctx := context.Background()
			namespace := "default"
			rayJob := rayJobTemplate("rayjob-test-deleteself", namespace)
			deletionPolicy := rayv1.DeleteSelfDeletionPolicy
			rayJob.Spec.DeletionPolicy = &deletionPolicy
			rayJob.Spec.ShutdownAfterJobFinishes = false
			rayCluster := &rayv1.RayCluster{}

			By("Create a RayJob custom resource", func() {
				err := k8sClient.Create(ctx, rayJob)
				Expect(err).NotTo(HaveOccurred(), "Failed to create RayJob")
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: namespace}, rayJob),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "Should be able to see RayJob: %v", rayJob.Name)
			})

			By("RayJobs's JobDeploymentStatus transitions from New to Initializing.", func() {
				Eventually(
					getRayJobDeploymentStatus(ctx, rayJob),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusInitializing), "JobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)

				// In Initializing state, Status.RayClusterName, Status.JobId, and Status.StartTime must be set.
				Expect(rayJob.Status.RayClusterName).NotTo(BeEmpty())
				Expect(rayJob.Status.JobId).NotTo(BeEmpty())
				Expect(rayJob.Status.StartTime).NotTo(BeNil())
			})

			By("In Initializing state, the RayCluster should eventually be created.", func() {
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

			By("Make RayCluster.Status.State to be rayv1.Ready", func() {
				// The RayCluster is not 'Ready' yet because Pods are not running and ready.
				Expect(rayCluster.Status.State).NotTo(Equal(rayv1.Ready)) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288

				updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
				updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)

				// The RayCluster.Status.State should be Ready.
				Eventually(
					getClusterState(ctx, namespace, rayCluster.Name),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
			})

			By("RayJobs's JobDeploymentStatus transitions from Initializing to Running.", func() {
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

			By("RayJobs's JobDeploymentStatus transitions from Running to Complete.", func() {
				// Update fake dashboard client to return job info with "Succeeded" status.
				getJobInfo := func(context.Context, string) (*utils.RayJobInfo, error) { //nolint:unparam // This is a mock function so parameters are required
					return &utils.RayJobInfo{JobStatus: rayv1.JobStatusSucceeded}, nil
				}
				fakeRayDashboardClient.GetJobInfoMock.Store(&getJobInfo)

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
				Expect(k8sClient.Status().Update(ctx, job)).Should(Succeed())
			})

			By("If DeletionPolicy=DeleteSelf, the RayJob is deleted", func() {
				Eventually(
					func() bool {
						return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: namespace}, rayJob))
					}, time.Second*5, time.Millisecond*500).Should(BeTrue())
			})
		})

		It("DeletionPolicy=DeleteNone", func() {
			ctx := context.Background()
			namespace := "default"
			rayJob := rayJobTemplate("rayjob-test-deletionpolicy-deletenone", namespace)
			deletionPolicy := rayv1.DeleteNoneDeletionPolicy
			rayJob.Spec.DeletionPolicy = &deletionPolicy
			rayJob.Spec.ShutdownAfterJobFinishes = false
			rayCluster := &rayv1.RayCluster{}

			By("Verify RayJob spec", func() {
				Expect(*rayJob.Spec.DeletionPolicy).To(Equal(rayv1.DeleteNoneDeletionPolicy))
			})

			By("Create a RayJob custom resource", func() {
				err := k8sClient.Create(ctx, rayJob)
				Expect(err).NotTo(HaveOccurred(), "Failed to create RayJob")
				Eventually(
					getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: namespace}, rayJob),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "Should be able to see RayJob: %v", rayJob.Name)
			})

			By("RayJobs's JobDeploymentStatus transitions from New to Initializing.", func() {
				Eventually(
					getRayJobDeploymentStatus(ctx, rayJob),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusInitializing), "JobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)

				// In Initializing state, Status.RayClusterName, Status.JobId, and Status.StartTime must be set.
				Expect(rayJob.Status.RayClusterName).NotTo(BeEmpty())
				Expect(rayJob.Status.JobId).NotTo(BeEmpty())
				Expect(rayJob.Status.StartTime).NotTo(BeNil())
			})

			By("In Initializing state, the RayCluster should eventually be created.", func() {
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

			By("Make RayCluster.Status.State to be rayv1.Ready", func() {
				// The RayCluster is not 'Ready' yet because Pods are not running and ready.
				Expect(rayCluster.Status.State).NotTo(Equal(rayv1.Ready)) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288

				updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
				updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)

				// The RayCluster.Status.State should be Ready.
				Eventually(
					getClusterState(ctx, namespace, rayCluster.Name),
					time.Second*3, time.Millisecond*500).Should(Equal(rayv1.Ready))
			})

			By("RayJobs's JobDeploymentStatus transitions from Initializing to Running.", func() {
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

			By("RayJobs's JobDeploymentStatus transitions from Running to Complete.", func() {
				// Update fake dashboard client to return job info with "Succeeded" status.
				getJobInfo := func(context.Context, string) (*utils.RayJobInfo, error) { //nolint:unparam // This is a mock function so parameters are required
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
				Expect(k8sClient.Status().Update(ctx, job)).Should(Succeed())

				// RayJob transitions to Complete.
				Eventually(
					getRayJobDeploymentStatus(ctx, rayJob),
					time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusComplete), "jobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)
			})

			By("If DeletionPolicy=DeleteNone, no resources are deleted", func() {
				// RayJob exists
				Consistently(
					getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: namespace}, rayJob),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "RayJob %v not found", rayJob)

				// RayCluster exists
				Consistently(
					getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Status.RayClusterName, Namespace: namespace}, rayCluster),
					time.Second*3, time.Millisecond*500).Should(BeNil(), "RayCluster %v not found", rayJob.Status.RayClusterName)

				// Worker replicas set to 3
				Expect(*rayCluster.Spec.WorkerGroupSpecs[0].Replicas).To(Equal(int32(3)))

				// 3 worker Pods exist
				workerPods := corev1.PodList{}
				workerLabels := common.RayClusterWorkerPodsAssociationOptions(rayCluster).ToListOptions()
				Consistently(
					listResourceFunc(ctx, &workerPods, workerLabels...),
					time.Second*3, time.Millisecond*500).Should(Equal(3), "expected 3 workers")

				// Head Pod is still running
				headPods := corev1.PodList{}
				headLabels := common.RayClusterHeadPodsAssociationOptions(rayCluster).ToListOptions()
				Consistently(
					listResourceFunc(ctx, &headPods, headLabels...),
					time.Second*3, time.Millisecond*500).Should(Equal(1), "Head pod list should have only 1 Pod = %v", headPods.Items)

				namespacedName := common.RayJobK8sJobNamespacedName(rayJob)
				job := &batchv1.Job{}
				Consistently(
					getResourceFunc(ctx, namespacedName, job),
					time.Second*3, time.Millisecond*500).Should(BeNil())
			})
		})
	})
})
