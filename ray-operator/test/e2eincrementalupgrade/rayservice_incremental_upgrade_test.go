package e2eincrementalupgrade

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	"github.com/ray-project/kuberay/ray-operator/test/sampleyaml"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// helper function to get RayCluster head service external IP to use to poll the RayService
func GetHeadServiceExternalIP(t *testing.T, clusterName, namespace string) (string, error) {
	test := With(t)

	svc, err := test.Client().Core().CoreV1().Services(namespace).Get(test.Ctx(), clusterName+"-head-svc", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		return "", fmt.Errorf("no ingress for service %s", svc.Name)
	}
	return svc.Status.LoadBalancer.Ingress[0].IP, nil
}

func TestRayServiceIncrementalUpgrade(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.RayServiceIncrementalUpgrade, true)

	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()
	rayServiceName := "incremental-rayservice"

	// Create a RayService with IncrementalUpgrade enabled
	stepSize := ptr.To(int32(20))
	interval := ptr.To(int32(30))
	maxSurge := ptr.To(int32(10))

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).
		WithSpec(IncrementalUpgradeRayServiceApplyConfiguration(stepSize, interval, maxSurge))
	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())

	// Validate Gateway and HTTPRoute objects have been created for incremental upgrade.
	gatewayName := fmt.Sprintf("%s-%s", rayServiceName, "gateway")
	LogWithTimestamp(test.T(), "Waiting for Gateway %s/%s to be ready", rayService.Namespace, gatewayName)
	g.Eventually(Gateway(test, rayService.Namespace, gatewayName), TestTimeoutMedium).
		Should(WithTransform(utils.IsGatewayReady, BeTrue()))

	gateway, err := GetGateway(test, namespace.Name, fmt.Sprintf("%s-%s", rayServiceName, "gateway"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gateway).NotTo(BeNil())

	httpRouteName := fmt.Sprintf("%s-%s", "httproute", rayServiceName)
	LogWithTimestamp(test.T(), "Waiting for HTTPRoute %s/%s to be ready", rayService.Namespace, httpRouteName)
	g.Eventually(HTTPRoute(test, rayService.Namespace, httpRouteName), TestTimeoutMedium).
		Should(Not(BeNil()))
	httpRoute, err := GetHTTPRoute(test, namespace.Name, fmt.Sprintf("%s-%s", "httproute", rayServiceName))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(utils.IsHTTPRouteReady(gateway, httpRoute)).To(BeTrue())

	// Create curl pod to test traffic routing through Gateway to RayService
	curlPodName := "curl-pod"
	curlContainerName := "curl-container"
	curlPod, err := CreateCurlPod(test, curlPodName, curlContainerName, namespace.Name)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for Curl Pod %s to be ready", curlPodName)
	g.Eventually(func(g Gomega) *corev1.Pod {
		updatedPod, err := test.Client().Core().CoreV1().Pods(curlPod.Namespace).Get(test.Ctx(), curlPod.Name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		return updatedPod
	}, TestTimeoutShort).Should(WithTransform(sampleyaml.IsPodRunningAndReady, BeTrue()))

	// Get the Gateway endpoint to send requests to
	gatewayIP := GetGatewayIP(gateway)
	g.Expect(gatewayIP).NotTo(BeEmpty())

	LogWithTimestamp(test.T(), "Verifying RayService is serving traffic")
	stdout, _ := CurlRayServiceGateway(test, gatewayIP, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))
	stdout, _ = CurlRayServiceGateway(test, gatewayIP, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
	g.Expect(stdout.String()).To(Equal("15 pizzas please!"))

	// Trigger incremental upgrade by updating RayService serve config
	rayService, err = GetRayService(test, namespace.Name, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())

	serveConfig := rayService.Spec.ServeConfigV2
	serveConfig = strings.Replace(serveConfig, "price: 3", "price: 4", -1)
	serveConfig = strings.Replace(serveConfig, "factor: 5", "factor: 3", -1)
	rayService.Spec.ServeConfigV2 = serveConfig
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(
		test.Ctx(),
		rayService,
		metav1.UpdateOptions{},
	)
	g.Expect(err).NotTo(HaveOccurred())

	// Check that upgrade steps incrementally with traffic/capacity split between clusters
	LogWithTimestamp(test.T(), "Validating gradual traffic migration during IncrementalUpgrade")
	g.Eventually(func(g Gomega) {
		rayService, err := GetRayService(test, namespace.Name, rayServiceName)
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(rayService.Status.PendingServiceStatus).NotTo(BeNil())
		g.Expect(rayService.Status.PendingServiceStatus.TrafficRoutedPercent).NotTo(BeNil())
		g.Expect(rayService.Status.PendingServiceStatus.TargetCapacity).NotTo(BeNil())
		g.Expect(rayService.Status.ActiveServiceStatus).NotTo(BeNil())
		g.Expect(rayService.Status.ActiveServiceStatus.TrafficRoutedPercent).NotTo(BeNil())
		g.Expect(rayService.Status.ActiveServiceStatus.TargetCapacity).NotTo(BeNil())

		for _, val := range []int32{
			*rayService.Status.PendingServiceStatus.TrafficRoutedPercent,
			*rayService.Status.ActiveServiceStatus.TrafficRoutedPercent,
			*rayService.Status.PendingServiceStatus.TargetCapacity,
			*rayService.Status.ActiveServiceStatus.TargetCapacity,
		} {
			g.Expect(val).To(BeNumerically(">", 0))
			g.Expect(val).To(BeNumerically("<", 100))
		}
	}, TestTimeoutMedium).Should(Succeed())

	// Validate that traffic is split across old and new clusters of the RayService
	g.Eventually(func(g Gomega) {
		rayService, err := GetRayService(test, namespace.Name, rayServiceName)
		g.Expect(err).NotTo(HaveOccurred())

		activeSvcName := rayService.Status.ActiveServiceStatus.RayClusterStatus.Head.ServiceName
		pendingSvcName := rayService.Status.PendingServiceStatus.RayClusterStatus.Head.ServiceName

		activeResp, _ := CurlRayServiceHeadService(
			test, activeSvcName, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
		pendingResp, _ := CurlRayServiceHeadService(
			test, pendingSvcName, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)

		// Both clusters should still be serving traffic during the split
		g.Expect(activeResp.String()).To(Equal("6"))
		g.Expect(pendingResp.String()).To(Equal("6"))
	}, TestTimeoutMedium).Should(Succeed())

	// Validate incremental upgrade completes
	g.Eventually(func(g Gomega) {
		rayService, err := GetRayService(test, namespace.Name, rayServiceName)
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(rayService.Status.PendingServiceStatus.TrafficRoutedPercent).To(Equal(ptr.To(int32(100))))
		g.Expect(rayService.Status.ActiveServiceStatus.TrafficRoutedPercent).To(Equal(ptr.To(int32(0))))
		g.Expect(rayService.Status.PendingServiceStatus.TargetCapacity).To(Equal(ptr.To(int32(100))))
		g.Expect(rayService.Status.ActiveServiceStatus.TargetCapacity).To(Equal(ptr.To(int32(0))))
	}, TestTimeoutMedium).Should(Succeed())
}
