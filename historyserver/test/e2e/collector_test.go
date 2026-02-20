package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	. "github.com/ray-project/kuberay/historyserver/test/support"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
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
		testFunc func(Test, *WithT, *corev1.Namespace, *s3.S3)
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
// 6. Delete S3 bucket to ensure test isolation
func testCollectorUploadOnGracefulShutdown(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)

	// Submit a Ray job to the existing cluster.
	_ = ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	// Define variables for constructing S3 object prefix.
	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayCluster.Namespace)
	sessionID := GetSessionIDFromHeadPod(test, g, rayCluster)
	headNodeID := GetNodeIDFromPod(test, g, HeadPod(test, rayCluster), "ray-head")
	workerNodeID := GetNodeIDFromPod(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")
	sessionPrefix := fmt.Sprintf("log/%s/%s/", clusterNameID, sessionID)

	// Delete the Ray cluster to trigger log uploading and event flushing on deletion.
	err := test.Client().Ray().RayV1().
		RayClusters(rayCluster.Namespace).
		Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(func() error {
		_, err := GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		return err
	}, TestTimeoutMedium).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

	// Verify logs, node_events, and job_events are successfully uploaded to S3.
	verifyS3SessionDirs(test, g, s3Client, sessionPrefix, headNodeID, workerNodeID)

	// Delete S3 bucket to ensure test isolation.
	DeleteS3Bucket(test, g, s3Client)
}

// testCollectorSeparatesFilesBySession verifies that logs and node_events are successfully uploaded to S3 after the ray-head container is restarted.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster with the collector
// 2. Submit a Ray job to the existing cluster
// 3. Get the sessionID and nodeID for further verification
// 4. Kill the main process of ray-head container to trigger a container restart
// 5. Verify the old session logs have been processed on disk by checking existence of old session directories:
//   - /tmp/ray/prev-logs/{sessionID}
//   - /tmp/ray/persist-complete-logs/{sessionID}
//
// NOTE: Logs under /tmp/ray/session_latest are moved to /tmp/ray/prev-logs by the Ray container startup command.
//
// 6. Verify logs and node_events are successfully uploaded to S3. Expected S3 path structure:
//   - {S3BucketName}/log/{clusterName}_{clusterID}/{sessionID}/logs/...
//   - {S3BucketName}/log/{clusterName}_{clusterID}/{sessionID}/node_events/...
//
// 7. Delete S3 bucket to ensure test isolation
func testCollectorSeparatesFilesBySession(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)

	// Submit a Ray job to the existing cluster.
	_ = ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)

	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayCluster.Namespace)
	sessionID := GetSessionIDFromHeadPod(test, g, rayCluster)
	headNodeID := GetNodeIDFromPod(test, g, HeadPod(test, rayCluster), "ray-head")
	workerNodeID := GetNodeIDFromPod(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")
	sessionPrefix := fmt.Sprintf("log/%s/%s/", clusterNameID, sessionID)

	// NOTE: We use `kill 1` to simulate Kubernetes OOMKilled behavior.
	// Before Kubernetes 1.28 (cgroups v1), if one process in a multi-process container exceeded its memory limit,
	// the Linux OOM killer might kill only that process, leaving the container running and making OOM events hard to detect.
	// Since Kubernetes 1.28 (with cgroups v2 enabled), `memory.oom.group` is enabled by default: when any process in a cgroup
	// hits the memory limit, all processes in the container are killed together, thereby triggering container restart.
	// For more details, please refer to https://github.com/kubernetes/kubernetes/pull/117793
	killContainerAndWaitForRestart(test, g, HeadPod(test, rayCluster), "ray-head")
	killContainerAndWaitForRestart(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")

	verifySessionDirectoriesExist(test, g, rayCluster, sessionID)
	verifyS3SessionDirs(test, g, s3Client, sessionPrefix, headNodeID, workerNodeID)

	DeleteS3Bucket(test, g, s3Client)
}

// testCollectorResumesUploadsOnRestart verifies that the Collector scans and resumes uploads from
// the prev-logs directory on startup.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster with the collector and ensuring an empty S3 bucket.
// 2. Inject leftover logs before killing the collector:
//   - file1.log -> /tmp/ray/persist-complete-logs/{sessionID}/{nodeID}/logs/ (already uploaded)
//   - file2.log -> /tmp/ray/prev-logs/{sessionID}/{nodeID}/logs/ (pending upload)
//     Note: node_events are not injected or verified here; they are handled by the EventServer via a separate path,
//     and prev-logs processing only covers the logs directory.
//
// 3. Kill the collector sidecar container to trigger a container restart.
// 4. Wait for the collector container to restart and become Ready.
// 5. Verify S3 uploads: recovered log objects exist under log/{clusterName}_{clusterID}/{sessionID}/logs/ and have content.
// 6. Verify local state: the node directory is present under persist-complete-logs and removed from prev-logs.
// 7. Clean up the S3 bucket to ensure test isolation.
func testCollectorResumesUploadsOnRestart(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)

	// Directory variables for easier maintenance
	prevLogsBaseDir := "/tmp/ray/prev-logs"
	persistCompleteBaseDir := "/tmp/ray/persist-complete-logs"

	// Use namespace name to ensure test isolation (avoid conflicts from previous test runs)
	dummySessionID := fmt.Sprintf("test-recovery-session-%s", namespace.Name)
	dummyNodeID := fmt.Sprintf("head-node-%s", namespace.Name)
	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayCluster.Namespace)
	sessionPrefix := fmt.Sprintf("log/%s/%s/", clusterNameID, dummySessionID)

	// Inject "leftover" logs BEFORE killing collector.
	// This ensures files exist when collector restarts and performs its initial scan.
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

	// Kill the collector container to trigger a restart.
	// When collector restarts, WatchPrevLogsLoops() will scan prev-logs and find the injected files.
	LogWithTimestamp(test.T(), "Killing collector container to test startup scanning of prev-logs")
	_, stderrKill := ExecPodCmd(test, headPod, "collector", []string{"kill", "1"})
	g.Expect(stderrKill.String()).To(BeEmpty())

	// Wait for collector container to restart and become ready.
	LogWithTimestamp(test.T(), "Waiting for collector container to restart and become ready")
	g.Eventually(func(gg Gomega) {
		updatedPod, err := GetHeadPod(test, rayCluster)
		gg.Expect(err).NotTo(HaveOccurred())
		cs, err := GetContainerStatusByName(updatedPod, "collector")
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(cs.RestartCount).To(BeNumerically(">", 0))
		gg.Expect(cs.Ready).To(BeTrue())
	}, TestTimeoutMedium).Should(Succeed())

	// Verify that file2.log was actually uploaded to S3.
	// file1.log should NOT be uploaded because it was already marked as "completed" in persist-complete-logs.
	// file2.log should be uploaded because it was in prev-logs (pending upload).
	LogWithTimestamp(test.T(), "Verifying file2.log was uploaded to S3 (idempotency check)")
	g.Eventually(func(gg Gomega) {
		// List all objects under the session logs prefix
		logsPrefix := sessionPrefix + "logs/"
		objects, err := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
			Bucket: aws.String(S3BucketName),
			Prefix: aws.String(logsPrefix),
		})
		gg.Expect(err).NotTo(HaveOccurred())

		// Collect all uploaded file keys
		var uploadedKeys []string
		for _, obj := range objects.Contents {
			uploadedKeys = append(uploadedKeys, aws.StringValue(obj.Key))
		}
		LogWithTimestamp(test.T(), "Found uploaded objects: %v", uploadedKeys)

		// Verify file2.log exists in S3 (it was in prev-logs, so it should be uploaded)
		hasFile2 := false
		for _, key := range uploadedKeys {
			if strings.HasSuffix(key, "file2.log") {
				hasFile2 = true
				break
			}
		}
		gg.Expect(hasFile2).To(BeTrue(), "file2.log should be uploaded to S3 because it was in prev-logs")

		// Note: file1.log was only placed in persist-complete-logs (local marker),
		// it was never actually uploaded to S3 in this test scenario.
		// The persist-complete-logs directory is just a local marker to prevent re-upload.
	}, TestTimeoutMedium).Should(Succeed())

	// Verify local state: the node directory should be moved from prev-logs to persist-complete-logs.
	LogWithTimestamp(test.T(), "Verifying local state: node directory should be moved to %s", persistCompleteBaseDir)
	g.Eventually(func(gg Gomega) {
		currentHeadPod, err := GetHeadPod(test, rayCluster)
		gg.Expect(err).NotTo(HaveOccurred())
		// Check that the node directory exists in persist-complete-logs
		persistPath := filepath.Join(persistCompleteBaseDir, dummySessionID, dummyNodeID)
		checkCmd := fmt.Sprintf("test -d %s && echo 'exists'", persistPath)
		stdout, stderrCheck := ExecPodCmd(test, currentHeadPod, "ray-head", []string{"sh", "-c", checkCmd})
		gg.Expect(stderrCheck.String()).To(BeEmpty())
		gg.Expect(strings.TrimSpace(stdout.String())).To(Equal("exists"), "Node directory should be in persist-complete-logs")

		// Check that the node directory is gone from prev-logs
		prevPath := filepath.Join(prevLogsBaseDir, dummySessionID, dummyNodeID)
		checkGoneCmd := fmt.Sprintf("test ! -d %s && echo 'gone'", prevPath)
		stdoutGone, stderrGone := ExecPodCmd(test, currentHeadPod, "ray-head", []string{"sh", "-c", checkGoneCmd})
		gg.Expect(stderrGone.String()).To(BeEmpty())
		gg.Expect(strings.TrimSpace(stdoutGone.String())).To(Equal("gone"), "Node directory should be cleaned from prev-logs")
	}, TestTimeoutMedium).Should(Succeed())

	DeleteS3Bucket(test, g, s3Client)
}

func verifySessionDirectoriesExist(test Test, g *WithT, rayCluster *rayv1.RayCluster, sessionID string) {
	for _, dir := range []string{"prev-logs", "persist-complete-logs"} {
		dirPath := filepath.Join("/tmp/ray", dir, sessionID)
		LogWithTimestamp(test.T(), "Checking if session directory %s exists in %s", sessionID, dirPath)
		g.Eventually(func(gg Gomega) {
			headPod, err := GetHeadPod(test, rayCluster)
			gg.Expect(err).NotTo(HaveOccurred())
			stdout, stderr := ExecPodCmd(test, headPod, "ray-head", []string{"sh", "-c", fmt.Sprintf("test -d %s && echo 'exists' || echo 'not found'", dirPath)})
			gg.Expect(stderr.String()).To(BeEmpty())
			gg.Expect(strings.TrimSpace(stdout.String())).To(Equal("exists"), "Session directory %s should exist in %s", sessionID, dirPath)
		}, TestTimeoutMedium).Should(Succeed(), "Session directory %s should exist in %s", sessionID, dirPath)
	}
}

// testCollectorStoresClusterMetadata verifies that the Head collector fetches /api/v0/cluster_metadata
// from the Ray Dashboard once on startup and stores the result in S3.
//
// The test case follows these steps:
// 1. Prepare test environment by applying a Ray cluster with the collector
// 2. Wait for the cluster metadata file to appear in S3 at meta/restful__api__v0__cluster_metadata
// 3. Read the file and verify it contains valid JSON with the expected schema
// 4. Delete S3 bucket to ensure test isolation
func testCollectorStoresClusterMetadata(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := PrepareTestEnv(test, g, namespace, s3Client)

	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayCluster.Namespace)
	metaKey := fmt.Sprintf("log/%s/meta/%s", clusterNameID, "restful__api__v0__cluster_metadata")

	LogWithTimestamp(test.T(), "Waiting for cluster metadata to appear at S3 key: %s", metaKey)

	var metadataBody []byte
	g.Eventually(func(gg Gomega) {
		result, err := s3Client.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(S3BucketName),
			Key:    aws.String(metaKey),
		})
		gg.Expect(err).NotTo(HaveOccurred())
		defer result.Body.Close()

		body, err := io.ReadAll(result.Body)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(body).NotTo(BeEmpty(), "Cluster metadata file should not be empty")

		// Verify it is valid JSON
		var metadata map[string]interface{}
		err = json.Unmarshal(body, &metadata)
		gg.Expect(err).NotTo(HaveOccurred(), "Cluster metadata should be valid JSON")

		// The Ray dashboard returns {"result": true, "data": {"rayVersion": ..., "pythonVersion": ...}}.
		// Verify the "data" sub-object contains expected fields.
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

// verifyS3SessionDirs verifies file contents in logs/, node_events/, and job_events/ directories under a session prefix in S3.
// There are two phases of verification:
// 1. Verify file contents in logs/ directory
//   - For the head node, verify raylet.out, gcs_server.out, and monitor.out exist
//   - For the worker node, verify raylet.out exists
//
// 2. Verify event type coverage in node_events/ and job_events/ directories
//   - Aggregate all events from node_events/ and job_events/ directories
//   - Verify that all potential event types are present in the aggregated events
//
// NOTE: Since flushed node and job events are nondeterministic, we need to aggregate them first before verifying event type coverage.
func verifyS3SessionDirs(test Test, g *WithT, s3Client *s3.S3, sessionPrefix string, headNodeID string, workerNodeID string) {
	// Verify file contents in logs/ directory.
	headLogDirPrefix := fmt.Sprintf("%slogs/%s", sessionPrefix, headNodeID)
	workerLogDirPrefix := fmt.Sprintf("%slogs/%s", sessionPrefix, workerNodeID)

	LogWithTimestamp(test.T(), "Verifying raylet.out, gcs_server.out, and monitor.out exist in head log directory %s", headLogDirPrefix)
	for _, fileName := range []string{"raylet.out", "gcs_server.out", "monitor.out"} {
		assertFileExist(test, g, s3Client, headLogDirPrefix, fileName)
	}

	LogWithTimestamp(test.T(), "Verifying raylet.out exists in worker log directory %s", workerLogDirPrefix)
	assertFileExist(test, g, s3Client, workerLogDirPrefix, "raylet.out")

	// Verify event type coverage in node_events/ and job_events/ directories.
	LogWithTimestamp(test.T(), "Verifying all %d event types are covered, except for EVENT_TYPE_UNSPECIFIED: %v", len(types.AllEventTypes)-1, types.AllEventTypes)
	g.Eventually(func(gg Gomega) {
		uploadedEvents := []rayEvent{}

		// Load events from node_events directory.
		nodeEventsPrefix := sessionPrefix + "node_events/"
		nodeEvents, err := loadRayEventsFromS3(s3Client, S3BucketName, nodeEventsPrefix)
		gg.Expect(err).NotTo(HaveOccurred())
		uploadedEvents = append(uploadedEvents, nodeEvents...)
		LogWithTimestamp(test.T(), "Loaded %d events from node_events", len(nodeEvents))

		// Dynamically discover and load events from job_events directories.
		jobEventsPrefix := sessionPrefix + "job_events/"
		jobDirs, err := ListS3Directories(s3Client, S3BucketName, jobEventsPrefix)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(jobDirs).NotTo(BeEmpty())
		LogWithTimestamp(test.T(), "Found %d job directories: %v", len(jobDirs), jobDirs)

		for _, jobDir := range jobDirs {
			jobDirPrefix := jobEventsPrefix + jobDir + "/"
			jobEvents, err := loadRayEventsFromS3(s3Client, S3BucketName, jobDirPrefix)
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
func loadRayEventsFromS3(s3Client *s3.S3, bucket string, prefix string) ([]rayEvent, error) {
	var events []rayEvent

	// List all file objects in the directory.
	objects, err := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, err
	}

	for _, obj := range objects.Contents {
		fileKey := aws.StringValue(obj.Key)
		if strings.HasSuffix(fileKey, "/") {
			continue
		}

		// Get the file object content and decode it into Ray events.
		content, err := s3Client.GetObject(&s3.GetObjectInput{
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

// assertFileExist verifies that a file object exists under the given log directory prefix.
// For a Ray cluster with one head node and one worker node, there are two log directories to verify:
//   - logs/<headNodeID>/
//   - logs/<workerNodeID>/
func assertFileExist(test Test, g *WithT, s3Client *s3.S3, nodeLogDirPrefix string, fileName string) {
	fileKey := fmt.Sprintf("%s/%s", nodeLogDirPrefix, fileName)
	LogWithTimestamp(test.T(), "Verifying file %s exists", fileKey)
	g.Eventually(func(gg Gomega) {
		_, err := s3Client.HeadObject(&s3.HeadObjectInput{
			Bucket: aws.String(S3BucketName),
			Key:    aws.String(fileKey),
		})
		gg.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Verified file %s exists", fileKey)
	}, TestTimeoutMedium).Should(Succeed(), "Failed to verify file %s exists", fileKey)
}

// assertAllEventTypesCovered verifies that all potential event types are present in the events uploaded to S3.
// NOTE: EVENT_TYPE_UNSPECIFIED is excluded from verification.
//
// This function accepts Gomega (not *WithT) because it's called from within g.Eventually() callbacks,
// which provide a nested Gomega instance for assertions.
func assertAllEventTypesCovered(test Test, g Gomega, events []rayEvent) {
	foundEventTypes := map[string]bool{}
	for _, event := range events {
		foundEventTypes[event.EventType] = true
	}

	for _, eventType := range types.AllEventTypes {
		if eventType == types.EVENT_TYPE_UNSPECIFIED {
			LogWithTimestamp(test.T(), "Skipping verification for EVENT_TYPE_UNSPECIFIED")
			continue
		}
		g.Expect(foundEventTypes[string(eventType)]).To(BeTrue(), "Event type %s not found", eventType)
	}
}
