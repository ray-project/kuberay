package support

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

const (
	HistoryServerManifestPath = "../../config/historyserver.yaml"
	HistoryServerPort         = 30080
)

// HistoryServerEndpoints defines endpoints that should be proxied to Ray Dashboard
// Ref: https://github.com/ray-project/kuberay/blob/8fc4e2a0e644db392534927b7c03d15e3ab7bdbc/historyserver/pkg/historyserver/router.go#L66-L128
//
// Excluded endpoints that require parameters:
//   - /nodes/{node_id}
//   - /api/jobs/{job_id}
//   - /api/v0/logs (requires node_id)
//   - /logical/actors/{actor_id}
//
// Excluded endpoints that are not yet implemented:
//   - /events
//   - /api/cluster_status
//   - /api/grafana_health
//   - /api/prometheus_health
//   - /api/data/datasets/{job_id}
//   - /api/jobs
//   - /api/serve/applications
//   - /api/v0/placement_groups
//   - /api/v0/logs/file
var HistoryServerEndpoints = []string{
	"/nodes?view=summary",
	"/api/v0/tasks",
	"/api/v0/tasks/summarize",
	"/logical/actors",
}

// ApplyHistoryServer deploys the HistoryServer and RBAC resources.
func ApplyHistoryServer(test Test, g *WithT, namespace *corev1.Namespace) {
	// Read RBAC resources from YAML and modify namespace fields.
	sa, clusterRole, clusterRoleBinding := DeserializeRBACFromYAML(test, ServiceAccountManifestPath)
	sa.Namespace = namespace.Name
	clusterRoleBinding.Name = fmt.Sprintf("historyserver-%s", namespace.Name)
	clusterRoleBinding.Subjects[0].Namespace = namespace.Name

	// Create RBAC resources
	_, err := test.Client().Core().CoreV1().ServiceAccounts(namespace.Name).Create(test.Ctx(), sa, metav1.CreateOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	// ClusterRole is shared across tests, ignore if already exists.
	_, err = test.Client().Core().RbacV1().ClusterRoles().Create(test.Ctx(), clusterRole, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		g.Expect(err).NotTo(HaveOccurred())
	}
	_, err = test.Client().Core().RbacV1().ClusterRoleBindings().Create(test.Ctx(), clusterRoleBinding, metav1.CreateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	// ClusterRoleBinding is cluster-scoped and won't be deleted when the namespace is cleaned up.
	// Register cleanup to prevent accumulation across test runs.
	test.T().Cleanup(func() {
		_ = test.Client().Core().RbacV1().ClusterRoleBindings().Delete(
			context.Background(), clusterRoleBinding.Name, metav1.DeleteOptions{})
	})

	KubectlApplyYAML(test, HistoryServerManifestPath, namespace.Name)

	LogWithTimestamp(test.T(), "Waiting for HistoryServer to be ready")
	g.Eventually(func(gg Gomega) {
		pods, err := test.Client().Core().CoreV1().Pods(namespace.Name).List(
			test.Ctx(), metav1.ListOptions{
				LabelSelector: "app=historyserver",
			},
		)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(pods.Items).NotTo(BeEmpty())
		gg.Expect(AllPodsRunningAndReady(pods.Items)).To(BeTrue())
	}, TestTimeoutMedium).Should(Succeed())
	LogWithTimestamp(test.T(), "HistoryServer is ready")
}

// GetHistoryServerURL sets up port-forwarding to the history server and waits for it to be ready.
func GetHistoryServerURL(test Test, g *WithT, namespace *corev1.Namespace) string {
	PortForwardService(test, g, namespace.Name, "historyserver", HistoryServerPort)

	// Wait for port-forward to be ready
	historyServerURL := fmt.Sprintf("http://localhost:%d", HistoryServerPort)
	g.Eventually(func() error {
		resp, err := http.Get(historyServerURL + "/readz")
		if err != nil {
			return err
		}
		defer func() {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
		}
		return nil
	}, TestTimeoutMedium).Should(Succeed(), "HistoryServer should be ready")
	LogWithTimestamp(test.T(), "Port-forwarded HistoryServer API port to %s successfully", historyServerURL)

	return historyServerURL
}

// PrepareTestEnv prepares test environment for each test case, including applying a Ray cluster,
// checking the collector sidecar container exists in the head pod and an empty S3 bucket exists.
func PrepareTestEnv(test Test, g *WithT, namespace *corev1.Namespace, s3Client *s3.S3) *rayv1.RayCluster {
	// Deploy a Ray cluster with the collector.
	rayCluster := ApplyRayClusterWithCollector(test, g, namespace)

	// Check the collector sidecar exists in the head pod.
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod.Spec.Containers).To(ContainElement(
		WithTransform(func(c corev1.Container) string { return c.Name }, Equal("collector")),
	))

	// Check an empty S3 bucket is automatically created.
	_, err = s3Client.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(S3BucketName),
	})
	g.Expect(err).NotTo(HaveOccurred())

	return rayCluster
}
