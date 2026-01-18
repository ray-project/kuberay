package support

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	. "github.com/onsi/gomega"
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
)

// ApplyMinIO deploys minio once per test namespace, making sure it's idempotent.
// TODO(jwj): Check idempotency (for now, only manual check).
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

	// Port-forward the MinIO API port.
	ctx, cancel := context.WithCancel(context.Background())
	test.T().Cleanup(cancel)
	kubectlCmd := exec.CommandContext(
		ctx,
		"kubectl",
		"-n", MinioNamespace,
		"port-forward",
		"svc/minio-service",
		fmt.Sprintf("%d:%d", MinioAPIPort, MinioAPIPort),
	)
	err := kubectlCmd.Start()
	g.Expect(err).NotTo(HaveOccurred())

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
