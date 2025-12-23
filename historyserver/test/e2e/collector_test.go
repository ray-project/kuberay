package e2e

import (
	"context"
	"os/exec"
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
	bucketName = "ray-historyserver-log"

	minioManifestPath      = "../../config/minio.yaml"
	rayClusterManifestPath = "../../config/raycluster.yaml"
)

func TestLogCollector(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create an isolated Kubernetes namespace.
	namespace := test.NewTestNamespace()

	t.Run("Happy path: Logs should be uploaded to S3 on deletion", func(t *testing.T) {
		testLogUploadOnDeletion(test, g, namespace)
	})

	// Add other test cases below.
	//  ...
}

func testLogUploadOnDeletion(test Test, g *WithT, namespace *corev1.Namespace) {
	applyMinIO(test, g)

	// Port-forward the minio API port.
	ctx, cancel := context.WithCancel(context.Background())
	test.T().Cleanup(cancel)
	kubectlCmd := exec.CommandContext(
		ctx,
		"kubectl",
		"-n", "minio-dev",
		"port-forward",
		"svc/minio-service",
		"9000:9000",
	)
	err := kubectlCmd.Start()
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Port-forwarded minio API port to localhost:9000 successfully")

	s3Client, err := newMinIOClient("http://localhost:9000")
	g.Expect(err).NotTo(HaveOccurred())

	// Create a bucket.
	_, err = s3Client.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	g.Expect(err).NotTo(HaveOccurred())

	rayCluster := applyRayCluster(test, g, namespace)

	// Check the log collector sidecar exists in the head pod.
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod.Spec.Containers).To(ContainElement(
		WithTransform(func(c corev1.Container) string { return c.Name }, Equal("collector")),
	))

	// Delete the Ray cluster to trigger log uploading on deletion.
	err = test.Client().Ray().RayV1().
		RayClusters(rayCluster.Namespace).
		Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(func() error {
		_, err := GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		return err
	}, TestTimeoutMedium).Should(WithTransform(k8serrors.IsNotFound, BeTrue()))

	// Verify logs are successfully uploaded to minio.
	g.Eventually(func() int64 {
		objects, _ := s3Client.ListObjectsV2(&s3.ListObjectsV2Input{
			Bucket: aws.String(bucketName),
		})
		return aws.Int64Value(objects.KeyCount)
	}, TestTimeoutMedium).Should(BeNumerically(">", 0))
}

// Define some helpers.
// Deploy minio once per test namespace, making sure it's idempotent.
func applyMinIO(test Test, g *WithT) {
	KubectlApplyYAML(test, minioManifestPath, "minio-dev")

	// Wait for minio pods ready.
	g.Eventually(func(gg Gomega) {
		pods, err := test.Client().Core().CoreV1().Pods("minio-dev").List(
			test.Ctx(), metav1.ListOptions{
				LabelSelector: "app=minio",
			},
		)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(pods.Items).NotTo(BeEmpty())
		gg.Expect(AllPodsRunningAndReady(pods.Items)).To(BeTrue())
	}, TestTimeoutMedium).Should(Succeed())
}

// Deploy a Ray cluster with the log collector sidecar into the test namespace.
func applyRayCluster(test Test, g *WithT, namespace *corev1.Namespace) *rayv1.RayCluster {
	rayClusterFromYaml := DeserializeRayClusterYAML(test, rayClusterManifestPath)
	rayClusterFromYaml.Namespace = namespace.Name

	rayCluster, err := test.Client().Ray().RayV1().
		RayClusters(namespace.Name).
		Create(test.Ctx(), rayClusterFromYaml, metav1.CreateOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	return rayCluster
}

func newMinIOClient(endpoint string) (*s3.S3, error) {
	sess, err := session.NewSession(&aws.Config{
		Endpoint:         aws.String(endpoint),
		Region:           aws.String("e2e-test"),
		Credentials:      credentials.NewStaticCredentials("minioadmin", "minioadmin", ""),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
	})
	if err != nil {
		return nil, err
	}
	return s3.New(sess), nil
}
