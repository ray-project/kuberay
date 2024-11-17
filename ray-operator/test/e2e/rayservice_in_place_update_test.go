package e2e

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/test/sampleyaml"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayServiceInPlaceUpdate(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-sample"
	test.StreamKubeRayOperatorLogs()

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(rayServiceSampleYamlApplyConfiguration())

	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	test.T().Logf("Waiting for RayService %s/%s to running", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(RayServiceStatus, Equal(rayv1.Running)))

	// Get the latest RayService
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	// Wait for RayCluster name to be populated
	g.Eventually(RayService(test, namespace.Name, rayServiceName), TestTimeoutShort).Should(
		WithTransform(UnderlyingRayClusterName, Not(BeEmpty())),
	)
	rayClusterName := UnderlyingRayClusterName(rayService)

	test.T().Logf("Waiting for RayCluster %s/%s to be ready", namespace.Name, rayClusterName)
	g.Eventually(RayCluster(test, namespace.Name, rayClusterName), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	rayCluster, err := GetRayCluster(test, namespace.Name, rayClusterName)
	g.Expect(err).NotTo(HaveOccurred())

	// Check if the head pod is ready
	g.Eventually(HeadPod(test, rayCluster), TestTimeoutShort).Should(WithTransform(sampleyaml.IsPodRunningAndReady, BeTrue()))

	// Check if all worker pods are ready
	g.Eventually(WorkerPods(test, rayCluster), TestTimeoutShort).Should(WithTransform(sampleyaml.AllPodsRunningAndReady, BeTrue()))

	// Create curl pod
	curlPodName := "curl-pod"
	curlContainerName := "curl-container"

	curlPod, err := CreateCurlPod(test, curlPodName, curlContainerName, namespace.Name)
	g.Expect(err).NotTo(HaveOccurred())
	// Wait until curl pod is created
	g.Eventually(func(g Gomega) *corev1.Pod {
		updatedCurlPod, err := test.Client().Core().CoreV1().Pods(curlPod.Namespace).Get(test.Ctx(), curlPod.Name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		return updatedCurlPod
	}, TestTimeoutShort).Should(WithTransform(sampleyaml.IsPodRunningAndReady, BeTrue()))

	// test the default curl result
	// curl /fruit
	stdout, _ := curlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))
	// curl /calc
	stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
	g.Expect(stdout.String()).To(Equal("15 pizzas please!"))

	// In place update
	// Parse ServeConfigV2 and replace the string in the simplest way to update it.
	rayService, err = GetRayService(test, namespace.Name, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())

	serveConfig := rayService.Spec.ServeConfigV2
	serveConfig = strings.Replace(serveConfig, "price: 3", "price: 4", -1)
	serveConfig = strings.Replace(serveConfig, "factor: 5", "factor: 3", -1)

	rayService.Spec.ServeConfigV2 = serveConfig
	rayService, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(
		test.Ctx(),
		rayService,
		metav1.UpdateOptions{},
	)
	g.Expect(err).NotTo(HaveOccurred())

	g.Eventually(func(g Gomega) {
		// Get latest ray service
		newRs, err := GetRayService(test, rayService.Namespace, rayService.Name)
		g.Expect(err).NotTo(HaveOccurred())
		// Check Ray service status
		rsStatus := RayServiceStatus(newRs)
		g.Expect(rsStatus).To(Equal(rayv1.Running))
		g.Expect(newRs.Status.ObservedGeneration).To(Equal(newRs.Generation))
	}, TestTimeoutShort).Should(Succeed())

	// Test the new price and factor
	// curl /fruit
	stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("8"))
	// curl /calc
	stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
	g.Expect(stdout.String()).To(Equal("9 pizzas please!"))
}
