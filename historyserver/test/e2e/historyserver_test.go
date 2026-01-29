package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go/service/s3"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	. "github.com/ray-project/kuberay/historyserver/test/support"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

const (
	LiveSessionName = "live"
	EndpointLogFile = "/api/v0/logs/file"
)

func TestHistoryServer(t *testing.T) {
	// Share a single S3 client among subtests.
	s3Client := EnsureS3Client(t)

	tests := []struct {
		name     string
		testFunc func(Test, *WithT, *corev1.Namespace, *s3.S3)
	}{
		{
			name:     "Live cluster: historyserver endpoints should be accessible",
			testFunc: testLiveClusters,
		},
		{
			name:     "/v0/logs/file endpoint (live cluster)",
			testFunc: testLogFileEndpointLiveCluster,
		},
		{
			name:     "/v0/logs/file endpoint (dead cluster)",
			testFunc: testLogFileEndpointDeadCluster,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test := With(t)
			g := NewWithT(t)
			namespace := test.NewTestNamespace()

			tt.testFunc(test, g, namespace, s3Client)
		})
	}
}

func testLiveClusters(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyHistoryServer(test, g, namespace)
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)
	verifyHistoryServerEndpoints(test, g, client, historyServerURL)
	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Live clusters E2E test completed successfully")
}

// setClusterContext sets the cluster context via /enter_cluster/ endpoint and verifies the response.
func setClusterContext(test Test, g *WithT, client *http.Client, historyServerURL, namespace, clusterName, session string) {
	enterURL := fmt.Sprintf("%s/enter_cluster/%s/%s/%s", historyServerURL, namespace, clusterName, session)
	LogWithTimestamp(test.T(), "Setting cluster context: %s", enterURL)

	g.Eventually(func(gg Gomega) {
		resp, err := client.Get(enterURL)
		gg.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

		body, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())

		var result map[string]any
		err = json.Unmarshal(body, &result)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(result["result"]).To(Equal("success"))
		gg.Expect(result["name"]).To(Equal(clusterName))
		gg.Expect(result["namespace"]).To(Equal(namespace))
		gg.Expect(result["session"]).To(Equal(session))
	}, TestTimeoutShort).Should(Succeed())
}

// verifyHistoryServerEndpoints tests all history server endpoints
func verifyHistoryServerEndpoints(test Test, g *WithT, client *http.Client, historyServerURL string) {
	for _, endpoint := range HistoryServerEndpoints {
		LogWithTimestamp(test.T(), "Testing history server endpoint: %s", endpoint)
		g.Eventually(func(gg Gomega) {
			resp, err := client.Get(historyServerURL + endpoint)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(resp.StatusCode).To(Equal(200),
				"Endpoint %s should return 200, got %d: %s", endpoint, resp.StatusCode, string(body))

			LogWithTimestamp(test.T(), "Endpoint %s returned status %d", endpoint, resp.StatusCode)
		}, TestTimeoutShort).Should(Succeed())
	}
}

// getClusterFromList retrieves a cluster from the /clusters/ endpoint by name and namespace.
func getClusterFromList(test Test, g *WithT, historyServerURL, clusterName, namespace string) *utils.ClusterInfo {
	LogWithTimestamp(test.T(), "Getting cluster %s/%s from /clusters/ endpoint", namespace, clusterName)

	var result *utils.ClusterInfo
	g.Eventually(func(gg Gomega) {
		result = nil // Reset to avoid stale value from previous iteration
		resp, err := http.Get(historyServerURL + "/clusters/")
		gg.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())

		var clusters []utils.ClusterInfo
		err = json.Unmarshal(body, &clusters)
		gg.Expect(err).NotTo(HaveOccurred())

		for i, c := range clusters {
			if c.Name == clusterName && c.Namespace == namespace {
				result = &clusters[i]
				break
			}
		}
		gg.Expect(result).NotTo(BeNil(), "Cluster %s/%s should be in the list", namespace, clusterName)
		LogWithTimestamp(test.T(), "Found cluster: %s/%s with sessionName=%s",
			result.Namespace, result.Name, result.SessionName)
	}, TestTimeoutMedium).Should(Succeed())

	return result
}

// testLogFileEndpointLiveCluster verifies that the history server can fetch log files from a live cluster.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster
// 2. Submit a Ray job to the existing cluster
// 3. Apply History Server and get its URL
// 4. Get the cluster info from the list
// 5. Verify that the history server can fetch log content (raylet.out)
// 6. Verify that the history server rejects path traversal attempts
// 7. Delete S3 bucket to ensure test isolation
func testLogFileEndpointLiveCluster(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyHistoryServer(test, g, namespace)
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	nodeID := GetOneOfNodeID(g, client, historyServerURL)
	// Hardcode "raylet.out" for deterministic testing.
	filename := "raylet.out"

	// Define test cases covering different parameters
	logFileTestCases := []struct {
		name           string
		buildURL       func(baseURL, nodeID string) string
		expectedStatus int
	}{
		// lines parameter
		{"lines=100", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=100", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"lines=0 (default 1000)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"lines=-1 (all)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=-1", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// timeout parameter
		{"timeout=5", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=5", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"timeout=30", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=30", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// attempt_number parameter
		{"attempt_number=0", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=0", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"attempt_number=1", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=1", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// download_filename parameter
		{"download_filename=custom.log", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&download_filename=custom.log", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// filter_ansi_code parameter
		{"filter_ansi_code=true", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&filter_ansi_code=true", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"filter_ansi_code=false", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&filter_ansi_code=false", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// Combined parameters
		{"lines+timeout+filter", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=50&timeout=10&filter_ansi_code=true", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// Missing mandatory parameters
		{"missing node_id", func(u, n string) string { return fmt.Sprintf("%s%s?filename=%s", u, EndpointLogFile, filename) }, http.StatusBadRequest},
		{"missing filename", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"missing both", func(u, n string) string { return fmt.Sprintf("%s%s", u, EndpointLogFile) }, http.StatusBadRequest},

		// Invalid parameters
		// NOTE: Ray Dashboard will only return 500 (Internal Server Error) for the exceptions (including file not found and invalid parameter)
		// ref: https://github.com/ray-project/ray/blob/68d01c4c48a59c7768ec9c2359a1859966c446b6/python/ray/dashboard/modules/state/state_head.py#L282-L284
		{"invalid lines (string)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=abc", u, EndpointLogFile, n, filename) }, http.StatusInternalServerError},
		{"invalid timeout (string)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=invalid", u, EndpointLogFile, n, filename) }, http.StatusInternalServerError},
		{"invalid attempt_number (string)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=xyz", u, EndpointLogFile, n, filename) }, http.StatusInternalServerError},
		{"file not found", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=nonexistent.log", u, EndpointLogFile, n) }, http.StatusInternalServerError},

		// Path traversal attacks
		{"traversal ../etc/passwd", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=../etc/passwd", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"traversal ..", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=..", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"traversal /etc/passwd", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=/etc/passwd", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"traversal ../../secret", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=../../secret", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"traversal in node_id", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=../evil&filename=%s", u, EndpointLogFile, filename) }, http.StatusBadRequest},
	}

	for _, tc := range logFileTestCases {
		test.T().Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Eventually(func(gg Gomega) {
				url := tc.buildURL(historyServerURL, nodeID)
				resp, err := client.Get(url)
				gg.Expect(err).NotTo(HaveOccurred())
				defer func() {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}()

				gg.Expect(resp.StatusCode).To(Equal(tc.expectedStatus),
					"Test case '%s' failed: expected status %d, got %d", tc.name, tc.expectedStatus, resp.StatusCode)

				if tc.expectedStatus == http.StatusOK {
					body, err := io.ReadAll(resp.Body)
					gg.Expect(err).NotTo(HaveOccurred())
					gg.Expect(len(body)).To(BeNumerically(">", 0),
						"Test case '%s' should return non-empty body", tc.name)
				}
			}, TestTimeoutShort).Should(Succeed())
		})
	}

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Log file endpoint tests completed")
}

// testLogFileEndpointDeadCluster verifies that the history server can fetch log files from S3 after a cluster is deleted.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster
// 2. Submit a Ray job to the existing cluster
// 3. Delete RayCluster to trigger log upload to S3
// 4. Apply History Server and get its URL
// 5. Verify that the history server can fetch log content from S3
// 6. Verify parameter validation for dead cluster
// 7. Verify security (path traversal) protection
// 8. Delete S3 bucket to ensure test isolation
func testLogFileEndpointDeadCluster(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	// Delete RayCluster to trigger log upload
	err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Deleted RayCluster %s/%s", namespace.Name, rayCluster.Name)

	// Wait for cluster to be fully deleted (ensures logs are uploaded to S3)
	g.Eventually(func() error {
		_, err := GetRayCluster(test, namespace.Name, rayCluster.Name)
		return err
	}, TestTimeoutMedium).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

	ApplyHistoryServer(test, g, namespace)
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).NotTo(Equal(LiveSessionName))

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	nodeID := GetOneOfNodeID(g, client, historyServerURL)
	// Hardcode "raylet.out" for deterministic testing.
	filename := "raylet.out"

	// Define test cases for dead cluster (S3 storage)
	// Note: Dead cluster only supports basic parameters (node_id, filename, lines)
	// Advanced features like timeout, attempt_number, download_filename, filter_ansi_code
	// are only available for live clusters
	logFileTestCases := []struct {
		name           string
		buildURL       func(baseURL, nodeID string) string
		expectedStatus int
	}{
		// Supported parameters
		{"lines=100", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=100", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"lines=0 (default 1000)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"lines=-1 (all)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=-1", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// Missing mandatory parameters
		{"missing node_id", func(u, n string) string { return fmt.Sprintf("%s%s?filename=%s", u, EndpointLogFile, filename) }, http.StatusBadRequest},
		{"missing filename", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"missing both", func(u, n string) string { return fmt.Sprintf("%s%s", u, EndpointLogFile) }, http.StatusBadRequest},

		// Invalid parameters
		{"invalid lines (string)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=abc", u, EndpointLogFile, n, filename) }, http.StatusBadRequest},
		{"file not found", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=nonexistent.log", u, EndpointLogFile, n) }, http.StatusNotFound},

		// Path traversal attacks
		{"traversal ../etc/passwd", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=../etc/passwd", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"traversal ..", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=..", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"traversal /etc/passwd", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=/etc/passwd", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"traversal ../../secret", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=../../secret", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"traversal in node_id", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=../evil&filename=%s", u, EndpointLogFile, filename) }, http.StatusBadRequest},
	}

	for _, tc := range logFileTestCases {
		test.T().Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Eventually(func(gg Gomega) {
				url := tc.buildURL(historyServerURL, nodeID)
				resp, err := client.Get(url)
				gg.Expect(err).NotTo(HaveOccurred())
				defer func() {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}()

				gg.Expect(resp.StatusCode).To(Equal(tc.expectedStatus),
					"Test case '%s' failed: expected status %d, got %d", tc.name, tc.expectedStatus, resp.StatusCode)

				if tc.expectedStatus == http.StatusOK {
					body, err := io.ReadAll(resp.Body)
					gg.Expect(err).NotTo(HaveOccurred())
					gg.Expect(len(body)).To(BeNumerically(">", 0),
						"Test case '%s' should return non-empty body", tc.name)
				}
			}, TestTimeoutShort).Should(Succeed())
		})
	}

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster log file endpoint tests completed")
}
