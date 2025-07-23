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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

var _ = Context("RayJob with schedule operation", func() {
	Describe("When creating a RayJob with a schedule field and NO cluster deletion", Ordered, func() {
		// The states should transition from Scheduled -> ... -> Initializing  ->  Running -> Complete -> Scheduled
		// In the last scheduled state the cluster should still exist since ShutdownAfterJobFinishes is False
		ctx := context.Background()
		namespace := "default"
		cronSchedule := "*/1 * * * *"
		rayJob := rayJobTemplate("rayjob-scheduled-no-deletion", namespace)
		rayJob.Spec.Schedule = cronSchedule
		rayJob.Spec.ShutdownAfterJobFinishes = false
		rayCluster := &rayv1.RayCluster{}

		It("Verify RayJob spec", func() {
			Expect(rayJob.Spec.ShutdownAfterJobFinishes).To(BeFalse())
		})

		It("should create a RayJob object with the schedule", func() {
			err := k8sClient.Create(ctx, rayJob)
			Expect(err).NotTo(HaveOccurred(), "failed to create test scheduled RayJob resource")
		})

		// We dont control the time till next schedule so it could schedule then immediately run the job which which can cause errors without the Or
		It("should have a JobDeploymentStatus reflecting its scheduled, new, or ", func() {
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*5).Should(
				Or(
					Equal(rayv1.JobDeploymentStatusScheduled),
					Equal(rayv1.JobDeploymentStatusNew),
					Equal(rayv1.JobDeploymentStatusInitializing),
				),
				"JobDeploymentStatus should be Scheduled, New or Initializing",
			)
		})

		// The cron job runs every minute so it will take at most 1 minute to run
		It("should transition to the Initializing", func() {
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*60).Should(Equal(rayv1.JobDeploymentStatusInitializing),
				"JobDeploymentStatus should be Initializing")
		})

		It("should create a raycluster object", func() {
			Eventually(
				getRayClusterNameForRayJob(ctx, rayJob),
				time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()))
			Eventually(

				getResourceFunc(ctx, common.RayJobRayClusterNamespacedName(rayJob), rayCluster),
				time.Second*3, time.Millisecond*500).Should(Succeed())
		})

		It("should NOT create the underlying K8s job yet because the cluster is not ready", func() {
			underlyingK8sJob := &batchv1.Job{}
			Consistently(
				// k8sClient client throws error if resource not found
				func() bool {
					err := getResourceFunc(ctx, common.RayJobK8sJobNamespacedName(rayJob), underlyingK8sJob)()
					return errors.IsNotFound(err)
				},
				time.Second*3, time.Millisecond*500).Should(BeTrue())
		})

		It("should be able to update all Pods to Running", func() {
			updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
			updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
		})

		It("Dashboard URL should be set", func() {
			Eventually(
				getDashboardURLForRayJob(ctx, rayJob),
				time.Second*3, time.Millisecond*500).Should(HavePrefix(rayJob.Name), "Dashboard URL = %v", rayJob.Status.DashboardURL)
		})

		It("should create the underlying Kubernetes Job object", func() {
			underlyingK8sJob := &batchv1.Job{}
			// The underlying Kubernetes Job should be created when the RayJob is created
			Eventually(
				getResourceFunc(ctx, common.RayJobK8sJobNamespacedName(rayJob), underlyingK8sJob),
				time.Second*3, time.Millisecond*500).Should(Succeed(), "Expected Kubernetes job to be present")
		})

		It("should transition to the Running", func() {
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusRunning),
				"JobDeploymentStatus should be Running")
		})

		It("RayJobs's JobDeploymentStatus transitions to Scheduled after Job is Complete.", func() {
			// Update fake dashboard client to return job info with "Succeeded" status.
			getJobInfo := func(context.Context, string) (*utils.RayJobInfo, error) { //nolint:unparam // This is a mock function so parameters are required
				return &utils.RayJobInfo{JobStatus: rayv1.JobStatusSucceeded, EndTime: uint64(time.Now().UnixMilli())}, nil
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

			// RayJob transitions to Scheduled.
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusScheduled), "jobDeploymentStatus = %v", rayJob.Status.JobDeploymentStatus)
		})

		It("The raycluster object should still exist", func() {
			Eventually(
				func() bool {
					err := getResourceFunc(ctx, client.ObjectKey{Name: rayJob.Status.RayClusterName, Namespace: namespace}, rayCluster)()
					return err == nil
				},
				time.Second*15, time.Millisecond*500).Should(BeTrue(), "Expected RayCluster to still exist")
		})
	})
})
