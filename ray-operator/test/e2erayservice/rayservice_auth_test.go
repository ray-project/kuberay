package e2erayservice

import (
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayServiceAuthToken(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Create the RayService for testing with auth token using programmatic configuration
	rayServiceName := "rayservice-auth"
	rayServiceSpec := RayServiceSampleYamlApplyConfiguration()
	rayServiceSpec.RayClusterSpec.WithAuthOptions(rayv1ac.AuthOptions().WithMode(rayv1.AuthModeToken))

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(rayServiceSpec)

	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())
	LogWithTimestamp(test.T(), "Created RayService %s/%s successfully with AuthModeToken", rayService.Namespace, rayService.Name)

	// Wait for RayService to be ready
	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	// Get the RayService
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "RayService %s/%s is ready", rayService.Namespace, rayService.Name)

	// Get the underlying RayCluster of the RayService
	rayClusterName := rayService.Status.ActiveServiceStatus.RayClusterName
	g.Expect(rayClusterName).NotTo(BeEmpty(), "RayCluster name should be populated")
	LogWithTimestamp(test.T(), "RayService %s/%s has active RayCluster %s", rayService.Namespace, rayService.Name, rayClusterName)

	rayCluster, err := GetRayCluster(test, namespace.Name, rayClusterName)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the head pod has auth token environment variables
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod).NotTo(BeNil())
	LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

	// Verify Ray container has auth token env vars
	VerifyContainerAuthTokenEnvVars(test, rayCluster, &headPod.Spec.Containers[utils.RayContainerIndex])
	LogWithTimestamp(test.T(), "Verified auth token env vars in head pod Ray container")

	// Clean up the RayService
	err = test.Client().Ray().RayV1().RayServices(namespace.Name).Delete(test.Ctx(), rayService.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Deleted RayService %s/%s successfully", rayService.Namespace, rayService.Name)
}
