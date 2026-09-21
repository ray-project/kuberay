package support

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

const (
	// MinIO configuration
	MinioNamespace    = "minio-dev"
	MinioManifestPath = "../../config/minio.yaml"
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

	req := c.test.Client().Core().CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command:   cmd,
			Container: MinioContainerName,
			Stdout:    true,
			Stderr:    true,
		}, clientgoscheme.ParameterCodec)

	cfg := c.test.Client().Config()
	executor, err := remotecommand.NewSPDYExecutor(&cfg, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create executor for %q: %w", strings.Join(cmd, " "), err)
	}

	var stdout, stderr bytes.Buffer
	if err := executor.StreamWithContext(c.test.Ctx(), remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		return "", fmt.Errorf("%q failed: %w (stderr: %s)", strings.Join(cmd, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

// StatObject returns nil if the object exists. An empty key checks the bucket itself.
func (c *S3TestClient) StatObject(bucket, key string) error {
	// When the key itself is absent, mc falls back to a LIST and succeeds if the key is a
	// prefix of anything. --no-list disables that fallback.
	_, err := c.execMC("stat", "-q", "--no-list", path.Join(minioMCAlias, bucket, key))
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

// ListObjectKeys returns the full keys of all objects under bucket/prefix, recursively.
func (c *S3TestClient) ListObjectKeys(bucket, prefix string) ([]string, error) {
	// Trailing slash makes mc list the prefix's contents rather than the entry itself.
	out, err := c.execMC("ls", "--json", "--recursive", path.Join(minioMCAlias, bucket, prefix)+"/")
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var entry mcListEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("failed to parse mc ls output line %q: %w", line, err)
		}
		if entry.Type == "file" {
			keys = append(keys, path.Join(prefix, entry.Key))
		}
	}
	return keys, nil
}

// DeleteBucket removes the bucket and everything in it. A missing bucket is not an error.
func (c *S3TestClient) DeleteBucket(bucket string) error {
	_, err := c.execMC("rb", "--force", path.Join(minioMCAlias, bucket))
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

// EnsureS3Client deploys MinIO and returns a client once the S3 API responds.
func EnsureS3Client(t *testing.T) *S3TestClient {
	test := With(t)
	g := NewWithT(t)
	ApplyMinIO(test, g)

	s3Client := NewS3TestClient(test)
	g.Eventually(func() error {
		_, err := s3Client.execMC("ls", minioMCAlias) // Dummy operation to ensure accessibility
		return err
	}, TestTimeoutMedium).Should(Succeed(), "MinIO API endpoint should be ready")

	return s3Client
}

// DeleteS3Bucket deletes the S3 bucket and everything in it. Cleanup failures
// are logged rather than failing the test.
func DeleteS3Bucket(test Test, _ *WithT, s3Client *S3TestClient) {
	LogWithTimestamp(test.T(), "Deleting S3 bucket %s", S3BucketName)
	if err := s3Client.DeleteBucket(S3BucketName); err != nil {
		test.T().Logf("Failed to delete bucket %s: %v", S3BucketName, err)
	}
}
