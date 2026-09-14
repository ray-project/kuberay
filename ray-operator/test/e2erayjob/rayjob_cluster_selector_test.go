package e2erayjob

import (
	"net"
	"strconv"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

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

	test.T().Run("Successful RayJob with dashboard address override", func(t *testing.T) {
		t.Parallel()
		test := With(t)
		g := NewWithT(t)

		headServiceName, err := utils.GenerateHeadServiceName(utils.RayClusterCRD, rayCluster.Spec, rayCluster.Name)
		g.Expect(err).NotTo(HaveOccurred())
		headService, err := test.Client().Core().CoreV1().Services(namespace.Name).Get(test.Ctx(), headServiceName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(net.ParseIP(headService.Spec.ClusterIP)).NotTo(BeNil())

		dashboardHost := utils.GenerateFQDNServiceName(test.Ctx(), *rayCluster, namespace.Name)
		dashboardPort := strconv.Itoa(utils.DefaultDashboardPort)
		dashboardAddress := net.JoinHostPort(dashboardHost, dashboardPort)
		overrideAddress := "http://" + net.JoinHostPort(headService.Spec.ClusterIP, dashboardPort)

		// We first create a normal submitter pod configuration
		submitterTemplate := JobSubmitterPodTemplateApplyConfiguration()

		// We then purposefully break the dashboard API hostname. We set it to be
		// the submitter's own loopback address (where no dashboard API is running).
		// The healthcheck must honor RAY_ADDRESS to reach the real dashboard and allow job submission to proceed.
		submitterTemplate.Spec.WithHostAliases(corev1ac.HostAlias().
			WithIP("127.0.0.1").WithHostnames(dashboardHost))
		submitterTemplate.Spec.Containers[0].WithEnv(corev1ac.EnvVar().
			WithName("RAY_ADDRESS").WithValue(overrideAddress))
		// Leave the command unset to exercise the operator-generated health probe.
		rayJobAC := rayv1ac.RayJob("dashboard-address-override", namespace.Name).
			WithSpec(rayv1ac.RayJobSpec().
				WithSubmissionMode(rayv1.K8sJobMode).
				WithClusterSelector(map[string]string{utils.RayClusterLabelKey: rayCluster.Name}).
				WithEntrypoint("python -c 'print(\"address override works\")'").
				WithSubmitterPodTemplate(submitterTemplate))

		rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(t, "Waiting for RayJob %s/%s to succeed with RAY_ADDRESS=%s and %s mapped to loopback",
			rayJob.Namespace, rayJob.Name, overrideAddress, dashboardHost)

		// Without the fix, the health check keeps retrying the broken hostname
  		// and this wait times out. With the fix, it uses RAY_ADDRESS and the job
  		// can submit and complete successfully.
		g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
			Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

		rayJob, err = GetRayJob(test, rayJob.Namespace, rayJob.Name)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(rayJob.Status.JobStatus).To(Equal(rayv1.JobStatusSucceeded))
		// Ensure the blocked hostname matches the address generated by the operator.
		g.Expect(rayJob.Status.DashboardURL).To(Equal(dashboardAddress))
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
