package e2e

import (
	"testing"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayJobLightWeightMode(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Job scripts
	jobsAC := newConfigMap(namespace.Name, files(test, "counter.py", "fail.py", "stop.py"))
	jobs, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), jobsAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", jobs.Namespace, jobs.Name)

	test.T().Run("Successful RayJob", func(_ *testing.T) {
		rayJobAC := rayv1ac.RayJob("counter", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithSubmissionMode(rayv1.HTTPMode).
				WithEntrypoint("python /home/ray/jobs/counter.py").
				WithEntrypointNumCpus(2).
				WithEntrypointNumGpus(2).
				WithEntrypointResources(`{"R1": 2}`).
				WithRuntimeEnvYAML(`
env_vars:
  counter_name: test_counter
`).
				WithShutdownAfterJobFinishes(true).
				WithRayClusterSpec(rayv1ac.RayClusterSpec().
					WithRayVersion(GetRayVersion()).
					WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
						WithRayStartParams(map[string]string{
							"dashboard-host": "0.0.0.0",
							"num-gpus":       "4",
							"num-cpus":       "4",
							"resources":      `'{"R1": 4}'`,
						}).
						WithTemplate(podTemplateSpecApplyConfiguration(headPodTemplateApplyConfiguration(),
							mountConfigMap[corev1ac.PodTemplateSpecApplyConfiguration](jobs, "/home/ray/jobs"))))))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

		// Assert the RayJob has completed successfully
		g.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusSucceeded)))

		// And the RayJob deployment status is updated accordingly
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name)).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

		// Refresh the RayJob status
		rayJob, err = GetRayJob(test, rayJob.Namespace, rayJob.Name)
		g.Expect(err).NotTo(HaveOccurred())

		// TODO (kevin85421): We may need to use `Eventually` instead if the assertion is flaky.
		// Assert the RayCluster has been torn down
		_, err = GetRayCluster(test, namespace.Name, rayJob.Status.RayClusterName)
		g.Expect(k8serrors.IsNotFound(err)).To(BeTrue())
	})

	test.T().Run("Failing RayJob without cluster shutdown after finished", func(_ *testing.T) {
		rayJobAC := rayv1ac.RayJob("fail", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithSubmissionMode(rayv1.HTTPMode).
				WithEntrypoint("python /home/ray/jobs/fail.py").
				WithShutdownAfterJobFinishes(false).
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

		// Assert the Ray job has failed
		g.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusFailed)))

		// Assert that the RayJob deployment status and RayJob reason have been updated accordingly.
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name)).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusFailed)))
		g.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobReason, Equal(rayv1.AppFailed)))

		// In the lightweight submission mode, the submitter Kubernetes Job should not be created.
		g.Eventually(Jobs(test, namespace.Name)).Should(BeEmpty())
	})

	test.T().Run("Should transition to 'Complete' if the Ray job has stopped.", func(_ *testing.T) {
		// `stop.py` will sleep for 20 seconds so that the RayJob has enough time to transition to `RUNNING`
		// and then stop the Ray job. If the Ray job is stopped, the RayJob should transition to `Complete`.
		rayJobAC := rayv1ac.RayJob("stop", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithSubmissionMode(rayv1.HTTPMode).
				WithEntrypoint("python /home/ray/jobs/stop.py").
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to be 'Running'", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusRunning)))

		test.T().Logf("Waiting for RayJob %s/%s to be 'Complete'", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

		// Refresh the RayJob status
		g.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusStopped)))

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)
	})
}
