package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayJob(t *testing.T) {
	test := With(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Job scripts
	jobs := newConfigMap(namespace.Name, "jobs", files(test, "counter.py", "fail.py", "stop.py"))
	jobs, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Create(test.Ctx(), jobs, metav1.CreateOptions{})
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", jobs.Namespace, jobs.Name)

	test.T().Run("Successful RayJob", func(t *testing.T) {
		// RayJob
		rayJob := &rayv1.RayJob{
			TypeMeta: metav1.TypeMeta{
				APIVersion: rayv1.GroupVersion.String(),
				Kind:       "RayJob",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "counter",
				Namespace: namespace.Name,
			},
			Spec: rayv1.RayJobSpec{
				RayClusterSpec: newRayClusterSpec(mountConfigMap[rayv1.RayClusterSpec](jobs, "/home/ray/jobs")),
				Entrypoint:     "python /home/ray/jobs/counter.py",
				RuntimeEnvYAML: `
env_vars:
  counter_name: test_counter
`,
				ShutdownAfterJobFinishes: true,
				SubmitterPodTemplate:     jobSubmitterPodTemplate(),
			},
		}
		rayJob, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Create(test.Ctx(), rayJob, metav1.CreateOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

		// Assert the RayJob has completed successfully
		test.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusSucceeded)))

		// And the RayJob deployment status is updated accordingly
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name)).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

		// Refresh the RayJob status
		rayJob = GetRayJob(test, rayJob.Namespace, rayJob.Name)

		// TODO (kevin85421): We may need to use `Eventually` instead if the assertion is flaky.
		// Assert the RayCluster has been torn down
		_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Get(test.Ctx(), rayJob.Status.RayClusterName, metav1.GetOptions{})
		test.Expect(err).To(MatchError(k8serrors.NewNotFound(rayv1.Resource("rayclusters"), rayJob.Status.RayClusterName)))

		// Assert the submitter Job has not been deleted
		test.Eventually(Jobs(test, namespace.Name)).ShouldNot(BeEmpty())

		// TODO (kevin85421): Check whether the Pods associated with the RayCluster and the submitter Job have been deleted.
		// For Kubernetes Jobs, the default deletion behavior is "orphanDependents," which means the Pods will not be
		// cascadingly deleted with the Kubernetes Job by default.

		test.T().Logf("Update `suspend` to true. However, since the RayJob is completed, the status should not be updated to `Suspended`.")
		rayJob.Spec.Suspend = true
		// TODO (kevin85421): We may need to retry `Update` if 409 conflict makes the test flaky.
		rayJob, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Update(test.Ctx(), rayJob, metav1.UpdateOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.Consistently(RayJob(test, rayJob.Namespace, rayJob.Name)).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)
	})

	test.T().Run("Failing RayJob without cluster shutdown after finished", func(t *testing.T) {
		// RayJob
		rayJob := &rayv1.RayJob{
			TypeMeta: metav1.TypeMeta{
				APIVersion: rayv1.GroupVersion.String(),
				Kind:       "RayJob",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "fail",
				Namespace: namespace.Name,
			},
			Spec: rayv1.RayJobSpec{
				RayClusterSpec:           newRayClusterSpec(mountConfigMap[rayv1.RayClusterSpec](jobs, "/home/ray/jobs")),
				Entrypoint:               "python /home/ray/jobs/fail.py",
				ShutdownAfterJobFinishes: false,
				SubmitterPodTemplate:     jobSubmitterPodTemplate(),
			},
		}
		rayJob, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Create(test.Ctx(), rayJob, metav1.CreateOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

		// Assert the Ray job has failed
		test.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusFailed)))

		// And the RayJob deployment status is updated accordingly
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name)).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

		// TODO (kevin85421): Ensure the RayCluster and Kubernetes Job are not deleted because `ShutdownAfterJobFinishes` is false.

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

	test.T().Run("Failing submitter K8s Job", func(t *testing.T) {
		// RayJob
		rayJob := &rayv1.RayJob{
			TypeMeta: metav1.TypeMeta{
				APIVersion: rayv1.GroupVersion.String(),
				Kind:       "RayJob",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "fail-k8s-job",
				Namespace: namespace.Name,
			},
			Spec: rayv1.RayJobSpec{
				RayClusterSpec:           newRayClusterSpec(mountConfigMap[rayv1.RayClusterSpec](jobs, "/home/ray/jobs")),
				Entrypoint:               "The command will be overridden by the submitter Job",
				ShutdownAfterJobFinishes: true,
				SubmitterPodTemplate:     jobSubmitterPodTemplate(),
			},
		}
		// In this test, we try to simulate the case where the submitter Job can't connect to the RayCluster successfully.
		// Hence, KubeRay can't get the Ray job information from the RayCluster. When the submitter Job reaches the backoff
		// limit, it will be marked as failed. Then, the RayJob should transition to `Complete`.
		rayJob.Spec.SubmitterPodTemplate.Spec.Containers[0].Command = []string{"ray", "job", "submit", "--address", "http://do-not-exist:8265", "--", "echo 123"}

		rayJob, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Create(test.Ctx(), rayJob, metav1.CreateOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Equal(rayv1.JobStatusNew)))

		// Refresh the RayJob status
		rayJob = GetRayJob(test, rayJob.Namespace, rayJob.Name)

		// Assert the RayCluster has been deleted because ShutdownAfterJobFinishes is true.
		test.Eventually(NotFound(RayClusterOrError(test, namespace.Name, rayJob.Status.RayClusterName)), TestTimeoutMedium).
			Should(BeTrue())
		// Asset submitter Job is not deleted yet
		test.Eventually(Jobs(test, namespace.Name)).ShouldNot(BeEmpty())

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)
	})

	test.T().Run("Should transition to 'Complete' if the Ray job has stopped.", func(t *testing.T) {
		// `stop.py` will sleep for 20 seconds so that the RayJob has enough time to transition to `RUNNING`
		// and then stop the Ray job. If the Ray job is stopped, the RayJob should transition to `Complete`.
		rayJobAC := rayv1ac.RayJob("stop", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithEntrypoint("python /home/ray/jobs/stop.py").
				WithSubmitterPodTemplate(jobSubmitterPodTemplateApplyConfiguration()).
				WithRayClusterSpec(rayv1ac.RayClusterSpec().
					WithRayVersion(GetRayVersion()).
					WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
						WithRayStartParams(map[string]string{
							"dashboard-host": "0.0.0.0",
						}).
						WithTemplate(podTemplateSpecApplyConfiguration(headPodTemplateApplyConfiguration(),
							mountConfigMap[corev1ac.PodTemplateSpecApplyConfiguration](jobs, "/home/ray/jobs"))))))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to be 'Running'", rayJob.Namespace, rayJob.Name)
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusRunning)))

		test.T().Logf("Waiting for RayJob %s/%s to be 'Complete'", rayJob.Namespace, rayJob.Name)
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

		// Refresh the RayJob status
		rayJob = GetRayJob(test, rayJob.Namespace, rayJob.Name)
		test.Expect(rayJob.Status.JobStatus).To(Equal(rayv1.JobStatusStopped))

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)
	})

	test.T().Run("RuntimeEnvYAML is not a valid YAML string", func(t *testing.T) {
		rayJobAC := rayv1ac.RayJob("invalid-yamlstr", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithEntrypoint("python /home/ray/jobs/counter.py").
				WithRuntimeEnvYAML(`invalid_yaml_string`).
				WithRayClusterSpec(rayv1ac.RayClusterSpec().
					WithRayVersion(GetRayVersion()).
					WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
						WithRayStartParams(map[string]string{
							"dashboard-host": "0.0.0.0",
						}).
						WithTemplate(podTemplateSpecApplyConfiguration(headPodTemplateApplyConfiguration(),
							mountConfigMap[corev1ac.PodTemplateSpecApplyConfiguration](jobs, "/home/ray/jobs"))))))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		// `RuntimeEnvYAML` is not a valid YAML string, so the RayJob controller will not do anything with the CR.
		test.Consistently(RayJob(test, rayJob.Namespace, rayJob.Name), 5*time.Second).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusNew)))
	})
}
