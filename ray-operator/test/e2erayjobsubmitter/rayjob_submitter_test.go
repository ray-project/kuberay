package e2erayjobsubmitter

import (
	"io"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

const (
	SubmitterImage = "kuberay/submitter:nightly"
)

func TestRayJobSubmitter(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	TestScriptAC := NewConfigMap(namespace.Name, Files(test, "counter.py"))
	TestScript, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), TestScriptAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", TestScript.Namespace, TestScript.Name)
	// We need to specify Args and Command in order to use the light weight submitter.
	SubmitterPodTemplate := JobSubmitterPodTemplateApplyConfiguration()
	image := SubmitterImage
	SubmitterPodTemplate.Spec.Containers[0].Image = &image
	SubmitterPodTemplate.Spec.Containers[0].Command = []string{"/submitter"}
	SubmitterPodTemplate.Spec.Containers[0].Args = []string{"--runtime-env-json", `{"pip":["requests==2.26.0","pendulum==2.1.2"],"env_vars":{"counter_name":"test_counter"}}`, "--", "python", "/home/ray/jobs/counter.py"}

	test.T().Run("Successful RayJob with light weight submitter", func(_ *testing.T) {
		rayJobAC := rayv1ac.RayJob("successful-rayjob", namespace.Name).WithSpec(
			rayv1ac.RayJobSpec().
				WithRayClusterSpec(NewRayClusterSpec(MountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](TestScript, "/home/ray/jobs"))).
				WithSubmitterPodTemplate(SubmitterPodTemplate).
				WithShutdownAfterJobFinishes(true),
		)
		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

		// Assert the RayJob has completed successfully
		g.Expect(GetRayJob(test, rayJob.Namespace, rayJob.Name)).
			To(WithTransform(RayJobStatus, Equal(rayv1.JobStatusSucceeded)))

		// Check the RayJob deployment status is updated accordingly
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name)).
			Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))

		// Get and verify submitter pod logs
		submitterPods := Pods(test, namespace.Name, LabelSelector("job-name=successful-rayjob"))(g)
		g.Expect(submitterPods).NotTo(BeEmpty(), "Expected to find at least one submitter pod with label job-name=successful-rayjob")

		submitterPod := submitterPods[0]
		stream, err := test.Client().Core().CoreV1().Pods(namespace.Name).GetLogs(submitterPod.Name, &corev1.PodLogOptions{Container: "ray-job-submitter"}).Stream(test.Ctx())
		g.Expect(err).NotTo(HaveOccurred())
		defer stream.Close()

		logBytes, err := io.ReadAll(stream)
		g.Expect(err).NotTo(HaveOccurred())
		logContent := string(logBytes)

		// Verify the logs contain expected content
		g.Expect(logContent).To(ContainSubstring("test_counter got 1"))
		g.Expect(logContent).To(ContainSubstring("test_counter got 2"))
		g.Expect(logContent).To(ContainSubstring("test_counter got 3"))
		g.Expect(logContent).To(ContainSubstring("test_counter got 4"))
		g.Expect(logContent).To(ContainSubstring("test_counter got 5"))

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)
	})

	test.T().Run("Failed RayJob with light weight submitter", func(_ *testing.T) {
		failedSubmitterPodTemplate := SubmitterPodTemplate
		failedSubmitterPodTemplate.Spec.Containers[0].Args = []string{"--entrypoint-resources", `{"cpu":"Intentionally wrong value"}`}
		// To trigger the error, we intentionally set the entrypoint resources to an invalid value.
		rayJobAC := rayv1ac.RayJob("failed-rayjob", namespace.Name).WithSpec(
			rayv1ac.RayJobSpec().
				WithRayClusterSpec(NewRayClusterSpec(MountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](TestScript, "/home/ray/jobs"))).
				WithSubmitterPodTemplate(failedSubmitterPodTemplate).
				WithShutdownAfterJobFinishes(true),
		)
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

		submitterPods := Pods(test, namespace.Name, LabelSelector("job-name=failed-rayjob"))(g)
		g.Expect(submitterPods).To(HaveLen(3), "Expected to find exactly three submitter pods with label job-name=failed-rayjob")

		// Delete the RayJob
		err = test.Client().Ray().RayV1().RayJobs(namespace.Name).Delete(test.Ctx(), rayJob.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Deleted RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)
	})
}
