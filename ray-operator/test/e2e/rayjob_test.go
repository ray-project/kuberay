package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayJob(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Job scripts
	jobsAC := newConfigMap(namespace.Name, files(test, "counter.py", "fail.py", "stop.py", "long_running.py"))
	jobs, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), jobsAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", jobs.Namespace, jobs.Name)

	test.T().Run("Successful RayJob", func(_ *testing.T) {
		// RayJob
		rayJobAC := rayv1ac.RayJob("counter", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))).
				WithEntrypoint("python /home/ray/jobs/counter.py").
				WithRuntimeEnvYAML(`
env_vars:
  counter_name: test_counter
`).
				WithShutdownAfterJobFinishes(true).
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
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

		// Assert the RayCluster has been torn down
		g.Eventually(func() error {
			_, err = GetRayCluster(test, namespace.Name, rayJob.Status.RayClusterName)
			return err
		}, TestTimeoutShort).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

		// Assert the submitter Job has not been deleted
		g.Eventually(Jobs(test, namespace.Name)).ShouldNot(BeEmpty())

		// TODO (kevin85421): Check whether the Pods associated with the RayCluster and the submitter Job have been deleted.
		// For Kubernetes Jobs, the default deletion behavior is "orphanDependents," which means the Pods will not be
		// cascadingly deleted with the Kubernetes Job by default.

		LogWithTimestamp(test.T(), "Update `suspend` to true. However, since the RayJob is completed, the status should not be updated to `Suspended`.")
		rayJobAC.Spec.WithSuspend(true)
		rayJob, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		g.Consistently(RayJob(test, rayJob.Namespace, rayJob.Name)).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)
	})

	test.T().Run("Failing RayJob without cluster shutdown after finished", func(_ *testing.T) {
		// RayJob
		rayJobAC := rayv1ac.RayJob("fail", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))).
				WithEntrypoint("python /home/ray/jobs/fail.py").
				WithShutdownAfterJobFinishes(false).
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
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

		// TODO (kevin85421): Ensure the RayCluster and Kubernetes Job are not deleted because `ShutdownAfterJobFinishes` is false.

		// Refresh the RayJob status
		rayJob, err = GetRayJob(test, rayJob.Namespace, rayJob.Name)
		g.Expect(err).NotTo(HaveOccurred())

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		// Assert the RayCluster has been cascade deleted
		g.Eventually(func() error {
			_, err := GetRayCluster(test, namespace.Name, rayJob.Status.RayClusterName)
			return err
		}).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

		// Assert the submitter Job has been cascade deleted
		g.Eventually(Jobs(test, namespace.Name)).Should(BeEmpty())
	})

	test.T().Run("Failing submitter K8s Job", func(_ *testing.T) {
		// RayJob
		rayJobAC := rayv1ac.RayJob("fail-k8s-job", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))).
				WithEntrypoint("The command will be overridden by the submitter Job").
				WithShutdownAfterJobFinishes(true).
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()))

		// In this test, we try to simulate the case where the submitter Job can't connect to the RayCluster successfully.
		// Hence, KubeRay can't get the Ray job information from the RayCluster. When the submitter Job reaches the backoff
		// limit, it will be marked as failed. Then, the RayJob should transition to `Complete`.
		rayJobAC.Spec.SubmitterPodTemplate.Spec.Containers[0].WithCommand("ray", "job", "submit", "--address", "http://do-not-exist:8265", "--", "echo 123")

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusFailed)))
		g.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusNew)))
		g.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobReason, Equal(rayv1.SubmissionFailed)))

		// Refresh the RayJob status
		rayJob, err = GetRayJob(test, rayJob.Namespace, rayJob.Name)
		g.Expect(err).NotTo(HaveOccurred())

		// Assert the RayCluster has been deleted because ShutdownAfterJobFinishes is true.
		g.Eventually(func() error {
			_, err := GetRayCluster(test, namespace.Name, rayJob.Status.RayClusterName)
			return err
		}).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))
		// Asset submitter Job is not deleted yet
		g.Eventually(Jobs(test, namespace.Name)).ShouldNot(BeEmpty())

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)
	})

	test.T().Run("Should transition to 'Complete' if the Ray job has stopped.", func(_ *testing.T) {
		// `stop.py` will sleep for 20 seconds so that the RayJob has enough time to transition to `RUNNING`
		// and then stop the Ray job. If the Ray job is stopped, the RayJob should transition to `Complete`.
		rayJobAC := rayv1ac.RayJob("stop", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithEntrypoint("python /home/ray/jobs/stop.py").
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()).
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to be 'Running'", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusRunning)))

		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to be 'Complete'", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

		// Refresh the RayJob status
		g.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusStopped)))

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)
	})

	test.T().Run("RuntimeEnvYAML is not a valid YAML string", func(_ *testing.T) {
		rayJobAC := rayv1ac.RayJob("invalid-yamlstr", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithEntrypoint("python /home/ray/jobs/counter.py").
				WithRuntimeEnvYAML(`invalid_yaml_string`).
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		// `RuntimeEnvYAML` is not a valid YAML string, so the RayJob controller will not do anything with the CR.
		g.Consistently(RayJob(test, rayJob.Namespace, rayJob.Name), 5*time.Second).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusNew)))
	})

	test.T().Run("RayJob has passed ActiveDeadlineSeconds", func(_ *testing.T) {
		rayJobAC := rayv1ac.RayJob("long-running", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))).
				WithEntrypoint("python /home/ray/jobs/long_running.py").
				WithShutdownAfterJobFinishes(true).
				WithTTLSecondsAfterFinished(600).
				WithActiveDeadlineSeconds(5).
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		// The RayJob will transition to `Complete` because it has passed `ActiveDeadlineSeconds`.
		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to be 'Complete'", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutShort).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusFailed)))
		g.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobReason, Equal(rayv1.DeadlineExceeded)))
	})

	test.T().Run("RayJob should be created, but not updated when managed externally", func(_ *testing.T) {
		// RayJob
		rayJobAC := rayv1ac.RayJob("managed-externally", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithRayClusterSpec(newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))).
				WithEntrypoint("python /home/ray/jobs/counter.py").
				WithRuntimeEnvYAML(`
env_vars:
  counter_name: test_counter
`).
				WithShutdownAfterJobFinishes(true).
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()).
				WithManagedBy("kueue.x-k8s.io/multikueue"))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		// Should not to be able to change managedBy field as it's immutable
		rayJobAC.Spec.WithManagedBy(utils.KubeRayController)
		_, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).To(HaveOccurred())
		g.Eventually(RayJob(test, *rayJobAC.Namespace, *rayJobAC.Name)).
			Should(WithTransform(RayJobManagedBy, Equal(ptr.To("kueue.x-k8s.io/multikueue"))))

		// Refresh the RayJob status and assert it has not been updated
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name)).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusNew)))

		// Assert the associated RayCluster has not beed created
		rcList, err := test.Client().Ray().RayV1().RayClusters(rayJob.Namespace).List(test.Ctx(), metav1.ListOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		for _, rc := range rcList.Items {
			g.Expect(rc.Name).NotTo(HaveSuffix(*rayJobAC.Name))
		}

		// Assert the submitter Job has not been created
		g.Eventually(Jobs(test, namespace.Name)).Should(BeEmpty())

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), *rayJobAC.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Deleted RayJob %s/%s successfully", *rayJobAC.Namespace, *rayJobAC.Name)
	})
}
