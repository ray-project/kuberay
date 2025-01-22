package e2e

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/test/sampleyaml"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayServiceUpgrade(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-sample"

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(rayServiceSampleYamlApplyConfiguration())

	// TODO: This test will fail on Ray 2.40.0. Pin the Ray version to 2.9.0 as a workaround. Need to remove this after the issue is fixed.
	rayServiceAC.Spec.RayClusterSpec.WithRayVersion("2.9.0")
	rayServiceAC.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].WithImage("rayproject/ray:2.9.0")

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
	g.Eventually(func(g Gomega) {
		// curl /fruit
		stdout, _ := curlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
		g.Expect(stdout.String()).To(Equal("6"))
		// curl /calc
		stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
		g.Expect(stdout.String()).To(Equal("15 pizzas please!"))
	}, TestTimeoutShort).Should(Succeed())

	// After the above curl test, now the RayService is ready.
	// We manually delete the "ray.io/serve" label on the Head for testing the reconciliation later.
	g.Eventually(func(g Gomega) {
		head, err := test.Client().Core().CoreV1().Pods(namespace.Name).Get(test.Ctx(), rayService.Status.ActiveServiceStatus.RayClusterStatus.Head.PodName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		delete(head.Labels, utils.RayClusterServingServiceLabelKey)
		_, err = test.Client().Core().CoreV1().Pods(namespace.Name).Update(test.Ctx(), head, metav1.UpdateOptions{})
		g.Expect(err).NotTo(HaveOccurred())
	}, TestTimeoutShort).Should(Succeed())

	// upgrade the cluster and the serve application
	// Parse ServeConfigV2 and replace the string in the simplest way to update it.
	rayService, err = GetRayService(test, namespace.Name, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())

	serveConfig := rayService.Spec.ServeConfigV2
	serveConfig = strings.Replace(serveConfig, "price: 3", "price: 4", -1)
	serveConfig = strings.Replace(serveConfig, "factor: 5", "factor: 3", -1)

	// modify EnableInTreeAutoscaling to trigger a zero downtime upgrade.
	rayService.Spec.RayClusterSpec.EnableInTreeAutoscaling = ptr.To[bool](true)
	rayService.Spec.ServeConfigV2 = serveConfig

	rayService, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(
		test.Ctx(),
		rayService,
		metav1.UpdateOptions{},
	)
	g.Expect(err).NotTo(HaveOccurred())

	// After upgrade the RayService, there should be 2 Heads with "ray.io/serve=true".
	g.Eventually(func(g Gomega) int {
		heads, err := test.Client().Core().CoreV1().Pods(namespace.Name).List(test.Ctx(), metav1.ListOptions{
			LabelSelector: "ray.io/serve=true",
		})
		g.Expect(err).NotTo(HaveOccurred())
		return len(heads.Items)
	}, TestTimeoutShort).Should(Equal(2))

	// Test the new price and factor
	g.Eventually(func(g Gomega) {
		// curl /fruit
		stdout, _ := curlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
		g.Expect(stdout.String()).To(Equal("8"))
		// curl /calc
		stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
		g.Expect(stdout.String()).To(Equal("9 pizzas please!"))
	}, TestTimeoutShort).Should(Succeed())
}
