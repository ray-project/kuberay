package e2eincrementalupgrade

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// TestRayServiceIncrementalUpgrade is the functional test for the basic incremental upgrade flow of the RayService,
// focusing on the behaviors of the RayService during the upgrade process.
//
// It iterates over incrementalUpgradeCombinations to cover diverse upgrade scenarios:
//   - BlueGreen
//   - StandardGradual
//   - ConservativeGradual
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
// 13. Perform behavioral checks, covering:
//   - Current state value matches the expected value or the RayService finished upgrading
//   - Both old and new versions served traffic during the upgrade and no requests are dropped
//   - Traffic is migrated with respect to the interval seconds
//   - Active TargetCapacity is monotonically decreasing, and Pending TargetCapacity is monotonically increasing
//   - Active TrafficRoutedPercent is monotonically decreasing, and Pending TrafficRoutedPercent is monotonically increasing
//
// 14. Wait for incremental upgrade to complete
// 15. Validate the RayService uses updated ServeConfig after upgrade completes
func TestRayServiceIncrementalUpgrade(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.RayServiceIncrementalUpgrade, true)

	for _, params := range incrementalUpgradeCombinations {
		t.Run(params.Name, func(t *testing.T) {
			test := With(t)
			g := NewWithT(t)

			namespace := test.NewTestNamespace()
			rayServiceName := "incremental-rayservice"

			stepSize, interval, maxSurge := params.ptrs()
			serveConfigV2 := defaultIncrementalUpgradeServeConfigV2

			// Create RayService with IncrementalUpgrade enabled and wait for key components to be ready
			rayService, httpRoute, gatewayIP := bootstrapIncrementalRayService(test, g, namespace.Name, rayServiceName, stepSize, interval, maxSurge, serveConfigV2)

			// Create curl pod to test traffic routing through Gateway to RayService
			curlPod, err := CreateCurlPod(g, test, CurlPodName, CurlContainerName, namespace.Name)
			g.Expect(err).NotTo(HaveOccurred())

			LogWithTimestamp(test.T(), "Verifying RayService is serving traffic")
			stdout, _ := CurlRayServiceGateway(test, gatewayIP, curlPod, CurlContainerName, http.MethodPost, "/fruit", `["MANGO", 2]`)
			g.Expect(stdout.String()).To(Equal("6"))
			stdout, _ = CurlRayServiceGateway(test, gatewayIP, curlPod, CurlContainerName, http.MethodPost, "/calc", `["MUL", 3]`)
			g.Expect(stdout.String()).To(Equal("15 pizzas please!"))

			LogWithTimestamp(test.T(), "Triggering incremental upgrade by updating RayCluster spec and RayService serve config")
			g.Eventually(incrementalUpgrade(test, namespace.Name, rayServiceName), TestTimeoutShort).Should(Succeed(), "Failed to update RayService to trigger upgrade")
			LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be true", rayService.Namespace, rayService.Name)
			g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeTrue()))

			LogWithTimestamp(test.T(), "Verifying active and pending cluster serve services and HTTPRoute backends")
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
			g.Eventually(func(gg Gomega) {
				_, err = test.Client().Core().CoreV1().Services(namespace.Name).Get(test.Ctx(), pendingServeSvcName, metav1.GetOptions{})
				gg.Expect(err).NotTo(HaveOccurred(), "The serve service for the pending cluster should be created.")
			}, TestTimeoutShort).Should(Succeed())

			LogWithTimestamp(test.T(), "Waiting for pending RayCluster %s to have a ready head pod", pendingClusterName)
			g.Eventually(RayCluster(test, namespace.Name, pendingClusterName), TestTimeoutMedium).
				Should(WithTransform(StatusCondition(rayv1.HeadPodReady), MatchCondition(metav1.ConditionTrue, rayv1.HeadPodRunningAndReady)))

			// Wait for the HTTPRoute to reflect the two backends.
			LogWithTimestamp(test.T(), "Waiting for HTTPRoute to have two backends")
			g.Eventually(func(gg Gomega) {
				route, err := GetHTTPRoute(test, namespace.Name, httpRoute.Name)
				gg.Expect(err).NotTo(HaveOccurred())
				gg.Expect(route.Spec.Rules).To(HaveLen(1))
				gg.Expect(route.Spec.Rules[0].BackendRefs).To(HaveLen(2))
				gg.Expect(string(route.Spec.Rules[0].BackendRefs[1].Name)).To(Equal(pendingServeSvcName))
			}, TestTimeoutShort).Should(Succeed())

			// Validate expected behavior during an IncrementalUpgrade. The following checks ensure
			// that no requests are dropped throughout the upgrade process.
			LogWithTimestamp(test.T(), "Performing behavioral checks, validating stepwise traffic and capacity migration")

			oldVersionServed := false
			newVersionServed := false

			intervalSeconds := *interval
			var lastMigratedTime *metav1.Time

			prevActiveCapacity := int32(100)
			prevPendingCapacity := int32(0)
			prevActiveTraffic := int32(100)
			prevPendingTraffic := int32(0)

			upgradeSteps := generateUpgradeSteps(*stepSize, *maxSurge)
			for _, step := range upgradeSteps {
				// Behavior 1: Current state value matches the expected value or the RayService finished upgrading
				LogWithTimestamp(test.T(), "%s", step.name)
				g.Eventually(func(gg Gomega) {
					svc, err := GetRayService(test, namespace.Name, rayServiceName)
					gg.Expect(err).NotTo(HaveOccurred())
					gg.Expect(step.getValue(svc) == step.expectedValue || !IsRayServiceUpgrading(svc)).
						To(BeTrue())
				}, TestTimeoutMedium).Should(Succeed())

				// Behavior 2: Both old and new versions served traffic during the upgrade and no requests are dropped
				stdout, _ := CurlRayServiceGateway(test, gatewayIP, curlPod, CurlContainerName, http.MethodPost, "/fruit", `["MANGO", 2]`)
				response := stdout.String()
				g.Expect(response).To(Or(Equal("6"), Equal("8")), "Response should be from the old or new app version during the upgrade")
				if response == "6" {
					oldVersionServed = true
				}
				if response == "8" {
					newVersionServed = true
				}

				svc, err := GetRayService(test, namespace.Name, rayServiceName)
				g.Expect(err).NotTo(HaveOccurred())
				if !IsRayServiceUpgrading(svc) {
					break
				}

				// Behavior 3: Traffic is migrated with respect to the interval seconds
				if strings.Contains(step.name, "pending traffic to shift") {
					currentMigratedTime := GetLastTrafficMigratedTime(svc)
					g.Expect(currentMigratedTime).NotTo(BeNil())

					// Verify IntervalSeconds have passed since last TrafficRoutedPercent update.
					if lastMigratedTime != nil {
						duration := currentMigratedTime.Sub(lastMigratedTime.Time)
						intervalDuration := time.Duration(intervalSeconds) * time.Second
						g.Expect(duration).To(BeNumerically(">=", intervalDuration),
							"Time between traffic steps should be >= IntervalSeconds")
					}
					lastMigratedTime = currentMigratedTime
				}

				// Behavior 4: Active TargetCapacity is monotonically decreasing, and Pending TargetCapacity is monotonically increasing
				currentActiveCapacity := GetActiveCapacity(svc)
				currentPendingCapacity := GetPendingCapacity(svc)
				g.Expect(currentActiveCapacity).To(BeNumerically("<=", prevActiveCapacity),
					"TargetCapacity in active cluster must be monotonically decreasing: prev %d, curr %d", prevActiveCapacity, currentActiveCapacity)
				g.Expect(currentPendingCapacity).To(BeNumerically(">=", prevPendingCapacity),
					"TargetCapacity in pending cluster must be monotonically increasing: prev %d, curr %d", prevPendingCapacity, currentPendingCapacity)
				prevActiveCapacity = currentActiveCapacity
				prevPendingCapacity = currentPendingCapacity

				// Behavior 5: Active TrafficRoutedPercent is monotonically decreasing, and Pending TrafficRoutedPercent is monotonically increasing
				currentActiveTraffic := GetActiveTraffic(svc)
				currentPendingTraffic := GetPendingTraffic(svc)
				g.Expect(currentActiveTraffic).To(BeNumerically("<=", prevActiveTraffic),
					"TrafficRoutedPercent in active cluster must be monotonically decreasing: prev %d, curr %d", prevActiveTraffic, currentActiveTraffic)
				g.Expect(currentPendingTraffic).To(BeNumerically(">=", prevPendingTraffic),
					"TrafficRoutedPercent in pending cluster must be monotonically increasing: prev %d, curr %d", prevPendingTraffic, currentPendingTraffic)
				prevActiveTraffic = currentActiveTraffic
				prevPendingTraffic = currentPendingTraffic
			}

			LogWithTimestamp(test.T(), "Verifying both old and new versions served traffic during the upgrade")
			g.Expect(oldVersionServed).To(BeTrue(), "The old version of the service should have served traffic during the upgrade.")
			g.Expect(newVersionServed).To(BeTrue(), "The new version of the service should have served traffic during the upgrade.")

			// Check that RayService completed upgrade
			LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be false", rayService.Namespace, rayService.Name)
			g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeFalse()))

			LogWithTimestamp(test.T(), "Verifying RayService uses updated ServeConfig after upgrade completes")
			stdout, _ = CurlRayServiceGateway(test, gatewayIP, curlPod, CurlContainerName, http.MethodPost, "/fruit", `["MANGO", 2]`)
			g.Expect(stdout.String()).To(Equal("8"))
		})
	}
}

// TestRayServiceIncrementalUpgradeWithLocust tests the incremental upgrade flow of the RayService
// by running a Locust load test throughout the entire upgrade lifecycle:
//   - Initial serving on the old cluster
//   - During the upgrade phase
//   - Final serving on the new cluster
//
// It iterates over incrementalUpgradeCombinations to cover diverse upgrade scenarios under load:
//   - BlueGreen
//   - StandardGradual
//   - ConservativeGradual
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
// 9. Wait for Locust to ramp up and enter the steady state
// 10. Update the RayCluster spec and RayService serve config to trigger upgrade
//   - NOTE: Incremental upgrade is triggered by RayCluster spec changes, not serve config changes
//
// 11. Wait for incremental upgrade to complete
// 12. Ensure all remaining traffic is routed to the new cluster after upgrade completes
//
// NOTE: locust_runner.py validates whether the Locust load test completed with zero failures.
// So we don't need to validate this here.
func TestRayServiceIncrementalUpgradeWithLocust(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.RayServiceIncrementalUpgrade, true)

	for _, params := range incrementalUpgradeCombinations {
		t.Run(params.Name, func(t *testing.T) {
			test := With(t)
			g := NewWithT(t)

			namespace := test.NewTestNamespace()
			rayServiceName := "incremental-rayservice"
			stepSize, interval, maxSurge := params.ptrs()
			serveConfigV2 := highRPSServeConfigV2

			// Phase 1: Create RayService with incremental upgrade and wait for key components to be ready
			_, _, gatewayIP := bootstrapIncrementalRayService(test, g, namespace.Name, rayServiceName, stepSize, interval, maxSurge, serveConfigV2)

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
			locustHost := fmt.Sprintf("http://%s", gatewayIP)

			eg, _ := errgroup.WithContext(test.Ctx())
			eg.Go(func() error {
				LogWithTimestamp(test.T(), "Starting Locust load test against %s", locustHost)
				_, _, err := ExecPodCmdWithError(test, locustHeadPod, common.RayHeadContainer, []string{
					"python", "/locust-runner/locust_runner.py",
					"-f", "/locustfile/locustfile.py",
					"--host", locustHost,
				})
				return err
			})

			// Allow Locust to ramp up and send traffic to the old cluster before triggering upgrade.
			err = warmupLocust(test, locustHeadPod, locustWarmupRPSThreshold, locustWarmupStableWindowSeconds, locustWarmupTimeout)
			g.Expect(err).NotTo(HaveOccurred())

			// Phase 4: Trigger incremental upgrade
			LogWithTimestamp(test.T(), "Triggering incremental upgrade by updating RayCluster spec and RayService serve config")
			g.Eventually(incrementalUpgrade(test, namespace.Name, rayServiceName), TestTimeoutShort).Should(Succeed(), "Failed to update RayService to trigger upgrade")
			LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be true", namespace.Name, rayServiceName)
			g.Eventually(RayService(test, namespace.Name, rayServiceName), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeTrue()))

			// Phase 5: Wait for upgrade to complete and validate remaining traffic is routed to the new cluster
			// Since no additional validation is needed during the upgrade, we use a longer timeout.
			LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be false", namespace.Name, rayServiceName)
			g.Eventually(RayService(test, namespace.Name, rayServiceName), TestTimeoutMedium).Should(WithTransform(IsRayServiceUpgrading, BeFalse()))

			LogWithTimestamp(test.T(), "Waiting for Locust load test goroutine to finish")
			g.Expect(eg.Wait()).NotTo(HaveOccurred(), "Locust load test failed")

			LogWithTimestamp(test.T(), "Validating remaining traffic is routed to the new cluster after upgrade completes")
			curlPod, err := CreateCurlPod(g, test, CurlPodName, CurlContainerName, namespace.Name)
			g.Expect(err).NotTo(HaveOccurred())

			stdout, _ := CurlRayServiceGateway(test, gatewayIP, curlPod, CurlContainerName, http.MethodGet, "/test", "")
			var resp struct {
				Status string `json:"status"`
			}
			err = json.Unmarshal(stdout.Bytes(), &resp)
			g.Expect(err).NotTo(HaveOccurred(), "GET /test response should be valid JSON, got: %s", stdout.String())
			g.Expect(resp.Status).To(Equal("ok"), "GET /test status field should be ok, got: %s", resp.Status)
		})
	}
}

func TestRayServiceIncrementalUpgradeRollback(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.RayServiceIncrementalUpgrade, true)

	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()
	rayServiceName := "rollback-rayservice"

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

	// Copy original spec to use to trigger a rollback later.
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	originalSpec := rayService.Spec.DeepCopy()

	// Verify Gateway and HTTPRoute are ready.
	gatewayName := fmt.Sprintf("%s-%s", rayServiceName, "gateway")
	g.Eventually(Gateway(test, rayService.Namespace, gatewayName), TestTimeoutMedium).
		Should(WithTransform(utils.IsGatewayReady, BeTrue()))

	gateway, err := GetGateway(test, namespace.Name, fmt.Sprintf("%s-%s", rayServiceName, "gateway"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gateway).NotTo(BeNil())

	httpRouteName := fmt.Sprintf("%s-%s", rayServiceName, "httproute")
	LogWithTimestamp(test.T(), "Waiting for HTTPRoute %s/%s to be ready", rayService.Namespace, httpRouteName)
	g.Eventually(HTTPRoute(test, rayService.Namespace, httpRouteName), TestTimeoutMedium).
		Should(Not(BeNil()))

	httpRoute, err := GetHTTPRoute(test, namespace.Name, httpRouteName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(utils.IsHTTPRouteReady(gateway, httpRoute)).To(BeTrue())

	// Trigger an incremental upgrade through a change to the RayCluster spec.
	LogWithTimestamp(test.T(), "Triggering an upgrade for RayService %s/%s", rayService.Namespace, rayService.Name)
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("500m")
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s UpgradeInProgress condition to be true", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeTrue()))

	// Wait for the upgrade to be underway with traffic partially migrated.
	LogWithTimestamp(test.T(), "Waiting for upgrade to be partially complete")
	var pendingClusterName string
	g.Eventually(func(g Gomega) {
		svc, err := GetRayService(test, namespace.Name, rayServiceName)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(svc.Status.PendingServiceStatus.TrafficRoutedPercent).NotTo(BeNil())
		g.Expect(*svc.Status.PendingServiceStatus.TrafficRoutedPercent).Should(BeNumerically(">", 0))

		// Capture the pending cluster name before the rollback starts
		pendingClusterName = svc.Status.PendingServiceStatus.RayClusterName
		g.Expect(pendingClusterName).NotTo(BeEmpty())
	}, TestTimeoutMedium).Should(Succeed())

	// Trigger a rollback by updating the spec back to the original version.
	LogWithTimestamp(test.T(), "Triggering a rollback for RayService %s/%s", rayService.Namespace, rayService.Name)
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec = *originalSpec
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify that the controller enters the rollback state.
	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s RollbackInProgress condition to be true", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceRollingBack, BeTrue()))

	// Verify that traffic gradually shifts back to the active cluster.
	LogWithTimestamp(test.T(), "Verifying traffic shifts back to the active cluster")
	g.Eventually(func(g Gomega) {
		svc, err := GetRayService(test, namespace.Name, rayServiceName)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(svc.Status.ActiveServiceStatus.TrafficRoutedPercent).NotTo(BeNil())
		g.Expect(*svc.Status.ActiveServiceStatus.TrafficRoutedPercent).Should(Equal(int32(100)))
		g.Expect(svc.Status.PendingServiceStatus.TrafficRoutedPercent).NotTo(BeNil())
		g.Expect(*svc.Status.PendingServiceStatus.TrafficRoutedPercent).Should(Equal(int32(0)))
	}, TestTimeoutMedium).Should(Succeed())

	// Verify that the rollback completes and the pending cluster is cleaned up.
	LogWithTimestamp(test.T(), "Waiting for rollback to complete and pending cluster to be deleted")
	g.Eventually(func(g Gomega) {
		svc, err := GetRayService(test, namespace.Name, rayServiceName)
		g.Expect(err).NotTo(HaveOccurred())
		// Rollback is done when both conditions are false and pending status is empty.
		g.Expect(IsRayServiceRollingBack(svc)).To(BeFalse())
		g.Expect(IsRayServiceUpgrading(svc)).To(BeFalse())
		g.Expect(svc.Status.PendingServiceStatus.RayClusterName).To(BeEmpty())
	}, TestTimeoutMedium).Should(Succeed())

	// Check that the pending RayCluster resource is deleted.
	LogWithTimestamp(test.T(), "Verifying the pending RayCluster resource was deleted")
	g.Eventually(func() error {
		_, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Get(test.Ctx(), pendingClusterName, metav1.GetOptions{})
		return err
	}, TestTimeoutMedium).Should(WithTransform(errors.IsNotFound, BeTrue()))

	// The HTTPRoute should now only have one backend after the rollback completes.
	g.Eventually(HTTPRoute(test, namespace.Name, httpRouteName), TestTimeoutShort).
		Should(WithTransform(func(route *gwv1.HTTPRoute) int {
			if route == nil || len(route.Spec.Rules) == 0 {
				return 0
			}
			return len(route.Spec.Rules[0].BackendRefs)
		}, Equal(1)))
}
