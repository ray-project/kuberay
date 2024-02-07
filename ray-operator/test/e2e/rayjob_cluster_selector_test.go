package e2e

import (
	"testing"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayJobWithClusterSelector(t *testing.T) {
	test := With(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Job scripts
	jobs := newConfigMap(namespace.Name, "jobs", files(test, "counter.py", "fail.py"))
	jobs, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Create(test.Ctx(), jobs, metav1.CreateOptions{})
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", jobs.Namespace, jobs.Name)

	// RayCluster
	rayCluster := &rayv1.RayCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rayv1.GroupVersion.String(),
			Kind:       "RayCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster",
			Namespace: namespace.Name,
		},
		Spec: *newRayClusterSpec(mountConfigMap[rayv1.RayClusterSpec](jobs, "/home/ray/jobs")),
	}
	rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Create(test.Ctx(), rayCluster, metav1.CreateOptions{})
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	test.T().Logf("Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	test.T().Run("Successful RayJob", func(t *testing.T) {
		t.Parallel()

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
				ClusterSelector: map[string]string{
					utils.RayClusterLabelKey: rayCluster.Name,
				},
				Entrypoint: "python /home/ray/jobs/counter.py",
				RuntimeEnvYAML: `
env_vars:
  counter_name: test_counter
`,
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

		// Assert the Ray job has completed successfully
		test.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusSucceeded)))
	})

	test.T().Run("Failing RayJob", func(t *testing.T) {
		t.Parallel()

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
				ClusterSelector: map[string]string{
					utils.RayClusterLabelKey: rayCluster.Name,
				},
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
	})
}
