package e2eincrementalupgrade

import (
	"fmt"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
	stepSize := ptr.To(int32(25))
	interval := ptr.To(int32(10))
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

	// Trigger incremental upgrade by updating RayService serve config and RayCluster spec
	rayService, err = GetRayService(test, namespace.Name, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())

	rayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests["CPU"] = resource.MustParse("500m")
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

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be true", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeTrue()))

	LogWithTimestamp(test.T(), "Validating stepwise traffic and capacity migration")
	stepSizeVal := *stepSize
	intervalVal := *interval
	maxSurgeVal := *maxSurge

	var lastPendingCapacity, lastPendingTraffic, lastActiveCapacity, lastActiveTraffic int32

	// Validate expected behavior during IncrementalUpgrade
	for {
		// Wait IntervalSeconds in between updates
		time.Sleep(time.Duration(intervalVal) * time.Second)

		// Fetch updated RayService
		rayService, err := GetRayService(test, namespace.Name, rayServiceName)
		g.Expect(err).NotTo(HaveOccurred())

		pending := rayService.Status.PendingServiceStatus
		active := rayService.Status.ActiveServiceStatus

		if pending.RayClusterName == "" {
			// No pending cluster - upgrade has completed
			break
		}

		// Incremental Upgrade related status fields should be set
		g.Expect(pending.TrafficRoutedPercent).NotTo(BeNil())
		g.Expect(pending.TargetCapacity).NotTo(BeNil())
		g.Expect(active.TrafficRoutedPercent).NotTo(BeNil())
		g.Expect(active.TargetCapacity).NotTo(BeNil())

		pendingTraffic := *pending.TrafficRoutedPercent
		pendingCapacity := *pending.TargetCapacity
		activeTraffic := *active.TrafficRoutedPercent
		activeCapacity := *active.TargetCapacity

		LogWithTimestamp(test.T(), "pendingTraffic: %d, pendingCapacity: %d, activeTraffic: %d, activeCapacity: %d", pendingTraffic, pendingCapacity, activeTraffic, activeCapacity)

		// Initial iteration - set weights
		if pendingTraffic == 0 && pendingCapacity == 0 && activeTraffic == 100 && activeCapacity == 100 {
			lastPendingCapacity = pendingCapacity
			lastPendingTraffic = pendingTraffic
			lastActiveCapacity = activeCapacity
			lastActiveTraffic = activeTraffic
			continue
		}

		// Validate that pending TargetCapacity increases by MaxSurgePercent
		if pendingCapacity > lastPendingCapacity {
			g.Expect(pendingCapacity - lastPendingCapacity).To(Equal(maxSurgeVal))
			lastPendingCapacity = pendingCapacity
		}

		// Incremental traffic migration steps
		if pendingTraffic < pendingCapacity {
			if lastPendingTraffic != 0 {
				g.Expect(pendingTraffic - lastPendingTraffic).To(Equal(stepSizeVal))
				g.Expect(lastActiveTraffic - activeTraffic).To(Equal(stepSizeVal))
			}
			lastPendingTraffic = pendingTraffic
			lastActiveTraffic = activeTraffic
			continue
		}

		// Once pending TrafficRoutedPercent equals TargetCapacity, active
		// TargetCapacity can be reduced by MaxSurgePercent.
		if pendingTraffic == pendingCapacity && activeCapacity > 0 {
			rayService, err = GetRayService(test, namespace.Name, rayServiceName)
			g.Expect(err).NotTo(HaveOccurred())
			newActiveCapacity := *rayService.Status.ActiveServiceStatus.TargetCapacity
			g.Expect(lastActiveCapacity - newActiveCapacity).To(Equal(maxSurgeVal))
			lastActiveCapacity = newActiveCapacity
			continue
		}
	}
	// Check that RayService completed upgrade
	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be false", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeFalse()))

	LogWithTimestamp(test.T(), "Verifying RayService uses updated ServeConfig after upgrade completes")
	stdout, _ = CurlRayServiceGateway(test, gatewayIP, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("8"))
}
