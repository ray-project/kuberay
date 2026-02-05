package support

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
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
	S3Region          = "us-east-1"
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
func EnsureS3Client(t *testing.T) *s3.Client {
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
		_, err = s3Client.ListBuckets(test.Ctx(), &s3.ListBucketsInput{}) // Dummy operation to ensure accessibility
		return err
	}, TestTimeoutMedium).Should(Succeed(), "MinIO API endpoint should be ready")
	LogWithTimestamp(test.T(), "Port-forwarded MinIO API port to localhost:%d successfully", MinioAPIPort)

	s3Client, err := NewS3Client(MinioAPIEndpoint)
	g.Expect(err).NotTo(HaveOccurred())

	return s3Client
}

// NewS3Client creates a new S3 client.
func NewS3Client(endpoint string) (*s3.Client, error) {
	ctx := context.Background()
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(S3Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(MinioUsername, MinioSecret, "")),
	)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String(endpoint)
		// MinIO uses HTTP; tell the SDK to use the endpoint's scheme.
		o.EndpointOptions.DisableHTTPS = strings.HasPrefix(endpoint, "http://")
	}), nil
}

// DeleteS3Bucket deletes the S3 bucket. Note that objects under the bucket should be deleted first.
func DeleteS3Bucket(test Test, g *WithT, s3Client *s3.Client) {
	LogWithTimestamp(test.T(), "Deleting S3 bucket %s", S3BucketName)

	paginator := s3.NewListObjectsV2Paginator(s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(S3BucketName),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(test.Ctx())
		if err != nil {
			test.T().Logf("Failed to list objects in bucket: %v", err)
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
			Bucket: aws.String(S3BucketName),
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
func ListS3Directories(s3Client *s3.Client, bucket string, prefix string) ([]string, error) {
	result, err := s3Client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
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
		fullPrefix := aws.ToString(commonPrefix.Prefix)
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
