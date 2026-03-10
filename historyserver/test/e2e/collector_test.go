package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	. "github.com/onsi/gomega"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	. "github.com/ray-project/kuberay/historyserver/test/support"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// rayEvent contains specific fields in the Ray event JSON schema. For now, we keep only two fields,
// eventId and eventType while ensuring future extensibility (e.g., sessionName, timestamp, sourceType, etc.).
type rayEvent struct {
	EventID   string `json:"eventId"`
	EventType string `json:"eventType"`
}

func TestCollector(t *testing.T) {
	// Share a single S3 client among subtests.
	s3Client := EnsureS3Client(t)

	tests := []struct {
		name     string
		testFunc func(Test, *WithT, *corev1.Namespace, *s3.Client)
	}{
		{
			name:     "Happy path: Logs and events should be uploaded to S3 on deletion",
			testFunc: testCollectorUploadOnGracefulShutdown,
		},
		{
			name:     "Simulate OOMKilled behavior: Single session single node logs and events should be uploaded to S3 after the ray-head container is restarted",
			testFunc: testCollectorSeparatesFilesBySession,
		},
		{
			name:     "Collector restart: should scan prev-logs and resume uploads left by a crash",
			testFunc: testCollectorResumesUploadsOnRestart,
		},
		{
			name:     "Cluster metadata: collector should fetch and store cluster_metadata endpoint data once on startup",
			testFunc: testCollectorStoresClusterMetadata,
		},
		{
			name:     "Placement groups: collector should poll and store placement_groups endpoint data",
			testFunc: testCollectorStoresPlacementGroups,
		},
		{
			name:     "Timezone: collector should fetch and store timezone endpoint data once on startup",
			testFunc: testCollectorStoresTimezone,
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

// testCollectorUploadOnGracefulShutdown verifies that logs, node_events, and job_events are successfully uploaded to S3 on cluster deletion.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster with the collector
// 2. Submit a Ray job to the existing Ray cluster
// 3. Get the sessionID and nodeID for further verification
// 4. Delete the Ray cluster to trigger log uploading and event flushing on deletion. When the Ray cluster is deleted,
// logs, node_events, and job_events are processed as follows:
//   - logs: Trigger RayLogHandler.processSessionLatestLog to process logs under /tmp/ray/session_latest
//   - node_events: Trigger EventServer.flushEvents, which calls es.flushNodeEventsForHour to process in-memory node events
//   - job_events: Trigger EventServer.flushEvents, which calls es.flushJobEventsForHour to process in-memory job events
//
// 5. Verify logs, node_events, and job_events are successfully uploaded to S3. Expected S3 path structure:
//   - {S3BucketName}/log/{clusterName}_{clusterID}/{sessionID}/logs/...
//   - {S3BucketName}/log/{clusterName}_{clusterID}/{sessionID}/node_events/...
//   - {S3BucketName}/log/{clusterName}_{clusterID}/{sessionID}/job_events/AgAAAA==/...
//   - {S3BucketName}/log/{clusterName}_{clusterID}/{sessionID}/job_events/AQAAAA==/...
//
// For detailed verification logic, please refer to verifyS3SessionDirs.
//
// 6. Delete S3 bucket to ensure test isolationCollapse comment
func testCollectorUploadOnGracefulShutdown(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.Client) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)

	_ = ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	// Define variables for constructing S3 object prefix.
	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, namespace.Name)
	sessionID := GetSessionIDFromHeadPod(test, g, rayCluster)
	headNodeID := GetNodeIDFromPod(test, g, HeadPod(test, rayCluster), "ray-head")
	workerNodeID := GetNodeIDFromPod(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")
	sessionPrefix := fmt.Sprintf("log/%s/%s/", clusterNameID, sessionID)

	err := test.Client().Ray().RayV1().
		RayClusters(rayCluster.Namespace).
		Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(func() error {
		_, err := GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		return err
	}, TestTimeoutMedium).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

	verifyS3SessionDirs(test, g, s3Client, sessionPrefix, headNodeID, workerNodeID)
	DeleteS3Bucket(test, g, s3Client)
}

// testCollectorSeparatesFilesBySession verifies that logs and node_events are successfully uploaded to S3 after the ray-head container is restarted.
func testCollectorSeparatesFilesBySession(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.Client) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)

	_ = ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayCluster.Namespace)
	sessionID := GetSessionIDFromHeadPod(test, g, rayCluster)
	headNodeID := GetNodeIDFromPod(test, g, HeadPod(test, rayCluster), "ray-head")
	workerNodeID := GetNodeIDFromPod(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")
	sessionPrefix := fmt.Sprintf("log/%s/%s/", clusterNameID, sessionID)

	// NOTE: We use `kill 1` to simulate Kubernetes OOMKilled behavior.
	killContainerAndWaitForRestart(test, g, HeadPod(test, rayCluster), "ray-head")
	killContainerAndWaitForRestart(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")

	VerifySessionDirectoriesExist(test, g, rayCluster, sessionID)
	verifyS3SessionDirs(test, g, s3Client, sessionPrefix, headNodeID, workerNodeID)
	DeleteS3Bucket(test, g, s3Client)
}

// testCollectorResumesUploadsOnRestart verifies that the Collector scans and resumes uploads from
// the prev-logs directory on startup.
func testCollectorResumesUploadsOnRestart(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.Client) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)

	prevLogsBaseDir := "/tmp/ray/prev-logs"
	persistCompleteBaseDir := "/tmp/ray/persist-complete-logs"

	dummySessionID := fmt.Sprintf("test-recovery-session-%s", namespace.Name)
	dummyNodeID := fmt.Sprintf("head-node-%s", namespace.Name)
	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayCluster.Namespace)
	sessionPrefix := fmt.Sprintf("log/%s/%s/", clusterNameID, dummySessionID)

	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Injecting logs into %s before killing collector", prevLogsBaseDir)
	sessionDir := filepath.Join(prevLogsBaseDir, dummySessionID, dummyNodeID)
	persistDir := filepath.Join(persistCompleteBaseDir, dummySessionID, dummyNodeID)
	injectCmd := fmt.Sprintf(
		"mkdir -p %s/logs && "+
			"echo 'file1 content' > %s/logs/file1.log && "+
			"mkdir -p %s/logs && "+
			"echo 'file2 content' > %s/logs/file2.log",
		persistDir,
		persistDir,
		sessionDir,
		sessionDir,
	)
	_, stderr := ExecPodCmd(test, headPod, "ray-head", []string{"sh", "-c", injectCmd})
	g.Expect(stderr.String()).To(BeEmpty())

	LogWithTimestamp(test.T(), "Killing collector container to test startup scanning of prev-logs")
	_, stderrKill := ExecPodCmd(test, headPod, "collector", []string{"kill", "1"})
	g.Expect(stderrKill.String()).To(BeEmpty())

	LogWithTimestamp(test.T(), "Waiting for collector container to restart and become ready")
	g.Eventually(func(gg Gomega) {
		updatedPod, err := GetHeadPod(test, rayCluster)
		gg.Expect(err).NotTo(HaveOccurred())
		cs, err := GetContainerStatusByName(updatedPod, "collector")
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(cs.RestartCount).To(BeNumerically(">", 0))
		gg.Expect(cs.Ready).To(BeTrue())
	}, TestTimeoutMedium).Should(Succeed())

	LogWithTimestamp(test.T(), "Verifying file2.log was uploaded to S3 (idempotency check)")
	g.Eventually(func(gg Gomega) {
		logsPrefix := sessionPrefix + "logs/"
		objects, err := s3Client.ListObjectsV2(test.Ctx(), &s3.ListObjectsV2Input{
			Bucket: aws.String(S3BucketName),
			Prefix: aws.String(logsPrefix),
		})
		gg.Expect(err).NotTo(HaveOccurred())

		var uploadedKeys []string
		for _, obj := range objects.Contents {
			uploadedKeys = append(uploadedKeys, aws.ToString(obj.Key))
		}
		LogWithTimestamp(test.T(), "Found uploaded objects: %v", uploadedKeys)

		hasFile2 := false
		for _, key := range uploadedKeys {
			if strings.HasSuffix(key, "file2.log") {
				hasFile2 = true
				break
			}
		}
		gg.Expect(hasFile2).To(BeTrue(), "file2.log should be uploaded to S3 because it was in prev-logs")
	}, TestTimeoutMedium).Should(Succeed())

	LogWithTimestamp(test.T(), "Verifying local state: node directory should be moved to %s", persistCompleteBaseDir)
	g.Eventually(func(gg Gomega) {
		currentHeadPod, err := GetHeadPod(test, rayCluster)
		gg.Expect(err).NotTo(HaveOccurred())

		persistPath := filepath.Join(persistCompleteBaseDir, dummySessionID, dummyNodeID)
		checkCmd := fmt.Sprintf("test -d %s && echo 'exists'", persistPath)
		stdout, stderrCheck := ExecPodCmd(test, currentHeadPod, "ray-head", []string{"sh", "-c", checkCmd})
		gg.Expect(stderrCheck.String()).To(BeEmpty())
		gg.Expect(strings.TrimSpace(stdout.String())).To(Equal("exists"), "Node directory should be in persist-complete-logs")

		prevPath := filepath.Join(prevLogsBaseDir, dummySessionID, dummyNodeID)
		checkGoneCmd := fmt.Sprintf("test ! -d %s && echo 'gone'", prevPath)
		stdoutGone, stderrGone := ExecPodCmd(test, currentHeadPod, "ray-head", []string{"sh", "-c", checkGoneCmd})
		gg.Expect(stderrGone.String()).To(BeEmpty())
		gg.Expect(strings.TrimSpace(stdoutGone.String())).To(Equal("gone"), "Node directory should be cleaned from prev-logs")
	}, TestTimeoutMedium).Should(Succeed())

	DeleteS3Bucket(test, g, s3Client)
}

// testCollectorStoresClusterMetadata verifies that the Head collector fetches /api/v0/cluster_metadata
// from the Ray Dashboard once on startup and stores the result in S3 under the session path.
func testCollectorStoresClusterMetadata(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.Client) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)

	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayCluster.Namespace)
	sessionID := GetSessionIDFromHeadPod(test, g, rayCluster)
	storageKey := utils.EndpointPathToStorageKey("/api/v0/cluster_metadata")
	metaKey := fmt.Sprintf("log/%s/%s/%s/%s", clusterNameID, sessionID, utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME, storageKey)

	LogWithTimestamp(test.T(), "Waiting for cluster metadata to appear at S3 key: %s", metaKey)

	var metadataBody []byte
	g.Eventually(func(gg Gomega) {
		result, err := s3Client.GetObject(test.Ctx(), &s3.GetObjectInput{
			Bucket: aws.String(S3BucketName),
			Key:    aws.String(metaKey),
		})
		gg.Expect(err).NotTo(HaveOccurred())
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(body).NotTo(BeEmpty(), "Cluster metadata file should not be empty")

		var metadata map[string]interface{}
		err = json.Unmarshal(body, &metadata)
		gg.Expect(err).NotTo(HaveOccurred(), "Cluster metadata should be valid JSON")

		gg.Expect(metadata).To(HaveKey("data"), "Cluster metadata should contain data field")
		data, ok := metadata["data"].(map[string]interface{})
		gg.Expect(ok).To(BeTrue(), "data field should be a JSON object")
		gg.Expect(data).To(HaveKey("rayVersion"), "Cluster metadata should contain rayVersion")
		gg.Expect(data).To(HaveKey("pythonVersion"), "Cluster metadata should contain pythonVersion")

		metadataBody = body
	}, TestTimeoutMedium).Should(Succeed())

	LogWithTimestamp(test.T(), "Cluster metadata stored successfully: %s", string(metadataBody))
	DeleteS3Bucket(test, g, s3Client)
}

// testCollectorStoresTimezone verifies that the Head collector fetches /timezone and stores it in S3.
func testCollectorStoresTimezone(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.Client) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)

	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayCluster.Namespace)
	sessionID := GetSessionIDFromHeadPod(test, g, rayCluster)
	storageKey := utils.EndpointPathToStorageKey(EndpointTimezone)
	timezoneKey := fmt.Sprintf("log/%s/%s/%s/%s", clusterNameID, sessionID, utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME, storageKey)

	LogWithTimestamp(test.T(), "Waiting for timezone data to appear at S3 key: %s", timezoneKey)

	var timezoneBody []byte
	g.Eventually(func(gg Gomega) {
		result, err := s3Client.GetObject(test.Ctx(), &s3.GetObjectInput{
			Bucket: aws.String(S3BucketName),
			Key:    aws.String(timezoneKey),
		})
		gg.Expect(err).NotTo(HaveOccurred())
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(body).NotTo(BeEmpty(), "Timezone file should not be empty")

		var timezone map[string]interface{}
		err = json.Unmarshal(body, &timezone)
		gg.Expect(err).NotTo(HaveOccurred(), "Timezone data should be valid JSON")

		gg.Expect(timezone).To(HaveKey("offset"), "Timezone data should contain offset field")
		gg.Expect(timezone).To(HaveKey("value"), "Timezone data should contain value field")
		gg.Expect(timezone["offset"]).NotTo(BeEmpty(), "Timezone offset should not be empty")
		gg.Expect(timezone["value"]).NotTo(BeEmpty(), "Timezone value should not be empty")

		timezoneBody = body
	}, TestTimeoutMedium).Should(Succeed())

	LogWithTimestamp(test.T(), "Timezone data stored successfully: %s", string(timezoneBody))
	DeleteS3Bucket(test, g, s3Client)
}

// testCollectorStoresPlacementGroups verifies that the collector polls /api/v0/placement_groups and stores it in S3.
func testCollectorStoresPlacementGroups(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.Client) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayCluster.Namespace)
	sessionID := GetSessionIDFromHeadPod(test, g, rayCluster)
	storageKey := utils.EndpointPathToStorageKey("/api/v0/placement_groups")
	pgKey := fmt.Sprintf("log/%s/%s/%s/%s", clusterNameID, sessionID, utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME, storageKey)

	LogWithTimestamp(test.T(), "Waiting for placement groups data to appear at S3 key: %s", pgKey)

	var pgBody []byte
	g.Eventually(func(gg Gomega) {
		result, err := s3Client.GetObject(test.Ctx(), &s3.GetObjectInput{
			Bucket: aws.String(S3BucketName),
			Key:    aws.String(pgKey),
		})
		gg.Expect(err).NotTo(HaveOccurred())
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(body).NotTo(BeEmpty(), "Placement groups file should not be empty")

		var response map[string]interface{}
		err = json.Unmarshal(body, &response)
		gg.Expect(err).NotTo(HaveOccurred(), "Placement groups response should be valid JSON")

		gg.Expect(response).To(HaveKey("result"), "Placement groups response should contain result field")
		gg.Expect(response["result"]).To(BeTrue(), "result field should be true")
		gg.Expect(response).To(HaveKey("data"), "Placement groups response should contain data field")
		data, ok := response["data"].(map[string]interface{})
		gg.Expect(ok).To(BeTrue(), "data field should be a JSON object")
		gg.Expect(data).To(HaveKey("result"), "data should contain result field")
		resultObj, ok := data["result"].(map[string]interface{})
		gg.Expect(ok).To(BeTrue(), "data.result field should be a JSON object")
		gg.Expect(resultObj).To(HaveKey("result"), "data.result should contain result field")

		pgList, ok := resultObj["result"].([]interface{})
		gg.Expect(ok).To(BeTrue(), "data.result.result should be a JSON array")
		gg.Expect(pgList).NotTo(BeEmpty(), "placement groups list should not be empty (RayJob creates a detached PG)")

		pgBody = body
	}, TestTimeoutMedium).Should(Succeed())

	LogWithTimestamp(test.T(), "Placement groups data stored successfully: %s", string(pgBody))
	DeleteS3Bucket(test, g, s3Client)
}

// verifyS3SessionDirs verifies that directories logs/<headNodeID>/, logs/<workerNodeID>/, and node_events/ exist under a session prefix in S3.
func verifyS3SessionDirs(test Test, g *WithT, s3Client *s3.Client, sessionPrefix string, headNodeID string, workerNodeID string) {
	headLogDirPrefix := fmt.Sprintf("%slogs/%s", sessionPrefix, headNodeID)
	workerLogDirPrefix := fmt.Sprintf("%slogs/%s", sessionPrefix, workerNodeID)

	g.Eventually(func(gg Gomega) {
		for _, fileName := range []string{"raylet.out", "gcs_server.out", "monitor.out"} {
			fileKey := fmt.Sprintf("%s/%s", headLogDirPrefix, fileName)
			LogWithTimestamp(test.T(), "Verifying head log file %s exists", fileKey)
			obj, err := s3Client.HeadObject(test.Ctx(), &s3.HeadObjectInput{
				Bucket: aws.String(S3BucketName),
				Key:    aws.String(fileKey),
			})
			gg.Expect(err).NotTo(HaveOccurred())
			// Allow >= 0 so 0-byte placeholders (e.g. monitor.out before content) and Azure e2e consistency pass
			gg.Expect(aws.ToInt64(obj.ContentLength)).To(BeNumerically(">=", 0))
		}

		workerFileKey := fmt.Sprintf("%s/%s", workerLogDirPrefix, "raylet.out")
		LogWithTimestamp(test.T(), "Verifying worker log file %s exists", workerFileKey)
		obj, err := s3Client.HeadObject(test.Ctx(), &s3.HeadObjectInput{
			Bucket: aws.String(S3BucketName),
			Key:    aws.String(workerFileKey),
		})
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(aws.ToInt64(obj.ContentLength)).To(BeNumerically(">=", 0))
	}, TestTimeoutMedium).Should(Succeed(), "Failed to verify required log files under %s", sessionPrefix+"logs/")

	LogWithTimestamp(test.T(), "Verifying all %d event types are covered, except for EVENT_TYPE_UNSPECIFIED: %v", len(eventtypes.AllEventTypes)-1, eventtypes.AllEventTypes)
	g.Eventually(func(gg Gomega) {
		uploadedEvents := []rayEvent{}

		nodeEventsPrefix := sessionPrefix + "node_events/"
		nodeEvents, err := loadRayEventsFromS3(test.Ctx(), s3Client, S3BucketName, nodeEventsPrefix)
		gg.Expect(err).NotTo(HaveOccurred())
		uploadedEvents = append(uploadedEvents, nodeEvents...)
		LogWithTimestamp(test.T(), "Loaded %d events from node_events", len(nodeEvents))

		jobEventsPrefix := sessionPrefix + "job_events/"
		jobDirs, err := ListS3Directories(s3Client, S3BucketName, jobEventsPrefix)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(jobDirs).NotTo(BeEmpty())
		LogWithTimestamp(test.T(), "Found %d job directories: %v", len(jobDirs), jobDirs)

		for _, jobDir := range jobDirs {
			jobDirPrefix := jobEventsPrefix + jobDir + "/"
			jobEvents, err := loadRayEventsFromS3(test.Ctx(), s3Client, S3BucketName, jobDirPrefix)
			gg.Expect(err).NotTo(HaveOccurred())
			uploadedEvents = append(uploadedEvents, jobEvents...)
			LogWithTimestamp(test.T(), "Loaded %d events from job_events/%s", len(jobEvents), jobDir)
		}

		assertAllEventTypesCovered(test, gg, uploadedEvents)
	}, TestTimeoutMedium).Should(Succeed())
}

// killContainerAndWaitForRestart kills the main process of a container and waits for the container to restart and become ready.
func killContainerAndWaitForRestart(test Test, g *WithT, getPod func() (*corev1.Pod, error), containerName string) {
	LogWithTimestamp(test.T(), "Killing main process of %s container to trigger a restart", containerName)
	g.Eventually(func(gg Gomega) {
		pod, err := getPod()
		gg.Expect(err).NotTo(HaveOccurred())
		_, stderr := ExecPodCmd(test, pod, containerName, []string{"kill", "1"})
		gg.Expect(stderr.String()).To(BeEmpty())
	}, TestTimeoutMedium).Should(Succeed(), "Failed to kill main process of %s container", containerName)

	LogWithTimestamp(test.T(), "Waiting for %s container to restart and become ready", containerName)
	g.Eventually(func(gg Gomega) {
		pod, err := getPod()
		gg.Expect(err).NotTo(HaveOccurred())
		containerStatus, err := GetContainerStatusByName(pod, containerName)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(containerStatus.RestartCount).To(BeNumerically(">", 0))
		gg.Expect(containerStatus.Ready).To(BeTrue())
	}, TestTimeoutShort).Should(Succeed(), "%s container should restart and become ready", containerName)
}

// loadRayEventsFromS3 loads Ray events from S3.
func loadRayEventsFromS3(ctx context.Context, s3Client *s3.Client, bucket string, prefix string) ([]rayEvent, error) {
	var events []rayEvent

	objects, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, err
	}

	for _, obj := range objects.Contents {
		fileKey := aws.ToString(obj.Key)
		if strings.HasSuffix(fileKey, "/") {
			continue
		}

		content, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(fileKey),
		})
		if err != nil {
			return nil, err
		}

		var fileEvents []rayEvent
		if err := json.NewDecoder(content.Body).Decode(&fileEvents); err != nil {
			content.Body.Close()
			return nil, fmt.Errorf("failed to decode Ray events from %s: %w", fileKey, err)
		}
		content.Body.Close()

		events = append(events, fileEvents...)
	}

	return events, nil
}

// assertAllEventTypesCovered verifies that all potential event types are present in the events uploaded to S3.
// NOTE: EVENT_TYPE_UNSPECIFIED is excluded from verification.
func assertAllEventTypesCovered(test Test, g Gomega, events []rayEvent) {
	foundEventTypes := map[string]bool{}
	for _, event := range events {
		foundEventTypes[event.EventType] = true
	}

	for _, eventType := range eventtypes.AllEventTypes {
		if eventType == eventtypes.EVENT_TYPE_UNSPECIFIED {
			continue
		}
		g.Expect(foundEventTypes[string(eventType)]).To(BeTrue(), "Event type %s not found", eventType)
	}
}
