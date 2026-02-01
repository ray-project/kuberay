package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
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

// ansiEscapePattern matches ANSI escape sequences (same pattern as in reader.go)
// Pattern: \x1b\[[0-9;]+m
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]+m`)

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

		// suffix parameter
		{"suffix=out (default)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&suffix=out", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"suffix=err", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&suffix=err", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// Combined parameters
		{"lines+timeout+filter", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=50&timeout=10&filter_ansi_code=true", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// Missing mandatory parameters
		{"missing node_id and node_ip", func(u, n string) string { return fmt.Sprintf("%s%s?filename=%s", u, EndpointLogFile, filename) }, http.StatusBadRequest},
		{"missing filename", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"missing both", func(u, n string) string { return fmt.Sprintf("%s%s", u, EndpointLogFile) }, http.StatusBadRequest},

		// Invalid parameters
		{"invalid lines (string)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=abc", u, EndpointLogFile, n, filename) }, http.StatusBadRequest},
		{"invalid timeout (string)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=invalid", u, EndpointLogFile, n, filename) }, http.StatusBadRequest},
		{"invalid attempt_number (string)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=xyz", u, EndpointLogFile, n, filename) }, http.StatusBadRequest},
		{"invalid suffix", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&suffix=invalid", u, EndpointLogFile, n, filename) }, http.StatusBadRequest},
		// NOTE: Ray Dashboard will return 500 (Internal Server Error) for the file not found error
		// ref: https://github.com/ray-project/ray/blob/68d01c4c48a59c7768ec9c2359a1859966c446b6/python/ray/dashboard/modules/state/state_head.py#L282-L284
		{"file not found", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=nonexistent.log", u, EndpointLogFile, n) }, http.StatusInternalServerError},
		{"task_id invalid (not found)", func(u, n string) string { return fmt.Sprintf("%s%s?task_id=nonexistent-task-id", u, EndpointLogFile) }, http.StatusInternalServerError},
		{"node_ip invalid (non-existent)", func(u, n string) string { return fmt.Sprintf("%s%s?node_ip=192.168.255.255&filename=%s", u, EndpointLogFile, filename) }, http.StatusInternalServerError},
		{"pid invalid (string)", func(u, n string) string { return fmt.Sprintf("%s%s?pid=abc&node_id=%s", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"pid non-existent", func(u, n string) string { return fmt.Sprintf("%s%s?pid=999999&node_id=%s", u, EndpointLogFile, n) }, http.StatusInternalServerError},

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

			url := tc.buildURL(historyServerURL, nodeID)
			resp, err := client.Get(url)
			g.Expect(err).NotTo(HaveOccurred())
			defer func() {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}()

			body, err := io.ReadAll(resp.Body)
			g.Expect(err).NotTo(HaveOccurred())

			if resp.StatusCode != tc.expectedStatus {
				LogWithTimestamp(t, "Test case '%s' failed: expected %d, got %d, body: %s",
					tc.name, tc.expectedStatus, resp.StatusCode, string(body))
			}

			g.Expect(resp.StatusCode).To(Equal(tc.expectedStatus),
				"Test case '%s' failed: expected %d, got %d", tc.name, tc.expectedStatus, resp.StatusCode)

			if tc.expectedStatus == http.StatusOK {
				g.Expect(len(body)).To(BeNumerically(">", 0))
			}
		})
	}

	// Sub-test for task_id parameter (live cluster)
	test.T().Run("task_id parameter", func(t *testing.T) {
		g := NewWithT(t)

		// Get all eligible task IDs
		taskIDs := getAllEligibleTaskIDs(g, client, historyServerURL)
		LogWithTimestamp(t, "Found %d eligible task IDs for testing", len(taskIDs))

		var successCount int
		var lastError string

		// Try each task ID until one succeeds
		for _, taskID := range taskIDs {
			LogWithTimestamp(t, "Testing task_id: %s", taskID)

			url := fmt.Sprintf("%s%s?task_id=%s", historyServerURL, EndpointLogFile, taskID)
			resp, err := client.Get(url)
			if err != nil {
				lastError = fmt.Sprintf("HTTP error for task %s: %v", taskID, err)
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				successCount++
				LogWithTimestamp(t, "Task %s succeeded, returned %d bytes", taskID, len(body))
				break
			} else {
				lastError = fmt.Sprintf("task %s returned %d: %s", taskID, resp.StatusCode, string(body))
				LogWithTimestamp(t, "Task %s failed: %s", taskID, lastError)
			}
		}

		g.Expect(successCount).To(BeNumerically(">", 0),
			"At least one task_id should succeed. Last error: %s", lastError)
	})

	// Sub-test for actor_id parameter (live cluster)
	test.T().Run("actor_id parameter", func(t *testing.T) {
		g := NewWithT(t)

		// Get all eligible actor IDs
		actorIDs := getAllEligibleActorIDs(g, client, historyServerURL)
		LogWithTimestamp(t, "Found %d eligible actor IDs for testing", len(actorIDs))

		var successCount int
		var lastError string

		// Try each actor ID until one succeeds
		for _, actorID := range actorIDs {
			LogWithTimestamp(t, "Testing actor_id: %s", actorID)

			url := fmt.Sprintf("%s%s?actor_id=%s", historyServerURL, EndpointLogFile, actorID)
			resp, err := client.Get(url)
			if err != nil {
				lastError = fmt.Sprintf("HTTP error for actor %s: %v", actorID, err)
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				successCount++
				LogWithTimestamp(t, "Actor %s succeeded, returned %d bytes", actorID, len(body))
				break
			} else {
				lastError = fmt.Sprintf("actor %s returned %d: %s", actorID, resp.StatusCode, string(body))
				LogWithTimestamp(t, "Actor %s failed: %s", actorID, lastError)
			}
		}

		g.Expect(successCount).To(BeNumerically(">", 0),
			"At least one actor_id should succeed. Last error: %s", lastError)
	})

	// Sub-test for pid parameter (live cluster)
	test.T().Run("pid parameter", func(t *testing.T) {
		g := NewWithT(t)

		// Get an eligible worker PID and its node ID
		pid, nodeID := getEligibleWorkerPID(g, client, historyServerURL)
		LogWithTimestamp(t, "Found eligible worker PID %d on node %s for testing", pid, nodeID)

		// Test successful case
		url := fmt.Sprintf("%s%s?pid=%d&node_id=%s", historyServerURL, EndpointLogFile, pid, nodeID)
		resp, err := client.Get(url)
		g.Expect(err).NotTo(HaveOccurred())
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Expected OK for valid pid and node_id, got %d: %s", resp.StatusCode, string(body))
		g.Expect(len(body)).To(BeNumerically(">", 0))

		// Test missing node_id
		url = fmt.Sprintf("%s%s?pid=%d", historyServerURL, EndpointLogFile, pid)
		resp, err = client.Get(url)
		g.Expect(err).NotTo(HaveOccurred())
		resp.Body.Close()
		g.Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
	})

	// Sub-test for node_ip parameter (live cluster)
	test.T().Run("node_ip parameter", func(t *testing.T) {
		g := NewWithT(t)

		// Get node IP from head pod (use Pod IP, not Host IP)
		// Ray registers nodes with Pod IP (--node-ip-address flag)
		headPod, err := GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())
		nodeIP := headPod.Status.PodIP
		g.Expect(nodeIP).NotTo(BeEmpty(), "Head pod should have a pod IP")
		LogWithTimestamp(t, "Found head pod with IP: %s", nodeIP)

		// Test successful case: node_ip + filename
		url := fmt.Sprintf("%s%s?node_ip=%s&filename=%s", historyServerURL, EndpointLogFile, nodeIP, filename)
		resp, err := client.Get(url)
		g.Expect(err).NotTo(HaveOccurred())
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		// For live cluster, the request is proxied to Ray Dashboard
		// The dashboard should be able to resolve node_ip to node_id
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Expected OK for valid node_ip, got %d: %s", resp.StatusCode, string(body))
		g.Expect(len(body)).To(BeNumerically(">", 0))
	})

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

	// Capture node IP and ID before deleting cluster (for node_ip tests later)
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	savedNodeIP := headPod.Status.PodIP
	savedNodeID := GetNodeIDFromHeadPod(test, g, rayCluster)
	LogWithTimestamp(test.T(), "Captured node IP %s and node ID %s before cluster deletion", savedNodeIP, savedNodeID)

	// Delete RayCluster to trigger log upload
	err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
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

	logFileTestCases := []struct {
		name           string
		buildURL       func(baseURL, nodeID string) string
		expectedStatus int
	}{
		// Basic parameters
		{"lines=100", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=100", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"lines=0 (default 1000)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"lines=-1 (all)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=-1", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// timeout parameter
		// NOTE: timeout feature is not yet implemented, we just accept and validate the timeout parameter
		{"timeout=5", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=5", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"timeout=30", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=30", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// attempt_number parameter
		{"attempt_number=0", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=0", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"attempt_number=1 (not found)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=1", u, EndpointLogFile, n, filename) }, http.StatusNotFound},

		// download_file parameter
		{"download_file=true", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&download_file=true", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"download_file=false", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&download_file=false", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// filter_ansi_code parameter
		{"filter_ansi_code=true", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&filter_ansi_code=true", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"filter_ansi_code=false", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&filter_ansi_code=false", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// suffix parameter
		{"suffix=out (default)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&suffix=out", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"suffix=err", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&suffix=err", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// Combined parameters
		{"lines+timeout+filter", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=50&timeout=10&filter_ansi_code=true", u, EndpointLogFile, n, filename) }, http.StatusOK},
		{"all parameters", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=100&timeout=15&attempt_number=0&download_file=true&filter_ansi_code=true", u, EndpointLogFile, n, filename) }, http.StatusOK},

		// Missing mandatory parameters
		{"missing node_id and node_ip", func(u, n string) string { return fmt.Sprintf("%s%s?filename=%s", u, EndpointLogFile, filename) }, http.StatusBadRequest},
		{"missing filename", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s", u, EndpointLogFile, n) }, http.StatusBadRequest},
		{"missing both", func(u, n string) string { return fmt.Sprintf("%s%s", u, EndpointLogFile) }, http.StatusBadRequest},

		// Invalid parameters
		{"invalid lines (string)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=abc", u, EndpointLogFile, n, filename) }, http.StatusBadRequest},
		{"invalid timeout (string)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=invalid", u, EndpointLogFile, n, filename) }, http.StatusBadRequest},
		{"invalid attempt_number (string)", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=xyz", u, EndpointLogFile, n, filename) }, http.StatusBadRequest},
		{"file not found", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=nonexistent.log", u, EndpointLogFile, n) }, http.StatusNotFound},
		{"invalid suffix", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=%s&suffix=invalid", u, EndpointLogFile, n, filename) }, http.StatusBadRequest},
		{"task_id invalid (not found)", func(u, n string) string { return fmt.Sprintf("%s%s?task_id=nonexistent-task-id", u, EndpointLogFile) }, http.StatusBadRequest},
		{"non-existent pid", func(u, n string) string { return fmt.Sprintf("%s%s?pid=999999&node_id=%s", u, EndpointLogFile, n) }, http.StatusNotFound},

		// node_ip parameter tests
		{"node_ip invalid (non-existent)", func(u, n string) string { return fmt.Sprintf("%s%s?node_ip=192.168.255.255&filename=%s", u, EndpointLogFile, filename) }, http.StatusNotFound},

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

			url := tc.buildURL(historyServerURL, nodeID)
			resp, err := client.Get(url)
			g.Expect(err).NotTo(HaveOccurred())
			defer func() {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}()

			body, err := io.ReadAll(resp.Body)
			g.Expect(err).NotTo(HaveOccurred())

			if resp.StatusCode != tc.expectedStatus {
				LogWithTimestamp(t, "Test case '%s' failed: expected %d, got %d, body: %s",
					tc.name, tc.expectedStatus, resp.StatusCode, string(body))
			}

			g.Expect(resp.StatusCode).To(Equal(tc.expectedStatus),
				"Test case '%s' failed: expected %d, got %d", tc.name, tc.expectedStatus, resp.StatusCode)

			if tc.expectedStatus == http.StatusOK {
				g.Expect(len(body)).To(BeNumerically(">", 0))
			}
		})
	}

	// Sub-tests for specific parameter behaviors
	test.T().Run("download_file header validation", func(t *testing.T) {
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			// Test with download_file=true
			urlWithDownload := fmt.Sprintf("%s%s?node_id=%s&filename=%s&download_file=true", historyServerURL, EndpointLogFile, nodeID, filename)
			resp, err := client.Get(urlWithDownload)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))
			contentDisposition := resp.Header.Get("Content-Disposition")
			gg.Expect(contentDisposition).To(ContainSubstring("attachment"))
			gg.Expect(contentDisposition).To(ContainSubstring(fmt.Sprintf("filename=\"%s\"", filename)))
			LogWithTimestamp(test.T(), "download_file=true sets Content-Disposition header: %s", contentDisposition)

			// Test with download_file=false, should not have Content-Disposition header
			urlWithoutDownload := fmt.Sprintf("%s%s?node_id=%s&filename=%s&download_file=false", historyServerURL, EndpointLogFile, nodeID, filename)
			resp2, err := client.Get(urlWithoutDownload)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp2.Body.Close()

			gg.Expect(resp2.StatusCode).To(Equal(http.StatusOK))
			contentDisposition2 := resp2.Header.Get("Content-Disposition")
			gg.Expect(contentDisposition2).To(BeEmpty(), "Content-Disposition should not be set when download_file=false")
		}, TestTimeoutShort).Should(Succeed())
	})

	test.T().Run("filter_ansi_code behavior", func(t *testing.T) {
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			// Fetch with filter_ansi_code=false (original content with ANSI codes)
			urlWithoutFilter := fmt.Sprintf("%s%s?node_id=%s&filename=%s&filter_ansi_code=false&lines=100", historyServerURL, EndpointLogFile, nodeID, filename)
			resp, err := client.Get(urlWithoutFilter)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))
			bodyWithoutFilter, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())

			// Fetch with filter_ansi_code=true (ANSI codes should be removed)
			urlWithFilter := fmt.Sprintf("%s%s?node_id=%s&filename=%s&filter_ansi_code=true&lines=100", historyServerURL, EndpointLogFile, nodeID, filename)
			resp2, err := client.Get(urlWithFilter)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp2.Body.Close()

			gg.Expect(resp2.StatusCode).To(Equal(http.StatusOK))
			bodyWithFilter, err := io.ReadAll(resp2.Body)
			gg.Expect(err).NotTo(HaveOccurred())

			// Check if original content contains ANSI codes using the same pattern as reader.go
			hasAnsiInOriginal := ansiEscapePattern.Match(bodyWithoutFilter)

			if hasAnsiInOriginal {
				LogWithTimestamp(test.T(), "Original log contains ANSI codes, verifying they are filtered")
				// Filtered content should NOT contain ANSI escape sequences
				hasAnsiInFiltered := ansiEscapePattern.Match(bodyWithFilter)
				gg.Expect(hasAnsiInFiltered).To(BeFalse(), "Filtered content should not contain ANSI escape sequences")
			} else {
				LogWithTimestamp(test.T(), "Log doesn't contain ANSI codes, check is skipped...")
			}
		}, TestTimeoutShort).Should(Succeed())
	})

	test.T().Run("attempt_number behavior", func(t *testing.T) {
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			// Test with attempt_number=0
			urlAttempt0 := fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=0", historyServerURL, EndpointLogFile, nodeID, filename)
			resp, err := client.Get(urlAttempt0)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))
			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(len(body)).To(BeNumerically(">", 0))
			LogWithTimestamp(test.T(), "attempt_number=0 returned %d bytes", len(body))

			// attempt_number=1 should fail as retry log doesn't exist for normal execution
			urlAttempt1 := fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=1", historyServerURL, EndpointLogFile, nodeID, filename)
			resp2, err := client.Get(urlAttempt1)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp2.Body.Close()

			gg.Expect(resp2.StatusCode).To(Equal(http.StatusNotFound),
				"attempt_number=1 should return 404 when retry log doesn't exist")
			LogWithTimestamp(test.T(), "attempt_number=1 correctly returns 404 (file not found)")
		}, TestTimeoutShort).Should(Succeed())
	})

	// Sub-test for task_id parameter
	test.T().Run("task_id parameter", func(t *testing.T) {
		g := NewWithT(t)

		// Get all eligible task IDs
		taskIDs := getAllEligibleTaskIDs(g, client, historyServerURL)
		LogWithTimestamp(t, "Found %d eligible task IDs for testing", len(taskIDs))

		var successCount int
		var lastError string

		// Try each task ID until one succeeds
		for _, taskID := range taskIDs {
			LogWithTimestamp(t, "Testing task_id: %s", taskID)

			url := fmt.Sprintf("%s%s?task_id=%s", historyServerURL, EndpointLogFile, taskID)
			resp, err := client.Get(url)
			if err != nil {
				lastError = fmt.Sprintf("HTTP error for task %s: %v", taskID, err)
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				successCount++
				LogWithTimestamp(t, "Task %s succeeded, returned %d bytes", taskID, len(body))
				break
			} else {
				lastError = fmt.Sprintf("task %s returned %d: %s", taskID, resp.StatusCode, string(body))
				LogWithTimestamp(t, "Task %s failed: %s", taskID, lastError)
			}
		}

		g.Expect(successCount).To(BeNumerically(">", 0),
			"At least one task_id should succeed. Last error: %s", lastError)
	})

	// Sub-test for actor_id parameter (dead cluster)
	test.T().Run("actor_id parameter", func(t *testing.T) {
		g := NewWithT(t)

		// Get all eligible actor IDs
		actorIDs := getAllEligibleActorIDs(g, client, historyServerURL)
		LogWithTimestamp(t, "Found %d eligible actor IDs for testing", len(actorIDs))

		var successCount int
		var lastError string

		// Try each actor ID until one succeeds
		for _, actorID := range actorIDs {
			LogWithTimestamp(t, "Testing actor_id: %s", actorID)

			url := fmt.Sprintf("%s%s?actor_id=%s", historyServerURL, EndpointLogFile, actorID)
			resp, err := client.Get(url)
			if err != nil {
				lastError = fmt.Sprintf("HTTP error for actor %s: %v", actorID, err)
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				successCount++
				LogWithTimestamp(t, "Actor %s succeeded, returned %d bytes", actorID, len(body))
				break
			} else {
				lastError = fmt.Sprintf("actor %s returned %d: %s", actorID, resp.StatusCode, string(body))
				LogWithTimestamp(t, "Actor %s failed: %s", actorID, lastError)
			}
		}

		g.Expect(successCount).To(BeNumerically(">", 0),
			"At least one actor_id should succeed. Last error: %s", lastError)
	})

	// Sub-test for pid parameter (dead cluster)
	// NOTE: This test is skipped because Ray export events don't include worker_pid.
	// See: https://github.com/ray-project/ray/issues/60129
	// Worker lifecycle events are not yet exported, so we cannot obtain worker PIDs
	// from historical data for dead clusters.
	test.T().Run("pid parameter", func(t *testing.T) {
		t.Skip("Skipping pid parameter test for dead cluster: worker_pid not available in Ray export events (see https://github.com/ray-project/ray/issues/60129)")
	})

	// Sub-test for node_ip parameter (dead cluster)
	test.T().Run("node_ip parameter", func(t *testing.T) {
		g := NewWithT(t)

		// Use the captured node IP and ID from before cluster deletion
		LogWithTimestamp(t, "Testing node_ip parameter with IP: %s, ID: %s", savedNodeIP, savedNodeID)

		// Test successful case: node_ip + filename
		url := fmt.Sprintf("%s%s?node_ip=%s&filename=%s", historyServerURL, EndpointLogFile, savedNodeIP, filename)
		resp, err := client.Get(url)
		g.Expect(err).NotTo(HaveOccurred())
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Expected OK for valid node_ip, got %d: %s", resp.StatusCode, string(body))
		g.Expect(len(body)).To(BeNumerically(">", 0))

		// Test that node_ip and node_id point to the same node (should return same content)
		urlWithNodeID := fmt.Sprintf("%s%s?node_id=%s&filename=%s", historyServerURL, EndpointLogFile, savedNodeID, filename)
		resp2, err := client.Get(urlWithNodeID)
		g.Expect(err).NotTo(HaveOccurred())
		bodyWithNodeID, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		g.Expect(resp2.StatusCode).To(Equal(http.StatusOK))
		g.Expect(len(body)).To(Equal(len(bodyWithNodeID)), "node_ip and node_id should return same content")
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster log file endpoint tests completed")
}

// getAllEligibleTaskIDs retrieves all non-actor task IDs with node_id from the /api/v0/tasks endpoint.
// Returns a list of task IDs that are eligible for log file testing.
// Note: We filter out actor tasks because they don't have task_log_info by default
// (unless RAY_ENABLE_RECORD_ACTOR_TASK_LOGGING=1 is set).
func getAllEligibleTaskIDs(g *WithT, client *http.Client, historyServerURL string) []string {
	var taskIDs []string
	resp, err := client.Get(historyServerURL + "/api/v0/tasks")
	g.Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	g.Expect(resp.StatusCode).To(Equal(http.StatusOK))

	body, err := io.ReadAll(resp.Body)
	g.Expect(err).NotTo(HaveOccurred())

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	g.Expect(err).NotTo(HaveOccurred())

	// Extract task_id from response
	// Response format: {"result": true, "msg": "...", "data": {"result": {"result": [tasks...], ...}}}
	data, ok := result["data"].(map[string]interface{})
	g.Expect(ok).To(BeTrue(), "response should have 'data' field")

	dataResult, ok := data["result"].(map[string]interface{})
	g.Expect(ok).To(BeTrue(), "data should have 'result' field")

	tasks, ok := dataResult["result"].([]interface{})
	g.Expect(ok).To(BeTrue(), "result should have 'result' array")
	g.Expect(len(tasks)).To(BeNumerically(">", 0), "should have at least one task")

	// Find all non-actor tasks with node_id
	for _, t := range tasks {
		task, ok := t.(map[string]interface{})
		if !ok {
			continue
		}

		// Check if this is an actor task
		actorID, _ := task["actor_id"].(string)
		if actorID != "" {
			// Skip actor tasks - they don't have task_log_info unless
			// RAY_ENABLE_RECORD_ACTOR_TASK_LOGGING=1 is set
			continue
		}

		// Check if it has node_id
		nodeID, _ := task["node_id"].(string)
		if nodeID == "" {
			// If nodeID is empty, it means the task is not scheduled yet. Skip it
			// as it will not have logs
			continue
		}

		// Found a non-actor task with logs
		taskID, ok := task["task_id"].(string)
		if ok && taskID != "" {
			taskIDs = append(taskIDs, taskID)
		}
	}

	g.Expect(len(taskIDs)).To(BeNumerically(">", 0), "should have at least one eligible task")

	return taskIDs
}

// getAllEligibleActorIDs retrieves all actor IDs with node_id and worker_id from the /logical/actors endpoint.
// Returns a list of actor IDs that are eligible for log file testing (actors that have been scheduled and are running).
func getAllEligibleActorIDs(g *WithT, client *http.Client, historyServerURL string) []string {
	var actorIDs []string
	resp, err := client.Get(historyServerURL + "/logical/actors")
	g.Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	g.Expect(resp.StatusCode).To(Equal(http.StatusOK))

	body, err := io.ReadAll(resp.Body)
	g.Expect(err).NotTo(HaveOccurred())

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	g.Expect(err).NotTo(HaveOccurred())

	// Extract actor_id from response
	// Response format: {"result": true, "msg": "...", "data": {"actors": {actor_id: {...}, ...}}}
	data, ok := result["data"].(map[string]interface{})
	g.Expect(ok).To(BeTrue(), "response should have 'data' field")

	actors, ok := data["actors"].(map[string]interface{})
	g.Expect(ok).To(BeTrue(), "data should have 'actors' field")
	g.Expect(len(actors)).To(BeNumerically(">", 0), "should have at least one actor")

	// Find all actors with node_id and worker_id (scheduled actors)
	for actorID := range actors {
		actorIDs = append(actorIDs, actorID)
	}

	fmt.Printf("Actor IDs: %+v", actorIDs)

	g.Expect(len(actorIDs)).To(BeNumerically(">", 0), "should have at least one eligible actor")

	return actorIDs
}

// getEligibleWorkerPID retrieves an eligible worker PID and its node ID for log testing.
// It queries the /api/v0/tasks endpoint to find any task with a valid worker_pid and node_id.
func getEligibleWorkerPID(g *WithT, client *http.Client, historyServerURL string) (pid int, nodeID string) {
	resp, err := client.Get(historyServerURL + "/api/v0/tasks")
	g.Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	g.Expect(resp.StatusCode).To(Equal(http.StatusOK))

	body, err := io.ReadAll(resp.Body)
	g.Expect(err).NotTo(HaveOccurred())

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	g.Expect(err).NotTo(HaveOccurred())

	// Response format: {"result": true, "msg": "...", "data": {"result": {"result": [tasks...], ...}}}
	data, ok := result["data"].(map[string]interface{})
	g.Expect(ok).To(BeTrue(), "response should have 'data' field")

	dataResult, ok := data["result"].(map[string]interface{})
	g.Expect(ok).To(BeTrue(), "data should have 'result' field")

	tasks, ok := dataResult["result"].([]interface{})
	g.Expect(ok).To(BeTrue(), "result should have 'result' array")
	g.Expect(len(tasks)).To(BeNumerically(">", 0), "should have at least one task")

	fmt.Printf("Tasks: %+v", tasks)

	// Find a task with valid worker_pid and node_id
	for _, t := range tasks {
		task, ok := t.(map[string]interface{})
		if !ok {
			continue
		}

		// Check for worker_pid (must be non-zero)
		workerPidFloat, pidOk := task["worker_pid"].(float64)
		if !pidOk || workerPidFloat == 0 {
			continue
		}

		// Check for node_id (must be non-empty)
		taskNodeID, nodeOk := task["node_id"].(string)
		if !nodeOk || taskNodeID == "" {
			continue
		}

		// Found an eligible task with worker PID
		return int(workerPidFloat), taskNodeID
	}

	// If we get here, no eligible task was found
	g.Fail("should find at least one task with valid worker_pid and node_id")
	return 0, ""
}
