package e2e

import (
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayJobRetry(t *testing.T) {
	test := With(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Job scripts
	jobsAC := newConfigMap(namespace.Name, "jobs", files(test, "fail.py"))
	jobs, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), jobsAC, TestApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", jobs.Namespace, jobs.Name)

	test.T().Run("Failing RayJob without cluster shutdown after finished", func(_ *testing.T) {
		// RayJob: Set RayJob.BackoffLimit to 2
		rayJobAC := rayv1ac.RayJob("fail", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithBackoffLimit(2).
				WithSubmitterConfig(rayv1ac.SubmitterConfig().
					WithBackoffLimit(0)).
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))).
				WithEntrypoint("python /home/ray/jobs/fail.py").
				WithShutdownAfterJobFinishes(false).
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)

		// Assert that the RayJob deployment status and RayJob reason have been updated accordingly.
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutLong).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusFailed)))
		test.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusFailed)))
		test.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobReason, Equal(rayv1.AppFailed)))
		test.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusFailed)))

		// Check whether the controller respects the backoffLimit.
		test.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			Should(WithTransform(RayJobFailed, Equal(int32(3))))
		test.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			Should(WithTransform(RayJobSucceeded, Equal(int32(0))))

		// Refresh the RayJob status
		rayJob = GetRayJob(test, rayJob.Namespace, rayJob.Name)

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		// Assert the RayCluster has been cascade deleted
		test.Eventually(NotFound(RayClusterOrError(test, namespace.Name, rayJob.Status.RayClusterName))).
			Should(BeTrue())

		// Assert the submitter Job has been cascade deleted
		test.Eventually(Jobs(test, namespace.Name)).Should(BeEmpty())
	})

	// test.T().Run("Failing submitter K8s Job", func(_ *testing.T) {
	// TODO: The K8s submitter Job fails to connect to the RayCluster due to misconfiguration.
	// This test is similar to the "Failing submitter K8s Job" test in rayjob_test.go. The difference
	// is that here we set RayJob.BackoffLimit.
	// })

	// test.T().Run("RayJob has passed ActiveDeadlineSeconds", func(_ *testing.T) {
	// TODO: Add a test case to verify that the RayJob has passed ActiveDeadlineSeconds
	// and ensure that the RayJob transitions to JobDeploymentStatusFailed
	// regardless of the value of backoffLimit. Refer to rayjob_test.go for an example.
	// })

	// test.T().Run("Failing RayJob in HTTPMode", func(_ *testing.T) {
	// TODO: The RayJob in HTTPMode fails and retries. This test is similar to the
	// "Failing RayJob without cluster shutdown after finished" test in rayjob_lightweight_test.go.
	// The difference is that here we set RayJob.BackoffLimit.
	// })
}
