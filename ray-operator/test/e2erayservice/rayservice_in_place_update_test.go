package e2erayservice

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
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

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(RayServiceSampleYamlApplyConfiguration())

	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

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

	LogWithTimestamp(test.T(), "Sending requests to the RayService to make sure it is ready to serve requests")
	stdout, _ := CurlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))
	stdout, _ = CurlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
	g.Expect(stdout.String()).To(Equal("15 pizzas please!"))

	// In-place update
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

	// Test the new price and factor
	g.Eventually(func(g Gomega) {
		// curl /fruit
		stdout, _ := CurlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
		g.Expect(stdout.String()).To(Equal("8"))
		// curl /calc
		stdout, _ = CurlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
		g.Expect(stdout.String()).To(Equal("9 pizzas please!"))
	}, TestTimeoutShort).Should(Succeed())
}

func TestRayServiceInPlaceUpdateWithRayClusterSpec(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-sample"

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(RayServiceSampleYamlApplyConfiguration())

	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

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

	LogWithTimestamp(test.T(), "Sending requests to the RayService to make sure it is ready to serve requests")
	stdout, _ := CurlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))
	stdout, _ = CurlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
	g.Expect(stdout.String()).To(Equal("15 pizzas please!"))

	// In-place update
	// Parse ServeConfigV2 and replace the string in the simplest way to update it.
	rayService, err = GetRayService(test, namespace.Name, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())

	serveConfig := rayService.Spec.ServeConfigV2
	serveConfig = strings.Replace(serveConfig, "price: 3", "price: 456", -1)
	rayService.Spec.ServeConfigV2 = serveConfig

	rayService.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "FOO", Value: "BAR"}}

	var activeRayClusterBeforeUpdate *rayv1.RayCluster
	activeRayClusterBeforeUpdate, err = GetRayCluster(test, namespace.Name, rayService.Status.ActiveServiceStatus.RayClusterName)
	g.Expect(err).NotTo(HaveOccurred())

	rayService, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(
		test.Ctx(),
		rayService,
		metav1.UpdateOptions{},
	)
	g.Expect(err).NotTo(HaveOccurred())

	// Wait active RayCluster has been terminated
	g.Eventually(func() bool {
		_, err := GetRayCluster(test, activeRayClusterBeforeUpdate.Namespace, activeRayClusterBeforeUpdate.Name)
		return k8sApiErrors.IsNotFound(err)
	}, TestTimeoutMedium).Should(BeTrue())

	// Make sure the serveConfig is updated to new ray cluster.
	g.Eventually(func(g Gomega) {
		// curl /fruit
		stdout, _ := CurlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
		g.Expect(stdout.String()).To(Equal("912"))
	}, TestTimeoutShort).Should(Succeed())

	events, err := test.Client().Core().CoreV1().Events(rayService.Namespace).List(test.Ctx(), metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("involvedObject.name", rayService.Name).String(),
	})
	g.Expect(err).NotTo(HaveOccurred())

	// event UpdatedServeApplications or FailedToUpdateServeApplications should not occur on activeRayClusterBeforeUpdate after RayService update.
	count := 0
	for _, event := range events.Items {
		if event.Reason != string(utils.UpdatedServeApplications) && event.Reason != string(utils.FailedToUpdateServeApplications) {
			continue
		}
		count++
	}
	g.Expect(count).To(Equal(2))
}

func TestRayServiceInPlaceUpdateWithRayClusterSpecWithoutZeroDowntime(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-sample"

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(RayServiceSampleYamlApplyConfiguration().
		WithUpgradeStrategy(rayv1ac.RayServiceUpgradeStrategy().WithType(rayv1.None)))

	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

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

	LogWithTimestamp(test.T(), "Sending requests to the RayService to make sure it is ready to serve requests")
	stdout, _ := CurlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))
	stdout, _ = CurlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
	g.Expect(stdout.String()).To(Equal("15 pizzas please!"))

	// In-place update
	// Parse ServeConfigV2 and replace the string in the simplest way to update it.
	rayService, err = GetRayService(test, namespace.Name, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())

	serveConfig := rayService.Spec.ServeConfigV2
	serveConfig = strings.Replace(serveConfig, "price: 3", "price: 456", -1)
	rayService.Spec.ServeConfigV2 = serveConfig

	rayService.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "FOO", Value: "BAR"}}

	var activeRayClusterBeforeUpdate *rayv1.RayCluster
	activeRayClusterBeforeUpdate, err = GetRayCluster(test, namespace.Name, rayService.Status.ActiveServiceStatus.RayClusterName)
	g.Expect(err).NotTo(HaveOccurred())

	rayService, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(
		test.Ctx(),
		rayService,
		metav1.UpdateOptions{},
	)
	g.Expect(err).NotTo(HaveOccurred())

	// Test the new price and factor
	g.Eventually(func(g Gomega) {
		// curl /fruit
		stdout, _ := CurlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
		g.Expect(stdout.String()).To(Equal("912"))
		// curl /calc
		stdout, _ = CurlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
		g.Expect(stdout.String()).To(Equal("15 pizzas please!"))
	}, TestTimeoutShort).Should(Succeed())

	// RayCluster is not changed
	activeRayClusterBeforeUpdate, err = GetRayCluster(test, namespace.Name, activeRayClusterBeforeUpdate.Name)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(RayClusterState(activeRayClusterBeforeUpdate)).To(Equal(rayv1.Ready))
}
