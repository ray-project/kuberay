package e2erayjob

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// TestRayJobHTTPClientMetrics verifies that the kuberay operator emits Prometheus
// histogram and counter metrics for outbound HTTP requests to the Ray dashboard
// jobs API. A RayJob exercises the dashboard client's jobs endpoint (SubmitJob,
// GetJobInfo) during reconciliation.
func TestRayJobHTTPClientMetrics(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	// Job scripts
	jobsAC := NewConfigMap(namespace.Name, Files(test, "counter.py"))
	jobs, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), jobsAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", jobs.Namespace, jobs.Name)

	// Deploy a RayJob — the operator will call the dashboard client's jobs API.
	rayJobAC := rayv1ac.RayJob("metrics-test", namespace.Name).
		WithSpec(rayv1ac.RayJobSpec().
			WithRayClusterSpec(NewRayClusterSpec(MountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](jobs, "/home/ray/jobs"))).
			WithEntrypoint("python /home/ray/jobs/counter.py").
			WithRuntimeEnvYAML(`
env_vars:
  counter_name: test_counter
`).
			WithShutdownAfterJobFinishes(true).
			WithSubmitterPodTemplate(JobSubmitterPodTemplateApplyConfiguration()))

	rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

	// Wait for the RayJob to complete — this ensures dashboard client calls to the jobs API have been made.
	LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to complete", rayJob.Namespace, rayJob.Name)
	g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
		Should(WithTransform(RayJobStatus, Satisfy(rayv1.IsJobTerminal)))

	// Find a ready operator pod to scrape metrics.
	operatorPod := GetReadyOperatorPod(g, test)

	// Create a curl pod to scrape the operator's metrics endpoint from within the cluster.
	curlPodName := "metrics-curl-pod"
	curlContainerName := "curl"
	curlPod, err := CreateCurlPod(g, test, curlPodName, curlContainerName, namespace.Name)
	g.Expect(err).NotTo(HaveOccurred())

	// Scrape the operator metrics via its pod IP.
	metricsURL := fmt.Sprintf("http://%s:8080/metrics", operatorPod.Status.PodIP)
	LogWithTimestamp(test.T(), "Scraping operator metrics from %s", metricsURL)
	stdout, stderr := ExecPodCmd(test, curlPod, curlContainerName, []string{"curl", "-sS", "-f", metricsURL})
	g.Expect(stderr.String()).To(BeEmpty(), "curl stderr should be empty; metrics scrape may have failed")
	metricsOutput := stdout.String()

	// Assert dashboard client metrics for the jobs endpoint are present.
	g.Expect(metricsOutput).To(ContainSubstring("kuberay_dashboard_client_request_duration_seconds_count"),
		"Dashboard client histogram count should be present")
	g.Expect(metricsOutput).To(ContainSubstring("kuberay_dashboard_client_requests_total"),
		"Dashboard client counter should be present")

	// Assert the jobs endpoint label appears (from SubmitJob / GetJobInfo calls).
	g.Expect(metricsOutput).To(ContainSubstring(`ray_endpoint="jobs"`),
		"Expected jobs ray_endpoint label from SubmitJob/GetJobInfo calls")

	// Assert successful response codes appear.
	g.Expect(metricsOutput).To(ContainSubstring(`code="200"`),
		"Expected successful response code 200 in metrics")

	// Log all matching metric lines for debugging.
	for line := range strings.SplitSeq(metricsOutput, "\n") {
		if strings.Contains(line, "kuberay_dashboard_client") && !strings.HasPrefix(line, "#") {
			LogWithTimestamp(test.T(), "Metric: %s", strings.TrimSpace(line))
		}
	}
}
