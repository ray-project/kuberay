package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/dashboardclient"
	utiltypes "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/types"
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

		// Get auth token and dashboard URL for job submission tests
		authToken := getAuthTokenFromPod(test, rayCluster, headPod)
		g.Expect(authToken).NotTo(BeEmpty(), "Auth token should be present")

		dashboardURL := fmt.Sprintf("http://%s-head-svc.%s.svc.cluster.local:8265",
			rayCluster.Name, rayCluster.Namespace)

		// Test job submission with auth token
		test.T().Run("Submit job with auth token should succeed", func(_ *testing.T) {
			LogWithTimestamp(test.T(), "Testing job submission WITH auth token")
			clientWithAuth := &dashboardclient.RayDashboardClient{}
			httpClient := &http.Client{Timeout: 30 * time.Second}
			clientWithAuth.InitClient(httpClient, dashboardURL, authToken)

			jobRequest := &utiltypes.RayJobRequest{
				Entrypoint:   "python -c \"import ray; ray.init(); print('Job with auth succeeded')\"",
				SubmissionId: fmt.Sprintf("test-job-with-auth-%d", time.Now().Unix()),
				RuntimeEnv:   map[string]interface{}{},
			}

			jobId, err := clientWithAuth.SubmitJobReq(test.Ctx(), jobRequest)
			g.Expect(err).NotTo(HaveOccurred(), "Job submission with token should succeed")
			g.Expect(jobId).NotTo(BeEmpty())
			LogWithTimestamp(test.T(), "Successfully submitted job with auth: %s", jobId)

			// Verify job was created and can be queried
			g.Eventually(func(g Gomega) {
				jobInfo, err := clientWithAuth.GetJobInfo(test.Ctx(), jobId)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(jobInfo).NotTo(BeNil())
				g.Expect(jobInfo.JobId).To(Equal(jobId))
			}, TestTimeoutShort).Should(Succeed())

			// Wait for job to reach terminal state
			LogWithTimestamp(test.T(), "Waiting for job %s to reach terminal state", jobId)
			g.Eventually(func(g Gomega) {
				jobInfo, err := clientWithAuth.GetJobInfo(test.Ctx(), jobId)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(rayv1.IsJobTerminal(jobInfo.JobStatus)).To(BeTrue())
			}, TestTimeoutMedium).Should(Succeed())

			// Cleanup: Delete the job
			err = clientWithAuth.DeleteJob(test.Ctx(), jobId)
			g.Expect(err).NotTo(HaveOccurred())
			LogWithTimestamp(test.T(), "Successfully deleted job %s", jobId)
		})

		test.T().Run("Submit job without auth token should fail with 401", func(_ *testing.T) {
			LogWithTimestamp(test.T(), "Testing job submission WITHOUT auth token (should fail with 401)")
			clientWithoutAuth := &dashboardclient.RayDashboardClient{}
			httpClient := &http.Client{Timeout: 30 * time.Second}
			clientWithoutAuth.InitClient(httpClient, dashboardURL, "")

			jobRequestNoAuth := &utiltypes.RayJobRequest{
				Entrypoint:   "python -c \"print('Should not run')\"",
				SubmissionId: fmt.Sprintf("test-job-no-auth-%d", time.Now().Unix()),
				RuntimeEnv:   map[string]interface{}{},
			}

			jobId, err := clientWithoutAuth.SubmitJobReq(test.Ctx(), jobRequestNoAuth)

			// Expect error due to missing authentication
			g.Expect(err).To(HaveOccurred(), "Job submission without token should fail")
			g.Expect(jobId).To(BeEmpty())

			// Verify error message indicates Unauthorized
			g.Expect(err.Error()).To(ContainSubstring("Unauthorized"), "Error should indicate Unauthorized")

			LogWithTimestamp(test.T(), "Job submission correctly rejected as Unauthorized: %v", err)
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
