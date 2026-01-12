package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

const (
	// S3 storage provider
	minioNamespace    = "minio-dev"
	minioManifestPath = "../../config/minio.yaml"
	minioUsername     = "minioadmin"
	minioSecret       = "minioadmin"
	minioAPIEndpoint  = "http://localhost:9000"
	s3BucketName      = "ray-historyserver"

	// Ray cluster
	rayClusterManifestPath = "../../config/raycluster.yaml"
	rayClusterID           = "default"

	// Ray job
	rayJobManifestPath = "../../config/rayjob.yaml"
)

// rayEventTypes includes all potential event types defined in Ray:
// https://github.com/ray-project/ray/blob/3b41c97fa90c58b0b72c0026f57005b92310160d/src/ray/protobuf/public/events_base_event.proto#L49-L61
var rayEventTypes = []string{
	"ACTOR_DEFINITION_EVENT",
	"ACTOR_LIFECYCLE_EVENT",
	"ACTOR_TASK_DEFINITION_EVENT",
	"DRIVER_JOB_DEFINITION_EVENT",
	"DRIVER_JOB_LIFECYCLE_EVENT",
	"TASK_DEFINITION_EVENT",
	"TASK_LIFECYCLE_EVENT",
	"TASK_PROFILE_EVENT",
	"NODE_DEFINITION_EVENT",
	"NODE_LIFECYCLE_EVENT",
}

// rayEvent contains specific fields in the Ray event JSON schema. For now, we keep only two fields,
// eventId and eventType while ensuring future extensibility (e.g., sessionName, timestamp, sourceType, etc.).
type rayEvent struct {
	EventID   string `json:"eventId"`
	EventType string `json:"eventType"`
}

func TestCollector(t *testing.T) {
	// Share a single S3 client among subtests.
	s3Client := ensureS3Client(t)

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
//   - {s3BucketName}/log/{clusterName}_{clusterID}/{sessionID}/logs/...
//   - {s3BucketName}/log/{clusterName}_{clusterID}/{sessionID}/node_events/...
//   - {s3BucketName}/log/{clusterName}_{clusterID}/{sessionID}/job_events/AgAAAA==/...
//   - {s3BucketName}/log/{clusterName}_{clusterID}/{sessionID}/job_events/AQAAAA==/...
//
// For detailed verification logic, please refer to verifyS3SessionDirs.
//
// 6. Delete S3 bucket to ensure test isolation
func testCollectorUploadOnGracefulShutdown(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := prepareTestEnv(test, g, namespace, s3Client)

	// Submit a Ray job to the existing cluster.
	_ = applyRayJobToCluster(test, g, namespace, rayCluster)

	// Define variables for constructing S3 object prefix.
	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayClusterID)
	sessionID := getSessionIDFromHeadPod(test, g, rayCluster)
	headNodeID := getNodeIDFromPod(test, g, HeadPod(test, rayCluster), "ray-head")
	workerNodeID := getNodeIDFromPod(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")
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
	deleteS3Bucket(test, g, s3Client)
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
//   - {s3BucketName}/log/{clusterName}_{clusterID}/{sessionID}/logs/...
//   - {s3BucketName}/log/{clusterName}_{clusterID}/{sessionID}/node_events/...
//
// 7. Delete S3 bucket to ensure test isolation
func testCollectorSeparatesFilesBySession(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	rayCluster := prepareTestEnv(test, g, namespace, s3Client)

	// Submit a Ray job to the existing cluster.
	_ = applyRayJobToCluster(test, g, namespace, rayCluster)

	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayClusterID)
	sessionID := getSessionIDFromHeadPod(test, g, rayCluster)
	headNodeID := getNodeIDFromPod(test, g, HeadPod(test, rayCluster), "ray-head")
	workerNodeID := getNodeIDFromPod(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")
	sessionPrefix := fmt.Sprintf("log/%s/%s/", clusterNameID, sessionID)

	// NOTE: We use `kill 1` to simulate Kubernetes OOMKilled behavior.
	// Before Kubernetes 1.28 (cgroups v1), if one process in a multi-process container exceeded its memory limit,
	// the Linux OOM killer might kill only that process, leaving the container running and making OOM events hard to detect.
	// Since Kubernetes 1.28 (with cgroups v2 enabled), `memory.oom.group` is enabled by default: when any process in a cgroup
	// hits the memory limit, all processes in the container are killed together, thereby triggering container restart.
	// For more details, please refer to https://github.com/kubernetes/kubernetes/pull/117793
	killContainerAndWaitForRestart(test, g, HeadPod(test, rayCluster), "ray-head")
	// TODO(jwj): Clarify the automatic restart mechanism.
	// Force kill the ray-worker container before automatic restart.
	killContainerAndWaitForRestart(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")

	// Verify the old session logs have been processed on disk.
	dirs := []string{"prev-logs", "persist-complete-logs"}
	for _, dir := range dirs {
		dirPath := filepath.Join("/tmp/ray", dir, sessionID)
		LogWithTimestamp(test.T(), "Checking if session directory %s exists in %s", sessionID, dirPath)
		g.Eventually(func(gg Gomega) {
			headPod, err := GetHeadPod(test, rayCluster)
			gg.Expect(err).NotTo(HaveOccurred())
			checkDirExistsCmd := fmt.Sprintf("test -d %s && echo 'exists' || echo 'not found'", dirPath)
			stdout, stderr := ExecPodCmd(test, headPod, "ray-head", []string{"sh", "-c", checkDirExistsCmd})
			gg.Expect(stderr.String()).To(BeEmpty())
			gg.Expect(strings.TrimSpace(stdout.String())).To(Equal("exists"), "Session directory %s should exist in %s", sessionID, dirPath)
		}, TestTimeoutMedium).Should(Succeed(), "Session directory %s should exist in %s", sessionID, dirPath)
	}

	// Verify logs, node_events, and job_events are successfully uploaded to S3.
	verifyS3SessionDirs(test, g, s3Client, sessionPrefix, headNodeID, workerNodeID)

	deleteS3Bucket(test, g, s3Client)
}

// ensureS3Client creates an S3 client and ensures API endpoint accessibility.
func ensureS3Client(t *testing.T) *s3.S3 {
	test := With(t)
	g := NewWithT(t)
	applyMinIO(test, g)

	// Port-forward the minio API port.
	ctx, cancel := context.WithCancel(context.Background())
	test.T().Cleanup(cancel)
	kubectlCmd := exec.CommandContext(
		ctx,
		"kubectl",
		"-n", minioNamespace,
		"port-forward",
		"svc/minio-service",
		"9000:9000",
	)
	err := kubectlCmd.Start()
	g.Expect(err).NotTo(HaveOccurred())

	// Check readiness of the minio API endpoint.
	g.Eventually(func() error {
		s3Client, err := newS3Client(minioAPIEndpoint)
		if err != nil {
			return err
		}
		_, err = s3Client.ListBuckets(&s3.ListBucketsInput{}) // Dummy operation to ensure accessibility
		return err
	}, TestTimeoutMedium).Should(Succeed(), "MinIO API endpoint should be ready")
	LogWithTimestamp(test.T(), "Port-forwarded minio API port to localhost:9000 successfully")

	s3Client, err := newS3Client(minioAPIEndpoint)
	g.Expect(err).NotTo(HaveOccurred())

	return s3Client
}

// applyMinIO deploys minio once per test namespace, making sure it's idempotent.
// TODO(jwj): Check idempotency (for now, only manual check).
func applyMinIO(test Test, g *WithT) {
	KubectlApplyYAML(test, minioManifestPath, minioNamespace)

	// Wait for minio pods ready.
	g.Eventually(func(gg Gomega) {
		pods, err := test.Client().Core().CoreV1().Pods(minioNamespace).List(
			test.Ctx(), metav1.ListOptions{
				LabelSelector: "app=minio",
			},
		)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(pods.Items).NotTo(BeEmpty())
		gg.Expect(AllPodsRunningAndReady(pods.Items)).To(BeTrue())
	}, TestTimeoutMedium).Should(Succeed())
}

// newS3Client creates a new S3 client.
func newS3Client(endpoint string) (*s3.S3, error) {
	sess, err := session.NewSession(&aws.Config{
		Endpoint:         aws.String(endpoint),
		Region:           aws.String("e2e-test"),
		Credentials:      credentials.NewStaticCredentials(minioUsername, minioSecret, ""),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
	})
	if err != nil {
		return nil, err
	}
	return s3.New(sess), nil
}

// prepareTestEnv prepares test environment for each test case, including applying a Ray cluster,
// checking the collector sidecar container exists in the head pod and an empty S3 bucket exists.
func prepareTestEnv(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) *rayv1.RayCluster {
	// Deploy a Ray cluster with the collector.
	rayCluster := applyRayCluster(test, g, namespace)

	// Check the collector sidecar exists in the head pod.
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod.Spec.Containers).To(ContainElement(
		WithTransform(func(c corev1.Container) string { return c.Name }, Equal("collector")),
	))

	// Check an empty S3 bucket is automatically created.
	_, err = s3Client.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(s3BucketName),
	})
	g.Expect(err).NotTo(HaveOccurred())

	return rayCluster
}

// deleteS3Bucket deletes the S3 bucket. Note that objects under the bucket should be deleted first.
func deleteS3Bucket(test Test, g *WithT, s3Client *s3.S3) {
	// TODO(jwj): Better err handling during cleanup.
	LogWithTimestamp(test.T(), "Deleting S3 bucket %s", s3BucketName)

	err := s3Client.ListObjectsV2Pages(&s3.ListObjectsV2Input{
		Bucket: aws.String(s3BucketName),
	}, func(page *s3.ListObjectsV2Output, lastPage bool) bool {
		if len(page.Contents) == 0 {
			return false
		}

		var objectsToDelete []*s3.ObjectIdentifier
		for _, obj := range page.Contents {
			objectsToDelete = append(objectsToDelete, &s3.ObjectIdentifier{
				Key: obj.Key,
			})
		}

		_, err := s3Client.DeleteObjects(&s3.DeleteObjectsInput{
			Bucket: aws.String(s3BucketName),
			Delete: &s3.Delete{
				Objects: objectsToDelete,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			test.T().Logf("Failed to delete objects: %v", err)
			return false
		}

		return true
	})
	if err != nil {
		test.T().Logf("Failed to list/delete objects in bucket: %v", err)
	}

	_, err = s3Client.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(s3BucketName),
	})
	if err != nil {
		test.T().Logf("Failed to delete bucket %s: %v (this is OK if bucket doesn't exist)", s3BucketName, err)
	} else {
		LogWithTimestamp(test.T(), "Deleted S3 bucket %s successfully", s3BucketName)
	}
}

// applyRayCluster deploys a Ray cluster with the collector sidecar into the test namespace.
func applyRayCluster(test Test, g *WithT, namespace *corev1.Namespace) *rayv1.RayCluster {
	rayClusterFromYaml := DeserializeRayClusterYAML(test, rayClusterManifestPath)
	rayClusterFromYaml.Namespace = namespace.Name

	rayCluster, err := test.Client().Ray().RayV1().
		RayClusters(namespace.Name).
		Create(test.Ctx(), rayClusterFromYaml, metav1.CreateOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutLong).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	LogWithTimestamp(test.T(), "Waiting for head pod of RayCluster %s/%s to be running and ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(HeadPod(test, rayCluster), TestTimeoutMedium).
		Should(WithTransform(IsPodRunningAndReady, BeTrue()))

	return rayCluster
}

// applyRayJobToCluster applies a Ray job to the existing Ray cluster.
func applyRayJobToCluster(test Test, g *WithT, namespace *corev1.Namespace, rayCluster *rayv1.RayCluster) *rayv1.RayJob {
	rayJobFromYaml := DeserializeRayJobYAML(test, rayJobManifestPath)
	rayJobFromYaml.Namespace = namespace.Name

	rayJob, err := test.Client().Ray().RayV1().
		RayJobs(namespace.Name).
		Create(test.Ctx(), rayJobFromYaml, metav1.CreateOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

	LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to complete successfully", rayJob.Namespace, rayJob.Name)
	g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
		Should(SatisfyAll(
			WithTransform(RayJobStatus, Equal(rayv1.JobStatusSucceeded)),
			WithTransform(RayJobDeploymentStatus, Equal(rayv1.JobDeploymentStatusComplete)),
		))
	LogWithTimestamp(test.T(), "RayJob %s/%s completed successfully", rayJob.Namespace, rayJob.Name)

	return rayJob
}

// verifyS3SessionDirs verifies file contents in logs/, node_events/, and job_events/ directories under a session prefix in S3.
// There are two phases of verification:
// 1. Verify file contents in logs/ directory
//   - For the head node, verify raylet.out and gcs_server.out exist and have content > 0 bytes
//   - For the worker node, verify raylet.out exists and have content > 0 bytes
//
// 2. Verify event type coverage in node_events/ and job_events/ directories
//   - Aggregate all events from node_events/ and job_events/ directories
//   - Verify that all potential event types are present in the aggregated events
//
// NOTE: Since flushed node and job events are nondeterministic, we need to aggregate them first before verifying event type coverage.
func verifyS3SessionDirs(test Test, g *WithT, s3Client *s3.S3, sessionPrefix string, headNodeID string, workerNodeID string) {
	// Verify file contents in logs/ directory.
	headLogDirPrefix := fmt.Sprintf("%slogs/%s/", sessionPrefix, headNodeID)
	workerLogDirPrefix := fmt.Sprintf("%slogs/%s/", sessionPrefix, workerNodeID)

	LogWithTimestamp(test.T(), "Verifying raylet.out and gcs_server.out exist in head log directory %s", headLogDirPrefix)
	for _, fileName := range []string{"raylet.out", "gcs_server.out"} {
		assertNonEmptyFileExist(test, g, s3Client, headLogDirPrefix, fileName)
	}

	LogWithTimestamp(test.T(), "Verifying raylet.out exists in worker log directory %s", workerLogDirPrefix)
	assertNonEmptyFileExist(test, g, s3Client, workerLogDirPrefix, "raylet.out")

	// Verify event type coverage in node_events/ and job_events/ directories.
	LogWithTimestamp(test.T(), "Verifying all %d event types are covered: %v", len(rayEventTypes), rayEventTypes)
	uploadedEvents := []rayEvent{}
	for _, dir := range []string{"node_events", "job_events/AgAAAA==", "job_events/AQAAAA=="} {
		dirPrefix := sessionPrefix + dir + "/"
		events, err := loadRayEventsFromS3(s3Client, s3BucketName, dirPrefix)
		g.Expect(err).NotTo(HaveOccurred())
		uploadedEvents = append(uploadedEvents, events...)
	}
	assertAllEventTypesCovered(test, g, uploadedEvents)
}

// getSessionIDFromHeadPod retrieves the sessionID from the Ray head pod by reading the symlink
// /tmp/ray/session_latest and getting its basename.
func getSessionIDFromHeadPod(test Test, g *WithT, rayCluster *rayv1.RayCluster) string {
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())

	getSessionIDCmd := `if [ -L "/tmp/ray/session_latest" ]; then
  session_path=$(readlink /tmp/ray/session_latest)
  basename "$session_path"
else
  echo "session_latest is not a symlink"
  exit 1
fi`
	output, _ := ExecPodCmd(test, headPod, "ray-head", []string{"sh", "-c", getSessionIDCmd})

	// Parse output to extract the sessionID.
	sessionID := strings.TrimSpace(output.String())
	LogWithTimestamp(test.T(), "Retrieved sessionID: %s", sessionID)
	g.Expect(sessionID).NotTo(BeEmpty(), "sessionID should not be empty")

	return sessionID
}

// getNodeIDFromPod retrieves the nodeID from the Ray head or worker pod by reading /tmp/ray/raylet_node_id.
func getNodeIDFromPod(test Test, g *WithT, getPod func() (*corev1.Pod, error), containerName string) string {
	pod, err := getPod()
	g.Expect(err).NotTo(HaveOccurred())

	getNodeIDCmd := `if [ -f "/tmp/ray/raylet_node_id" ]; then
  cat /tmp/ray/raylet_node_id
else
  echo "raylet_node_id not found"
  exit 1
fi`
	output, _ := ExecPodCmd(test, pod, containerName, []string{"sh", "-c", getNodeIDCmd})

	// Parse output to extract the nodeID.
	nodeID := strings.TrimSpace(output.String())
	LogWithTimestamp(test.T(), "Retrieved nodeID: %s", nodeID)
	g.Expect(nodeID).NotTo(BeEmpty(), "nodeID should not be empty")

	return nodeID

}

// FirstWorkerPod returns a function that gets the first worker pod from the Ray cluster.
// It adapts the WorkerPods function to be used with functions expecting a single pod.
func FirstWorkerPod(test Test, rayCluster *rayv1.RayCluster) func() (*corev1.Pod, error) {
	return func() (*corev1.Pod, error) {
		workerPods, err := GetWorkerPods(test, rayCluster)
		if err != nil {
			return nil, err
		}
		if len(workerPods) == 0 {
			return nil, fmt.Errorf("no worker pods found")
		}
		return &workerPods[0], nil
	}
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
		containerStatus, err := getContainerStatusByName(pod, containerName)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(containerStatus.RestartCount).To(BeNumerically(">", 0))
		gg.Expect(containerStatus.Ready).To(BeTrue())
	}, TestTimeoutShort).Should(Succeed(), "%s container should restart and become ready", containerName)
}

// getContainerStatusByName retrieves the container status by container name.
// NOTE: ContainerStatuses order doesn't guarantee to match Spec.Containers order.
// For more details, please refer to the following link:
// https://github.com/ray-project/kuberay/blob/7791a8786861818f0cebcce381ef221436a0fa4d/ray-operator/controllers/ray/raycluster_controller.go#L1160C1-L1171C2
func getContainerStatusByName(pod *corev1.Pod, containerName string) (*corev1.ContainerStatus, error) {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Name == containerName {
			return &containerStatus, nil
		}
	}
	return nil, fmt.Errorf("container %s not found in pod %s/%s", containerName, pod.Namespace, pod.Name)
}

// loadRayEventsFromS3 loads Ray events from S3.
func loadRayEventsFromS3(s3Client *s3.S3, bucket string, prefix string) ([]rayEvent, error) {
	objects, err := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, err
	}

	// Find the first file object for loading Ray events.
	var fileKey string
	for _, obj := range objects.Contents {
		if key := aws.StringValue(obj.Key); !strings.HasSuffix(key, "/") {
			fileKey = key
			break
		}
	}
	if fileKey == "" {
		return nil, fmt.Errorf("no file object found in directory %s", prefix)
	}

	// Get and decode the file object content into Ray events.
	content, err := s3Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(fileKey),
	})
	if err != nil {
		return nil, err
	}
	defer content.Body.Close()

	var events []rayEvent
	if err := json.NewDecoder(content.Body).Decode(&events); err != nil {
		return nil, err
	}
	return events, nil
}

// assertNonEmptyFileExist verifies that a file exists and has content (> 0 bytes).
// For a Ray cluster with one head node and one worker node, there are two log directories to verify:
//   - logs/<headNodeID>/
//   - logs/<workerNodeID>/
func assertNonEmptyFileExist(test Test, g *WithT, s3Client *s3.S3, nodeLogDirPrefix string, fileName string) {
	fileKey := fmt.Sprintf("%s/%s", nodeLogDirPrefix, fileName)
	LogWithTimestamp(test.T(), "Verifying file %s has content (> 0 bytes)", fileKey)
	g.Eventually(func(gg Gomega) {
		// Verify the file has content by checking file size.
		obj, err := s3Client.HeadObject(&s3.HeadObjectInput{
			Bucket: aws.String(s3BucketName),
			Key:    aws.String(fileKey),
		})
		gg.Expect(err).NotTo(HaveOccurred())
		fileSize := aws.Int64Value(obj.ContentLength)
		gg.Expect(fileSize).To(BeNumerically(">", 0))
		LogWithTimestamp(test.T(), "Verified file %s has content: %d bytes", fileKey, fileSize)
	}, TestTimeoutMedium).Should(Succeed(), "Failed to verify file %s has content (> 0 bytes)", fileKey)
}

// assertAllEventTypesCovered verifies that all potential event types are present in the events uploaded to S3.
func assertAllEventTypesCovered(test Test, g *WithT, events []rayEvent) {
	foundEventTypes := map[string]bool{}
	for _, event := range events {
		foundEventTypes[event.EventType] = true
	}

	for _, eventType := range rayEventTypes {
		g.Expect(foundEventTypes[eventType]).To(BeTrue(), "Event type %s not found", eventType)
	}
}
