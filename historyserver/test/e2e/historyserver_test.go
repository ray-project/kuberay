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
			name:     "Live cluster: /api/v0/tasks?detail=1 should return the detailed task information of all task attempts",
			testFunc: testLiveClusterTasks,
		},
		{
			name:     "Dead cluster: /api/v0/tasks should return the detailed task information of all task attempts (historical replay isn't supported)",
			testFunc: testDeadClusterTasks,
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
	endpoint := "/api/v0/tasks?detail=1"

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
	verifyTasksRespSchema(test, g, tasksResp)

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
// 6. Hit /api/v0/tasks endpoint to get the detailed task information of all task attempts without historical replay
// 7. Verify the response status code is 200
// 8. Verify the response API schema
// 9. Delete S3 bucket to ensure test isolation
func testDeadClusterTasks(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	endpoint := "/api/v0/tasks"

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

	LogWithTimestamp(test.T(), "Verifying /api/v0/tasks response schema for dead cluster")
	verifyTasksRespSchema(test, g, tasksResp)

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster /api/v0/tasks tests completed successfully")
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

// verifyTasksRespSchema verifies that the /v0/tasks response is valid according to the API schema.
func verifyTasksRespSchema(test Test, g *WithT, tasksResp map[string]any) {
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
	LogWithTimestamp(test.T(), "Task response schema verified successfully")
}
