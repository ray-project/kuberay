package e2e

import (
	"encoding/json"
	"fmt"
	"github.com/google/go-cmp/cmp"
	"io"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/service/s3"
	. "github.com/onsi/gomega"
	. "github.com/ray-project/kuberay/historyserver/test/support"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
	corev1 "k8s.io/api/core/v1"
)

const (
	LiveDashboardPort      = 8265
	TasksEndpoint          = "/api/v0/tasks"
	JobsEndpoint           = "/api/jobs/"
	TasksByJobIdEndpoint   = "/api/v0/tasks?filter_keys=job_id&filter_predicates=%3D&filter_values={job_id}"
	TasksSummarizeEndpoint = "/api/v0/tasks/summarize"
	ActorsEndpoint         = "/logical/actors"
	ActorByActorIdEndpoint = "/logical/actors/{actor_id}"
	NodesSummaryEndpoint   = "/nodes?view=summary"
)

func TestDeadClusterHistory(t *testing.T) {
	// Share a single S3 client among subtests.
	s3Client := EnsureS3Client(t)

	tests := []struct {
		name           string
		testFunc       func(Test, *WithT, *corev1.Namespace, *s3.S3, bool)
		dataValidation bool
	}{
		{
			name:           "Dead Cluster: Tasks Endpoint should return data with 200 Ok and valid json",
			testFunc:       testTasksEndpoint,
			dataValidation: false,
		},
		{
			name:           "Dead Cluster: Tasks By JobId endpoint should return data with 200 Ok and valid json for individual jobs",
			testFunc:       testTasksByJobIDEndpoint,
			dataValidation: false,
		},
		{
			name:           "Dead Cluster: Tasks Summarize endpoint should return data with 200 Ok and valid json",
			testFunc:       testTaskSummarizeEndpoint,
			dataValidation: false,
		},
		{
			name:           "Dead Cluster: Actors endpoint should return data with 200 Ok and valid json",
			testFunc:       testActorsEndpoint,
			dataValidation: false,
		},
		{
			name:           "Dead Cluster: Nodes Summary endpoint should return data with 200 Ok and valid json",
			testFunc:       testNodesSummaryEndpoint,
			dataValidation: false,
		},
		{
			name:           "Dead Cluster: Actor by Actor ID Endpoint should return data with 200 Ok and valid json for individual actors",
			testFunc:       testActoryByActorIdEndpoint,
			dataValidation: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test := With(t)
			g := NewWithT(t)
			namespace := test.NewTestNamespace()

			tt.testFunc(test, g, namespace, s3Client, tt.dataValidation)
		})
	}
}

// GetLiveDashboardURL sets up port-forwarding to the live ray cluster and wait for it to be ready.
func GetLiveDashboardURL(test Test, g *WithT, namespace *corev1.Namespace) string {
	PortForwardService(test, g, namespace.Name, "raycluster-historyserver-head-svc", LiveDashboardPort)

	// Wait for port-forward to be ready
	liveServerURL := fmt.Sprintf("http://localhost:%d", LiveDashboardPort)
	g.Eventually(func() error {
		// fetching API version endpoint to check if it is live, lightweight health check.
		resp, err := http.Get(liveServerURL + "/api/version")
		if err != nil {
			return err
		}
		defer func() {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
		}
		return nil
	}, TestTimeoutMedium).Should(Succeed(), "LiveCluster should be ready")
	LogWithTimestamp(test.T(), "Port-forwarded Live Cluster API port to %s successfully", liveServerURL)

	return liveServerURL
}

// compareJsons compares two jsons print the diff, and return true/false based on comparison.
func compareJsons(test Test, gg Gomega, expected string, actual string) bool {
	var expectedJson map[string]interface{}
	err := json.Unmarshal([]byte(expected), &expectedJson)
	gg.Expect(err).NotTo(HaveOccurred())

	var actualJson map[string]interface{}
	err = json.Unmarshal([]byte(actual), &actualJson)
	gg.Expect(err).NotTo(HaveOccurred())

	if diff := cmp.Diff(expectedJson, actualJson); diff != "" {
		LogWithTimestamp(test.T(), "The jsons are different \n %s", diff)
		return false
	}

	return true
}

// testJsonEndpoint this test does the following in order to test any json result endpoint
// 1. Starts a ray cluster.
// 2. Submits two ray jobs and wait for their completion
// 3. Fetches the json data from live cluster and store it in the variable.
// 4. Destroys the live ray cluster.
// 5. Apply ray history server and Gets Its URL
// 6. Verify that json endpoint is reachable.
// 7. If dataValidation is true, Verify that json endpoint result is equal to live ray cluster result, otherwise logs the difference.
// 8. Delete S3 bucket to ensure test isolation
func testJsonEndpoint(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3, endpoint string, dataValidation bool) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	liveDashboardURL := GetLiveDashboardURL(test, g, namespace)
	client := CreateHTTPClientWithCookieJar(g)
	requiredBody := []byte{}

	g.Eventually(func(gg Gomega) {
		resp, err := client.Get(liveDashboardURL + endpoint)
		gg.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(resp.StatusCode).To(Equal(200),
			"Endpoint %s should return 200, got %d: %s", endpoint, resp.StatusCode, string(body))
		gg.Expect(body).NotTo(BeNil(), "body should not be nil")
		gg.Expect(string(body)).NotTo(Equal("{}"), "body should not be empty")

		LogWithTimestamp(test.T(), "Endpoint %s returned status %d", endpoint, resp.StatusCode)

		requiredBody = body
	}, TestTimeoutMedium).Should(Succeed())

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

	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	test.T().Run("should validate the json endpoint is callable 200 Ok, and returns valid data", func(t *testing.T) {
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			tasksURL := fmt.Sprintf("%s%s", historyServerURL, endpoint)
			resp, err := client.Get(tasksURL)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(len(body)).To(BeNumerically(">", 0))
			val := compareJsons(test, gg, string(requiredBody), string(body))
			if dataValidation {
				gg.Expect(val).To(BeTrue())
			}
		}, TestTimeoutShort).Should(Succeed())
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster task endpoint test completed")
}

// testTasksEndpoint this test does the following in order to test TasksEndpoint
// 1. Starts a ray cluster.
// 2. Submits two ray jobs amd wait for their completion
// 3. Fetches the tasks data from live cluster and store it in the variable.
// 4. Destroys the live ray cluster.
// 5. Apply ray history server and Gets Its URL
// 6. Verify that TasksEndpoint is reachable.
// 7. If dataValidation is true, Verify that TasksEndpoint result is equal to live ray cluster result, otherwise logs the difference.
// 8. Delete S3 bucket to ensure test isolation
func testTasksEndpoint(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3, dataValidation bool) {
	testJsonEndpoint(test, g, namespace, s3Client, TasksEndpoint, dataValidation)
}

// testTasksByJobIDEndpoint this test does the following in order to test TasksByJobIdEndpoint
// 1. Starts a ray cluster.
// 2. Submits two ray jobs and Waits for their completion
// 3. Finds the Job IDs of those jobs by fetching the jobs.
// 4. Finds the tasks of both Jobs by fetching the tasksByJobIdEndpoint with those job IDs. and store it in the map jobId <> tasks
// 5. Destroys the live ray cluster.
// 6. Apply ray history server and Gets Its URL
// 7. Verify that First job tasks are returned with jobId, validate data sanity if the check dataValidation is on otherwise logs the difference of data.
// 8. Verify that Second job tasks are returned with jobId, validate data sanity if the check dataValidation is on otherwise logs the difference of data.
// 9. Delete S3 bucket to ensure test isolation
func testTasksByJobIDEndpoint(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3, dataValidation bool) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	job1 := ""
	job2 := ""
	liveDashboardURL := GetLiveDashboardURL(test, g, namespace)
	client := CreateHTTPClientWithCookieJar(g)
	jobResponses := make(map[string]string, 0)

	g.Eventually(func(gg Gomega) {
		resp, err := client.Get(liveDashboardURL + JobsEndpoint)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(resp.StatusCode).To(Equal(200))

		defer resp.Body.Close()
		jobs := make([]interface{}, 0)

		data, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())

		err = json.Unmarshal(data, &jobs)
		gg.Expect(err).NotTo(HaveOccurred())

		gg.Expect(len(jobs)).To(BeNumerically("==", 2))

		if len(jobs) != 2 {
			return
		}

		// find the job_id for first job.
		j1, ok := jobs[0].(map[string]interface{})
		gg.Expect(ok).To(BeTrue())
		job1, ok = j1["job_id"].(string)
		gg.Expect(ok).To(BeTrue())

		// find the job_id for the second job
		j2, ok := jobs[1].(map[string]interface{})
		gg.Expect(ok).To(BeTrue())
		job2, ok = j2["job_id"].(string)
		gg.Expect(ok).To(BeTrue())

		gg.Expect(job1).NotTo(BeEmpty())
		gg.Expect(job2).NotTo(BeEmpty())

		LogWithTimestamp(test.T(), "Jobs %s, %s found successfully", job1, job2)
	}, TestTimeoutShort).Should(Succeed())

	g.Eventually(func(gg Gomega) {
		// make a call to first one.
		resp, err := client.Get(liveDashboardURL + strings.Replace(
			TasksByJobIdEndpoint,
			"{job_id}",
			url.QueryEscape(job1),
			1,
		))
		gg.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(resp.StatusCode).To(Equal(200),
			"Endpoint %s should return 200, got %d: %s", TasksByJobIdEndpoint, resp.StatusCode, string(body))
		gg.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Endpoint %s returned status %d", TasksByJobIdEndpoint, resp.StatusCode)
		jobResponses[job1] = string(body)
	}, TestTimeoutMedium).Should(Succeed())

	g.Eventually(func(gg Gomega) {
		resp, err := client.Get(liveDashboardURL + strings.Replace(
			TasksByJobIdEndpoint,
			"{job_id}",
			url.QueryEscape(job2),
			1,
		))
		gg.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(resp.StatusCode).To(Equal(200),
			"Endpoint %s should return 200, got %d: %s", TasksByJobIdEndpoint, resp.StatusCode, string(body))
		gg.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Endpoint %s returned status %d", TasksByJobIdEndpoint, resp.StatusCode)
		jobResponses[job2] = string(body)
	}, TestTimeoutMedium).Should(Succeed())

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

	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	test.T().Run("First Job Should be found and has valid data", func(t *testing.T) {
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			tasksURL := fmt.Sprintf("%s%s", historyServerURL, strings.Replace(
				TasksByJobIdEndpoint,
				"{job_id}",
				url.QueryEscape(job1),
				1,
			))

			resp, err := client.Get(tasksURL)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(len(body)).To(BeNumerically(">", 0))

			val := compareJsons(test, gg, jobResponses[job1], string(body))
			if dataValidation {
				gg.Expect(val).To(BeTrue())
			}
		}, TestTimeoutShort).Should(Succeed())
	})

	test.T().Run("Second Job Should be found and has valid data", func(t *testing.T) {
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			tasksURL := fmt.Sprintf("%s%s", historyServerURL, strings.Replace(
				TasksByJobIdEndpoint,
				"{job_id}",
				url.QueryEscape(job2),
				1,
			))

			resp, err := client.Get(tasksURL)
			gg.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(len(body)).To(BeNumerically(">", 0))

			val := compareJsons(test, gg, jobResponses[job2], string(body))
			if dataValidation {
				gg.Expect(val).To(BeTrue())
			}
		}, TestTimeoutShort).Should(Succeed())
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster task endpoint test completed")
}

// testTaskSummarize this test does the following in order to test TasksSummarizeEndpoint
// 1. Starts a ray cluster.
// 2. Submits two ray jobs and wait for their completion
// 3. Fetches the TasksSummarizeEndpoint data from live cluster and store it in the variable.
// 4. Destroys the live ray cluster.
// 5. Apply ray history server and Gets Its URL
// 6. Verify that TasksSummarizeEndpoint is reachable.
// 7. If dataValidation is true, Verify that tasks TasksSummarizeEndpoint result is equal to live ray cluster result, otherwise logs the difference.
// 8. Delete S3 bucket to ensure test isolation
func testTaskSummarizeEndpoint(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3, dataValidation bool) {
	testJsonEndpoint(test, g, namespace, s3Client, TasksSummarizeEndpoint, dataValidation)
}

// testActorsEndpoint this test does the following in order to test ActorsEndpoint
// 1. Starts a ray cluster.
// 2. Submits two ray jobs and wait for their completion
// 3. Fetches the ActorsEndpoint data from live cluster and store it in the variable.
// 4. Destroys the live ray cluster.
// 5. Apply ray history server and Gets Its URL
// 6. Verify that ActorsEndpoint is reachable.
// 7. If dataValidation is true, Verify that ActorsEndpoint result is equal to live ray cluster result, otherwise logs the difference.
// 8. Delete S3 bucket to ensure test isolation
func testActorsEndpoint(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3, dataValidation bool) {
	testJsonEndpoint(test, g, namespace, s3Client, ActorsEndpoint, dataValidation)
}

// testNodesSummaryEndpoint this test does the following in order to test NodesSummaryEndpoint
// 1. Starts a ray cluster.
// 2. Submits two ray jobs and wait for their completion
// 3. Fetches the NodesSummaryEndpoint data from live cluster and store it in the variable.
// 4. Destroys the live ray cluster.
// 5. Apply ray history server and Gets Its URL
// 6. Verify that NodesSummaryEndpoint is reachable.
// 7. If dataValidation is true, Verify that NodesSummaryEndpoint result is equal to live ray cluster result, otherwise logs the difference.
// 8. Delete S3 bucket to ensure test isolation
func testNodesSummaryEndpoint(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3, dataValidation bool) {
	testJsonEndpoint(test, g, namespace, s3Client, NodesSummaryEndpoint, dataValidation)
}

// testActoryByActorIdEndpoint this test does the following in order to test ActorByActorIdEndpoint
// 1. Starts a ray cluster.
// 2. Submits two ray jobs and Waits for their completion
// 3. Finds all the actors by calling ActorsEndpoint.
// 4. Finds Actor data by calling ActorByActorIdEndpoint with all actor ID in loop, and store in map actorId <> result.
// 5. Destroys the live ray cluster.
// 6. Apply ray history server and Gets Its URL
// 7. Verify that all actors are returned in the given the actor id and the data returned is matching with the actual result, assertion only when dataValidation is set to true.
// 9. Delete S3 bucket to ensure test isolation
func testActoryByActorIdEndpoint(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3, dataValidation bool) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	actorIDs := []string{}
	actorResponses := make(map[string]string, 0)
	liveDashboardURL := GetLiveDashboardURL(test, g, namespace)
	client := CreateHTTPClientWithCookieJar(g)

	g.Eventually(func(gg Gomega) {
		resp, err := client.Get(liveDashboardURL + ActorsEndpoint)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(resp.StatusCode).To(Equal(200))

		actors := make(map[string]interface{}, 0)

		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		gg.Expect(err).NotTo(HaveOccurred())

		err = json.Unmarshal(data, &actors)
		gg.Expect(err).NotTo(HaveOccurred())

		actors_data, ok := actors["data"].(map[string]interface{})
		gg.Expect(ok).To(BeTrue())

		actorJsons, ok := actors_data["actors"].(map[string]interface{})
		gg.Expect(ok).To(BeTrue())

		actorIDs = make([]string, 0)

		for actorId := range actorJsons {
			resp, err := client.Get(liveDashboardURL + strings.Replace(
				ActorByActorIdEndpoint,
				"{actor_id}",
				url.QueryEscape(actorId),
				1,
			))
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

			body, err := io.ReadAll(resp.Body)
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(resp.Body.Close()).To(Succeed())
			actorResponses[actorId] = string(body)
			actorIDs = append(actorIDs, actorId)
		}

		gg.Expect(len(actorIDs)).To(BeNumerically(">", 0))
	}, TestTimeoutMedium).Should(Succeed())

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

	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)

	test.T().Run("All Actors should be present and return valid data", func(t *testing.T) {
		g := NewWithT(t)
		g.Eventually(func(gg Gomega) {
			for _, actorId := range actorIDs {
				resp, err := client.Get(historyServerURL + strings.Replace(
					ActorByActorIdEndpoint,
					"{actor_id}",
					url.QueryEscape(actorId),
					1,
				))
				LogWithTimestamp(test.T(), "fetching Actor %s", actorId)
				gg.Expect(err).NotTo(HaveOccurred())
				gg.Expect(resp.StatusCode).To(Equal(http.StatusOK))

				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				gg.Expect(err).NotTo(HaveOccurred())
				gg.Expect(len(string(body))).To(BeNumerically(">", 0))

				val := compareJsons(test, gg, actorResponses[actorId], string(body))
				if dataValidation {
					gg.Expect(val).To(BeTrue())
				}
			}
		}, TestTimeoutMedium).Should(Succeed())
	})

	DeleteS3Bucket(test, g, s3Client)
	LogWithTimestamp(test.T(), "Dead cluster task endpoint test completed")
}
