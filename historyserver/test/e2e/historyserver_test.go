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

	test.T().Run("should return log content", func(t *testing.T) {
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			logFileURL := fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=100", historyServerURL, EndpointLogFile, nodeID, filename)
			resp, err := client.Get(logFileURL)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(len(body)).To(BeNumerically(">", 0))
		}, TestTimeoutShort).Should(Succeed())
	})

	test.T().Run("should reject path traversal", func(t *testing.T) {
		g := NewWithT(t)
		maliciousPaths := []string{"../etc/passwd", "..", "/etc/passwd", "../../secret"}

		for _, malicious := range maliciousPaths {
			g.Eventually(func(gg Gomega) {
				url := fmt.Sprintf("%s%s?node_id=%s&filename=%s", historyServerURL, EndpointLogFile, nodeID, malicious)
				resp, err := client.Get(url)
				gg.Expect(err).NotTo(HaveOccurred())
				defer func() {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}()
				gg.Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			}, TestTimeoutShort).Should(Succeed())
		}
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
// 5. Verify that the history server can fetch log content from S3 (raylet.out)
// 6. Verify that the history server rejects path traversal attempts from S3
// 7. Delete S3 bucket to ensure test isolation
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

	test.T().Run("should return log content from S3", func(t *testing.T) {
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			logFileURL := fmt.Sprintf("%s%s?node_id=%s&filename=%s&lines=100", historyServerURL, EndpointLogFile, nodeID, filename)
			resp, err := client.Get(logFileURL)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(len(body)).To(BeNumerically(">", 0))
		}, TestTimeoutShort).Should(Succeed())
	})

	test.T().Run("should reject path traversal from S3", func(t *testing.T) {
		g := NewWithT(t)
		maliciousPaths := []string{"../etc/passwd", "..", "/etc/passwd", "../../secret"}

		for _, malicious := range maliciousPaths {
			g.Eventually(func(gg Gomega) {
				url := fmt.Sprintf("%s%s?node_id=%s&filename=%s", historyServerURL, EndpointLogFile, nodeID, malicious)
				resp, err := client.Get(url)
				gg.Expect(err).NotTo(HaveOccurred())
				defer func() {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}()
				gg.Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
			}, TestTimeoutShort).Should(Succeed())
		}
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster log file endpoint tests completed")
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
	ApplyHistoryServer(test, g, namespace)
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)
	endpointURL := historyServerURL + endpoint
	LogWithTimestamp(test.T(), "Testing %s endpoint for live cluster: %s", endpoint, endpointURL)

	var nodesResp map[string]any
	g.Eventually(func(gg Gomega) {
		resp, err := client.Get(endpointURL)
		gg.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(resp.StatusCode).To(Equal(http.StatusOK),
			"[GET] %s should return 200, got %d: %s", endpointURL, resp.StatusCode, string(body))

		err = json.Unmarshal(body, &nodesResp)
		gg.Expect(err).NotTo(HaveOccurred())
	}, TestTimeoutShort).Should(Succeed())

	LogWithTimestamp(test.T(), "Verifying /nodes response schema for live cluster (isLive=true)")
	verifyNodesRespSchema(test, g, nodesResp, true)

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
	endpoint := "/nodes"

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

	ApplyHistoryServer(test, g, namespace)
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(SatisfyAll(Not(BeEmpty()), Not(Equal(LiveSessionName))))

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	endpointURL := historyServerURL + endpoint
	LogWithTimestamp(test.T(), "Testing %s endpoint for dead cluster: %s", endpoint, endpointURL)

	var nodesResp map[string]any
	g.Eventually(func(gg Gomega) {
		resp, err := client.Get(endpointURL)
		gg.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(resp.StatusCode).To(Equal(http.StatusOK),
			"[GET] %s should return 200, got %d: %s", endpointURL, resp.StatusCode, string(body))

		err = json.Unmarshal(body, &nodesResp)
		gg.Expect(err).NotTo(HaveOccurred())
	}, TestTimeoutShort).Should(Succeed())

	LogWithTimestamp(test.T(), "Verifying /nodes response schema for dead cluster (isLive=false)")
	verifyNodesRespSchema(test, g, nodesResp, false)

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

	ApplyHistoryServer(test, g, namespace)
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	for _, nodeId := range []string{headNodeID, workerNodeID} {
		endpoint := fmt.Sprintf("/nodes/%s", nodeId)
		endpointURL := historyServerURL + endpoint
		LogWithTimestamp(test.T(), "Testing %s endpoint for live cluster: %s", endpoint, endpointURL)

		var nodeResp map[string]any
		g.Eventually(func(gg Gomega) {
			resp, err := client.Get(endpointURL)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK),
				"[GET] %s should return 200, got %d: %s", endpointURL, resp.StatusCode, string(body))

			err = json.Unmarshal(body, &nodeResp)
			gg.Expect(err).NotTo(HaveOccurred())
		}, TestTimeoutShort).Should(Succeed())

		LogWithTimestamp(test.T(), "Verifying /nodes/%s response schema for live cluster (isLive=true)", nodeId)
		verifyNodeRespSchema(test, g, nodeResp, true)
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

	ApplyHistoryServer(test, g, namespace)
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(SatisfyAll(Not(BeEmpty()), Not(Equal(LiveSessionName))))

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	for _, nodeId := range []string{headNodeID, workerNodeID} {
		endpoint := fmt.Sprintf("/nodes/%s", nodeId)
		endpointURL := historyServerURL + endpoint
		LogWithTimestamp(test.T(), "Testing %s endpoint for dead cluster: %s", endpoint, endpointURL)

		var nodeResp map[string]any
		g.Eventually(func(gg Gomega) {
			resp, err := client.Get(endpointURL)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK),
				"[GET] %s should return 200, got %d: %s", endpointURL, resp.StatusCode, string(body))

			err = json.Unmarshal(body, &nodeResp)
			gg.Expect(err).NotTo(HaveOccurred())
		}, TestTimeoutShort).Should(Succeed())

		LogWithTimestamp(test.T(), "Verifying /nodes/%s response schema for dead cluster (isLive=false)", nodeId)
		verifyNodeRespSchema(test, g, nodeResp, false)
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

	// Verify raylet field
	raylet, ok := nodeSummary["raylet"].(map[string]any)
	g.Expect(ok).To(BeTrue(), "'raylet' should be a map")
	g.Expect(raylet).To(HaveKey("nodeId"))
	g.Expect(raylet).To(HaveKey("nodeManagerAddress"))
	g.Expect(raylet).To(HaveKey("rayletSocketName"))
	g.Expect(raylet).To(HaveKey("objectStoreSocketName"))
	g.Expect(raylet).To(HaveKey("resourcesTotal"))
	g.Expect(raylet).To(HaveKey("nodeTypeName"))
	g.Expect(raylet).To(HaveKey("startTimeMs"))
	g.Expect(raylet).To(HaveKey("isHeadNode"))
	g.Expect(raylet).To(HaveKey("labels"))
	g.Expect(raylet).To(HaveKey("state"))
}
