package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
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
)

func TestCollector(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create an isolated Kubernetes namespace.
	namespace := test.NewTestNamespace()

	// Share a single S3 client among subtests.
	s3Client, err := ensureS3Client(test, g)
	g.Expect(err).NotTo(HaveOccurred())

	t.Run("Happy path: Logs and events should be uploaded to S3 on deletion", func(t *testing.T) {
		testLogAndEventUploadOnDeletion(test, g, namespace, s3Client)
	})

	t.Run("Single session single node logs should be uploaded to S3 during runtime", func(t *testing.T) {
		testPrevLogsRuntimeUpload(test, g, namespace, s3Client)
	})

	// Add other test cases below.
	// ...
}

func testLogAndEventUploadOnDeletion(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
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

	// Submit a Ray job to the existing cluster.
	_ = applyRayJobToCluster(test, g, namespace, rayCluster)

	// Retrieve sessionID from the head pod.
	sessionID, err := getSessionIDFromHeadPod(test, namespace, rayCluster)
	g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve sessionID from head pod")
	LogWithTimestamp(test.T(), "Retrieved sessionID: %s", sessionID)

	// Delete the Ray cluster to trigger log uploading on deletion.
	err = test.Client().Ray().RayV1().
		RayClusters(rayCluster.Namespace).
		Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(func() error {
		_, err := GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		return err
	}, TestTimeoutMedium).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

	// Verify logs and node_events are successfully uploaded to minio.
	// Expected S3 path structure:
	//   {s3BucketName}/log/{clusterName}_{clusterID}/{sessionId}/logs/...
	//   {s3BucketName}/log/{clusterName}_{clusterID}/{sessionId}/node_events/...
	clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, "default") // namespace.Name)
	sessionPrefix := fmt.Sprintf("log/%s/%s/", clusterNameID, sessionID)
	g.Eventually(func() error {
		// Check for logs/ directory.
		logsPrefix := sessionPrefix + "logs/"
		logsObjects, err := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
			Bucket:  aws.String(s3BucketName),
			Prefix:  aws.String(logsPrefix),
			MaxKeys: aws.Int64(1), // Efficiently check if any objects exist
		})
		if err != nil {
			return fmt.Errorf("failed to list logs in %s: %w", logsPrefix, err)
		}
		if aws.Int64Value(logsObjects.KeyCount) == 0 {
			return fmt.Errorf("logs directory %s is empty", logsPrefix)
		}

		// Check for node_events/ directory.
		nodeEventsPrefix := sessionPrefix + "node_events/"
		nodeEventsObjects, err := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
			Bucket:  aws.String(s3BucketName),
			Prefix:  aws.String(nodeEventsPrefix),
			MaxKeys: aws.Int64(1), // Efficiently check if any objects exist
		})
		if err != nil {
			return fmt.Errorf("failed to list node_events in %s: %w", nodeEventsPrefix, err)
		}
		if aws.Int64Value(nodeEventsObjects.KeyCount) == 0 {
			return fmt.Errorf("node_events directory %s is empty", nodeEventsPrefix)
		}

		LogWithTimestamp(test.T(), "Verified session %s has both logs/ (%d objects) and node_events/ (%d objects)",
			sessionPrefix, aws.Int64Value(logsObjects.KeyCount), aws.Int64Value(nodeEventsObjects.KeyCount))
		return nil
	}, TestTimeoutMedium).Should(Succeed(), "Logs and node_events should be uploaded to S3")

	// TODO(jwj): Refactor cleanup tasks
	// Delete S3 bucket to ensure test isolation.
	deleteS3Bucket(test, g, s3Client)
}

// testPrevLogsRuntimeUpload verifies logs under /tmp/ray/prev-logs are uploaded during runtime.
// This makes sure WatchPrevLogsLoops processes logs as they appear.
//
// NOTE: For now, logs under /tmp/ray/session_latest are moved to /tmp/ray/prev-logs explicitly.
// The reason is that this data movement serves as the startup command of the Ray container.
// To trigger the filesystem watcher in WatchPrevLogsLoops during runtime, we have to move logs manually.
func testPrevLogsRuntimeUpload(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) {
	// TODO(jwj): Refactor preparatory tasks, including applying a Ray cluster, checking the collector
	// sidecar container exists in the head pod, and checking an empty S3 bucket exists.
	rayCluster := applyRayCluster(test, g, namespace)

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

	// Submit a Ray job to the existing cluster.
	applyRayJobToCluster(test, g, namespace, rayCluster)

	// Explicitly move logs from session_lastest to prev-logs.
	// NOTE: The command in raycluster.yaml only runs at container startup, not when sessions change.
	LogWithTimestamp(test.T(), "Moving logs from session_latest to prev-logs")
	moveLogsCmd := `if [ -d "/tmp/ray/session_latest" ] && [ -f "/tmp/ray/raylet_node_id" ]; then
session_id=$(basename $(readlink /tmp/ray/session_latest))
node_id=$(cat /tmp/ray/raylet_node_id)
dest="/tmp/ray/prev-logs/${session_id}/${node_id}"
echo "Moving logs from session_latest to ${dest}"
mkdir -p "${dest}"
if [ -d "/tmp/ray/session_latest/logs" ]; then
mv /tmp/ray/session_latest/logs "${dest}/logs"
echo "Successfully moved logs to ${dest}/logs"
else
echo "No logs directory found in session_latest"
fi
else
echo "session_latest or raylet_node_id not found"
fi`
	g.Eventually(func() error {
		headPod, err = GetHeadPod(test, rayCluster)
		if err != nil {
			return err
		}
		return execKubectlExec(test, namespace, headPod.Name, []string{"sh", "-c", moveLogsCmd})
	}, TestTimeoutMedium).Should(Succeed(), "Failed to move logs to prev-logs directory")

	LogWithTimestamp(test.T(), "Waiting for collector to detect and process prev-logs")
	time.Sleep(3 * time.Second)

	// clusterNameID := fmt.Sprintf("%s_%s", rayCluster.Name, namespace.Name)
	g.Eventually(func() int64 {
		objects, _ := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
			Bucket: aws.String(s3BucketName),
			// Prefix: aws.String(fmt.Sprintf("log/%s/", clusterNameID)),
		})
		return aws.Int64Value(objects.KeyCount)
	}, TestTimeoutMedium).Should(BeNumerically(">", 0))
	LogWithTimestamp(test.T(), "Verified logs uploaded successfully during runtime")

	err = test.Client().Ray().RayV1().
		RayClusters(rayCluster.Namespace).
		Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(func() error {
		_, err := GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		return err
	}, TestTimeoutMedium).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

	deleteS3Bucket(test, g, s3Client)
}

// ensureS3Client creates an S3 client and ensures API endpoint accessibility.
func ensureS3Client(test Test, g *WithT) (*s3.S3, error) {
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

	return s3Client, err
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

// Deploy a Ray cluster with the collector sidecar into the test namespace.
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

func applyRayJobToCluster(test Test, g *WithT, namespace *corev1.Namespace, rayCluster *rayv1.RayCluster) *rayv1.RayJob {
	jobScript := "import ray; ray.init(); print(ray.cluster_resources())"
	rayJobAC := rayv1ac.RayJob("ray-job", namespace.Name).
		WithSpec(rayv1ac.RayJobSpec().
			WithClusterSelector(map[string]string{utils.RayClusterLabelKey: rayCluster.Name}).
			WithEntrypoint(fmt.Sprintf("python -c %q", jobScript)).
			WithShutdownAfterJobFinishes(false). // Keep cluster running.
			WithSubmitterPodTemplate(JobSubmitterPodTemplateApplyConfiguration()))

	rayJob, err := test.Client().Ray().RayV1().RayJobs(namespace.Name).Apply(test.Ctx(), rayJobAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayJob %s/%s successfully", rayJob.Namespace, rayJob.Name)

	LogWithTimestamp(test.T(), "Waiting for RayJob %s/%s to complete successfully", rayJob.Namespace, rayJob.Name)
	g.Eventually(RayJob(test, rayJob.Namespace, rayJob.Name), TestTimeoutMedium).
		Should(WithTransform(RayJobStatus, Equal(rayv1.JobStatusSucceeded)))

	return rayJob
}

// getSessionIDFromHeadPod retrieves the sessionID from the Ray head pod, by reading the symlink
// /tmp/ray/session_latest and getting its basename.
func getSessionIDFromHeadPod(test Test, namespace *corev1.Namespace, rayCluster *rayv1.RayCluster) (string, error) {
	headPod, err := GetHeadPod(test, rayCluster)
	if err != nil {
		return "", fmt.Errorf("failed to get head pod: %w", err)
	}

	getSessionIDCmd := `if [ -L "/tmp/ray/session_latest" ]; then
  session_path=$(readlink /tmp/ray/session_latest)
  basename "$session_path"
else
  echo "session_latest is not a symlink"
  exit 1
fi`
	cmd := exec.CommandContext(
		test.Ctx(),
		"kubectl",
		"exec",
		"-n", namespace.Name,
		"-c", "ray-head",
		headPod.Name,
		"--",
		"sh", "-c", getSessionIDCmd,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get sessionID: %w, output: %s", err, string(output))
	}

	// Parse output to extract only the sessionID.
	outputStr := strings.TrimSpace(string(output))
	lines := strings.Split(outputStr, "\n")
	var sessionID string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "session_") {
			sessionID = line
			break
		}
	}
	if sessionID == "" {
		return "", fmt.Errorf("sessionID not found in output, output: %s", outputStr)
	}

	return sessionID, nil
}

func execKubectlExec(test Test, namespace *corev1.Namespace, podName string, command []string) error {
	args := []string{
		"exec",
		"-n", namespace.Name,
		podName,
		"--",
	}
	args = append(args, command...)

	cmd := exec.CommandContext(test.Ctx(), "kubectl", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("kubectl exec failed: %w, output: %s", err, string(output))
	}
	return nil
}
