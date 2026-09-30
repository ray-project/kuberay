package support

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"

	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

const (
	// MinIO configuration
	MinioNamespace    = "minio-dev"
	MinioManifestPath = "../../config/minio.yaml"
	S3BucketName      = "ray-historyserver"

	// The S3 client sidecar container (config/minio.yaml), which ships the rc (RustFS CLI) binary.
	S3ClientContainerName = "rc"
	// Alias configured via the RC_HOST_local env var on the client container.
	rcAlias = "local"
	// rc exits with this code when the bucket or object does not exist:
	// https://github.com/rustfs/cli/blob/v0.1.36/crates/cli/src/exit_code.rs.
	rcExitNotFound = 5
	// Scratch bucket that EnsureS3Client writes to before any test runs.
	s3ReadinessBucketName = "e2e-readiness"
)

// S3TestClient verifies bucket contents by executing rc commands in the S3 client container.
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

// execRC runs an rc command in the S3 client container and returns its stdout.
func (c *S3TestClient) execRC(args ...string) (string, error) {
	pod, err := c.minioPod()
	if err != nil {
		return "", err
	}
	cmd := append([]string{"rc"}, args...)

	req := c.test.Client().Core().CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command:   cmd,
			Container: S3ClientContainerName,
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
	// rc stat only accepts an object path, so check a bucket by listing it.
	if key == "" {
		_, err := c.execRC("ls", path.Join(rcAlias, bucket))
		return err
	}
	// Unlike mc, rc stat does not fall back to a LIST when the key is only a prefix.
	_, err := c.execRC("stat", "-q", path.Join(rcAlias, bucket, key))
	return err
}

// ReadObject returns the object's content.
func (c *S3TestClient) ReadObject(bucket, key string) ([]byte, error) {
	out, err := c.execRC("cat", path.Join(rcAlias, bucket, key))
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// rcListOutput is the `rc ls --json` output.
type rcListOutput struct {
	Items []struct {
		Key string `json:"key"` // full object key
	} `json:"items"`
	Truncated bool `json:"truncated"`
}

// ListObjectKeys returns the full keys of all objects under bucket/prefix, recursively.
func (c *S3TestClient) ListObjectKeys(bucket, prefix string) ([]string, error) {
	// Trailing slash makes rc list the prefix's contents rather than every key starting with it.
	out, err := c.execRC("ls", "--json", "--recursive", path.Join(rcAlias, bucket, prefix)+"/")
	if err != nil {
		return nil, err
	}
	var listing rcListOutput
	if err := json.Unmarshal([]byte(out), &listing); err != nil {
		return nil, fmt.Errorf("failed to parse rc ls output %q: %w", out, err)
	}
	if listing.Truncated {
		return nil, fmt.Errorf("rc ls output for %s/%s is truncated", bucket, prefix)
	}
	var keys []string
	for _, item := range listing.Items {
		// Skip zero-byte directory markers such as "logs/".
		if !strings.HasSuffix(item.Key, "/") {
			keys = append(keys, item.Key)
		}
	}
	return keys, nil
}

// DeleteBucket removes the bucket and everything in it. A missing bucket is not an error.
func (c *S3TestClient) DeleteBucket(bucket string) error {
	// rb --force also lists object versions, which Ozone does not implement, so remove the
	// objects first and then the empty bucket.
	_, err := c.execRC("rm", "--recursive", "--force", path.Join(rcAlias, bucket)+"/")
	if err == nil {
		_, err = c.execRC("rb", path.Join(rcAlias, bucket))
	}
	var exitErr utilexec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitStatus() == rcExitNotFound {
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
	LogWithTimestamp(test.T(), "MinIO pods are running and ready")
}

// EnsureS3Client deploys MinIO and returns a client once the S3 API accepts writes.
func EnsureS3Client(t *testing.T) *S3TestClient {
	test := With(t)
	g := NewWithT(t)
	ApplyMinIO(test, g)

	s3Client := NewS3TestClient(test)
	// Some backends answer reads before they accept writes, so upload a probe object to a
	// scratch bucket. The test bucket itself must stay absent until the collector creates it.
	readinessBucket := path.Join(rcAlias, s3ReadinessBucketName)
	g.Eventually(func() error {
		if _, err := s3Client.execRC("mb", "--ignore-existing", readinessBucket); err != nil {
			return err
		}
		_, err := s3Client.execRC("cp", "/etc/hostname", path.Join(readinessBucket, "probe"))
		return err
	}, TestTimeoutMedium).Should(Succeed(), "S3 API should accept writes")
	g.Expect(s3Client.DeleteBucket(s3ReadinessBucketName)).To(Succeed())
	LogWithTimestamp(test.T(), "S3 API accepts writes")

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
