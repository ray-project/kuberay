package e2e

import (
	"testing"

	. "github.com/onsi/gomega"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayJobSuspend(t *testing.T) {
	test := With(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Job scripts
	jobs := newConfigMap(namespace.Name, "jobs", files(test, "long_running.py"))
	jobs, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Create(test.Ctx(), jobs, metav1.CreateOptions{})
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", jobs.Namespace, jobs.Name)

	test.T().Run("Suspend the RayJob when its status is 'Running', and then resume it.", func(t *testing.T) {
		// RayJob
		rayJob := &rayv1.RayJob{
			TypeMeta: metav1.TypeMeta{
				APIVersion: rayv1.GroupVersion.String(),
				Kind:       "RayJob",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "long-running",
				Namespace: namespace.Name,
			},
			Spec: rayv1.RayJobSpec{
				RayClusterSpec:           newRayClusterSpec(mountConfigMap(jobs, "/home/ray/jobs")),
				Entrypoint:               "python /home/ray/jobs/long_running.py",
				ShutdownAfterJobFinishes: true,
				TTLSecondsAfterFinished:  600,
				SubmitterPodTemplate:     jobSubmitterPodTemplate(),
			},
		}
		rayJob, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Create(test.Ctx(), rayJob, metav1.CreateOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		test.T().Logf("Waiting for RayJob %s/%s to be 'Running'", rayJob.Namespace, rayJob.Name)
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusRunning)))

		// Refresh the RayJob status
		rayJob = GetRayJob(test, rayJob.Namespace, rayJob.Name)

		test.T().Logf("Suspend the RayJob %s/%s", rayJob.Namespace, rayJob.Name)
		rayJob.Spec.Suspend = true
		// TODO (kevin85421): We may need to retry `Update` if 409 conflict makes the test flaky.
		rayJob, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Update(test.Ctx(), rayJob, metav1.UpdateOptions{})
		test.Expect(err).NotTo(HaveOccurred())

		test.T().Logf("Waiting for RayJob %s/%s to be 'Suspended'", rayJob.Namespace, rayJob.Name)
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusSuspended)))

		// TODO (kevin85421): We may need to use `Eventually` instead if the assertion is flaky.
		// Assert the RayCluster has been torn down
		_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Get(test.Ctx(), rayJob.Status.RayClusterName, metav1.GetOptions{})
		test.Expect(err).To(MatchError(k8serrors.NewNotFound(rayv1.Resource("rayclusters"), rayJob.Status.RayClusterName)))

		// Assert the submitter Job has been cascade deleted
		test.Eventually(Jobs(test, namespace.Name)).Should(BeEmpty())

		// TODO (kevin85421): Check whether the Pods associated with the RayCluster and the submitter Job have been deleted.
		// For Kubernetes Jobs, the default deletion behavior is "orphanDependents," which means the Pods will not be
		// cascadingly deleted with the Kubernetes Job by default.

		// Refresh the RayJob status
		rayJob = GetRayJob(test, rayJob.Namespace, rayJob.Name)

		test.T().Logf("Resume the RayJob by updating `suspend` to false.")
		rayJob.Spec.Suspend = false
		// TODO (kevin85421): We may need to retry `Update` if 409 conflict makes the test flaky.
		rayJob, err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Update(test.Ctx(), rayJob, metav1.UpdateOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusRunning)))

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)
	})
}
