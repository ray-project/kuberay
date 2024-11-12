package e2e

import (
	"path"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/test/sampleyaml"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayServiceInPlaceUpdate(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	fileName := "ray-service.sample.yaml"

	yamlFilePath := path.Join(sampleyaml.GetSampleYAMLDir(test), fileName)
	rayServiceFromYaml := DeserializeRayServiceYAML(test, yamlFilePath)
	KubectlApplyYAML(test, yamlFilePath, namespace.Name)

	rayService, err := GetRayService(test, namespace.Name, rayServiceFromYaml.Name)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	var rayClusterName string
	// Wait for RayCluster name to be populated
	g.Eventually(func(g Gomega) {
		rs, err := GetRayService(test, namespace.Name, rayServiceFromYaml.Name)
		g.Expect(err).NotTo(HaveOccurred())
		if rs.Status.PendingServiceStatus.RayClusterName != "" {
			rayClusterName = rs.Status.PendingServiceStatus.RayClusterName
		} else if rs.Status.ActiveServiceStatus.RayClusterName != "" {
			rayClusterName = rs.Status.ActiveServiceStatus.RayClusterName
		}
		g.Expect(rayClusterName).NotTo(BeEmpty())
	}, TestTimeoutShort).Should(Succeed())

	test.T().Logf("Waiting for RayCluster %s/%s to be ready", namespace.Name, rayClusterName)
	g.Eventually(RayCluster(test, namespace.Name, rayClusterName), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	rayCluster, err := GetRayCluster(test, namespace.Name, rayClusterName)
	g.Expect(err).NotTo(HaveOccurred())

	// Create curl pod
	curlPodName := "curl-pod"
	curlContainerName := "curl-container"

	curlPod, err := CreateCurlPod(test, curlPodName, curlContainerName, namespace.Name)
	g.Expect(err).NotTo(HaveOccurred())

	// Check if the head pod is ready
	g.Eventually(HeadPod(test, rayCluster), TestTimeoutShort).Should(WithTransform(sampleyaml.IsPodRunningAndReady, BeTrue()))

	// Check if all worker pods are ready
	g.Eventually(WorkerPods(test, rayCluster), TestTimeoutShort).Should(WithTransform(sampleyaml.AllPodsRunningAndReady, BeTrue()))

	// Wait until pod is created
	g.Eventually(func(g Gomega) *corev1.Pod {
		updatedPod, err := test.Client().Core().CoreV1().Pods(curlPod.Namespace).Get(test.Ctx(), curlPod.Name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		return updatedPod
	}, TestTimeoutShort).Should(WithTransform(sampleyaml.IsPodRunningAndReady, BeTrue()))

	// test the default curl result
	// curl /fruit
	stdout, _ := curlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))
	// curl /calc
	stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
	g.Expect(stdout.String()).To(Equal("15 pizzas please!"))

	// In place update
	g.Eventually(func(g Gomega) {
		// Parse ServeConfigV2 and replace the string in the simplest way to update it.
		serveConfig := rayService.Spec.ServeConfigV2
		serveConfig = strings.Replace(serveConfig, "price: 3", "price: 4", -1)
		serveConfig = strings.Replace(serveConfig, "factor: 5", "factor: 3", -1)

		rayService.Spec.ServeConfigV2 = serveConfig
		rs, err := UpdateRayServiceConfig(test, namespace.Name, rayServiceFromYaml.Name, serveConfig)
		g.Expect(err).NotTo(HaveOccurred())
		// Check Ray service status
		rsStatus := RayServiceStatus(rs)
		g.Expect(rsStatus).To(Equal(rayv1.Running))
	}, TestTimeoutShort).Should(Succeed())

	// Test the new price and factor
	g.Eventually(func(g Gomega) {
		newRs, err := GetRayService(test, namespace.Name, rayServiceFromYaml.Name)
		g.Expect(err).NotTo(HaveOccurred())
		// curl /fruit
		stdout, _ = curlRayServicePod(test, newRs, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
		g.Expect(stdout.String()).To(Equal("8"))
		// curl /calc
		stdout, _ = curlRayServicePod(test, newRs, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
		g.Expect(stdout.String()).To(Equal("9 pizzas please!"))
	}, TestTimeoutShort).Should(Succeed())
}
