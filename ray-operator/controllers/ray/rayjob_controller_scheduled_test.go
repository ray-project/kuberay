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
	// batchv1 "k8s.io/api/batch/v1"
	// "k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry" // For creating pointers to int32, string, bool etc.
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
)

func scheduledRayJobTemplate(name, namespace, schedule string) *rayv1.RayJob {
	job := rayJobTemplate(name, namespace)
	job.Spec.Schedule = schedule
	return job
}

var _ = Context("RayJob with schedule operation", func() {
	// This Describe block focuses on the lifecycle of a RayJob configured with a schedule.
	Describe("When creating a RayJob with a schedule field and NO cluster deletion", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		cronSchedule := "0 0 0 0 0"
		rayJob := scheduledRayJobTemplate("rayjob-scheduled-no-deletion", namespace, cronSchedule)
		// rayCluster := &rayv1.RayCluster{}

		It("should create a RayJob object with the schedule", func() {
			err := k8sClient.Create(ctx, rayJob)
			Expect(err).NotTo(HaveOccurred(), "failed to create test scheduled RayJob resource")
		})

		It("should have a JobDeploymentStatus reflecting its scheduled state", func() {
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusScheduled),
				"JobDeploymentStatus should be Scheduled")
		})

		It("should update the CronJob's schedule when the RayJob's schedule is modified", func() {
			newCronSchedule := "*/1 * * * *"
			updatedRayJob := &rayv1.RayJob{}
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: namespace}, updatedRayJob)
				if err != nil {
					return err
				}
				updatedRayJob.Spec.Schedule = newCronSchedule
				return k8sClient.Update(ctx, updatedRayJob)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update RayJob schedule")
		})

		// The cron job runs every minute so it will take at most 1 minute to run
		It("should transition to the New state", func() {
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*60).Should(Equal(rayv1.JobDeploymentStatusNew),
				"JobDeploymentStatus should be New")
		})
		// It("should create a raycluster object", func() {
		// 	// Ray Cluster name can be present on RayJob's CRD
		// 	Eventually(
		// 		getRayClusterNameForRayJob(ctx, rayJob),
		// 		time.Second*15, time.Millisecond*500).Should(Not(BeEmpty()))
		// 	// The actual cluster instance and underlying resources SHOULD be created when suspend == false
		// 	Eventually(

		// 		getResourceFunc(ctx, common.RayJobRayClusterNamespacedName(rayJob), rayCluster),
		// 		time.Second*3, time.Millisecond*500).Should(Succeed())
		// })

		// It("should NOT create the underlying K8s job yet because the cluster is not ready", func() {
		// 	underlyingK8sJob := &batchv1.Job{}
		// 	Consistently(
		// 		// k8sClient client throws error if resource not found
		// 		func() bool {
		// 			err := getResourceFunc(ctx, common.RayJobK8sJobNamespacedName(rayJob), underlyingK8sJob)()
		// 			return errors.IsNotFound(err)
		// 		},
		// 		time.Second*3, time.Millisecond*500).Should(BeTrue())
		// })

		// It("should be able to update all Pods to Running", func() {
		// 	updateHeadPodToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
		// 	updateWorkerPodsToRunningAndReady(ctx, rayJob.Status.RayClusterName, namespace)
		// })

		// It("Dashboard URL should be set", func() {
		// 	Eventually(
		// 		getDashboardURLForRayJob(ctx, rayJob),
		// 		time.Second*3, time.Millisecond*500).Should(HavePrefix(rayJob.Name), "Dashboard URL = %v", rayJob.Status.DashboardURL)
		// })

		// It("should create the underlying Kubernetes Job object", func() {
		// 	underlyingK8sJob := &batchv1.Job{}
		// 	// The underlying Kubernetes Job should be created when the RayJob is created
		// 	Eventually(
		// 		getResourceFunc(ctx, common.RayJobK8sJobNamespacedName(rayJob), underlyingK8sJob),
		// 		time.Second*3, time.Millisecond*500).Should(Succeed(), "Expected Kubernetes job to be present")
		// })

		// It("should transition to the Initailizing", func() {
		// 	Eventually(
		// 		getRayJobDeploymentStatus(ctx, rayJob),
		// 		time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusInitializing),
		// 		"JobDeploymentStatus should be Initializing")
		// })

		// It("should transition to the Running", func() {
		// 	Eventually(
		// 		getRayJobDeploymentStatus(ctx, rayJob),
		// 		time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusRunning),
		// 		"JobDeploymentStatus should be Initializing")
		// })

		// It("should transition to the Complete", func() {
		// 	Eventually(
		// 		getRayJobDeploymentStatus(ctx, rayJob),
		// 		time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusComplete),
		// 		"JobDeploymentStatus should be Initializing")
		// })

		// It("should transition to the Scheduled", func() {
		// 	Eventually(
		// 		getRayJobDeploymentStatus(ctx, rayJob),
		// 		time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusScheduled),
		// 		"JobDeploymentStatus should be Initializing")
		// })

		// It("The raycluster object should still exist", func() {
		// 	Eventually(

		// 		getResourceFunc(ctx, common.RayJobRayClusterNamespacedName(rayJob), rayCluster),
		// 		time.Second*3, time.Millisecond*500).Should(Succeed())
		// })
	})

	Describe("When creating a RayJob with a schedule field and WITH cluster deletion", Ordered, func() {
		ctx := context.Background()
		namespace := "default"
		cronSchedule := "*/1 * * * *"
		rayJob := scheduledRayJobTemplate("rayjob-scheduled-with-deletion", namespace, cronSchedule)

		It("should create a RayJob object with the schedule", func() {
			err := k8sClient.Create(ctx, rayJob)
			Expect(err).NotTo(HaveOccurred(), "failed to create test scheduled RayJob resource")
		})

		It("should have a JobDeploymentStatus reflecting its scheduled state", func() {
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*5, time.Millisecond*500).Should(Equal(rayv1.JobDeploymentStatusScheduled),
				"JobDeploymentStatus should be Scheduled")
		})

		It("should NOT create a RayCluster object immediately", func() {
			rayCluster := &rayv1.RayCluster{}
			Consistently(
				func() bool {
					err := k8sClient.Get(ctx, common.RayJobRayClusterNamespacedName(rayJob), rayCluster)
					return apierrors.IsNotFound(err)
				},
				time.Second*3, time.Millisecond*500).Should(BeTrue(), "RayCluster should NOT be created upon scheduled RayJob creation")
		})

		It("should update the CronJob's schedule when the RayJob's schedule is modified", func() {
			newCronSchedule := "0 1 * * *"
			updatedRayJob := &rayv1.RayJob{}
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: rayJob.Name, Namespace: namespace}, updatedRayJob)
				if err != nil {
					return err
				}
				updatedRayJob.Spec.Schedule = newCronSchedule
				return k8sClient.Update(ctx, updatedRayJob)
			})
			Expect(err).NotTo(HaveOccurred(), "failed to update RayJob schedule")
		})

		It("should transition to the New state", func() {
			Eventually(
				getRayJobDeploymentStatus(ctx, rayJob),
				time.Second*60).Should(Equal(rayv1.JobDeploymentStatusNew),
				"JobDeploymentStatus should be New")
		})
	})
})
