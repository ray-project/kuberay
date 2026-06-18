package e2erayjob

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayJobWithClusterSelector(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Job scripts
	jobsAC := NewConfigMap(namespace.Name, Files(test, "counter.py", "fail.py"))
	jobs, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), jobsAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", jobs.Namespace, jobs.Name)

	// RayCluster
	rayClusterAC := rayv1ac.RayCluster("raycluster", namespace.Name).
		WithSpec(NewRayClusterSpec(MountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs")))

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	test.T().Run("Successful RayJob", func(t *testing.T) {
		t.Parallel()

		// RayJob
		rayJobAC := rayv1ac.RayJob("counter", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithClusterSelector(map[string]string{utils.RayClusterLabelKey: rayCluster.Name}).
				WithEntrypoint("python /home/ray/jobs/counter.py").
				WithRuntimeEnvYAML(`
env_vars:
  counter_name: test_counter
`).
				WithSubmitterPodTemplate(JobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

		// Assert the Ray job has completed successfully
		g.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusSucceeded)))
	})

	test.T().Run("Failing RayJob", func(t *testing.T) {
		t.Parallel()

		// RayJob
		rayJobAC := rayv1ac.RayJob("fail", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithClusterSelector(map[string]string{utils.RayClusterLabelKey: rayCluster.Name}).
				WithEntrypoint("python /home/ray/jobs/fail.py").
				WithShutdownAfterJobFinishes(false).
				WithSubmitterPodTemplate(JobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

		// Assert the Ray job has failed
		g.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusFailed)))
	})

	test.T().Run("RayJob should be created but not to be updated when managed externally", func(_ *testing.T) {
		t.Parallel()

		// RayJob
		rayJobAC := rayv1ac.RayJob("managed-externally", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithClusterSelector(map[string]string{utils.RayClusterLabelKey: rayCluster.Name}).
				WithEntrypoint("python /home/ray/jobs/counter.py").
				WithRuntimeEnvYAML(`
env_vars:
  counter_name: test_counter
`).
				WithSubmitterPodTemplate(JobSubmitterPodTemplateApplyConfiguration()).
				WithManagedBy("kueue.x-k8s.io/multikueue"))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		// Assert the Ray job status has not been updated
		g.Consistently(func(gg Gomega) {
			rayJob, err = GetRayJob(test, rayJob.Namespace, rayJob.Name)
			gg.Expect(err).ToNot(HaveOccurred())
			gg.Expect(rayJob.Status.JobDeploymentStatus).To(Equal(rayv1.JobDeploymentStatusNew))
		}, time.Second*3, time.Millisecond*500).Should(Succeed())
	})

	test.T().Run("Suspend and resume a clusterSelector RayJob", func(t *testing.T) {
		t.Parallel()

		// RayJob with a long-running entrypoint so we can suspend while it is running
		rayJobAC := rayv1ac.RayJob("suspend-cluster-selector", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithClusterSelector(map[string]string{utils.RayClusterLabelKey: rayCluster.Name}).
				WithEntrypoint("python /home/ray/jobs/long_running.py").
				WithShutdownAfterJobFinishes(true).
				WithSubmissionMode(rayv1.HTTPMode).
				WithSubmitterPodTemplate(JobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to be 'Running'", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusRunning)))

		// Suspend the RayJob
		LogWithTimestamp(test.T(), "Suspending RayJob %s/%s", rayJob.Namespace, rayJob.Name)
		rayJobAC.Spec.WithSuspend(true)
		rayJob, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())

		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to be 'Suspended'", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusSuspended)))

		// Assert the RayCluster still exists (clusterSelector does not delete the cluster)
		_, err = GetRayCluster(test, namespace.Name, rayCluster.Name)
		g.Expect(err).NotTo(HaveOccurred())

		// Assert suspended status fields
		rayJob, err = GetRayJob(test, rayJob.Namespace, rayJob.Name)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(rayJob.Status.JobStatus).To(Equal(rayv1.JobStatusStopped))
		g.Expect(rayJob.Status.JobId).To(BeEmpty())
		g.Expect(rayJob.Status.DashboardURL).NotTo(BeEmpty())
		g.Expect(rayJob.Status.RayClusterName).To(Equal(rayCluster.Name))

		// Resume the RayJob
		LogWithTimestamp(test.T(), "Resuming RayJob %s/%s", rayJob.Namespace, rayJob.Name)
		rayJobAC.Spec.WithSuspend(false)
		rayJob, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())

		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to be 'Running' again", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusRunning)))

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)
	})

	test.T().Run("RayJob should not be created due to managedBy invalid value", func(_ *testing.T) {
		// RayJob
		rayJobAC := rayv1ac.RayJob("managed-externally-invalid", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithClusterSelector(map[string]string{utils.RayClusterLabelKey: rayCluster.Name}).
				WithEntrypoint("python /home/ray/jobs/counter.py").
				WithRuntimeEnvYAML(`
env_vars:
  counter_name: test_counter
`).
				WithSubmitterPodTemplate(JobSubmitterPodTemplateApplyConfiguration()).
				WithManagedBy("invalid.com/controller"))

		_, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(errors.IsInvalid(err)).To(BeTrue(), "error: %v", err)
	})
}
