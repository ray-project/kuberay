package support

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

const (
	// MinIO configuration
	MinioNamespace    = "minio-dev"
	MinioManifestPath = "../../config/minio.yaml"
	MinioUsername     = "minioadmin"
	MinioSecret       = "minioadmin"
	MinioAPIEndpoint  = "http://localhost:9000"
	MinioAPIPort      = 9000
	S3BucketName      = "ray-historyserver"

	// The MinIO server container (config/minio.yaml).
	MinioContainerName = "minio"
	// Alias configured via the MC_HOST_local env var on the MinIO container.
	minioMCAlias = "local"
)

// S3TestClient verifies bucket contents by executing mc commands in the MinIO container.
type S3TestClient struct {
	test Test
}

func NewS3TestClient(test Test) *S3TestClient {
	return &S3TestClient{test: test}
}

// minioPod returns the running MinIO pod.
func (c *S3TestClient) minioPod() (*corev1.Pod, error) {
	pods, err := c.test.Client().Core().CoreV1().Pods(MinioNamespace).List(
		c.test.Ctx(), metav1.ListOptions{LabelSelector: "app=minio"},
	)
	if err != nil {
		return nil, err
	}
	for i := range pods.Items {
		// A terminating pod still reports phase Running, but exec into it fails.
		if pods.Items[i].Status.Phase == corev1.PodRunning && pods.Items[i].DeletionTimestamp == nil {
			return &pods.Items[i], nil
		}
	}
	return nil, fmt.Errorf("no running MinIO pod found in namespace %s", MinioNamespace)
}

// execMC runs an mc command in the MinIO container and returns its stdout.
func (c *S3TestClient) execMC(args ...string) (string, error) {
	pod, err := c.minioPod()
	if err != nil {
		return "", err
	}
	cmd := append([]string{"mc"}, args...)
	stdout, stderr, err := ExecPodCmdWithError(c.test, pod, MinioContainerName, cmd)
	if err != nil {
		return "", fmt.Errorf("%q failed: %w (stderr: %s)", strings.Join(cmd, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

// StatObject returns nil if the object exists. An empty key checks the bucket itself.
func (c *S3TestClient) StatObject(bucket, key string) error {
	_, err := c.execMC("stat", "-q", path.Join(minioMCAlias, bucket, key))
	return err
}

// ReadObject returns the object's content.
func (c *S3TestClient) ReadObject(bucket, key string) ([]byte, error) {
	out, err := c.execMC("cat", path.Join(minioMCAlias, bucket, key))
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// mcListEntry is one line of `mc ls --json` output.
type mcListEntry struct {
	Type string `json:"type"` // "file" or "folder"
	Key  string `json:"key"`  // path relative to the listed prefix
}

// listEntries runs `mc ls --json` under bucket/prefix and parses the entries.
func (c *S3TestClient) listEntries(bucket, prefix string, recursive bool) ([]mcListEntry, error) {
	args := []string{"ls", "--json"}
	if recursive {
		args = append(args, "--recursive")
	}
	// Trailing slash makes mc list the prefix's contents rather than the entry itself.
	args = append(args, path.Join(minioMCAlias, bucket, prefix)+"/")

	out, err := c.execMC(args...)
	if err != nil {
		return nil, err
	}
	var entries []mcListEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var entry mcListEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("failed to parse mc ls output line %q: %w", line, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ListObjectKeys returns the full keys of all objects under bucket/prefix, recursively.
func (c *S3TestClient) ListObjectKeys(bucket, prefix string) ([]string, error) {
	entries, err := c.listEntries(bucket, prefix, true)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, entry := range entries {
		if entry.Type == "file" {
			keys = append(keys, path.Join(prefix, entry.Key))
		}
	}
	return keys, nil
}

// ListDirectories returns the names of the immediate subdirectories under bucket/prefix.
func (c *S3TestClient) ListDirectories(bucket, prefix string) ([]string, error) {
	entries, err := c.listEntries(bucket, prefix, false)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, entry := range entries {
		if entry.Type == "folder" {
			dirs = append(dirs, strings.TrimSuffix(entry.Key, "/"))
		}
	}
	return dirs, nil
}

// DeleteBucket removes the bucket and everything in it. A missing bucket is not an error.
func (c *S3TestClient) DeleteBucket(bucket string) error {
	_, err := c.execMC("rb", "--force", path.Join(minioMCAlias, bucket))
	if err != nil && strings.Contains(err.Error(), "does not exist") {
		return nil
	}
	return err
}

// ApplyMinIO deploys minio once per test namespace, making sure it's idempotent.
func ApplyMinIO(test Test, g *WithT) {
	KubectlApplyYAML(test, MinioManifestPath, MinioNamespace)

	// Wait for MinIO pods ready.
	g.Eventually(func(gg Gomega) {
		pods, err := test.Client().Core().CoreV1().Pods(MinioNamespace).List(
			test.Ctx(), metav1.ListOptions{
				LabelSelector: "app=minio",
			},
		)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(pods.Items).NotTo(BeEmpty())
		gg.Expect(AllPodsRunningAndReady(pods.Items)).To(BeTrue())
	}, TestTimeoutMedium).Should(Succeed())
}

// EnsureS3Client creates an S3 client and ensures API endpoint accessibility.
func EnsureS3Client(t *testing.T) *s3.S3 {
	test := With(t)
	g := NewWithT(t)
	ApplyMinIO(test, g)

	PortForwardService(test, g, MinioNamespace, "minio-service", MinioAPIPort)

	// Check readiness of the MinIO API endpoint.
	g.Eventually(func() error {
		s3Client, err := NewS3Client(MinioAPIEndpoint)
		if err != nil {
			return err
		}
		_, err = s3Client.ListBuckets(&s3.ListBucketsInput{}) // Dummy operation to ensure accessibility
		return err
	}, TestTimeoutMedium).Should(Succeed(), "MinIO API endpoint should be ready")
	LogWithTimestamp(test.T(), "Port-forwarded MinIO API port to localhost:%d successfully", MinioAPIPort)

	s3Client, err := NewS3Client(MinioAPIEndpoint)
	g.Expect(err).NotTo(HaveOccurred())

	return s3Client
}

// NewS3Client creates a new S3 client.
func NewS3Client(endpoint string) (*s3.S3, error) {
	sess, err := session.NewSession(&aws.Config{
		Endpoint:         aws.String(endpoint),
		Region:           aws.String("e2e-test"),
		Credentials:      credentials.NewStaticCredentials(MinioUsername, MinioSecret, ""),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
	})
	if err != nil {
		return nil, err
	}
	return s3.New(sess), nil
}

// DeleteS3Bucket deletes the S3 bucket. Note that objects under the bucket should be deleted first.
func DeleteS3Bucket(test Test, g *WithT, s3Client *s3.S3) {
	LogWithTimestamp(test.T(), "Deleting S3 bucket %s", S3BucketName)

	err := s3Client.ListObjectsV2Pages(&s3.ListObjectsV2Input{
		Bucket: aws.String(S3BucketName),
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
			Bucket: aws.String(S3BucketName),
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
		Bucket: aws.String(S3BucketName),
	})
	if err != nil {
		test.T().Logf("Failed to delete bucket %s: %v (this is OK if bucket doesn't exist)", S3BucketName, err)
	} else {
		LogWithTimestamp(test.T(), "Deleted S3 bucket %s successfully", S3BucketName)
	}
}

// ListS3Directories lists all directories (prefixes) under the given S3 prefix.
// In S3, directories are simulated using prefixes and delimiters.
// For example, given prefix "log/cluster/session/job_events/", this function returns ["AgAAAA==", "AQAAAA=="]
// which are the jobID directories under job_events/.
func ListS3Directories(s3Client *s3.S3, bucket string, prefix string) ([]string, error) {
	result, err := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
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
		fullPrefix := aws.StringValue(commonPrefix.Prefix)
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
