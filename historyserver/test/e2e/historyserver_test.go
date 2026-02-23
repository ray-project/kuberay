package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/aws/aws-sdk-go/service/s3"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/ray-project/kuberay/ray-operator/test/support"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	. "github.com/ray-project/kuberay/historyserver/test/support"
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
			name:     "Live cluster: grafana health only",
			testFunc: testLiveGrafanaHealth,
		},
		{
			name:     "Live cluster: prometheus health only",
			testFunc: testLivePrometheusHealth,
		},
		{
			name:     "/v0/logs/file endpoint (live cluster)",
			testFunc: testLogFileEndpointLiveCluster,
		},
		{
			name:     "/v0/logs/file endpoint (dead cluster)",
			testFunc: testLogFileEndpointDeadCluster,
		},
		{
			name:     "/v0/logs/stream endpoint",
			testFunc: testLogStreamEndpoint,
		},
		{
			name:     "/api/v0/logs endpoint with glob pattern (dead cluster)",
			testFunc: testNodeLogsEndpointDeadCluster,
		},
		{
			name:     "/api/v0/tasks/timeline endpoint (live cluster)",
			testFunc: testTimelineEndpointLiveCluster,
		},
		{
			name:     "/api/v0/tasks/timeline endpoint (dead cluster)",
			testFunc: testTimelineEndpointDeadCluster,
		},
		{
			name:     "/events endpoint (live cluster)",
			testFunc: testEventsEndpointLiveCluster,
		},
		{
			name:     "/events endpoint (dead cluster)",
			testFunc: testEventsEndpointDeadCluster,
		},
		{
			name:     "Live cluster: /api/v0/tasks?detail=1 should return the detailed task information of all task attempts",
			testFunc: testLiveClusterTasks,
		},
		{
			name:     "Dead cluster: /api/v0/tasks should return the detailed task information of all task attempts (historical replay isn't supported)",
			testFunc: testDeadClusterTasks,
		},
		{
			name:     "Live cluster: /nodes?view=summary should return the current snapshot containing node summary and resource usage information",
			testFunc: testLiveClusterNodes,
		},
		{
			name:     "Dead cluster: /nodes should return the historical replay containing node summary and resource usage snapshots of a cluster session",
			testFunc: testDeadClusterNodes,
		},
		{
			name:     "Live cluster: /nodes/{node_id} should return the current snapshot containing node summary of the specified node",
			testFunc: testLiveClusterNode,
		},
		{
			name:     "Dead cluster: /nodes/{node_id} should return the historical replay containing node summary snapshots of the specified node in a cluster session",
			testFunc: testDeadClusterNode,
		},
		{
			name:     "/api/cluster_status endpoint (live cluster)",
			testFunc: testLiveClusterStatus,
		},
		{
			name:     "/api/cluster_status endpoint (dead cluster)",
			testFunc: testDeadClusterStatus,
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
	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)
	verifyHistoryServerEndpoints(test, g, client, historyServerURL)
	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Live clusters E2E test completed successfully")
}

func testLiveGrafanaHealth(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnvWithPrometheusAndGrafana(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	sessionID := GetSessionIDFromHeadPod(test, g, rayCluster)

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)
	verifyHistoryServerGrafanaHealthEndpoint(test, g, client, historyServerURL, sessionID)
	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Live clusters grafana health E2E test completed successfully")
}

func testLivePrometheusHealth(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnvWithPrometheusAndGrafana(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)
	verifyHistoryServerPrometheusHealthEndpoint(test, g, client, historyServerURL)
	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Live clusters prometheus health E2E test completed successfully")
}

// testLogFileEndpointLiveCluster verifies that the history server can fetch log files from a live cluster.
func testLogFileEndpointLiveCluster(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	nodeID := GetOneOfNodeID(g, client, historyServerURL, true)
	filename := "raylet.out"

	logFileTestCases := []struct {
		name           string
		buildURL       func(baseURL, nodeID string) string
		expectedStatus int
	}{
		// lines parameter
		{"lines=100", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=100", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"lines=0 (default 1000)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"lines=-1 (all)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=-1", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// timeout parameter
		{"timeout=5", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=5", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"timeout=30", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=30", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// attempt_number parameter
		{"attempt_number=0", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=0", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"attempt_number=1", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=1", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// download_filename parameter
		{"download_filename=custom.log", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&download_filename=custom.log", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// filter_ansi_code parameter
		{"filter_ansi_code=true", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&filter_ansi_code=true", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"filter_ansi_code=false", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&filter_ansi_code=false", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// suffix parameter
		{"suffix=out (default)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&suffix=out", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"suffix=err", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&suffix=err", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// Combined parameters
		{"lines+timeout+filter", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=50&timeout=10&filter_ansi_code=true", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// Missing mandatory parameters
		{"missing node_id and node_ip", func(u, n string) string { return fmt.Sprintf("%s%s?filename=%s", u, EndpointLogsFile, filename) }, http.StatusBadRequest},
		{"missing filename", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s", u, EndpointLogsFile, n) }, http.StatusBadRequest},
		{"missing both", func(u, n string) string { return fmt.Sprintf("%s%s", u, EndpointLogsFile) }, http.StatusBadRequest},

		// Invalid parameters
		{"invalid lines (string)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=abc", u, EndpointLogsFile, n, filename)
		}, http.StatusBadRequest},
		{"invalid timeout (string)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=invalid", u, EndpointLogsFile, n, filename)
		}, http.StatusBadRequest},
		{"invalid attempt_number (string)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=xyz", u, EndpointLogsFile, n, filename)
		}, http.StatusBadRequest},
		{"invalid suffix", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&suffix=invalid", u, EndpointLogsFile, n, filename)
		}, http.StatusBadRequest},
		// NOTE: Ray Dashboard will return 500 (Internal Server Error) for the file not found error
		// ref: https://github.com/ray-project/ray/blob/68d01c4c48a59c7768ec9c2359a1859966c446b6/python/ray/dashboard/modules/state/state_head.py#L282-L284
		{"file not found", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=nonexistent.log", u, EndpointLogsFile, n)
		}, http.StatusInternalServerError},
		{"task_id invalid (not found)", func(u, n string) string { return fmt.Sprintf("%s%s?task_id=nonexistent-task-id", u, EndpointLogsFile) }, http.StatusInternalServerError},
		{"node_ip invalid (non-existent)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_ip=192.168.255.255&filename=%s", u, EndpointLogsFile, filename)
		}, http.StatusInternalServerError},
		{"pid invalid (string)", func(u, n string) string { return fmt.Sprintf("%s%s?pid=abc&node_id=%s", u, EndpointLogsFile, n) }, http.StatusBadRequest},
		{"pid non-existent", func(u, n string) string { return fmt.Sprintf("%s%s?pid=999999&node_id=%s", u, EndpointLogsFile, n) }, http.StatusInternalServerError},

		// Path traversal attacks
		{"traversal ../etc/passwd", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=../etc/passwd", u, EndpointLogsFile, n)
		}, http.StatusBadRequest},
		{"traversal ..", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=..", u, EndpointLogsFile, n) }, http.StatusBadRequest},
		{"traversal /etc/passwd", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=/etc/passwd", u, EndpointLogsFile, n)
		}, http.StatusBadRequest},
		{"traversal ../../secret", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=../../secret", u, EndpointLogsFile, n)
		}, http.StatusBadRequest},
		{"traversal in node_id", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=../evil&filename=%s", u, EndpointLogsFile, filename)
		}, http.StatusBadRequest},
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

			url := fmt.Sprintf("%s%s?task_id=%s", historyServerURL, EndpointLogsFile, url.QueryEscape(taskID))
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

			url := fmt.Sprintf("%s%s?actor_id=%s", historyServerURL, EndpointLogsFile, url.QueryEscape(actorID))
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
		url := fmt.Sprintf("%s%s?pid=%d&node_id=%s", historyServerURL, EndpointLogsFile, pid, nodeID)
		resp, err := client.Get(url)
		g.Expect(err).NotTo(HaveOccurred())
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Expected OK for valid pid and node_id, got %d: %s", resp.StatusCode, string(body))
		g.Expect(len(body)).To(BeNumerically(">", 0))

		// Test missing node_id
		url = fmt.Sprintf("%s%s?pid=%d", historyServerURL, EndpointLogsFile, pid)
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
		url := fmt.Sprintf("%s%s?node_ip=%s&filename=%s", historyServerURL, EndpointLogsFile, nodeIP, filename)
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
	DeleteRayClusterAndWait(test, g, namespace.Name, rayCluster.Name)

	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).NotTo(Equal(LiveSessionName))

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	nodeID := GetOneOfNodeID(g, client, historyServerURL, false)
	filename := "raylet.out"

	logFileTestCases := []struct {
		name           string
		buildURL       func(baseURL, nodeID string) string
		expectedStatus int
	}{
		// Basic parameters
		{"lines=100", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=100", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"lines=0 (default 1000)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"lines=-1 (all)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=-1", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// timeout parameter
		// NOTE: timeout feature is not yet implemented, we just accept and validate the timeout parameter
		{"timeout=5", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=5", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"timeout=30", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=30", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// attempt_number parameter
		{"attempt_number=0", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=0", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"attempt_number=1 (not found)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=1", u, EndpointLogsFile, n, filename)
		}, http.StatusNotFound},

		// download_filename parameter
		{"download_filename=custom.log", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&download_filename=custom.log", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// filter_ansi_code parameter
		{"filter_ansi_code=true", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&filter_ansi_code=true", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"filter_ansi_code=false", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&filter_ansi_code=false", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// suffix parameter
		{"suffix=out (default)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&suffix=out", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"suffix=err", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&suffix=err", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// Combined parameters
		{"lines+timeout+filter", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=50&timeout=10&filter_ansi_code=true", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},
		{"all parameters", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=100&timeout=15&attempt_number=0&download_filename=custom.log&filter_ansi_code=true", u, EndpointLogsFile, n, filename)
		}, http.StatusOK},

		// Missing mandatory parameters
		{"missing node_id and node_ip", func(u, n string) string { return fmt.Sprintf("%s%s?filename=%s", u, EndpointLogsFile, filename) }, http.StatusBadRequest},
		{"missing filename", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s", u, EndpointLogsFile, n) }, http.StatusBadRequest},
		{"missing both", func(u, n string) string { return fmt.Sprintf("%s%s", u, EndpointLogsFile) }, http.StatusBadRequest},

		// Invalid parameters
		{"invalid lines (string)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=abc", u, EndpointLogsFile, n, filename)
		}, http.StatusBadRequest},
		{"invalid timeout (string)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&timeout=invalid", u, EndpointLogsFile, n, filename)
		}, http.StatusBadRequest},
		{"invalid attempt_number (string)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=xyz", u, EndpointLogsFile, n, filename)
		}, http.StatusBadRequest},
		{"file not found", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=nonexistent.log", u, EndpointLogsFile, n)
		}, http.StatusNotFound},
		{"invalid suffix", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=%s&suffix=invalid", u, EndpointLogsFile, n, filename)
		}, http.StatusBadRequest},
		{"task_id invalid (not found)", func(u, n string) string { return fmt.Sprintf("%s%s?task_id=nonexistent-task-id", u, EndpointLogsFile) }, http.StatusBadRequest},
		{"non-existent pid", func(u, n string) string { return fmt.Sprintf("%s%s?pid=999999&node_id=%s", u, EndpointLogsFile, n) }, http.StatusNotFound},

		// node_ip parameter tests
		{"node_ip invalid (non-existent)", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_ip=192.168.255.255&filename=%s", u, EndpointLogsFile, filename)
		}, http.StatusNotFound},

		// Path traversal attacks
		{"traversal ../etc/passwd", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=../etc/passwd", u, EndpointLogsFile, n)
		}, http.StatusBadRequest},
		{"traversal ..", func(u, n string) string { return fmt.Sprintf("%s%s?node_id=%s&filename=..", u, EndpointLogsFile, n) }, http.StatusBadRequest},
		{"traversal /etc/passwd", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=/etc/passwd", u, EndpointLogsFile, n)
		}, http.StatusBadRequest},
		{"traversal ../../secret", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=%s&filename=../../secret", u, EndpointLogsFile, n)
		}, http.StatusBadRequest},
		{"traversal in node_id", func(u, n string) string {
			return fmt.Sprintf("%s%s?node_id=../evil&filename=%s", u, EndpointLogsFile, filename)
		}, http.StatusBadRequest},
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
	test.T().Run("download_filename header validation", func(t *testing.T) {
		g := NewWithT(t)
		// Test with download_filename parameter set
		customFilename := "custom_download.log"
		urlWithDownload := fmt.Sprintf("%s%s?node_id=%s&filename=%s&download_filename=%s", historyServerURL, EndpointLogsFile, nodeID, filename, customFilename)
		resp, err := client.Get(urlWithDownload)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
		contentDisposition := resp.Header.Get("Content-Disposition")
		g.Expect(contentDisposition).To(ContainSubstring("attachment"))
		g.Expect(contentDisposition).To(ContainSubstring(fmt.Sprintf("filename=%s", customFilename)))
	})

	test.T().Run("filter_ansi_code behavior", func(t *testing.T) {
		g := NewWithT(t)
		// Fetch with filter_ansi_code=false (original content with ANSI codes)
		urlWithoutFilter := fmt.Sprintf("%s%s?node_id=%s&filename=%s&filter_ansi_code=false&lines=100", historyServerURL, EndpointLogsFile, nodeID, filename)
		resp, err := client.Get(urlWithoutFilter)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
		bodyWithoutFilter, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())

		// Fetch with filter_ansi_code=true (ANSI codes should be removed)
		urlWithFilter := fmt.Sprintf("%s%s?node_id=%s&filename=%s&filter_ansi_code=true&lines=100", historyServerURL, EndpointLogsFile, nodeID, filename)
		resp2, err := client.Get(urlWithFilter)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp2.Body.Close()

		g.Expect(resp2.StatusCode).To(Equal(http.StatusOK))
		bodyWithFilter, err := io.ReadAll(resp2.Body)
		g.Expect(err).NotTo(HaveOccurred())

		// Check if original content contains ANSI codes using the same pattern as reader.go
		hasAnsiInOriginal := ansiEscapePattern.Match(bodyWithoutFilter)

		if hasAnsiInOriginal {
			LogWithTimestamp(test.T(), "Original log contains ANSI codes, verifying they are filtered")
			// Filtered content should NOT contain ANSI escape sequences
			hasAnsiInFiltered := ansiEscapePattern.Match(bodyWithFilter)
			g.Expect(hasAnsiInFiltered).To(BeFalse(), "Filtered content should not contain ANSI escape sequences")
		} else {
			LogWithTimestamp(test.T(), "Log doesn't contain ANSI codes, check is skipped...")
		}
	})

	test.T().Run("attempt_number behavior", func(t *testing.T) {
		g := NewWithT(t)
		// Test with attempt_number=0
		urlAttempt0 := fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=0", historyServerURL, EndpointLogsFile, nodeID, filename)
		resp, err := client.Get(urlAttempt0)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
		body, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(body)).To(BeNumerically(">", 0))
		LogWithTimestamp(test.T(), "attempt_number=0 returned %d bytes", len(body))

		// attempt_number=1 should fail as retry log doesn't exist for normal execution
		urlAttempt1 := fmt.Sprintf("%s%s?node_id=%s&filename=%s&attempt_number=1", historyServerURL, EndpointLogsFile, nodeID, filename)
		resp2, err := client.Get(urlAttempt1)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp2.Body.Close()

		g.Expect(resp2.StatusCode).To(Equal(http.StatusNotFound),
			"attempt_number=1 should return 404 when retry log doesn't exist")
		LogWithTimestamp(test.T(), "attempt_number=1 correctly returns 404 (file not found)")
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

			url := fmt.Sprintf("%s%s?task_id=%s", historyServerURL, EndpointLogsFile, url.QueryEscape(taskID))
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

			url := fmt.Sprintf("%s%s?actor_id=%s", historyServerURL, EndpointLogsFile, url.QueryEscape(actorID))
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
		url := fmt.Sprintf("%s%s?node_ip=%s&filename=%s", historyServerURL, EndpointLogsFile, savedNodeIP, filename)
		resp, err := client.Get(url)
		g.Expect(err).NotTo(HaveOccurred())
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Expected OK for valid node_ip, got %d: %s", resp.StatusCode, string(body))
		g.Expect(len(body)).To(BeNumerically(">", 0))

		// Test that node_ip and node_id point to the same node (should return same content)
		urlWithNodeID := fmt.Sprintf("%s%s?node_id=%s&filename=%s", historyServerURL, EndpointLogsFile, savedNodeID, filename)
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

// getAllEligibleActorIDs retrieves all actor IDs from the /logical/actors endpoint.
// Returns a list of actor IDs.
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

	// Find all actors
	for actorID := range actors {
		actorIDs = append(actorIDs, actorID)
	}

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

// testLogStreamEndpoint verifies the /v0/logs/stream endpoint behavior for both live and dead clusters.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster
// 2. Submit a Ray job to the existing cluster
// 3. Apply History Server and get its URL
// 4. Test live cluster: streaming should work (return 200)
// 5. Delete cluster to test dead cluster behavior
// 6. Test dead cluster: streaming should return 501 Not Implemented
// 7. Delete S3 bucket to ensure test isolation
func testLogStreamEndpoint(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	// Test 1: Live cluster - streaming should work
	LogWithTimestamp(test.T(), "Testing /v0/logs/stream for live cluster")
	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName))

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	nodeID := GetOneOfNodeID(g, client, historyServerURL, true)
	filename := "raylet.out"
	streamURL := fmt.Sprintf("%s%s?node_id=%s&filename=%s", historyServerURL, EndpointLogsStream, nodeID, filename)

	// Test live cluster streaming endpoint
	test.T().Run("live cluster", func(t *testing.T) {
		g := NewWithT(t)

		resp, err := client.Get(streamURL)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Live cluster should support log streaming")

		resp.Body.Close()
	})

	// Delete RayCluster to test dead cluster behavior
	err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Deleted RayCluster %s/%s", namespace.Name, rayCluster.Name)

	// Wait for cluster to be fully deleted
	g.Eventually(func() error {
		_, err := GetRayCluster(test, namespace.Name, rayCluster.Name)
		return err
	}, TestTimeoutMedium).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

	// Get the new cluster info
	deadClusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(deadClusterInfo.SessionName).NotTo(Equal(LiveSessionName))

	// Set context for dead cluster
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, deadClusterInfo.SessionName)

	// Test dead cluster streaming endpoint - should return 501
	test.T().Run("dead cluster", func(t *testing.T) {
		g := NewWithT(t)
		resp2, err := client.Get(streamURL)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp2.Body.Close()

		g.Expect(resp2.StatusCode).To(Equal(http.StatusNotImplemented), "Dead cluster should return 501 for log streaming")
		body, _ := io.ReadAll(resp2.Body)
		g.Expect(string(body)).To(ContainSubstring("Log streaming only available for live clusters"))
		LogWithTimestamp(test.T(), "Dead cluster correctly returns 501 Not Implemented")
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Log stream endpoint tests completed")
}

// testNodeLogsEndpointDeadCluster verifies that the /api/v0/logs endpoint correctly lists and filters
// log files for a dead cluster using the glob query parameter.
//
// The glob patterns tested mirror the four patterns constructed by the Ray Dashboard frontend:
// https://github.com/ray-project/ray/blob/60d14699591f1150eb71b8f657160cab9f767dfb/python/ray/dashboard/client/src/pages/log/Logs.tsx#L134-L141
//   - Case A: folder + filename search → "events/*RAYLET*"
//   - Case B: root + filename search   → "*dashboard*"
//   - Case C: folder only              → "events/*"
//   - Case D: no glob (omitted)        → server returns all files
//
// The test follows these steps:
// 1. Create a RayCluster and submit a Ray job.
// 2. Delete RayCluster to trigger log upload to S3.
// 3. Deploy the History Server and verify the cluster is listed as a dead session.
// 4. Case A — glob=events/*RAYLET*: a directory prefix combined with a wrapped wildcard matches a specific file in a subdirectory.
// 5. Case B — glob=*dashboard*: wildcards on both sides match any filename containing "dashboard" at root level.
// 6. Case C — glob=events/*: a directory prefix with a bare wildcard lists all files in that subdirectory.
// 7. Case D — no glob parameter: omitting the glob returns all log files (server default).
// 8. glob=raylet* — wildcard suffix matches raylet.out and raylet.err.
// 9. glob=nonexistent-*.xyz — a pattern matching no files returns an empty result.
// 10. glob=events/event_JOBS* — subdirectory prefix is split from the pattern, then the wildcard matches within that subdirectory.
// 11. Delete S3 bucket to ensure test isolation.
func testNodeLogsEndpointDeadCluster(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	DeleteRayClusterAndWait(test, g, namespace.Name, rayCluster.Name)

	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).NotTo(Equal(LiveSessionName), "Cluster should be a dead session after deletion")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	nodeID := GetOneOfNodeID(g, client, historyServerURL, false)

	// Case A: subdirectory + filename search — glob=events/*RAYLET*
	test.T().Run("Case A: glob=events/*RAYLET* matches RAYLET event file in subdirectory", func(t *testing.T) {
		g := NewWithT(t)

		logsURL := fmt.Sprintf("%s%s?node_id=%s&glob=%s", historyServerURL, EndpointLogs, nodeID, url.QueryEscape("events/*RAYLET*"))
		resp, err := client.Get(logsURL)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Expected OK, body: %s", string(body))

		result := parseLogsResponse(body)
		g.Expect(result).NotTo(BeNil(), "Response should be parseable, body: %s", string(body))

		// events/*RAYLET* should match only event_RAYLET.log in the events/ subdirectory.
		g.Expect(result).To(HaveLen(1), "Should only have one category, got: %v", result)
		internalFiles, _ := result["internal"].([]interface{})
		g.Expect(internalFiles).To(ConsistOf("event_RAYLET.log"), "events/*RAYLET* should match exactly event_RAYLET.log")
		LogWithTimestamp(t, "glob=events/*RAYLET* correctly returned %d file", len(internalFiles))
	})

	// Case B: root directory filename search — glob=*dashboard*
	test.T().Run("Case B: glob=*dashboard* matches dashboard files at root", func(t *testing.T) {
		g := NewWithT(t)

		logsURL := fmt.Sprintf("%s%s?node_id=%s&glob=%s", historyServerURL, EndpointLogs, nodeID, url.QueryEscape("*dashboard*"))
		resp, err := client.Get(logsURL)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Expected OK, body: %s", string(body))

		result := parseLogsResponse(body)
		g.Expect(result).NotTo(BeNil(), "Response should be parseable, body: %s", string(body))

		// *dashboard* should match files like dashboard.log, dashboard.err, etc.
		g.Expect(result).To(HaveKey("dashboard"), "Should have the 'dashboard' category, got: %v", result)
		dashboardFiles, _ := result["dashboard"].([]interface{})
		g.Expect(len(dashboardFiles)).To(BeNumerically(">", 0), "Should match at least one dashboard file")
		for _, f := range dashboardFiles {
			g.Expect(f.(string)).To(ContainSubstring("dashboard"), "Each matched file should contain 'dashboard'")
		}
		LogWithTimestamp(t, "glob=*dashboard* correctly returned %d dashboard files", len(dashboardFiles))
	})

	// Case C: enter subdirectory without search — glob=events/*
	test.T().Run("Case C: glob=events/* lists all files in events subdirectory", func(t *testing.T) {
		g := NewWithT(t)

		logsURL := fmt.Sprintf("%s%s?node_id=%s&glob=%s", historyServerURL, EndpointLogs, nodeID, url.QueryEscape("events/*"))
		resp, err := client.Get(logsURL)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Expected OK, body: %s", string(body))

		result := parseLogsResponse(body)
		g.Expect(result).NotTo(BeNil(), "Response should be parseable, body: %s", string(body))

		// events/* should list all event log files; they all fall into the "internal" category.
		totalFiles := countFiles(result)
		g.Expect(totalFiles).To(BeNumerically(">", 0), "events/* should return at least one file, got: %v", result)
		LogWithTimestamp(t, "glob=events/* correctly returned %d files across %d categories", totalFiles, len(result))
	})

	// Case D: no glob parameter — server returns all log files.
	test.T().Run("Case D: no glob parameter returns all log files", func(t *testing.T) {
		g := NewWithT(t)

		logsURL := fmt.Sprintf("%s%s?node_id=%s", historyServerURL, EndpointLogs, nodeID)
		resp, err := client.Get(logsURL)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Expected OK, body: %s", string(body))

		result := parseLogsResponse(body)
		g.Expect(result).NotTo(BeNil(), "Response should be parseable, body: %s", string(body))
		g.Expect(countFiles(result)).To(BeNumerically(">", 0), "No glob should return all files, got: %v", result)
		LogWithTimestamp(t, "No glob parameter correctly returned %d files across %d categories", countFiles(result), len(result))
	})

	test.T().Run("glob=raylet* matches exactly raylet.out and raylet.err", func(t *testing.T) {
		g := NewWithT(t)

		logsURL := fmt.Sprintf("%s%s?node_id=%s&glob=%s", historyServerURL, EndpointLogs, nodeID, url.QueryEscape("raylet*"))
		resp, err := client.Get(logsURL)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Expected OK, body: %s", string(body))

		result := parseLogsResponse(body)
		g.Expect(result).NotTo(BeNil(), "Response should be parseable, body: %s", string(body))

		// raylet* matches only raylet.out and raylet.err, so only the "raylet" category should be present.
		g.Expect(result).To(HaveLen(1), "Should only have the 'raylet' category, got: %v", result)
		rayletFiles, _ := result["raylet"].([]interface{})
		g.Expect(rayletFiles).To(ConsistOf("raylet.out", "raylet.err"),
			"raylet* should match exactly raylet.out and raylet.err")
		LogWithTimestamp(t, "glob=raylet* correctly returned %d raylet files", len(rayletFiles))
	})

	test.T().Run("glob pattern matching no files returns empty result", func(t *testing.T) {
		g := NewWithT(t)

		logsURL := fmt.Sprintf("%s%s?node_id=%s&glob=%s", historyServerURL, EndpointLogs, nodeID, url.QueryEscape("nonexistent-*.xyz"))
		resp, err := client.Get(logsURL)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Expected OK even when glob matches nothing, body: %s", string(body))

		result := parseLogsResponse(body)
		g.Expect(result).NotTo(BeNil(), "Response should be parseable, body: %s", string(body))
		g.Expect(countFiles(result)).To(Equal(0), "A glob matching no files should return an empty result")
		LogWithTimestamp(t, "Non-matching glob correctly returned 0 files")
	})

	test.T().Run("glob=events/event_JOBS* matches exactly one file in the events subdirectory", func(t *testing.T) {
		g := NewWithT(t)

		// glob=events/event_JOBS* should match only event_JOBS.log inside the events/ subdirectory.
		// Expected response:
		//   {"data":{"result":{"internal":["event_JOBS.log"]}},"msg":"","result":true}
		logsURL := fmt.Sprintf("%s%s?node_id=%s&glob=%s", historyServerURL, EndpointLogs, nodeID, url.QueryEscape("events/event_JOBS*"))
		resp, err := client.Get(logsURL)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK), "Expected OK, body: %s", string(body))

		result := parseLogsResponse(body)
		g.Expect(result).NotTo(BeNil(), "Response should be parseable, body: %s", string(body))

		// events/event_JOBS* maps to the "internal" category; no other category should be present.
		g.Expect(result).To(HaveLen(1), "Should only have the 'internal' category, got: %v", result)
		internalFiles, _ := result["internal"].([]interface{})
		g.Expect(internalFiles).To(ConsistOf("event_JOBS.log"), "glob=events/event_JOBS* should match exactly event_JOBS.log")
		LogWithTimestamp(t, "glob=events/event_JOBS* correctly returned %d file", len(internalFiles))
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster /api/v0/logs glob endpoint tests completed")
}

// parseLogsResponse parses the /api/v0/logs response body and returns the categorized
// file map from data.result. Returns nil on any parse failure.
func parseLogsResponse(body []byte) map[string]interface{} {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return nil
	}
	result, ok := data["result"].(map[string]interface{})
	if !ok {
		return nil
	}
	return result
}

// countFiles counts the total number of file entries across all categories in the result map.
func countFiles(result map[string]interface{}) int {
	total := 0
	for _, v := range result {
		if files, ok := v.([]interface{}); ok {
			total += len(files)
		}
	}
	return total
}

// testTimelineEndpointLiveCluster verifies that the history server can return timeline data from a live cluster.
//
// The test follows these steps:
// 1. Create a RayCluster and submit a Ray job.
// 2. Deploy the History Server and ensure the cluster is listed as a live session.
// 3. Verify the /api/v0/tasks/timeline endpoint behavior for a live cluster:
//   - Without params: returns a non-empty Chrome Tracing JSON array with required metadata
//     and trace events.
//   - With job_id=<id>: returns only events for the specified job.
//   - With download=1: sets Content-Disposition to attachment and includes a filename.
//   - With download=1&job_id=<id>: filename includes the job_id.
//
// 4. Delete S3 bucket to ensure test isolation
func testTimelineEndpointLiveCluster(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)
	jobID := GetOneOfJobID(g, client, historyServerURL)

	test.T().Run("should return valid timeline data", func(t *testing.T) {
		g := NewWithT(t)
		verifyTimelineResponse(g, client, historyServerURL, "", false)
	})
	test.T().Run("with valid job_id returns filtered events", func(t *testing.T) {
		g := NewWithT(t)
		verifyTimelineResponse(g, client, historyServerURL, jobID, false)
	})

	test.T().Run("download=1 sets Content-Disposition and filename", func(t *testing.T) {
		g := NewWithT(t)
		verifyTimelineResponse(g, client, historyServerURL, "", true)
	})

	test.T().Run("download=1 with job_id sets filename with job_id", func(t *testing.T) {
		g := NewWithT(t)
		verifyTimelineResponse(g, client, historyServerURL, jobID, true)
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Live cluster timeline endpoint test completed")
}

// testTimelineEndpointDeadCluster verifies that the history server can serve task timeline data from S3
// after a Ray cluster is deleted, and that the timeline endpoint supports query parameters.
//
// The test follows these steps:
// 1. Create a RayCluster and submit a Ray job.
// 2. Delete the RayCluster to trigger event export/upload to S3 and wait until deletion completes.
// 3. Deploy the History Server and switch the client context to a non-live (archived) session.
// 4. Verify the /api/v0/tasks/timeline endpoint behavior:
//   - Without params: returns a non-empty Chrome Tracing JSON array with required metadata + trace events.
//   - With job_id=<id>: returns only events for the specified job (job_id may require hex->base64 normalization
//     depending on the source endpoint/session).
//   - With download=1: sets Content-Disposition to attachment and includes a filename.
//   - With download=1&job_id=<id>: filename includes the job_id.
//
// 5. Delete S3 bucket to ensure test isolation
func testTimelineEndpointDeadCluster(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	// Delete RayCluster to trigger event upload
	err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Deleted RayCluster %s/%s", namespace.Name, rayCluster.Name)

	// Wait for cluster to be fully deleted (ensures events are uploaded to S3)
	g.Eventually(func() error {
		_, err := GetRayCluster(test, namespace.Name, rayCluster.Name)
		return err
	}, TestTimeoutMedium).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).NotTo(Equal(LiveSessionName))

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)
	jobID := GetOneOfJobID(g, client, historyServerURL)

	test.T().Run("should return timeline data from S3", func(t *testing.T) {
		g := NewWithT(t)
		verifyTimelineResponse(g, client, historyServerURL, "", false)
	})
	test.T().Run("with valid job_id returns filtered events", func(t *testing.T) {
		g := NewWithT(t)
		verifyTimelineResponse(g, client, historyServerURL, jobID, false)
	})

	test.T().Run("download=1 sets Content-Disposition and filename", func(t *testing.T) {
		g := NewWithT(t)
		verifyTimelineResponse(g, client, historyServerURL, "", true)
	})

	test.T().Run("download=1 with job_id sets filename with job_id", func(t *testing.T) {
		g := NewWithT(t)
		verifyTimelineResponse(g, client, historyServerURL, jobID, true)
	})
	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster timeline endpoint test completed")
}

// verifyTimelineResponse verifies the timeline endpoint returns valid Chrome Tracing format.
// jobID: optional filter; empty means no job_id query param.
// download: if true, adds download=1 and asserts Content-Disposition header and filename.
func verifyTimelineResponse(g *WithT, client *http.Client, historyServerURL string, jobID string, download bool) {
	baseURL := historyServerURL + "/api/v0/tasks/timeline"
	if jobID != "" || download {
		params := url.Values{}
		if jobID != "" {
			params.Set("job_id", jobID)
		}
		if download {
			params.Set("download", "1")
		}
		baseURL += "?" + params.Encode()
	}

	g.Eventually(func(gg Gomega) {
		resp, err := client.Get(baseURL)
		gg.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

		if download {
			cd := resp.Header.Get("Content-Disposition")
			gg.Expect(cd).To(ContainSubstring("attachment"), "Content-Disposition should contain attachment")
			gg.Expect(cd).To(ContainSubstring("filename="), "Content-Disposition should contain filename")
			if jobID != "" {
				gg.Expect(cd).To(ContainSubstring(jobID), "filename should contain job_id when job_id filter is set")
			}
		}

		body, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())

		var events []map[string]any
		err = json.Unmarshal(body, &events)
		gg.Expect(err).NotTo(HaveOccurred())

		// Should have at least some events
		gg.Expect(len(events)).To(BeNumerically(">", 0), "Timeline should have at least one event")

		// Verify all the job_id are same
		if jobID != "" {
			for _, event := range events {
				ph, _ := event["ph"].(string)
				if ph != "X" {
					continue
				}
				args, ok := event["args"].(map[string]any)
				gg.Expect(ok).To(BeTrue(), "trace event should have args")
				jid, ok := args["job_id"]
				gg.Expect(ok).To(BeTrue(), "trace event args should have job_id")
				gg.Expect(jid).To(Equal(jobID),
					"when job_id filter is %q, every trace event's args.job_id must be the same, got %v", jobID, jid)
			}
		}

		// Verify metadata and trace events exist
		hasProcessName := false
		hasThreadName := false
		hasTraceEvent := false

		for _, event := range events {
			name, _ := event["name"].(string)
			ph, _ := event["ph"].(string)

			if name == "process_name" && ph == "M" {
				hasProcessName = true
				argsAny, exists := event["args"]
				gg.Expect(exists).To(BeTrue(), "process_name should have 'args' field")

				args, ok := argsAny.(map[string]any)
				gg.Expect(ok).To(BeTrue(), "process_name args should be a map[string]any")
				gg.Expect(args["name"]).NotTo(BeNil(), "process_name args should have 'name'")
			}
			if name == "thread_name" && ph == "M" {
				hasThreadName = true
			}
			if ph == "X" {
				hasTraceEvent = true
				// Verify trace event has required fields
				gg.Expect(event["cat"]).NotTo(BeNil(), "Trace event should have 'cat'")
				gg.Expect(event["ts"]).NotTo(BeNil(), "Trace event should have 'ts'")
				gg.Expect(event["dur"]).NotTo(BeNil(), "Trace event should have 'dur'")
				gg.Expect(event["args"]).NotTo(BeNil(), "Trace event should have 'args'")

				// Verify args structure
				argsAny, exists := event["args"]
				gg.Expect(exists).To(BeTrue(), "Trace event should have 'args' field")

				args, ok := argsAny.(map[string]any)
				gg.Expect(ok).To(BeTrue(), "Trace event args should be a map[string]any")
				gg.Expect(args["task_id"]).NotTo(BeNil(), "args should have 'task_id'")
				gg.Expect(args["job_id"]).NotTo(BeNil(), "args should have 'job_id'")
			}
		}

		gg.Expect(hasProcessName).To(BeTrue(), "Should have process_name metadata event")
		gg.Expect(hasThreadName).To(BeTrue(), "Should have thread_name metadata event")
		gg.Expect(hasTraceEvent).To(BeTrue(), "Should have at least one trace event")
	}, TestTimeoutShort).Should(Succeed())
}

// testLiveClusterTasks verifies that the /v0/tasks?detail=1 endpoint for a live cluster will return the
// detailed task information of all task attempts.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster
// 2. Submit a Ray job to the existing cluster
// 3. Apply History Server and get its URL
// 4. Get the cluster information and set the cluster context with the session name 'live'
// 5. Hit /api/v0/tasks?detail=1 to get the detailed task information of all task attempts
// 6. Verify the response status code is 200
// 7. Verify the response API schema
// 8. Delete S3 bucket to ensure test isolation
func testLiveClusterTasks(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	endpoint := EndpointTasks + "?detail=1"

	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)
	endpointURL := historyServerURL + endpoint
	LogWithTimestamp(test.T(), "Testing %s endpoint for live cluster: %s", endpoint, endpointURL)

	var tasksResp map[string]any
	g.Eventually(func(gg Gomega) {
		resp, err := client.Get(endpointURL)
		gg.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(resp.StatusCode).To(Equal(http.StatusOK),
			"[GET] %s should return 200, got %d: %s", endpointURL, resp.StatusCode, string(body))

		err = json.Unmarshal(body, &tasksResp)
		gg.Expect(err).NotTo(HaveOccurred())
	}, TestTimeoutShort).Should(Succeed())

	LogWithTimestamp(test.T(), "Verifying /api/v0/tasks?detail=1 response schema for live cluster")
	verifyTasksRespSchema(test, g, tasksResp, true)

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Live cluster /api/v0/tasks?detail=1 tests completed successfully")
}

// testDeadClusterTasks verifies that the /api/v0/tasks endpoint for a dead cluster will return the
// detailed task information of all task attempts without historical replay.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster with the collector
// 2. Submit a Ray job to the existing cluster and wait for completion
// 3. Delete the Ray cluster to trigger event flushing and wait for cluster deletion to complete
// 4. Apply History Server and get its URL
// 5. Get the cluster information and set the cluster context with the session name of the dead cluster
// 6. Run /api/v0/tasks endpoint with various query parameters to test limit, detail, exclude_driver, and filters
// 7. Verify the response status code is 200
// 8. Verify the response API schema
// 9. Delete S3 bucket to ensure test isolation
//
// NOTE: timeout is not tested because tasks are in-memory and retrieval is typically fast.
func testDeadClusterTasks(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	// Delete the Ray cluster to trigger event flushing.
	LogWithTimestamp(test.T(), "Deleting RayCluster %s/%s to trigger event flushing", rayCluster.Namespace, rayCluster.Name)
	err := test.Client().Ray().RayV1().
		RayClusters(rayCluster.Namespace).
		Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(func() error {
		_, err := GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		return err
	}, TestTimeoutMedium).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(SatisfyAll(Not(BeEmpty()), Not(Equal(LiveSessionName))))

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	jobIDs := getAllEligibleJobIDs(g, client, historyServerURL)
	jobIDForFilter := jobIDs[0]

	verifyTasksEndpoint := func(
		t *testing.T,
		tcName,
		queryParams string,
		detail bool,
		expectedStatus int,
	) {
		g := NewWithT(t)

		url := historyServerURL + EndpointTasks + "?" + queryParams
		resp, err := client.Get(url)
		g.Expect(err).NotTo(HaveOccurred())
		defer func() {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()

		body, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.StatusCode).To(Equal(expectedStatus),
			"Test case '%s' failed: expected %d, got %d", tcName, expectedStatus, resp.StatusCode)

		if resp.StatusCode == http.StatusOK {
			var tasksResp map[string]any
			err = json.Unmarshal(body, &tasksResp)
			g.Expect(err).NotTo(HaveOccurred())

			LogWithTimestamp(test.T(), "Verifying %s response schema for dead cluster", url)
			verifyTasksRespSchema(test, g, tasksResp, detail)
		}
	}

	filterTestCases := []struct {
		name           string
		queryParams    string
		expectedStatus int
	}{
		// Basic / default
		{"no params (default)", "", http.StatusOK},

		// limit
		{"limit=1", "limit=1", http.StatusOK},
		{"limit=100", "limit=100", http.StatusOK},
		{"limit=10000", "limit=10000", http.StatusOK},
		{"limit=0", "limit=0", http.StatusOK},
		{"invalid limit (string)", "limit=abc", http.StatusBadRequest},

		// exclude_driver
		{"exclude_driver=true", "exclude_driver=true", http.StatusOK},
		{"exclude_driver=false", "exclude_driver=false", http.StatusOK},
		{"invalid exclude_driver (string)", "exclude_driver=invalid", http.StatusBadRequest},

		// Single filter
		{"state=FINISHED", "filter_keys=state&filter_predicates==&filter_values=FINISHED", http.StatusOK},
		{"state!=PENDING_ARGS_AVAIL", "filter_keys=state&filter_predicates=!=&filter_values=PENDING_ARGS_AVAIL", http.StatusOK},
		{fmt.Sprintf("job_id=%s", jobIDForFilter), "filter_keys=job_id&filter_predicates==&filter_values=" + jobIDForFilter, http.StatusOK},

		// Multiple filters
		{"statte=FINISHED & type=ACTOR_TASK", "filter_keys=state&filter_keys=task_type&filter_predicates==&filter_predicates==&filter_values=FINISHED&filter_values=ACTOR_TASK", http.StatusOK},

		// Invalid filters
		{"len(filter_keys) != len(filter_values)", "filter_keys=state&filter_keys=job_id&filter_predicates==&filter_values=FINISHED", http.StatusBadRequest},
		{"filter_keys only (missing predicates and values)", "filter_keys=state", http.StatusBadRequest},

		// Combined
		{"limit=5 & detail=true", "limit=5&detail=true", http.StatusOK},
		{"limit=5 & exclude_driver=true", "limit=5&exclude_driver=true", http.StatusOK},
		{"limit=5 & detail=true & exclude_driver=false", "limit=5&detail=true&exclude_driver=false", http.StatusOK},
		{"limit=10 & state=FINISHED", "limit=10&filter_keys=state&filter_predicates==&filter_values=FINISHED", http.StatusOK},
		{"limit=10 & detail=true & exclude_driver=false & state=FINISHED", "limit=10&detail=true&exclude_driver=false&filter_keys=state&filter_predicates==&filter_values=FINISHED", http.StatusOK},
	}
	for _, tc := range filterTestCases {
		test.T().Run(tc.name, func(t *testing.T) {
			verifyTasksEndpoint(t, tc.name, tc.queryParams, false, tc.expectedStatus)
		})
	}

	detailTestCases := []struct {
		name           string
		queryParams    string
		detail         bool
		expectedStatus int
	}{
		{"detail=true", "detail=true", true, http.StatusOK},
		{"detail=false", "detail=false", false, http.StatusOK},
		{"detail=1", "detail=1", true, http.StatusOK},
		{"invalid detail (string)", "detail=invalid", false, http.StatusBadRequest},
	}
	for _, tc := range detailTestCases {
		test.T().Run(tc.name, func(t *testing.T) {
			verifyTasksEndpoint(t, tc.name, tc.queryParams, tc.detail, tc.expectedStatus)
		})
	}

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster /api/v0/tasks tests completed successfully")
}

// testLiveClusterNodes verifies that the /nodes?view=summary endpoint for a live cluster will return the current
// snapshot containing node summary and resource usage information.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster with the collector
// 2. Submit a Ray job to the existing cluster and wait for completion
// 3. Apply History Server and get its URL
// 4. Get the cluster information and set the cluster context with the session name 'live'
// 5. Hit /nodes?view=summary to get the current snapshot containing node summary and resource usage information
// 6. Verify the response status code is 200
// 7. Verify the response API schema
// 8. Delete S3 bucket to ensure test isolation
func testLiveClusterNodes(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	// Explicitly specify the view parameter to get the current snapshot.
	// If the view parameter is not specified, the following error will be returned:
	// {"result": false, "msg": "Unknown view None", "data": {}}
	// Ref: https://github.com/ray-project/kuberay/pull/4412.
	endpoint := "/nodes?view=summary"

	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	LogWithTimestamp(test.T(), "Verifying /nodes response schema for live cluster (isLive=true)")
	endpointURL := historyServerURL + endpoint
	verifySingleEndpoint(test, g, client, endpointURL, func(test Test, g *WithT, data map[string]any) {
		verifyNodesRespSchema(test, g, data, true)
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Live cluster /nodes?view=summary tests completed successfully")
}

// testDeadClusterNodes verifies that the /nodes endpoint for a dead cluster will return the historical replay
// containing node summary and resource usage snapshots of a cluster session.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster with the collector
// 2. Submit a Ray job to the existing cluster and wait for completion
// 3. Delete the Ray cluster to trigger event flushing and wait for cluster deletion to complete
// 4. Apply History Server and get its URL
// 5. Get the cluster information and set the cluster context with the session name of the dead cluster
// 6. Hit /nodes endpoint to get the historical replay containing node summary and resource usage snapshots
// 7. Verify the response status code is 200
// 8. Verify the response API schema
// 9. Delete S3 bucket to ensure test isolation
func testDeadClusterNodes(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	// Delete the Ray cluster to trigger event flushing.
	LogWithTimestamp(test.T(), "Deleting RayCluster %s/%s to trigger event flushing", rayCluster.Namespace, rayCluster.Name)
	err := test.Client().Ray().RayV1().
		RayClusters(rayCluster.Namespace).
		Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(func() error {
		_, err := GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		return err
	}, TestTimeoutMedium).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(SatisfyAll(Not(BeEmpty()), Not(Equal(LiveSessionName))))

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	endpointURL := historyServerURL + EndpointNodes
	LogWithTimestamp(test.T(), "Testing %s endpoint for dead cluster: %s", EndpointNodes, endpointURL)

	LogWithTimestamp(test.T(), "Verifying /nodes?view=summary response schema for dead cluster (isLive=false)")
	verifySingleEndpoint(test, g, client, endpointURL+"?view=summary", func(test Test, g *WithT, data map[string]any) {
		verifyNodesRespSchema(test, g, data, false)
	})

	LogWithTimestamp(test.T(), "Verifying /nodes?view=hostNameList response schema for dead cluster (isLive=false)")
	verifySingleEndpoint(test, g, client, endpointURL+"?view=hostNameList", func(test Test, g *WithT, data map[string]any) {
		verifyNodesHostNameListSchema(test, g, data, false)
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster /nodes tests completed successfully")
}

// testLiveClusterNode verifies that the /nodes/{node_id} endpoint for a live cluster will return the current snapshot
// containing node summary of the specified node.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster with the collector
// 2. Submit a Ray job to the existing cluster and wait for completion
// 3. Get the head and worker node IDs
// 4. Apply History Server and get its URL
// 5. Get the cluster information and set the cluster context with the session name 'live'
// 6. Hit /nodes/{node_id} for both the head node and the worker node:
//   - Get the node details of the specified node
//   - Verify the response status code is 200
//   - Verify the response API schema
//
// 7. Delete S3 bucket to ensure test isolation
func testLiveClusterNode(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	headNodeID := GetNodeIDFromPod(test, g, HeadPod(test, rayCluster), "ray-head")
	workerNodeID := GetNodeIDFromPod(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")

	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	for _, nodeId := range []string{headNodeID, workerNodeID} {
		LogWithTimestamp(test.T(), "Verifying /nodes/%s response schema for live cluster (isLive=true)", nodeId)

		endpoint := fmt.Sprintf("%s/%s", EndpointNodes, nodeId)
		endpointURL := historyServerURL + endpoint
		verifySingleEndpoint(test, g, client, endpointURL, func(test Test, g *WithT, data map[string]any) {
			verifyNodeRespSchema(test, g, data, true)
		})
	}

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Live cluster /nodes/{node_id} tests completed successfully")
}

// testDeadClusterNode verifies that the /nodes/{node_id} endpoint for a dead cluster will return the historical replay
// containing node summary snapshots of the specified node in a cluster session.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster with the collector
// 2. Submit a Ray job to the existing cluster and wait for completion
// 3. Get the head and worker node IDs
// 4. Delete the Ray cluster to trigger event flushing and wait for cluster deletion to complete
// 5. Apply History Server and get its URL
// 6. Get the cluster information and set the cluster context with the session name of the dead cluster
// 7. Hit /nodes/{node_id} for both the head node and the worker node:
//   - Get the node details of the specified node
//   - Verify the response status code is 200
//   - Verify the response API schema
//
// 8. Delete S3 bucket to ensure test isolation
func testDeadClusterNode(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	headNodeID := GetNodeIDFromPod(test, g, HeadPod(test, rayCluster), "ray-head")
	workerNodeID := GetNodeIDFromPod(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")

	// Delete the Ray cluster to trigger event flushing.
	LogWithTimestamp(test.T(), "Deleting RayCluster %s/%s to trigger event flushing", rayCluster.Namespace, rayCluster.Name)
	err := test.Client().Ray().RayV1().
		RayClusters(rayCluster.Namespace).
		Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(func() error {
		_, err := GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		return err
	}, TestTimeoutMedium).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(SatisfyAll(Not(BeEmpty()), Not(Equal(LiveSessionName))))

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	for _, nodeId := range []string{headNodeID, workerNodeID} {
		LogWithTimestamp(test.T(), "Verifying /nodes/%s response schema for dead cluster (isLive=false)", nodeId)

		endpoint := fmt.Sprintf("%s/%s", EndpointNodes, nodeId)
		endpointURL := historyServerURL + endpoint
		verifySingleEndpoint(test, g, client, endpointURL, func(test Test, g *WithT, data map[string]any) {
			verifyNodeRespSchema(test, g, data, false)
		})
	}

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster /nodes/{node_id} tests completed successfully")
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
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK),
				"Endpoint %s should return 200, got %d: %s", endpoint, resp.StatusCode, string(body))

			LogWithTimestamp(test.T(), "Endpoint %s returned status %d", endpoint, resp.StatusCode)
		}, TestTimeoutShort).Should(Succeed())
	}
}

// verifyHistoryServerGrafanaHealthEndpoint tests the /api/grafana_health endpoint
func verifyHistoryServerGrafanaHealthEndpoint(test Test, g *WithT, client *http.Client, historyServerURL string, sessionID string) {
	endpoint := HistoryServerEndpointGrafanaHealth
	LogWithTimestamp(test.T(), "Testing history server endpoint: %s", endpoint)

	g.Eventually(func(gg Gomega) {
		resp, err := client.Get(historyServerURL + endpoint)
		gg.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(resp.StatusCode).To(Equal(200),
			"Endpoint %s should return 200, got %d: %s", endpoint, resp.StatusCode, string(body))

		gg.Expect(body).To(MatchJSON(fmt.Sprintf(HistoryServerGrafanaHealthResponse, RayGrafanaIframeHost, sessionID)))
		LogWithTimestamp(test.T(), "Endpoint %s returned status %d", endpoint, resp.StatusCode)
	}, TestTimeoutShort).Should(Succeed())

}

// verifyHistoryServerPrometheusHealthEndpoint tests the /api/prometheus_health endpoint
func verifyHistoryServerPrometheusHealthEndpoint(test Test, g *WithT, client *http.Client, historyServerURL string) {
	endpoint := HistoryServerEndpointPrometheusHealth
	LogWithTimestamp(test.T(), "Testing history server endpoint: %s", endpoint)

	g.Eventually(func(gg Gomega) {
		resp, err := client.Get(historyServerURL + endpoint)
		gg.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(resp.StatusCode).To(Equal(200),
			"Endpoint %s should return 200, got %d: %s", endpoint, resp.StatusCode, string(body))

		var result map[string]any
		err = json.Unmarshal(body, &result)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(result["result"]).To(Equal(true), "Response should have result=true")
		gg.Expect(result["msg"]).To(ContainSubstring("prometheus running"), "Response message should contain 'prometheus running'")
		LogWithTimestamp(test.T(), "Endpoint %s returned status %d with valid response", endpoint, resp.StatusCode)
	}, TestTimeoutShort).Should(Succeed())
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

// getAllEligibleJobIDs retrieves all job IDs from the /api/v0/tasks endpoint for the task filtering test cases.
func getAllEligibleJobIDs(g *WithT, client *http.Client, historyServerURL string) []string {
	var jobIDs []string

	resp, err := client.Get(historyServerURL + EndpointTasks)
	g.Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()

	g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	body, err := io.ReadAll(resp.Body)
	g.Expect(err).NotTo(HaveOccurred())

	var tasksResp map[string]any
	err = json.Unmarshal(body, &tasksResp)
	g.Expect(err).NotTo(HaveOccurred())

	tasksData, ok := tasksResp["data"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "'data' should be a map")
	tasksDataResult, ok := tasksData["result"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "'result' should be a map")
	formattedTasks, ok := tasksDataResult["result"].([]any)
	g.Expect(ok).To(BeTrue(), "'result' should be an array")
	for _, formattedTask := range formattedTasks {
		task, ok := formattedTask.(map[string]any)
		g.Expect(ok).To(BeTrue(), "formattedTask should be a map")
		g.Expect(task).To(HaveKey("job_id"))
		jobID, ok := task["job_id"].(string)
		g.Expect(ok).To(BeTrue(), "job_id should be a string")
		if jobID == "" {
			continue
		}
		jobIDs = append(jobIDs, jobID)
	}
	g.Expect(len(jobIDs)).To(BeNumerically(">", 0), "should have at least one eligible job ID")

	return jobIDs
}

// verifyTasksRespSchema verifies that the /v0/tasks response is valid according to the API schema.
func verifyTasksRespSchema(test Test, g *WithT, tasksResp map[string]any, detail bool) {
	// Verify top-level fields.
	g.Expect(tasksResp).To(HaveKeyWithValue("result", BeTrue()))
	g.Expect(tasksResp).To(HaveKeyWithValue("msg", Equal("")))
	g.Expect(tasksResp).To(HaveKey("data"))

	tasksData, ok := tasksResp["data"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "'data' should be a map")
	g.Expect(tasksData).To(HaveKey("result"))

	tasksDataResult, ok := tasksData["result"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "'result' should be a map")
	g.Expect(tasksDataResult).To(HaveKey("total"))
	g.Expect(tasksDataResult).To(HaveKey("result"))
	g.Expect(tasksDataResult).To(HaveKey("num_after_truncation"))
	g.Expect(tasksDataResult).To(HaveKey("num_filtered"))
	g.Expect(tasksDataResult).To(HaveKey("partial_failure_warning"))
	g.Expect(tasksDataResult).To(HaveKey("warnings"))

	// Verify formatted task fields.
	formattedTasks, ok := tasksDataResult["result"].([]any)
	g.Expect(ok).To(BeTrue(), "'result' should be an array")
	LogWithTimestamp(test.T(), "Verifying formatted task fields for each task attempt")
	for _, formattedTask := range formattedTasks {
		formattedTaskMap, ok := formattedTask.(map[string]any)
		g.Expect(ok).To(BeTrue(), "formattedTask should be a map")
		g.Expect(formattedTaskMap).To(HaveKey("task_id"))
		g.Expect(formattedTaskMap).To(HaveKey("attempt_number"))
		g.Expect(formattedTaskMap).To(HaveKey("name"))
		g.Expect(formattedTaskMap).To(HaveKey("state"))
		g.Expect(formattedTaskMap).To(HaveKey("job_id"))
		g.Expect(formattedTaskMap).To(HaveKey("actor_id"))
		g.Expect(formattedTaskMap).To(HaveKey("type"))
		g.Expect(formattedTaskMap).To(HaveKey("func_or_class_name"))
		g.Expect(formattedTaskMap).To(HaveKey("parent_task_id"))
		g.Expect(formattedTaskMap).To(HaveKey("node_id"))
		g.Expect(formattedTaskMap).To(HaveKey("worker_id"))
		g.Expect(formattedTaskMap).To(HaveKey("worker_pid"))
		g.Expect(formattedTaskMap).To(HaveKey("error_type"))
		if detail {
			g.Expect(formattedTaskMap).To(HaveKey("language"))
			g.Expect(formattedTaskMap).To(HaveKey("required_resources"))
			g.Expect(formattedTaskMap).To(HaveKey("runtime_env_info"))
			g.Expect(formattedTaskMap).To(HaveKey("placement_group_id"))
			g.Expect(formattedTaskMap).To(HaveKey("events"))
			// g.Expect(formattedTaskMap).To(HaveKey("profiling_data"))
			g.Expect(formattedTaskMap).To(HaveKey("creation_time_ms"))
			g.Expect(formattedTaskMap).To(HaveKey("start_time_ms"))
			g.Expect(formattedTaskMap).To(HaveKey("end_time_ms"))
			g.Expect(formattedTaskMap).To(HaveKey("task_log_info"))
			g.Expect(formattedTaskMap).To(HaveKey("error_message"))
			g.Expect(formattedTaskMap).To(HaveKey("is_debugger_paused"))
			g.Expect(formattedTaskMap).To(HaveKey("call_site"))
			g.Expect(formattedTaskMap).To(HaveKey("label_selector"))
		}
	}
	LogWithTimestamp(test.T(), "Task response schema verified successfully")
}

// verifySingleEndpoint verifies the response schema of a single endpoint.
func verifySingleEndpoint(test Test, g *WithT, client *http.Client, endpointURL string, verifySchema func(test Test, g *WithT, data map[string]any)) {
	var respData map[string]any
	g.Eventually(func(gg Gomega) {
		resp, err := client.Get(endpointURL)
		gg.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

		body, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())

		err = json.Unmarshal(body, &respData)
		gg.Expect(err).NotTo(HaveOccurred())
	}, TestTimeoutShort).Should(Succeed())

	verifySchema(test, g, respData)
}

// TODO(jwj): Make verification for node-related endpoints more robust.
// verifyNodesRespSchema verifies that the /nodes response is valid according to the API schema.
// isLive indicates whether the response is from a live cluster or a dead cluster:
//   - isLive: true for a live cluster (current snapshot)
//   - isLive: false for a dead cluster (historical replay)
func verifyNodesRespSchema(test Test, g *WithT, nodesResp map[string]any, isLive bool) {
	// Verify top-level fields.
	g.Expect(nodesResp).To(HaveKeyWithValue("result", BeTrue()))
	g.Expect(nodesResp).To(HaveKeyWithValue("msg", Equal("Node summary fetched.")))
	g.Expect(nodesResp).To(HaveKey("data"))

	data, ok := nodesResp["data"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "'data' should be a map")
	g.Expect(data).To(HaveKey("summary"))
	g.Expect(data).To(HaveKey("nodeLogicalResources"))

	// Verify summary field.
	LogWithTimestamp(test.T(), "Verifying summary field")
	summary, ok := data["summary"].([]any)
	g.Expect(ok).To(BeTrue(), "'summary' should be an array")

	if isLive {
		// Live cluster: summary contains node summary snapshot of each node in the cluster.
		g.Expect(len(summary)).To(Equal(2), "Live cluster should have 2 node summaries (one head node and one worker node)")
		for _, nodeSummary := range summary {
			nodeSummarySnapshot, ok := nodeSummary.(map[string]any)
			g.Expect(ok).To(BeTrue(), "nodeSummary should be a map")
			verifyNodeSummarySchema(test, g, nodeSummarySnapshot)
		}
	} else {
		// Dead cluster: summary contains node summary replay (array of snapshots) of each node in the cluster.
		// The node summary replay should follow the chronological order of the node state transitions.
		g.Expect(len(summary)).To(Equal(2), "Dead cluster should have 2 node summary replays (one head node and one worker node)")
		for _, nodeSummaryReplay := range summary {
			nodeSummarySnapshots, ok := nodeSummaryReplay.([]any)
			g.Expect(ok).To(BeTrue(), "nodeSummaryReplay should be an array")

			for _, nodeSummarySnapshot := range nodeSummarySnapshots {
				nodeSummarySnapshotMap, ok := nodeSummarySnapshot.(map[string]any)
				g.Expect(ok).To(BeTrue(), "nodeSummarySnapshot should be a map")
				verifyNodeSummarySchema(test, g, nodeSummarySnapshotMap)
			}
		}
	}

	// Verify nodeLogicalResources field.
	LogWithTimestamp(test.T(), "Verifying nodeLogicalResources field")
	nodeLogicalResources, ok := data["nodeLogicalResources"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "'nodeLogicalResources' should be a map")

	if isLive {
		// Live cluster: nodeLogicalResources contains resource string of each node in the cluster.
		g.Expect(len(nodeLogicalResources)).To(Equal(2), "Live cluster should have 2 resource strings (one head node and one worker node)")
		for nodeId, resourceString := range nodeLogicalResources {
			g.Expect(nodeId).NotTo(BeEmpty())
			g.Expect(resourceString).NotTo(BeEmpty())
		}
	} else {
		// Dead cluster: nodeLogicalResources contains resource string replay (array of snapshots) of each node in the cluster.
		// The resource string replay should follow the chronological order of the node state transitions.
		g.Expect(len(nodeLogicalResources)).To(Equal(2), "Dead cluster should have 2 resource string replays (one head node and one worker node)")
		for nodeId, resourceStringReplay := range nodeLogicalResources {
			g.Expect(nodeId).NotTo(BeEmpty())

			resourceStringSnapshots, ok := resourceStringReplay.([]any)
			g.Expect(ok).To(BeTrue(), "resourceStringReplay should be an array")
			for _, resourceStringSnapshot := range resourceStringSnapshots {
				resourceStringSnapshotMap, ok := resourceStringSnapshot.(map[string]any)
				g.Expect(ok).To(BeTrue(), "resourceStringSnapshot should be a map")
				g.Expect(resourceStringSnapshotMap).To(HaveKey("t"))
				g.Expect(resourceStringSnapshotMap).To(HaveKey("resourceString"))

				resourceString, ok := resourceStringSnapshotMap["resourceString"].(string)
				g.Expect(ok).To(BeTrue(), "resourceString should be a string")
				if resourceString != "" {
					g.Expect(resourceString).To(ContainSubstring("memory"))
					g.Expect(resourceString).To(ContainSubstring("object_store_memory"))
				}
			}
		}
	}

	LogWithTimestamp(test.T(), "/nodes response schema verification completed")
}

// verifyNodeRespSchema verifies that the /nodes/{node_id} response is valid according to the API schema.
// isLive indicates whether the response is from a live cluster or a dead cluster:
//   - isLive: true for a live cluster (current snapshot)
//   - isLive: false for a dead cluster (historical replay)
func verifyNodeRespSchema(test Test, g *WithT, nodeResp map[string]any, isLive bool) {
	// Verify top-level fields.
	g.Expect(nodeResp).To(HaveKeyWithValue("result", BeTrue()))
	g.Expect(nodeResp).To(HaveKeyWithValue("msg", Equal("Node details fetched.")))
	g.Expect(nodeResp).To(HaveKey("data"))

	data, ok := nodeResp["data"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "'data' should be a map")
	g.Expect(data).To(HaveKey("detail"))

	if isLive {
		// Live cluster: detail contains node summary snapshot of the specified node.
		nodeSummarySnapshot, ok := data["detail"].(map[string]any)
		g.Expect(ok).To(BeTrue(), "'detail' should be a map")
		verifyNodeSummarySchema(test, g, nodeSummarySnapshot)
	} else {
		// Dead cluster: detail contains node summary replay (array of snapshots) of the specified node.
		// The node summary replay should follow the chronological order of the node state transitions.
		nodeSummarySnapshots, ok := data["detail"].([]any)
		g.Expect(ok).To(BeTrue(), "'detail' should be an array")
		for _, nodeSummarySnapshot := range nodeSummarySnapshots {
			nodeSummarySnapshotMap, ok := nodeSummarySnapshot.(map[string]any)
			g.Expect(ok).To(BeTrue(), "nodeSummarySnapshot should be a map")
			verifyNodeSummarySchema(test, g, nodeSummarySnapshotMap)
		}
	}
}

// verifyNodeSummarySchema verifies that the node summary contains key fields.
func verifyNodeSummarySchema(test Test, g *WithT, nodeSummary map[string]any) {
	for _, field := range []string{"now", "hostname", "ip", "raylet"} {
		g.Expect(nodeSummary).To(HaveKey(field))
	}

	// Verify raylet field.
	raylet, ok := nodeSummary["raylet"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "'raylet' should be a map")
	g.Expect(raylet).To(HaveKey("storeStats"))
	g.Expect(raylet).To(HaveKey("nodeId"))
	g.Expect(raylet).To(HaveKey("nodeManagerAddress"))
	g.Expect(raylet).To(HaveKey("nodeManagerHostname"))
	g.Expect(raylet).To(HaveKey("rayletSocketName"))
	g.Expect(raylet).To(HaveKey("objectStoreSocketName"))
	g.Expect(raylet).To(HaveKey("metricsExportPort"))
	g.Expect(raylet).To(HaveKey("resourcesTotal"))
	g.Expect(raylet).To(HaveKey("nodeName"))
	g.Expect(raylet).To(HaveKey("instanceId"))
	g.Expect(raylet).To(HaveKey("nodeTypeName"))
	g.Expect(raylet).To(HaveKey("instanceTypeName"))
	g.Expect(raylet).To(HaveKey("startTimeMs"))
	g.Expect(raylet).To(HaveKey("isHeadNode"))
	g.Expect(raylet).To(HaveKey("labels"))
	g.Expect(raylet).To(HaveKey("state"))
	g.Expect(raylet).To(HaveKey("endTimeMs"))
	g.Expect(raylet).To(HaveKey("stateMessage"))
}

// verifyNodesHostNameListSchema verifies that the /nodes?view=hostNameList response is valid according to the API schema.
func verifyNodesHostNameListSchema(test Test, g *WithT, nodesResp map[string]any, isLive bool) {
	g.Expect(nodesResp).To(HaveKeyWithValue("result", BeTrue()))
	g.Expect(nodesResp).To(HaveKeyWithValue("msg", Equal("Node hostname list fetched.")))
	g.Expect(nodesResp).To(HaveKey("data"))
	data, ok := nodesResp["data"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "'data' should be a map")
	g.Expect(data).To(HaveKey("hostNameList"))
}

// testEventsEndpointLiveCluster verifies that the /events endpoint works for a live cluster.
// For live clusters, the request is proxied to Ray Dashboard.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster
// 2. Submit a Ray job to the existing cluster
// 3. Apply History Server and get its URL
// 4. Get the cluster info from the list and verify sessionName='live'
// 5. Set cluster context via /enter_cluster/ endpoint
// 6. Verify that the /events endpoint returns events with proper structure
// 7. Delete S3 bucket to ensure test isolation
func testEventsEndpointLiveCluster(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	test.T().Run("should return events", func(t *testing.T) {
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			resp, err := client.Get(historyServerURL + "/events")
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())

			var result map[string]any
			err = json.Unmarshal(body, &result)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(result["result"]).To(Equal(true))

			// Verify data.events exists
			data, ok := result["data"].(map[string]any)
			gg.Expect(ok).To(BeTrue(), "response should have 'data' field")
			_, ok = data["events"].(map[string]any)
			gg.Expect(ok).To(BeTrue(), "data should have 'events' field")
		}, TestTimeoutShort).Should(Succeed())
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Live cluster events endpoint test completed")
}

// testEventsEndpointDeadCluster verifies that the /events endpoint works for a dead cluster.
// Events are retrieved from the EventHandler's in-memory ClusterLogEventMap.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster
// 2. Submit a Ray job to the existing cluster
// 3. Delete RayCluster to trigger event upload to S3
// 4. Apply History Server and get its URL
// 5. Get the cluster info from the list and verify sessionName != 'live'
// 6. Set cluster context via /enter_cluster/ endpoint
// 7. Verify that the /events endpoint returns events from storage with proper structure
// 8. Verify that the /events endpoint supports job_id filter (non-existent job_id)
// 9. Verify that the /events endpoint handles empty job_id parameter correctly
// 10. Delete S3 bucket to ensure test isolation
func testEventsEndpointDeadCluster(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	// Delete RayCluster to trigger event upload
	err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Deleted RayCluster %s/%s", namespace.Name, rayCluster.Name)

	// Wait for cluster to be fully deleted
	g.Eventually(func() error {
		_, err := GetRayCluster(test, namespace.Name, rayCluster.Name)
		return err
	}, TestTimeoutMedium).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).NotTo(Equal(LiveSessionName))

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	test.T().Run("should return events from storage", func(t *testing.T) {
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			resp, err := client.Get(historyServerURL + "/events")
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())

			var result map[string]any
			err = json.Unmarshal(body, &result)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(result["result"]).To(Equal(true))
			gg.Expect(result["msg"]).To(Equal("All events fetched."))

			// Verify data.events exists (may be empty if no events were collected)
			data, ok := result["data"].(map[string]any)
			gg.Expect(ok).To(BeTrue(), "response should have 'data' field")
			events, ok := data["events"].(map[string]any)
			gg.Expect(ok).To(BeTrue(), "data should have 'events' field")

			// If we have events, verify their structure matches Cluster Events format
			// Cluster Events are read from logs/{nodeId}/events/event_*.log files
			// Fields: eventId, sourceType, sourceHostname, sourcePid, severity, label, message, timestamp, customFields
			for jobID, jobEvents := range events {
				eventsList, ok := jobEvents.([]any)
				gg.Expect(ok).To(BeTrue(), "events for job %s should be an array", jobID)
				for _, event := range eventsList {
					eventMap, ok := event.(map[string]any)
					gg.Expect(ok).To(BeTrue(), "each event should be an object")
					// Verify required Cluster Event fields exist (camelCase format from ToAPIResponse)
					gg.Expect(eventMap).To(HaveKey("eventId"))
					gg.Expect(eventMap).To(HaveKey("sourceType"))
					gg.Expect(eventMap).To(HaveKey("severity"))
					gg.Expect(eventMap).To(HaveKey("timestamp"))
					gg.Expect(eventMap).To(HaveKey("message"))
				}
			}
		}, TestTimeoutMedium).Should(Succeed())
	})

	test.T().Run("should support job_id filter", func(t *testing.T) {
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			// Use a non-existent job_id to test the filter
			resp, err := client.Get(historyServerURL + "/events?job_id=nonexistent")
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())

			var result map[string]any
			err = json.Unmarshal(body, &result)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(result["result"]).To(Equal(true))
			gg.Expect(result["msg"]).To(Equal("Job events fetched."))

			data, ok := result["data"].(map[string]any)
			gg.Expect(ok).To(BeTrue())
			gg.Expect(data["jobId"]).To(Equal("nonexistent"))
			events, ok := data["events"].([]any)
			gg.Expect(ok).To(BeTrue())
			gg.Expect(events).To(BeEmpty()) // No events for non-existent job
		}, TestTimeoutShort).Should(Succeed())
	})

	test.T().Run("should return empty for empty job_id parameter", func(t *testing.T) {
		// This test verifies the behavior when job_id is provided but empty (/events?job_id=)
		// It should align with Ray Dashboard behavior: filter by empty string (return empty)
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			resp, err := client.Get(historyServerURL + "/events?job_id=")
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())

			var result map[string]any
			err = json.Unmarshal(body, &result)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(result["result"]).To(Equal(true))
			gg.Expect(result["msg"]).To(Equal("Job events fetched."))

			data, ok := result["data"].(map[string]any)
			gg.Expect(ok).To(BeTrue())
			gg.Expect(data["jobId"]).To(Equal(""))
			events, ok := data["events"].([]any)
			gg.Expect(ok).To(BeTrue())
			gg.Expect(events).To(BeEmpty()) // Empty job_id filters by empty string
		}, TestTimeoutShort).Should(Succeed())
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster events endpoint tests completed")
}

// testLiveClusterStatus verifies that the /api/cluster_status endpoint works for a live cluster.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster
// 2. Submit a Ray job to the existing cluster
// 3. Apply History Server and get its URL
// 4. Get the cluster info from the list and verify sessionName='live'
// 5. Set cluster context via /enter_cluster/ endpoint
// 6. Verify /api/cluster_status returns valid JSON response with result=true
// 7. Verify /api/cluster_status?format=1 returns formatted cluster status string containing "Autoscaler status"
// 8. Delete S3 bucket to ensure test isolation
func testLiveClusterStatus(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	endpointURL := fmt.Sprintf("%s%s", historyServerURL, EndpointClusterStatus)

	test.T().Run("should proxy /api/cluster_status to live clusters", func(_ *testing.T) {
		verifySingleEndpoint(test, g, client, endpointURL, func(test Test, g *WithT, data map[string]any) {
			g.Expect(data).To(HaveKeyWithValue("result", true))
			g.Expect(data).To(HaveKeyWithValue("msg", "Got cluster status."))
			g.Expect(data).To(HaveKey("data"))
		})
	})

	test.T().Run("should proxy for /api/cluster_status?format=1", func(_ *testing.T) {
		verifySingleEndpoint(test, g, client, endpointURL+"?format=1", func(test Test, g *WithT, data map[string]any) {
			g.Expect(data).To(HaveKeyWithValue("result", true))
			g.Expect(data).To(HaveKeyWithValue("msg", "Got formatted cluster status."))
			g.Expect(data).To(HaveKey("data"))
			respData, ok := data["data"].(map[string]any)
			g.Expect(ok).To(BeTrue(), "'data' should be a map")
			clusterStatus, ok := respData["clusterStatus"].(string)
			g.Expect(ok).To(BeTrue(), "'clusterStatus' should be a string")
			g.Expect(clusterStatus).To(ContainSubstring("Autoscaler status"))
		})
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Live cluster status E2E test completed successfully")
}

// testDeadClusterStatus verifies that the /api/cluster_status endpoint works for a dead cluster.
// Cluster status is reconstructed from debug_state.txt and task/actor data stored in S3.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster
// 2. Submit a Ray job to the existing cluster
// 3. Delete RayCluster to trigger data upload to S3
// 4. Apply History Server and get its URL
// 5. Get the cluster info from the list and verify sessionName != 'live'
// 6. Set cluster context via /enter_cluster/ endpoint
// 7. Verify /api/cluster_status returns valid JSON response with result=true
// 8. Verify /api/cluster_status?format=1 returns formatted cluster status containing "Autoscaler status"
// 9. Delete S3 bucket to ensure test isolation
func testDeadClusterStatus(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	DeleteRayClusterAndWait(test, g, namespace.Name, rayCluster.Name)

	ApplyHistoryServer(test, g, namespace, "")
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).NotTo(Equal(LiveSessionName))

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	endpointURL := fmt.Sprintf("%s%s", historyServerURL, EndpointClusterStatus)

	test.T().Run("should return cluster status with /api/cluster_status", func(_ *testing.T) {
		verifySingleEndpoint(test, g, client, endpointURL, func(test Test, g *WithT, data map[string]any) {
			g.Expect(data).To(HaveKeyWithValue("result", true))
			g.Expect(data).To(HaveKeyWithValue("msg", "Got cluster status."))
			g.Expect(data).To(HaveKey("data"))
		})
	})

	test.T().Run("should return formatted cluster status with /api/cluster_status?format=1", func(_ *testing.T) {
		verifySingleEndpoint(test, g, client, endpointURL+"?format=1", func(test Test, g *WithT, data map[string]any) {
			g.Expect(data).To(HaveKeyWithValue("result", true))
			g.Expect(data).To(HaveKeyWithValue("msg", "Got formatted cluster status."))
			g.Expect(data).To(HaveKey("data"))
			respData, ok := data["data"].(map[string]any)
			g.Expect(ok).To(BeTrue(), "'data' should be a map")
			clusterStatus, ok := respData["clusterStatus"].(string)
			g.Expect(ok).To(BeTrue(), "'clusterStatus' should be a string")
			g.Expect(clusterStatus).To(ContainSubstring("Autoscaler status"))
		})
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster status E2E test completed successfully")
}
