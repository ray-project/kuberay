package e2erayjobsubmitter

import (
	"testing"

	. "github.com/onsi/gomega"

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
	SubmitterPodTemplate := JobSubmitterPodTemplateApplyConfiguration()
	image := SubmitterImage
	SubmitterPodTemplate.Spec.Containers[0].Image = &image
	SubmitterPodTemplate.Spec.Containers[0].Command = []string{"/submitter"}
	SubmitterPodTemplate.Spec.Containers[0].Args = []string{"--runtime-env-json", `{"pip":["requests==2.26.0","pendulum==2.1.2"],"env_vars":{"counter_name":"test_counter"}}`, "--", "python", "/home/ray/jobs/counter.py"}

	test.T().Run("Successful RayJob with light weight submitter", func(_ *testing.T) {
		rayJobAC := rayv1ac.RayJob("counter", namespace.Name).WithSpec(
			rayv1ac.RayJobSpec().
				WithRayClusterSpec(NewRayClusterSpec(MountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](TestScript, "/home/ray/jobs"))).
				WithSubmitterPodTemplate(SubmitterPodTemplate),
		)
		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).Should(WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)))
	})
}
