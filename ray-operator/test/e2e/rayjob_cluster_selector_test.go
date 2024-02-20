package e2e

import (
	"testing"

	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayJobWithClusterSelector(t *testing.T) {
	test := With(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Job scripts
	jobsAC := newConfigMap(namespace.Name, "jobs", files(test, "counter.py", "fail.py"))
	jobs, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), jobsAC, TestApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", jobs.Namespace, jobs.Name)

	// RayCluster
	rayClusterAC := rayv1ac.RayCluster("raycluster", namespace.Name).
		WithSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs")))

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	test.T().Logf("Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
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
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

		// Assert the Ray job has completed successfully
		test.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
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
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

		// Assert the Ray job has failed
		test.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusFailed)))
	})
}
