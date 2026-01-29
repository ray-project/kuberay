package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

<<<<<<< HEAD
=======
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
	. "github.com/onsi/gomega"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	hssupport "github.com/ray-project/kuberay/historyserver/test/support"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

<<<<<<< HEAD
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
=======
const (
	// S3 storage provider
	minioNamespace    = "minio-dev"
	minioManifestPath = "../../config/minio.yaml"
	minioUsername     = "minioadmin"
	minioSecret       = "minioadmin"
	minioAPIEndpoint  = "http://localhost:9000"
	s3BucketName      = "ray-historyserver"
	s3Region          = "us-east-1"

	// Ray cluster
	rayClusterManifestPath = "../../config/raycluster.yaml"
	rayClusterID           = "default"

	// Ray job
	rayJobManifestPath = "../../config/rayjob.yaml"
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
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
<<<<<<< HEAD
func testCollectorUploadOnGracefulShutdown(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
=======
func testCollectorUploadOnGracefulShutdown(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.Client) {
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
	rayCluster := prepareTestEnv(test, g, namespace, s3Client)

	// Submit a Ray job to the existing cluster.
	_ = applyRayJobAndWaitForCompletion(test, g, namespace)

	// Define variables for constructing S3 object prefix.
	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayClusterID)
<<<<<<< HEAD
	sessionID := getSessionIDFromHeadPod(test, g, rayCluster)
	headNodeID := getNodeIDFromPod(test, g, HeadPod(test, rayCluster), "ray-head")
	workerNodeID := getNodeIDFromPod(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")
=======
	sessionID := hssupport.GetSessionIDFromHeadPod(test, g, rayCluster)
	headNodeID := hssupport.GetNodeIDFromPod(test, g, HeadPod(test, rayCluster), "ray-head")
	workerNodeID := hssupport.GetNodeIDFromPod(test, g, hssupport.FirstWorkerPod(test, rayCluster), "ray-worker")
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
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
	verifyS3SessionDirs(test, g, s3Client, sessionPrefix, headNodeID, workerNodeID, false)

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
<<<<<<< HEAD
func testCollectorSeparatesFilesBySession(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
=======
func testCollectorSeparatesFilesBySession(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.Client) {
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
	rayCluster := prepareTestEnv(test, g, namespace, s3Client)

	// Submit a Ray job to the existing cluster.
	_ = applyRayJobAndWaitForCompletion(test, g, namespace)

	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayClusterID)
<<<<<<< HEAD
	sessionID := getSessionIDFromHeadPod(test, g, rayCluster)
	headNodeID := getNodeIDFromPod(test, g, HeadPod(test, rayCluster), "ray-head")
	workerNodeID := getNodeIDFromPod(test, g, FirstWorkerPod(test, rayCluster), "ray-worker")
=======
	sessionID := hssupport.GetSessionIDFromHeadPod(test, g, rayCluster)
	headNodeID := hssupport.GetNodeIDFromPod(test, g, HeadPod(test, rayCluster), "ray-head")
	workerNodeID := hssupport.GetNodeIDFromPod(test, g, hssupport.FirstWorkerPod(test, rayCluster), "ray-worker")
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
	sessionPrefix := fmt.Sprintf("log/%s/%s/", clusterNameID, sessionID)

	// NOTE: We use `kill 1` to simulate Kubernetes OOMKilled behavior.
	// Before Kubernetes 1.28 (cgroups v1), if one process in a multi-process container exceeded its memory limit,
	// the Linux OOM killer might kill only that process, leaving the container running and making OOM events hard to detect.
	// Since Kubernetes 1.28 (with cgroups v2 enabled), `memory.oom.group` is enabled by default: when any process in a cgroup
	// hits the memory limit, all processes in the container are killed together, thereby triggering container restart.
	// For more details, please refer to https://github.com/kubernetes/kubernetes/pull/117793
	killContainerAndWaitForRestart(test, g, HeadPod(test, rayCluster), "ray-head")
	killContainerAndWaitForRestart(test, g, hssupport.FirstWorkerPod(test, rayCluster), "ray-worker")

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
<<<<<<< HEAD

	// Verify logs, node_events, and job_events are successfully uploaded to S3.
	verifyS3SessionDirs(test, g, s3Client, sessionPrefix, headNodeID, workerNodeID)

=======

	// Verify logs, node_events, and job_events are successfully uploaded to S3.
	verifyS3SessionDirs(test, g, s3Client, sessionPrefix, headNodeID, workerNodeID, false)

>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
	deleteS3Bucket(test, g, s3Client)
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
<<<<<<< HEAD
func testCollectorResumesUploadsOnRestart(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
=======
func testCollectorResumesUploadsOnRestart(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.Client) {
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
	rayCluster := prepareTestEnv(test, g, namespace, s3Client)

	// Directory variables for easier maintenance
	prevLogsBaseDir := "/tmp/ray/prev-logs"
	persistCompleteBaseDir := "/tmp/ray/persist-complete-logs"

	// Use namespace name to ensure test isolation (avoid conflicts from previous test runs)
	dummySessionID := fmt.Sprintf("test-recovery-session-%s", namespace.Name)
	dummyNodeID := fmt.Sprintf("head-node-%s", namespace.Name)
	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, rayClusterID)
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
		cs, err := getContainerStatusByName(updatedPod, "collector")
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
<<<<<<< HEAD
		objects, err := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
=======
		objects, err := s3Client.ListObjectsV2(test.Ctx(), &s3.ListObjectsV2Input{
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
			Bucket: aws.String(s3BucketName),
			Prefix: aws.String(logsPrefix),
		})
		gg.Expect(err).NotTo(HaveOccurred())

		// Collect all uploaded file keys
		var uploadedKeys []string
		for _, obj := range objects.Contents {
			uploadedKeys = append(uploadedKeys, aws.ToString(obj.Key))
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

	deleteS3Bucket(test, g, s3Client)
}

// ensureS3Client creates an S3 client and ensures API endpoint accessibility.
<<<<<<< HEAD
func ensureS3Client(t *testing.T) *s3.S3 {
=======
func ensureS3Client(t *testing.T) *s3.Client {
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
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
<<<<<<< HEAD
		_, err = s3Client.ListBuckets(&s3.ListBucketsInput{}) // Dummy operation to ensure accessibility
=======
		_, err = s3Client.ListBuckets(test.Ctx(), &s3.ListBucketsInput{}) // Dummy operation to ensure accessibility
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
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
<<<<<<< HEAD
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
=======
func newS3Client(endpoint string) (*s3.Client, error) {
	ctx := context.Background()

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(s3Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(minioUsername, minioSecret, "")),
	)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String(endpoint)
	}), nil
}

// prepareTestEnv prepares test environment for each test case, including applying a Ray cluster,
// checking the collector sidecar container exists in the head pod and an empty S3 bucket exists.
func prepareTestEnv(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.Client) *rayv1.RayCluster {
	// Deploy a Ray cluster with the collector.
	rayCluster := applyRayCluster(test, g, namespace)

	// Check the collector sidecar exists in the head pod.
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod.Spec.Containers).To(ContainElement(
		WithTransform(func(c corev1.Container) string { return c.Name }, Equal("collector")),
	))

	// Check an empty S3 bucket is automatically created.
	_, err = s3Client.HeadBucket(test.Ctx(), &s3.HeadBucketInput{
		Bucket: aws.String(s3BucketName),
	})
	g.Expect(err).NotTo(HaveOccurred())

	return rayCluster
}

// deleteS3Bucket deletes the S3 bucket. Note that objects under the bucket should be deleted first.
func deleteS3Bucket(test Test, g *WithT, s3Client *s3.Client) {
	// TODO(jwj): Better err handling during cleanup.
	LogWithTimestamp(test.T(), "Deleting S3 bucket %s", s3BucketName)

	paginator := s3.NewListObjectsV2Paginator(s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s3BucketName),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(test.Ctx())
		if err != nil {
			test.T().Logf("Failed to list/delete objects in bucket: %v", err)
			break
		}
		if len(page.Contents) == 0 {
			break
		}

		objectsToDelete := make([]s3types.ObjectIdentifier, 0, len(page.Contents))
		for _, obj := range page.Contents {
			objectsToDelete = append(objectsToDelete, s3types.ObjectIdentifier{Key: obj.Key})
		}

		_, err = s3Client.DeleteObjects(test.Ctx(), &s3.DeleteObjectsInput{
			Bucket: aws.String(s3BucketName),
			Delete: &s3types.Delete{
				Objects: objectsToDelete,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			test.T().Logf("Failed to delete objects: %v", err)
			break
		}
	}

	_, err := s3Client.DeleteBucket(test.Ctx(), &s3.DeleteBucketInput{
		Bucket: aws.String(s3BucketName),
	})
	if err != nil {
		test.T().Logf("Failed to delete bucket %s: %v (this is OK if bucket doesn't exist)", s3BucketName, err)
	} else {
		LogWithTimestamp(test.T(), "Deleted S3 bucket %s successfully", s3BucketName)
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
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

// applyRayJobAndWaitForCompletion applies a Ray job to the existing Ray cluster and waits for it to complete successfully.
// In the RayJob manifest, the clusterSelector is set to the existing Ray cluster, raycluster-historyserver.
func applyRayJobAndWaitForCompletion(test Test, g *WithT, namespace *corev1.Namespace) *rayv1.RayJob {
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

// applyRayJobAndWaitForCompletion applies a Ray job to the existing Ray cluster and waits for it to complete successfully.
// In the RayJob manifest, the clusterSelector is set to the existing Ray cluster, raycluster-historyserver.
func applyRayJobAndWaitForCompletion(test Test, g *WithT, namespace *corev1.Namespace) *rayv1.RayJob {
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

// verifyS3SessionDirs verifies that directories logs/<headNodeID>/, logs/<workerNodeID>/, and node_events/ exist under a session prefix in S3.
// This helper function checks that required log files have content:
// - head logs: raylet.out, gcs_server.out, monitor.out
// - worker logs: raylet.out
// If skipNodeEvents is true, node_events directory verification will be skipped.
func verifyS3SessionDirs(test Test, g *WithT, s3Client *s3.Client, sessionPrefix string, headNodeID string, workerNodeID string, skipNodeEvents bool) {
	headLogDirPrefix := fmt.Sprintf("%slogs/%s", sessionPrefix, headNodeID)
	workerLogDirPrefix := fmt.Sprintf("%slogs/%s", sessionPrefix, workerNodeID)

	g.Eventually(func(gg Gomega) {
		for _, fileName := range []string{"raylet.out", "gcs_server.out", "monitor.out"} {
			fileKey := fmt.Sprintf("%s/%s", headLogDirPrefix, fileName)
			LogWithTimestamp(test.T(), "Verifying head log file %s exists", fileKey)
			obj, err := s3Client.HeadObject(test.Ctx(), &s3.HeadObjectInput{
				Bucket: aws.String(s3BucketName),
				Key:    aws.String(fileKey),
			})
			gg.Expect(err).NotTo(HaveOccurred())
			fileSize := aws.ToInt64(obj.ContentLength)
			gg.Expect(fileSize).To(BeNumerically(">", 0))
		}

		workerFileKey := fmt.Sprintf("%s/%s", workerLogDirPrefix, "raylet.out")
		LogWithTimestamp(test.T(), "Verifying worker log file %s exists", workerFileKey)
		obj, err := s3Client.HeadObject(test.Ctx(), &s3.HeadObjectInput{
			Bucket: aws.String(s3BucketName),
			Key:    aws.String(workerFileKey),
		})
		gg.Expect(err).NotTo(HaveOccurred())
		fileSize := aws.ToInt64(obj.ContentLength)
		gg.Expect(fileSize).To(BeNumerically(">", 0))
	}, TestTimeoutMedium).Should(Succeed(), "Failed to verify required log files under %s", sessionPrefix+"logs/")

	if skipNodeEvents {
		return
	}

	// Verify event type coverage in node_events/ and job_events/ directories.
	LogWithTimestamp(test.T(), "Verifying all %d event types are covered, except for EVENT_TYPE_UNSPECIFIED: %v", len(eventtypes.AllEventTypes)-1, eventtypes.AllEventTypes)
	g.Eventually(func(gg Gomega) {
		uploadedEvents := []rayEvent{}

		// Load events from node_events directory.
		nodeEventsPrefix := sessionPrefix + "node_events/"
<<<<<<< HEAD
		nodeEvents, err := loadRayEventsFromS3(s3Client, s3BucketName, nodeEventsPrefix)
=======
		nodeEvents, err := loadRayEventsFromS3(test.Ctx(), s3Client, s3BucketName, nodeEventsPrefix)
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
		gg.Expect(err).NotTo(HaveOccurred())
		uploadedEvents = append(uploadedEvents, nodeEvents...)
		LogWithTimestamp(test.T(), "Loaded %d events from node_events", len(nodeEvents))

		// Dynamically discover and load events from job_events directories.
		jobEventsPrefix := sessionPrefix + "job_events/"
<<<<<<< HEAD
		jobDirs, err := listS3Directories(s3Client, s3BucketName, jobEventsPrefix)
=======
		jobDirs, err := listS3Directories(test.Ctx(), s3Client, s3BucketName, jobEventsPrefix)
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(jobDirs).NotTo(BeEmpty())
		LogWithTimestamp(test.T(), "Found %d job directories: %v", len(jobDirs), jobDirs)

		for _, jobDir := range jobDirs {
			jobDirPrefix := jobEventsPrefix + jobDir + "/"
<<<<<<< HEAD
			jobEvents, err := loadRayEventsFromS3(s3Client, s3BucketName, jobDirPrefix)
=======
			jobEvents, err := loadRayEventsFromS3(test.Ctx(), s3Client, s3BucketName, jobDirPrefix)
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
			gg.Expect(err).NotTo(HaveOccurred())
			uploadedEvents = append(uploadedEvents, jobEvents...)
			LogWithTimestamp(test.T(), "Loaded %d events from job_events/%s", len(jobEvents), jobDir)
		}

		assertAllEventTypesCovered(test, gg, uploadedEvents)
	}, TestTimeoutMedium).Should(Succeed())
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

// listS3Directories lists all directories (prefixes) under the given S3 prefix.
// In S3, directories are simulated using prefixes and delimiters.
// For example, given prefix "log/cluster/session/job_events/", this function returns ["AgAAAA==", "AQAAAA=="]
// which are the jobID directories under job_events/.
<<<<<<< HEAD
func listS3Directories(s3Client *s3.S3, bucket string, prefix string) ([]string, error) {
	result, err := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
=======
func listS3Directories(ctx context.Context, s3Client *s3.Client, bucket string, prefix string) ([]string, error) {
	result, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
		Bucket:    aws.String(bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list S3 directories under %s: %w", prefix, err)
	}

	// Extract directory names from CommonPrefixes.
	var directories []string
	for _, commonPrefix := range result.CommonPrefixes {
<<<<<<< HEAD
		fullPrefix := aws.StringValue(commonPrefix.Prefix)
=======
		fullPrefix := aws.ToString(commonPrefix.Prefix)
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
		// Extract the directory name by removing the parent prefix and trailing slash.
		// Example: "log/cluster/session/job_events/AgAAAA==/" -> "AgAAAA=="
		dirName := strings.TrimPrefix(fullPrefix, prefix)
		dirName = strings.TrimSuffix(dirName, "/")
		if dirName != "" {
			directories = append(directories, dirName)
		}
	}

	return directories, nil
}

// loadRayEventsFromS3 loads Ray events from S3.
func loadRayEventsFromS3(ctx context.Context, s3Client *s3.Client, bucket string, prefix string) ([]rayEvent, error) {
	var events []rayEvent

	// List all file objects in the directory.
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

		// Get the file object content and decode it into Ray events.
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

<<<<<<< HEAD
// assertFileExist verifies that a file object exists under the given log directory prefix.
// For a Ray cluster with one head node and one worker node, there are two log directories to verify:
//   - logs/<headNodeID>/
//   - logs/<workerNodeID>/
func assertFileExist(test Test, g *WithT, s3Client *s3.S3, nodeLogDirPrefix string, fileName string) {
	fileKey := fmt.Sprintf("%s/%s", nodeLogDirPrefix, fileName)
	LogWithTimestamp(test.T(), "Verifying file %s exists", fileKey)
	g.Eventually(func(gg Gomega) {
		_, err := s3Client.HeadObject(&s3.HeadObjectInput{
			Bucket: aws.String(s3BucketName),
			Key:    aws.String(fileKey),
		})
		gg.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Verified file %s exists", fileKey)
	}, TestTimeoutMedium).Should(Succeed(), "Failed to verify file %s exists", fileKey)
}

=======
>>>>>>> 76c417f7 (Migrate to new AWS S3 SDK version)
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

	for _, eventType := range eventtypes.AllEventTypes {
		if eventType == eventtypes.EVENT_TYPE_UNSPECIFIED {
			continue
		}
		g.Expect(foundEventTypes[string(eventType)]).To(BeTrue(), "Event type %s not found", eventType)
	}
}
