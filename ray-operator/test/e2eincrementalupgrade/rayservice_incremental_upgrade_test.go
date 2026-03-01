package e2eincrementalupgrade

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// TestRayServiceIncrementalUpgrade tests the basic incremental upgrade flow of the RayService.
//
// The test case follows these steps:
// 1. Enable the RayServiceIncrementalUpgrade feature gate
// 2. Create a RayService with IncrementalUpgrade enabled
// 3. Wait for the RayService to be ready
// 4. Wait for the Gateway object to be ready (Accepted and Programmed conditions are True)
// 5. Wait for the corresponding HTTPRoute object to be ready (Accepted and ResolvedRefs conditions are True)
// 6. Create a Curl pod to test traffic routing through Gateway to RayService and wait for it to be ready
// 7. Validate RayService is serving traffic by sending requests through Gateway external IP
// 8. Update the RayCluster spec and RayService serve config to trigger upgrade
//   - NOTE: Incremental upgrade is triggered by RayCluster spec changes, not serve config changes
//
// 9. Wait for the RayService to be upgrading
// 10. Validate the active and pending cluster serve services have been created
// 11. Validate the pending cluster head pod is ready
// 12. Validate the HTTPRoute backends have two backends (active and pending cluster serve services)
// 13. Wait for incremental upgrade to complete
// 14. Validate the RayService uses updated ServeConfig after upgrade completes
// func TestRayServiceIncrementalUpgrade(t *testing.T) {
// 	features.SetFeatureGateDuringTest(t, features.RayServiceIncrementalUpgrade, true)

// 	test := With(t)
// 	g := NewWithT(t)

// 	namespace := test.NewTestNamespace()
// 	rayServiceName := "incremental-rayservice"

// 	// Create a RayService with IncrementalUpgrade enabled
// 	stepSize := ptr.To(int32(25))
// 	interval := ptr.To(int32(5))
// 	maxSurge := ptr.To(int32(50))
// 	serveConfigV2 := defaultIncrementalUpgradeServeConfigV2

// 	// Create RayService with IncrementalUpgrade enabled and wait for key components to be ready
// 	rayService, httpRoute, gatewayIP := boostrapIncrementalRayService(test, g, namespace.Name, rayServiceName, stepSize, interval, maxSurge, serveConfigV2)

// 	// Create curl pod to test traffic routing through Gateway to RayService
// 	curlPodName := "curl-pod"
// 	curlContainerName := "curl-container"
// 	curlPod, err := CreateCurlPod(g, test, curlPodName, curlContainerName, namespace.Name)
// 	g.Expect(err).NotTo(HaveOccurred())

// 	LogWithTimestamp(test.T(), "Waiting for Curl Pod %s to be ready", curlPodName)
// 	g.Eventually(func(g Gomega) *corev1.Pod {
// 		updatedPod, err := test.Client().Core().CoreV1().Pods(curlPod.Namespace).Get(test.Ctx(), curlPod.Name, metav1.GetOptions{})
// 		g.Expect(err).NotTo(HaveOccurred())
// 		return updatedPod
// 	}, TestTimeoutShort).Should(WithTransform(IsPodRunningAndReady, BeTrue()))

// 	LogWithTimestamp(test.T(), "Verifying RayService is serving traffic")
// 	stdout, _ := PostRayServiceGateway(test, gatewayIP, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
// 	g.Expect(stdout.String()).To(Equal("6"))
// 	stdout, _ = PostRayServiceGateway(test, gatewayIP, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
// 	g.Expect(stdout.String()).To(Equal("15 pizzas please!"))

// 	// Attempt to trigger NewClusterWithIncrementalUpgrade by updating RayService serve config and RayCluster spec
// 	g.Eventually(func() error {
// 		latestRayService, err := GetRayService(test, namespace.Name, rayServiceName)
// 		if err != nil {
// 			return err
// 		}
// 		latestRayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("500m")
// 		serveConfig := latestRayService.Spec.ServeConfigV2
// 		serveConfig = strings.ReplaceAll(serveConfig, "price: 3", "price: 4")
// 		serveConfig = strings.ReplaceAll(serveConfig, "factor: 5", "factor: 3")
// 		latestRayService.Spec.ServeConfigV2 = serveConfig

// 		_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(
// 			test.Ctx(),
// 			latestRayService,
// 			metav1.UpdateOptions{},
// 		)
// 		return err
// 	}, TestTimeoutShort).Should(Succeed(), "Failed to update RayService to trigger upgrade")

// 	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be true", rayService.Namespace, rayService.Name)
// 	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeTrue()))

// 	LogWithTimestamp(test.T(), "Verifying active and pending cluster serve services and HTTPRoute backends")
// 	upgradingRaySvc, err := GetRayService(test, namespace.Name, rayServiceName)
// 	g.Expect(err).NotTo(HaveOccurred())
// 	activeClusterName := upgradingRaySvc.Status.ActiveServiceStatus.RayClusterName
// 	g.Expect(activeClusterName).NotTo(BeEmpty(), "The active cluster should be set when a RayService is ready.")
// 	pendingClusterName := upgradingRaySvc.Status.PendingServiceStatus.RayClusterName
// 	g.Expect(pendingClusterName).NotTo(BeEmpty(), "The controller should have created a pending cluster.")

// 	// Validate serve service for the active cluster exists.
// 	activeServeSvcName := utils.GenerateServeServiceName(activeClusterName)
// 	_, err = test.Client().Core().CoreV1().Services(namespace.Name).Get(test.Ctx(), activeServeSvcName, metav1.GetOptions{})
// 	g.Expect(err).NotTo(HaveOccurred(), "The serve service for the active cluster should be created.")

// 	// Validate serve service for the pending cluster has been created for the upgrade.
// 	pendingServeSvcName := utils.GenerateServeServiceName(pendingClusterName)
// 	g.Eventually(func(g Gomega) {
// 		_, err = test.Client().Core().CoreV1().Services(namespace.Name).Get(test.Ctx(), pendingServeSvcName, metav1.GetOptions{})
// 		g.Expect(err).NotTo(HaveOccurred(), "The serve service for the pending cluster should be created.")
// 	}, TestTimeoutShort).Should(Succeed())

// 	LogWithTimestamp(test.T(), "Waiting for pending RayCluster %s to have a ready head pod", pendingClusterName)
// 	g.Eventually(RayCluster(test, namespace.Name, pendingClusterName), TestTimeoutMedium).
// 		Should(WithTransform(StatusCondition(rayv1.HeadPodReady), MatchCondition(metav1.ConditionTrue, rayv1.HeadPodRunningAndReady)))

// 	// Wait for the HTTPRoute to reflect the two backends.
// 	LogWithTimestamp(test.T(), "Waiting for HTTPRoute to have two backends")
// 	g.Eventually(func(g Gomega) {
// 		route, err := GetHTTPRoute(test, namespace.Name, httpRoute.Name)
// 		g.Expect(err).NotTo(HaveOccurred())
// 		g.Expect(route.Spec.Rules).To(HaveLen(1))
// 		g.Expect(route.Spec.Rules[0].BackendRefs).To(HaveLen(2))
// 		g.Expect(string(route.Spec.Rules[0].BackendRefs[1].Name)).To(Equal(pendingServeSvcName))
// 	}, TestTimeoutShort).Should(Succeed())

// 	LogWithTimestamp(test.T(), "Validating stepwise traffic and capacity migration")
// 	intervalSeconds := *interval
// 	var lastMigratedTime *metav1.Time
// 	oldVersionServed := false
// 	newVersionServed := false

// 	// Validate expected behavior during an IncrementalUpgrade. The following checks ensure
// 	// that no requests are dropped throughout the upgrade process.
// 	upgradeSteps := generateUpgradeSteps(*stepSize, *maxSurge)
// 	for _, step := range upgradeSteps {
// 		LogWithTimestamp(test.T(), "%s", step.name)
// 		g.Eventually(func(g Gomega) int32 {
// 			// Fetch updated RayService.
// 			svc, err := GetRayService(test, namespace.Name, rayServiceName)
// 			g.Expect(err).NotTo(HaveOccurred())
// 			return step.getValue(svc)
// 		}, TestTimeoutShort).Should(Equal(step.expectedValue))

// 		// Send a request to the RayService to validate no requests are dropped. Check that
// 		// both endpoints are serving requests.
// 		stdout, _ := PostRayServiceGateway(test, gatewayIP, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
// 		response := stdout.String()
// 		g.Expect(response).To(Or(Equal("6"), Equal("8")), "Response should be from the old or new app version during the upgrade")
// 		if response == "6" {
// 			oldVersionServed = true
// 		}
// 		if response == "8" {
// 			newVersionServed = true
// 		}

// 		if strings.Contains(step.name, "pending traffic to shift") {
// 			svc, err := GetRayService(test, namespace.Name, rayServiceName)
// 			g.Expect(err).NotTo(HaveOccurred())

// 			currentMigratedTime := svc.Status.PendingServiceStatus.LastTrafficMigratedTime
// 			g.Expect(currentMigratedTime).NotTo(BeNil())

// 			// Verify IntervalSeconds have passed since last TrafficRoutedPercent update.
// 			if lastMigratedTime != nil {
// 				duration := currentMigratedTime.Sub(lastMigratedTime.Time)
// 				g.Expect(duration).To(BeNumerically(">=", intervalSeconds),
// 					"Time between traffic steps should be >= IntervalSeconds")
// 			}
// 			lastMigratedTime = currentMigratedTime
// 		}
// 	}
// 	LogWithTimestamp(test.T(), "Verifying both old and new versions served traffic during the upgrade")
// 	g.Expect(oldVersionServed).To(BeTrue(), "The old version of the service should have served traffic during the upgrade.")
// 	g.Expect(newVersionServed).To(BeTrue(), "The new version of the service should have served traffic during the upgrade.")

// 	// Check that RayService completed upgrade
// 	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be false", rayService.Namespace, rayService.Name)
// 	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeFalse()))

// 	LogWithTimestamp(test.T(), "Verifying RayService uses updated ServeConfig after upgrade completes")
// 	stdout, _ = PostRayServiceGateway(test, gatewayIP, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
// 	g.Expect(stdout.String()).To(Equal("8"))
// }

// TestRayServiceIncrementalUpgradeWithLocust tests the incremental upgrade flow of the RayService
// by running a Locust load test throughout the entire upgrade lifecycle:
//   - Initial serving on the old cluster
//   - During the upgrade phase
//   - Final serving on the new cluster
//
// The ultimate goal is to ensure that there are no request drops during the upgrade.
//
// The test case follows these steps:
// 1. Enable the RayServiceIncrementalUpgrade feature gate
// 2. Create a RayService with IncrementalUpgrade enabled
// 3. Wait for the RayService to be ready
// 4. Wait for the Gateway object to be ready (Accepted and Programmed conditions are True)
// 5. Wait for the corresponding HTTPRoute object to be ready (Accepted and ResolvedRefs conditions are True)
// 6. Create a ConfigMap with Locust runner script
// 7. Deploy a head-only Locust RayCluster and install Locust in the head Pod
// 8. Start Locust in a background goroutine targeting Gateway IP
// 9. (Should be modified) Wait for Locust to ramp up
// 10. Update the RayCluster spec and RayService serve config to trigger upgrade
//   - NOTE: Incremental upgrade is triggered by RayCluster spec changes, not serve config changes
//
// 11. Validate TrafficRoutedPercent through upgrade steps, the expected behaviors are:
//   - Active cluster: TrafficRoutedPercent is monotonically decreasing
//   - Pending cluster: TrafficRoutedPercent is monotonically increasing
//
// 12. Wait for incremental upgrade to complete
// 13. Ensure all remaining traffic is routed to the new cluster after upgrade completes
//
// NOTE: locust_runner.py validates whether the Locust load test completed with zero failures.
// So we don't need to validate this here.
func TestRayServiceIncrementalUpgradeWithLocust(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.RayServiceIncrementalUpgrade, true)

	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()
	rayServiceName := "incremental-rayservice"

	// TODO(jwj): Loop through different combinations.
	stepSize := ptr.To(int32(25))
	interval := ptr.To(int32(5))
	maxSurge := ptr.To(int32(50))
	serveConfigV2 := highRPSServeConfigV2

	// Phase 1: Create RayService with incremental upgrade and wait for key components to be ready
	_, _, gatewayIP := boostrapIncrementalRayService(test, g, namespace.Name, rayServiceName, stepSize, interval, maxSurge, serveConfigV2)

	// Phase 2: Deploy Locust RayCluster and install Locust
	// TODO(jwj): Extract a helper for cross-module reusability (rayservice_ha_test.go) if needed.
	locustYamlFile := "testdata/locust-cluster.incremental-upgrade.yaml"

	configMapAC := newLocustRunnerConfigMapAC(namespace.Name, Files(test, "locust_runner.py"))
	configMap, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), configMapAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", configMap.Namespace, configMap.Name)

	KubectlApplyYAML(test, locustYamlFile, namespace.Name)
	locustCluster, err := GetRayCluster(test, namespace.Name, "locust-cluster")
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created Locust RayCluster %s/%s successfully", locustCluster.Namespace, locustCluster.Name)

	g.Eventually(RayCluster(test, locustCluster.Namespace, locustCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	g.Expect(GetRayCluster(test, locustCluster.Namespace, locustCluster.Name)).
		To(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))

	locustHeadPod, err := GetHeadPod(test, locustCluster)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Found Locust head pod %s/%s", locustHeadPod.Namespace, locustHeadPod.Name)

	ExecPodCmd(test, locustHeadPod, common.RayHeadContainer, []string{"pip", "install", "locust==2.32.10"})

	// Phase 3: Start Locust in a background goroutine targeting Gateway IP
	var wg sync.WaitGroup
	locustHost := fmt.Sprintf("http://%s", gatewayIP)

	wg.Go(func() {
		LogWithTimestamp(test.T(), "Starting Locust load test against %s", locustHost)
		ExecPodCmd(test, locustHeadPod, common.RayHeadContainer, []string{
			"python", "/locust-runner/locust_runner.py",
			"-f", "/locustfile/locustfile.py",
			"--host", locustHost,
		})
		LogWithTimestamp(test.T(), "Locust load test completed with zero failures")
	})

	// Allow Locust to ramp up and send traffic to the old cluster before triggering upgrade.
	err = warmupLocust(test, locustHeadPod, 900, 15, 120*time.Second)
	g.Expect(err).NotTo(HaveOccurred())

	// Phase 4: Trigger incremental upgrade
	LogWithTimestamp(test.T(), "Triggering incremental upgrade by updating RayCluster spec and RayService serve config")
	g.Eventually(func() error {
		latestRayService, err := GetRayService(test, namespace.Name, rayServiceName)
		if err != nil {
			return err
		}
		latestRayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("500m")
		serveConfig := latestRayService.Spec.ServeConfigV2
		serveConfig = strings.ReplaceAll(serveConfig, "price: 3", "price: 4")
		serveConfig = strings.ReplaceAll(serveConfig, "factor: 5", "factor: 3")
		latestRayService.Spec.ServeConfigV2 = serveConfig

		_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(
			test.Ctx(),
			latestRayService,
			metav1.UpdateOptions{},
		)
		return err
	}, TestTimeoutShort).Should(Succeed(), "Failed to update RayService to trigger upgrade")

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be true", namespace.Name, rayServiceName)
	g.Eventually(RayService(test, namespace.Name, rayServiceName), TestTimeoutShort).
		Should(WithTransform(IsRayServiceUpgrading, BeTrue()))

	// Phase 5: Validate monotonic TrafficRoutedPercent through upgrade steps
	LogWithTimestamp(test.T(), "Validating stepwise traffic migration with monotonic TrafficRoutedPercent")

	prevActiveTraffic := int32(100)
	prevPendingTraffic := int32(0)

	upgradeSteps := generateUpgradeSteps(*stepSize, *maxSurge)
	for _, step := range upgradeSteps {
		LogWithTimestamp(test.T(), "%s", step.name)
		g.Eventually(func(g Gomega) int32 {
			svc, err := GetRayService(test, namespace.Name, rayServiceName)
			g.Expect(err).NotTo(HaveOccurred())
			return step.getValue(svc)
		}, TestTimeoutShort).Should(Equal(step.expectedValue))

		svc, err := GetRayService(test, namespace.Name, rayServiceName)
		g.Expect(err).NotTo(HaveOccurred())

		currentActiveTraffic := GetActiveTraffic(svc)
		currentPendingTraffic := GetPendingTraffic(svc)

		g.Expect(currentActiveTraffic).To(BeNumerically("<=", prevActiveTraffic),
			"TrafficRoutedPercent in active cluster must be monotonically decreasing: prev %d, curr %d", prevActiveTraffic, currentActiveTraffic)
		g.Expect(currentPendingTraffic).To(BeNumerically(">=", prevPendingTraffic),
			"TrafficRoutedPercent in pending cluster must be monotonically increasing: prev %d, curr %d", prevPendingTraffic, currentPendingTraffic)

		prevActiveTraffic = currentActiveTraffic
		prevPendingTraffic = currentPendingTraffic
	}

	// Phase 6: Upgrade complete
	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be false", namespace.Name, rayServiceName)
	g.Eventually(RayService(test, namespace.Name, rayServiceName), TestTimeoutShort).
		Should(WithTransform(IsRayServiceUpgrading, BeFalse()))

	LogWithTimestamp(test.T(), "Waiting for Locust load test goroutine to finish")
	wg.Wait()

	LogWithTimestamp(test.T(), "Validating remaining traffic is routed to the new cluster after upgrade completes")
	curlPodName := "curl-pod"
	curlContainerName := "curl-container"
	curlPod, err := CreateCurlPod(g, test, curlPodName, curlContainerName, namespace.Name)
	g.Expect(err).NotTo(HaveOccurred())

	stdout, _ := GetRayServiceGateway(test, gatewayIP, curlPod, curlContainerName, "/test")
	var resp struct {
		Status string `json:"status"`
	}
	err = json.Unmarshal(stdout.Bytes(), &resp)
	g.Expect(err).NotTo(HaveOccurred(), "GET /test response should be valid JSON, got: %s", stdout.String())
	g.Expect(resp.Status).To(Equal("ok"), "GET /test status field should be ok, got: %s", resp.Status)
}
