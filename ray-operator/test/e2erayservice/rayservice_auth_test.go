package e2erayservice

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayServiceAuthToken(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Configuration: Define RayService with AuthModeToken
	rayServiceName := "rayservice-auth"
	rayServiceSpec := RayServiceSampleYamlApplyConfiguration()
	rayServiceSpec.RayClusterSpec.WithAuthOptions(rayv1ac.AuthOptions().WithMode(rayv1.AuthModeToken))

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(rayServiceSpec)

	// 1. Apply the RayService
	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())
	LogWithTimestamp(test.T(), "Created RayService %s/%s successfully with AuthModeToken", rayService.Namespace, rayService.Name)

	// 2. CRITICAL: Defer cleanup so it runs even if assertions below fail
	defer func() {
		err := test.Client().Ray().RayV1().RayServices(namespace.Name).Delete(test.Ctx(), rayService.Name, metav1.DeleteOptions{})
		if err != nil {
			LogWithTimestamp(test.T(), "WARNING: Failed to delete RayService %s: %v", rayService.Name, err)
		} else {
			LogWithTimestamp(test.T(), "Deleted RayService %s/%s successfully", rayService.Namespace, rayService.Name)
		}
	}()

	// 3. Wait for RayService to be ready
	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	// 4. Refresh the RayService object
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "RayService %s/%s is ready", rayService.Namespace, rayService.Name)

	// 5. Get the underlying RayCluster
	rayClusterName := rayService.Status.ActiveServiceStatus.RayClusterName
	g.Expect(rayClusterName).NotTo(BeEmpty(), "RayCluster name should be populated in status")
	LogWithTimestamp(test.T(), "RayService %s/%s has active RayCluster %s", rayService.Namespace, rayService.Name, rayClusterName)

	rayCluster, err := GetRayCluster(test, namespace.Name, rayClusterName)
	g.Expect(err).NotTo(HaveOccurred())

	// 6. Get the Head Pod
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod).NotTo(BeNil())
	LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

	// 7. Safer Container Lookup: Find "ray-head" specifically rather than assuming index 0
	var rayContainer *corev1.Container
	for i := range headPod.Spec.Containers {
		// "ray-head" is the standard name for the head container in KubeRay
		if headPod.Spec.Containers[i].Name == "ray-head" {
			rayContainer = &headPod.Spec.Containers[i]
			break
		}
	}
	g.Expect(rayContainer).NotTo(BeNil(), "Could not find 'ray-head' container in Head Pod")

	// 8. Verify Authentication Environment Variables
	VerifyContainerAuthTokenEnvVars(test, rayCluster, rayContainer)
	LogWithTimestamp(test.T(), "Verified auth token env vars in head pod Ray container")
}
