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

	namespace := test.NewTestNamespace()

	rayServiceName := "rayservice-auth"
	rayServiceSpec := RayServiceSampleYamlApplyConfiguration()
	rayServiceSpec.RayClusterSpec.WithAuthOptions(rayv1ac.AuthOptions().WithMode(rayv1.AuthModeToken))

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(rayServiceSpec)

	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())
	LogWithTimestamp(test.T(), "Created RayService %s/%s successfully with AuthModeToken", rayService.Namespace, rayService.Name)

	defer func() {
		err := test.Client().Ray().RayV1().RayServices(namespace.Name).Delete(test.Ctx(), rayService.Name, metav1.DeleteOptions{})
		if err != nil {
			LogWithTimestamp(test.T(), "WARNING: Failed to delete RayService %s: %v", rayService.Name, err)
		} else {
			LogWithTimestamp(test.T(), "Deleted RayService %s/%s successfully", rayService.Namespace, rayService.Name)
		}
	}()

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "RayService %s/%s is ready", rayService.Namespace, rayService.Name)

	rayClusterName := rayService.Status.ActiveServiceStatus.RayClusterName
	g.Expect(rayClusterName).NotTo(BeEmpty(), "RayCluster name should be populated in status")
	LogWithTimestamp(test.T(), "RayService %s/%s has active RayCluster %s", rayService.Namespace, rayService.Name, rayClusterName)

	rayCluster, err := GetRayCluster(test, namespace.Name, rayClusterName)
	g.Expect(err).NotTo(HaveOccurred())

	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod).NotTo(BeNil())
	LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

	var rayContainer *corev1.Container
	for i := range headPod.Spec.Containers {
		if headPod.Spec.Containers[i].Name == "ray-head" {
			rayContainer = &headPod.Spec.Containers[i]
			break
		}
	}
	g.Expect(rayContainer).NotTo(BeNil(), "Could not find 'ray-head' container in Head Pod")

	VerifyContainerAuthTokenEnvVars(test, rayCluster, rayContainer)
	LogWithTimestamp(test.T(), "Verified auth token env vars in head pod Ray container")
}
