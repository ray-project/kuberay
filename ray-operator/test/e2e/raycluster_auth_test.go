package e2e

import (
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// NewRayClusterSpecWithAuth creates a new RayClusterSpec with the specified AuthMode.
func NewRayClusterSpecWithAuth(authMode rayv1.AuthMode) *rayv1ac.RayClusterSpecApplyConfiguration {
	return NewRayClusterSpec().
		WithAuthOptions(rayv1ac.AuthOptions().WithMode(authMode))
}

func TestRayClusterAuthOptions(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	test.T().Run("RayCluster with token authentication enabled", func(t *testing.T) {
		t.Parallel()

		rayClusterAC := rayv1ac.RayCluster("raycluster-auth-token", namespace.Name).
			WithSpec(NewRayClusterSpecWithAuth(rayv1.AuthModeToken).WithRayVersion("2.52"))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully with AuthModeToken", rayCluster.Namespace, rayCluster.Name)

		LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

		headPod, err := GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(headPod).NotTo(BeNil())

		// Verify Ray container has auth token env vars
		VerifyContainerAuthTokenEnvVars(test, rayCluster, &headPod.Spec.Containers[utils.RayContainerIndex])

		// Verify worker pods have auth token env vars
		workerPods, err := GetWorkerPods(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(workerPods).ToNot(BeEmpty())
		for _, workerPod := range workerPods {
			VerifyContainerAuthTokenEnvVars(test, rayCluster, &workerPod.Spec.Containers[utils.RayContainerIndex])
		}

		// Get auth token for job submission tests
		authToken := getAuthTokenFromPod(test, rayCluster, headPod)
		g.Expect(authToken).NotTo(BeEmpty(), "Auth token should be present")

		// Test job submission with auth token using kubectl exec + curl
		test.T().Run("Submit job with auth token should succeed", func(_ *testing.T) {
			LogWithTimestamp(test.T(), "Testing job submission WITH auth token")

			submissionId := fmt.Sprintf("test-job-with-auth-%d", time.Now().Unix())
			jobPayload := fmt.Sprintf(`{"entrypoint":"python -c \\\"import ray; ray.init(); print('Job with auth succeeded')\\\"","submission_id":"%s"}`, submissionId)

			// Submit job via curl with auth header
			// Use -s (silent) to suppress progress meter, -S to show errors
			curlCmd := []string{
				"curl", "-sS", "-X", "POST",
				"-H", "Content-Type: application/json",
				"-H", fmt.Sprintf("x-ray-authorization: Bearer %s", authToken),
				"-d", jobPayload,
				"http://127.0.0.1:8265/api/jobs/",
			}

			stdout, stderr := ExecPodCmd(test, headPod, headPod.Spec.Containers[utils.RayContainerIndex].Name, curlCmd)
			g.Expect(stderr.String()).To(BeEmpty(), "curl stderr should be empty")

			LogWithTimestamp(test.T(), "Job submission response: %s", stdout.String())
			g.Expect(stdout.String()).To(ContainSubstring(submissionId), "Response should contain submission ID")

			// Verify job status - it should be queryable
			g.Eventually(func(g Gomega) {
				curlGetCmd := []string{
					"curl", "-sS", "-X", "GET",
					"-H", fmt.Sprintf("x-ray-authorization: Bearer %s", authToken),
					fmt.Sprintf("http://127.0.0.1:8265/api/jobs/%s", submissionId),
				}
				stdout, _ := ExecPodCmd(test, headPod, headPod.Spec.Containers[utils.RayContainerIndex].Name, curlGetCmd)
				g.Expect(stdout.String()).To(ContainSubstring(submissionId))
			}, TestTimeoutShort).Should(Succeed())

			LogWithTimestamp(test.T(), "Successfully submitted and verified job with auth token")
		})

		test.T().Run("Submit job without auth token should fail", func(_ *testing.T) {
			LogWithTimestamp(test.T(), "Testing job submission WITHOUT auth token (should fail)")

			submissionId := fmt.Sprintf("test-job-no-auth-%d", time.Now().Unix())
			jobPayload := fmt.Sprintf(`{"entrypoint":"python -c \\\"print('Should not run')\\\"","submission_id":"%s"}`, submissionId)

			// Submit job via curl WITHOUT auth header
			// Use -sS for silent mode with errors, -w to write out HTTP status code
			curlCmd := []string{
				"curl", "-sS", "-X", "POST", "-w", "\\nHTTP_STATUS:%{http_code}",
				"-H", "Content-Type: application/json",
				"-d", jobPayload,
				"http://127.0.0.1:8265/api/jobs/",
			}

			stdout, _ := ExecPodCmd(test, headPod, headPod.Spec.Containers[utils.RayContainerIndex].Name, curlCmd)
			response := stdout.String()

			LogWithTimestamp(test.T(), "Job submission response without auth: %s", response)

			// Verify response indicates unauthorized (401 or similar error)
			g.Expect(response).To(Or(
				ContainSubstring("HTTP_STATUS:401"),
				ContainSubstring("Unauthorized"),
			), "Response should indicate authentication failure")

			LogWithTimestamp(test.T(), "Job submission correctly rejected without auth token")
		})
	})
}

// getAuthTokenFromPod extracts the auth token from the pod's environment variables.
// It reads the token from the secret referenced by the RAY_AUTH_TOKEN environment variable.
func getAuthTokenFromPod(test Test, rayCluster *rayv1.RayCluster, pod *corev1.Pod) string {
	test.T().Helper()
	g := NewWithT(test.T())

	for _, envVar := range pod.Spec.Containers[utils.RayContainerIndex].Env {
		if envVar.Name == utils.RAY_AUTH_TOKEN_ENV_VAR {
			if envVar.ValueFrom != nil && envVar.ValueFrom.SecretKeyRef != nil {
				secret, err := test.Client().Core().CoreV1().Secrets(rayCluster.Namespace).
					Get(test.Ctx(), envVar.ValueFrom.SecretKeyRef.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				return string(secret.Data[envVar.ValueFrom.SecretKeyRef.Key])
			}
		}
	}
	return ""
}
