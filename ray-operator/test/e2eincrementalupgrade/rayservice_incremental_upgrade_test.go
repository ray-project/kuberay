package e2eincrementalupgrade

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
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
	stepSize := ptr.To(int32(25))
	interval := ptr.To(int32(5))
	maxSurge := ptr.To(int32(50))

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

	// Get the Gateway endpoint to send requests to
	gateway, err := GetGateway(test, namespace.Name, fmt.Sprintf("%s-%s", rayServiceName, "gateway"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gateway).NotTo(BeNil())

	httpRouteName := fmt.Sprintf("%s-%s", "httproute", gatewayName)
	LogWithTimestamp(test.T(), "Waiting for HTTPRoute %s/%s to be ready", rayService.Namespace, httpRouteName)
	g.Eventually(HTTPRoute(test, rayService.Namespace, httpRouteName), TestTimeoutMedium).
		Should(Not(BeNil()))

	httpRoute, err := GetHTTPRoute(test, namespace.Name, httpRouteName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(utils.IsHTTPRouteReady(gateway, httpRoute)).To(BeTrue())

	// Create curl pod to test traffic routing through Gateway to RayService
	curlPodName := "curl-pod"
	curlContainerName := "curl-container"
	curlPod, err := CreateCurlPod(g, test, curlPodName, curlContainerName, namespace.Name)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for Curl Pod %s to be ready", curlPodName)
	g.Eventually(func(g Gomega) *corev1.Pod {
		updatedPod, err := test.Client().Core().CoreV1().Pods(curlPod.Namespace).Get(test.Ctx(), curlPod.Name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		return updatedPod
	}, TestTimeoutShort).Should(WithTransform(IsPodRunningAndReady, BeTrue()))

	gatewayIP := GetGatewayIP(gateway)
	g.Expect(gatewayIP).NotTo(BeEmpty())

	hostname := fmt.Sprintf("%s.%s.svc.cluster.local", rayService.Name, rayService.Namespace)

	LogWithTimestamp(test.T(), "Verifying RayService is serving traffic")
	stdout, _ := CurlRayServiceGateway(test, gatewayIP, hostname, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))
	stdout, _ = CurlRayServiceGateway(test, gatewayIP, hostname, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
	g.Expect(stdout.String()).To(Equal("15 pizzas please!"))

	// Attempt to trigger incremental upgrade by updating RayService serve config and RayCluster spec
	g.Eventually(func() error {
		latestRayService, err := GetRayService(test, namespace.Name, rayServiceName)
		if err != nil {
			return err
		}
		latestRayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("500m")
		serveConfig := latestRayService.Spec.ServeConfigV2
		serveConfig = strings.Replace(serveConfig, "price: 3", "price: 4", -1)
		serveConfig = strings.Replace(serveConfig, "factor: 5", "factor: 3", -1)
		latestRayService.Spec.ServeConfigV2 = serveConfig

		_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(
			test.Ctx(),
			latestRayService,
			metav1.UpdateOptions{},
		)
		return err
	}, TestTimeoutShort).Should(Succeed(), "Failed to update RayService to trigger upgrade")

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be true", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeTrue()))

	LogWithTimestamp(test.T(), "Verifying temporary service creation and HTTPRoute backends")
	upgradingRaySvc, err := GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	activeClusterName := upgradingRaySvc.Status.ActiveServiceStatus.RayClusterName
	g.Expect(activeClusterName).NotTo(BeEmpty(), "The active cluster should be set when a RayService is ready.")
	pendingClusterName := upgradingRaySvc.Status.PendingServiceStatus.RayClusterName
	g.Expect(pendingClusterName).NotTo(BeEmpty(), "The controller should have created a pending cluster.")

	// Validate serve service for the active cluster exists.
	activeServeSvcName := utils.GenerateServeServiceName(activeClusterName)
	_, err = test.Client().Core().CoreV1().Services(namespace.Name).Get(test.Ctx(), activeServeSvcName, metav1.GetOptions{})
	g.Expect(err).NotTo(HaveOccurred(), "The serve service for the active cluster should be created.")

	// Validate serve service for the pending cluster has been created for the upgrade.
	pendingServeSvcName := utils.GenerateServeServiceName(pendingClusterName)
	g.Eventually(func(g Gomega) {
		_, err = test.Client().Core().CoreV1().Services(namespace.Name).Get(test.Ctx(), pendingServeSvcName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred(), "The serve service for the pending cluster should be created.")
	}, TestTimeoutShort).Should(Succeed())

	LogWithTimestamp(test.T(), "Waiting for pending RayCluster %s to have a ready head pod", pendingClusterName)
	g.Eventually(RayCluster(test, namespace.Name, pendingClusterName), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.HeadPodReady), MatchCondition(metav1.ConditionTrue, rayv1.HeadPodRunningAndReady)))

	// Wait for the HTTPRoute to reflect the two backends.
	LogWithTimestamp(test.T(), "Waiting for HTTPRoute to have two backends")
	g.Eventually(func(g Gomega) {
		route, err := GetHTTPRoute(test, namespace.Name, httpRouteName)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(route.Spec.Rules).To(HaveLen(1))
		g.Expect(route.Spec.Rules[0].BackendRefs).To(HaveLen(2))
		g.Expect(string(route.Spec.Rules[0].BackendRefs[1].Name)).To(Equal(pendingServeSvcName))
	}, TestTimeoutShort).Should(Succeed())

	LogWithTimestamp(test.T(), "Validating stepwise traffic and capacity migration")
	intervalSeconds := *interval
	var lastMigratedTime *metav1.Time

	// Validate expected behavior during an IncrementalUpgrade. The following checks ensures
	// that no requests are dropped throughout the upgrade process.
	upgradeSteps := generateUpgradeSteps(*stepSize, *maxSurge)
	for _, step := range upgradeSteps {
		LogWithTimestamp(test.T(), "%s", step.name)
		g.Eventually(func(g Gomega) int32 {
			// Fetch updated RayService.
			svc, err := GetRayService(test, namespace.Name, rayServiceName)
			g.Expect(err).NotTo(HaveOccurred())
			return step.getValue(svc)
		}, TestTimeoutShort).Should(Equal(step.expectedValue))

		// Send a request to the RayService to validate no requests are dropped.
		stdout, _ := CurlRayServiceGateway(test, gatewayIP, hostname, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
		g.Expect(stdout.String()).To(Or(Equal("6"), Equal("8")), "Response should be from the old or new app version during the upgrade")

		if strings.Contains(step.name, "pending traffic to shift") {
			svc, err := GetRayService(test, namespace.Name, rayServiceName)
			g.Expect(err).NotTo(HaveOccurred())

			currentMigratedTime := svc.Status.PendingServiceStatus.LastTrafficMigratedTime
			g.Expect(currentMigratedTime).NotTo(BeNil())

			// Verify IntervalSeconds have passed since last TrafficRoutedPercent update.
			if lastMigratedTime != nil {
				duration := currentMigratedTime.Sub(lastMigratedTime.Time)
				g.Expect(duration).To(BeNumerically(">=", intervalSeconds),
					"Time between traffic steps should be >= IntervalSeconds")
			}
			lastMigratedTime = currentMigratedTime
		}
	}
	// Check that RayService completed upgrade
	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be false", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeFalse()))

	LogWithTimestamp(test.T(), "Verifying RayService uses updated ServeConfig after upgrade completes")
	stdout, _ = CurlRayServiceGateway(test, gatewayIP, hostname, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("8"))
}
