package e2erayservice

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// TestRayServiceHTTPClientMetrics verifies that the kuberay operator emits
// Prometheus histogram and counter metrics for outbound HTTP requests to both
// the Ray dashboard API and the Ray Serve proxy actor. A RayService exercises
// both code paths: the dashboard client (GetServeDetails, UpdateDeployments)
// and the proxy client (CheckProxyActorHealth).
func TestRayServiceHTTPClientMetrics(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	// Deploy a RayService and wait for it to be ready.
	rayServiceName := "rayservice-metrics-test"

	rayServiceFullAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(RayServiceSampleYamlApplyConfiguration())

	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceFullAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayService %s/%s successfully", rayService.Namespace, rayService.Name)

	// Wait for RayService to be ready - this ensures both dashboard and proxy client calls have been made.
	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

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

	// Assert all histogram metrics are present.
	expectedMetrics := []string{
		"kuberay_dashboard_client_request_duration_seconds_bucket",
		"kuberay_dashboard_client_request_duration_seconds_count",
		"kuberay_serve_client_request_duration_seconds_bucket",
		"kuberay_serve_client_request_duration_seconds_count",
	}

	for _, metric := range expectedMetrics {
		g.Expect(metricsOutput).To(ContainSubstring(metric),
			"Expected metric %q to be present in metrics output", metric)
	}

	// Assert expected endpoint labels appear.
	g.Expect(metricsOutput).To(ContainSubstring(`ray_endpoint="serve_applications"`),
		"Expected serve_applications ray_endpoint label from GetServeDetails/UpdateDeployments calls")
	g.Expect(metricsOutput).To(ContainSubstring(`ray_endpoint="serve_proxy_health"`),
		"Expected serve_proxy_health ray_endpoint label from CheckProxyActorHealth calls")

	// Assert successful response codes appear.
	g.Expect(metricsOutput).To(ContainSubstring(`code="200"`),
		"Expected successful response code 200 in metrics")

	// Log matching metric lines for debugging only when the test fails.
	test.T().Cleanup(func() {
		if !test.T().Failed() {
			return
		}
		for line := range strings.SplitSeq(metricsOutput, "\n") {
			if (strings.Contains(line, "kuberay_dashboard_client") || strings.Contains(line, "kuberay_serve_client")) && !strings.HasPrefix(line, "#") {
				LogWithTimestamp(test.T(), "Metric: %s", strings.TrimSpace(line))
			}
		}
	})
}
