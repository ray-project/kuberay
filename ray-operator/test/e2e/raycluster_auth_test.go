package e2e

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

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
		verifyAuthTokenEnvVars(t, rayCluster, *headPod)

		workerPods, err := GetWorkerPods(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(workerPods).ToNot(BeEmpty())
		for _, workerPod := range workerPods {
			verifyAuthTokenEnvVars(t, rayCluster, workerPod)
		}

		// TODO(andrewsykim): add job submission test with and without token once a Ray version with token support is released.
	})
}

func verifyAuthTokenEnvVars(t *testing.T, rayCluster *rayv1.RayCluster, pod corev1.Pod) {
	g := NewWithT(t)

	var rayAuthModeEnvVar *corev1.EnvVar
	for _, envVar := range pod.Spec.Containers[0].Env {
		if envVar.Name == utils.RAY_AUTH_MODE_ENV_VAR {
			rayAuthModeEnvVar = &envVar
			break
		}
	}
	g.Expect(rayAuthModeEnvVar).NotTo(BeNil(), "RAY_AUTH_MODE environment variable should be set")
	g.Expect(rayAuthModeEnvVar.Value).To(Equal(string(rayv1.AuthModeToken)), "RAY_AUTH_MODE should be %s", rayv1.AuthModeToken)

	var rayAuthTokenEnvVar *corev1.EnvVar
	for _, envVar := range pod.Spec.Containers[0].Env {
		if envVar.Name == utils.RAY_AUTH_TOKEN_ENV_VAR {
			rayAuthTokenEnvVar = &envVar
			break
		}
	}
	g.Expect(rayAuthTokenEnvVar).NotTo(BeNil(), "RAY_AUTH_TOKEN environment variable should be set for AuthModeToken")
	g.Expect(rayAuthTokenEnvVar.ValueFrom).NotTo(BeNil(), "RAY_AUTH_TOKEN should be populated from a secret")
	g.Expect(rayAuthTokenEnvVar.ValueFrom.SecretKeyRef).NotTo(BeNil(), "RAY_AUTH_TOKEN should be populated from a secret key ref")
	g.Expect(rayAuthTokenEnvVar.ValueFrom.SecretKeyRef.Name).To(ContainSubstring(rayCluster.Name), "Secret name should contain RayCluster name")
	g.Expect(rayAuthTokenEnvVar.ValueFrom.SecretKeyRef.Key).To(Equal(utils.RAY_AUTH_TOKEN_SECRET_KEY), "Secret key should be %s", utils.RAY_AUTH_TOKEN_SECRET_KEY)
}
