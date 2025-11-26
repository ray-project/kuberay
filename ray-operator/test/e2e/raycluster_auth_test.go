package e2e

import (
	"testing"

	. "github.com/onsi/gomega"

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

		// TODO(andrewsykim): add job submission test with and without token once a Ray version with token support is released.
	})
}
