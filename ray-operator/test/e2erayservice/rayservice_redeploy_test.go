package e2e

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/test/sampleyaml"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRedeployRayServe(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-sample"

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(rayServiceSampleYamlApplyConfiguration())

	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	test.T().Logf("Waiting for RayService %s/%s to running", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	// Get the latest RayService
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	curlPodName := "curl-pod"
	curlContainerName := "curl-container"

	test.T().Logf("Creating curl pod %s/%s", namespace.Name, curlPodName)

	curlPod, err := CreateCurlPod(test, curlPodName, curlContainerName, namespace.Name)
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(func(g Gomega) *corev1.Pod {
		updatedCurlPod, err := test.Client().Core().CoreV1().Pods(curlPod.Namespace).Get(test.Ctx(), curlPod.Name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		return updatedCurlPod
	}, TestTimeoutShort).Should(WithTransform(sampleyaml.IsPodRunningAndReady, BeTrue()))
	test.T().Logf("Curl pod %s/%s is running and ready", namespace.Name, curlPodName)

	test.T().Logf("Sending requests to the RayService to make sure it is ready to serve requests")
	stdout, _ := curlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))
	stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
	g.Expect(stdout.String()).To(Equal("15 pizzas please!"))

	test.T().Logf("Deleting the current Head for recreating a new one")
	rayServiceUnderlyingRayCluster, err := GetRayCluster(test, namespace.Name, rayService.Status.ActiveServiceStatus.RayClusterName)
	g.Expect(err).NotTo(HaveOccurred())
	oldHeadPod, err := GetHeadPod(test, rayServiceUnderlyingRayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	err = test.Client().Core().CoreV1().Pods(namespace.Name).Delete(test.Ctx(), oldHeadPod.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	test.T().Logf("Checking that the K8s serve service eventually has 1 endpoint and the endpoint is not the old head Pod")
	g.Eventually(func(g Gomega) {
		svcName := utils.GenerateServeServiceName(rayService.Name)
		endpoints, err := test.Client().Core().CoreV1().Endpoints(namespace.Name).Get(test.Ctx(), svcName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(endpoints.Subsets).To(HaveLen(1))
		g.Expect(endpoints.Subsets[0].Addresses).To(HaveLen(1))
		g.Expect(endpoints.Subsets[0].Addresses[0].TargetRef.Name).NotTo(Equal(oldHeadPod.Name))
	}, TestTimeoutMedium).Should(Succeed())

	test.T().Logf("Waiting for RayService %s/%s to running", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	test.T().Logf("Sending requests to the RayService to make sure it is ready to serve requests")
	stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))
	stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
	g.Expect(stdout.String()).To(Equal("15 pizzas please!"))
}
