package e2eincrementalupgrade

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

// bootstrapIncrementalRayService creates a RayService with incremental upgrade enabled
// and waits for all required components to be ready, including:
//   - RayService
//   - Gateway
//   - HTTPRoute
//
// Parameters:
//   - stepSize: The percentage of traffic to shift from the old to the new cluster during each interval.
//   - interval: The time in seconds to wait between shifting traffic by stepSize.
//   - maxSurge: The percentage of capacity (Serve replicas) to add to the new cluster in each scaling step.
//   - serveConfigV2: The Serve config V2 to use for the RayService.
//
// Returns the RayService, HTTPRoute, and Gateway IP.
func bootstrapIncrementalRayService(
	test Test,
	g *WithT,
	namespace string,
	rayServiceName string,
	stepSize, interval, maxSurge *int32,
	serveConfigV2 serveConfigV2,
) (rayService *rayv1.RayService, httpRoute *gwv1.HTTPRoute, gatewayIP string) {
	var err error
	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace).
		WithSpec(incrementalUpgradeRayServiceApplyConfiguration(stepSize, interval, maxSurge, serveConfigV2))
	rayService, err = test.Client().Ray().RayV1().RayServices(namespace).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	LogWithTimestamp(test.T(), "Waiting for RayService %s/%s to be ready", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	gatewayName := fmt.Sprintf("%s-gateway", rayServiceName)
	LogWithTimestamp(test.T(), "Waiting for Gateway %s/%s to be ready", rayService.Namespace, gatewayName)
	g.Eventually(Gateway(test, rayService.Namespace, gatewayName), TestTimeoutMedium).
		Should(WithTransform(utils.IsGatewayReady, BeTrue()))

	var gateway *gwv1.Gateway
	gateway, err = GetGateway(test, rayService.Namespace, gatewayName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gateway).NotTo(BeNil())

	httpRouteName := fmt.Sprintf("%s-httproute", rayServiceName)
	LogWithTimestamp(test.T(), "Waiting for HTTPRoute %s/%s to be ready", rayService.Namespace, httpRouteName)
	g.Eventually(HTTPRoute(test, rayService.Namespace, httpRouteName), TestTimeoutMedium).
		Should(Not(BeNil()))

	g.Eventually(func() (bool, error) {
		httpRoute, err = GetHTTPRoute(test, rayService.Namespace, httpRouteName)
		return utils.IsHTTPRouteReady(gateway, httpRoute), err
	}, TestTimeoutMedium).Should(BeTrue())

	gatewayIP = GetGatewayIP(gateway)
	g.Expect(gatewayIP).NotTo(BeEmpty())

	return
}

// newLocustRunnerConfigMapAC creates a ConfigMap apply configuration for the Locust runner script.
func newLocustRunnerConfigMapAC(namespace string, options ...SupportOption[corev1ac.ConfigMapApplyConfiguration]) *corev1ac.ConfigMapApplyConfiguration {
	cmAC := corev1ac.ConfigMap("locust-runner-script", namespace).
		WithBinaryData(map[string][]byte{}).
		WithImmutable(true)

	return ConfigMapWith(cmAC, options...)
}

// CurlPodName and CurlContainerName are the default names used when creating a curl pod for gateway tests.
const (
	CurlPodName       = "curl-pod"
	CurlContainerName = "curl-container"
)

// CurlRayServiceGateway sends a request to the RayService Gateway using a Curl pod.
// This method currently supports POST and GET methods.
func CurlRayServiceGateway(
	t Test,
	gatewayIP string,
	curlPod *corev1.Pod,
	curlPodContainerName,
	method,
	rayServicePath,
	body string,
) (bytes.Buffer, bytes.Buffer) {
	cmd := []string{
		"curl",
		"--max-time", "10",
		"-X", method,
		"-H", "Connection: close", // avoid re-using the same connection for test
		"-H", "Content-Type: application/json",
		fmt.Sprintf("http://%s%s", gatewayIP, rayServicePath),
	}
	if body != "" {
		cmd = append(cmd, "-d", body)
	}

	return ExecPodCmd(t, curlPod, curlPodContainerName, cmd)
}

func incrementalUpgradeRayServiceApplyConfiguration(
	stepSizePercent, intervalSeconds, maxSurgePercent *int32,
	serveConfigV2 serveConfigV2,
) *rayv1ac.RayServiceSpecApplyConfiguration {
	return rayv1ac.RayServiceSpec().
		WithUpgradeStrategy(rayv1ac.RayServiceUpgradeStrategy().
			WithType(rayv1.RayServiceNewClusterWithIncrementalUpgrade).
			WithClusterUpgradeOptions(
				rayv1ac.ClusterUpgradeOptions().
					WithGatewayClassName("istio").
					WithStepSizePercent(*stepSizePercent).
					WithIntervalSeconds(*intervalSeconds).
					WithMaxSurgePercent(*maxSurgePercent),
			)).
		WithServeConfigV2(string(serveConfigV2)).
		WithRayClusterSpec(rayv1ac.RayClusterSpec().
			WithRayVersion(GetRayVersion()).
			WithEnableInTreeAutoscaling(true).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				// Prevent actors from being scheduled on the Ray head node.
				WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0", "num-cpus": "0"}).
				WithTemplate(corev1ac.PodTemplateSpec().
					WithSpec(corev1ac.PodSpec().
						WithRestartPolicy(corev1.RestartPolicyNever).
						WithContainers(corev1ac.Container().
							WithName("ray-head").
							WithImage(GetRayImage()).
							WithEnv(corev1ac.EnvVar().WithName(utils.RAY_ENABLE_AUTOSCALER_V2).WithValue("1")).
							WithPorts(
								corev1ac.ContainerPort().WithName(utils.GcsServerPortName).WithContainerPort(utils.DefaultGcsServerPort),
								corev1ac.ContainerPort().WithName(utils.ServingPortName).WithContainerPort(utils.DefaultServingPort),
								corev1ac.ContainerPort().WithName(utils.DashboardPortName).WithContainerPort(utils.DefaultDashboardPort),
								corev1ac.ContainerPort().WithName(utils.ClientPortName).WithContainerPort(utils.DefaultClientPort),
							))))).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(1).
				WithMinReplicas(1).
				WithMaxReplicas(2).
				WithGroupName("small-group").
				WithTemplate(corev1ac.PodTemplateSpec().
					WithSpec(corev1ac.PodSpec().
						WithRestartPolicy(corev1.RestartPolicyNever).
						WithContainers(corev1ac.Container().
							WithName("ray-worker").
							WithImage(GetRayImage()).
							WithResources(corev1ac.ResourceRequirements().
								WithRequests(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								}).
								WithLimits(corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								})))))),
		)
}

// GetGatewayIP retrieves the external IP for a Gateway object
func GetGatewayIP(gateway *gwv1.Gateway) string {
	if gateway == nil {
		return ""
	}
	for _, addr := range gateway.Status.Addresses {
		if addr.Type == nil || *addr.Type == gwv1.IPAddressType {
			return addr.Value
		}
	}

	return ""
}

// incrementalUpgrade is a wrapper around triggerIncrementalUpgrade that returns a function that can be used with g.Eventually.
func incrementalUpgrade(test Test, namespace, rayServiceName string) func() error {
	return func() error {
		return triggerIncrementalUpgrade(test, namespace, rayServiceName)
	}
}

// triggerIncrementalUpgrade updates the RayService to trigger an incremental upgrade:
//   - RayCluster spec: Set worker CPU request to 500m
//   - Serve config: Update (price 3->4, factor 5->3)
func triggerIncrementalUpgrade(test Test, namespace, rayServiceName string) error {
	rayService, err := GetRayService(test, namespace, rayServiceName)
	if err != nil {
		return err
	}

	// Update the RayCluster spec.
	rayService.Spec.RayClusterSpec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("500m")

	// Update the serve config.
	serveConfig := rayService.Spec.ServeConfigV2
	serveConfig = strings.ReplaceAll(serveConfig, "price: 3", "price: 4")
	serveConfig = strings.ReplaceAll(serveConfig, "factor: 5", "factor: 3")
	rayService.Spec.ServeConfigV2 = serveConfig

	_, err = test.Client().Ray().RayV1().RayServices(namespace).Update(
		test.Ctx(),
		rayService,
		metav1.UpdateOptions{},
	)
	return err
}

func GetPendingCapacity(rs *rayv1.RayService) int32 {
	return ptr.Deref(rs.Status.PendingServiceStatus.TargetCapacity, 0)
}

func GetPendingTraffic(rs *rayv1.RayService) int32 {
	return ptr.Deref(rs.Status.PendingServiceStatus.TrafficRoutedPercent, 0)
}

func GetActiveCapacity(rs *rayv1.RayService) int32 {
	return ptr.Deref(rs.Status.ActiveServiceStatus.TargetCapacity, 100)
}

func GetActiveTraffic(rs *rayv1.RayService) int32 {
	return ptr.Deref(rs.Status.ActiveServiceStatus.TrafficRoutedPercent, 100)
}

func GetLastTrafficMigratedTime(rs *rayv1.RayService) *metav1.Time {
	return rs.Status.PendingServiceStatus.LastTrafficMigratedTime
}

// testStep defines a validation condition to wait for during the upgrade.
type testStep struct {
	getValue      func(rs *rayv1.RayService) int32
	name          string
	expectedValue int32
}

// generateUpgradeSteps is a helper function for testing that the controller follows the expected
// sequence of updates to TrafficRoutedPercent and TargetCapacity during an incremental upgrade.
func generateUpgradeSteps(stepSize, maxSurge int32) []testStep {
	var steps []testStep

	pendingCapacity := int32(0)
	pendingTraffic := int32(0)
	activeCapacity := int32(100)
	activeTraffic := int32(100)

	for pendingTraffic < 100 {
		// Scale up the pending cluster's TargetCapacity.
		if pendingTraffic == pendingCapacity {
			nextPendingCapacity := min(pendingCapacity+maxSurge, 100)
			if nextPendingCapacity > pendingCapacity {
				steps = append(steps, testStep{
					name:          fmt.Sprintf("Waiting for pending capacity to scale up to %d", nextPendingCapacity),
					getValue:      GetPendingCapacity,
					expectedValue: nextPendingCapacity,
				})
				pendingCapacity = nextPendingCapacity
			}
		}

		// Shift traffic over from the active to the pending cluster by StepSizePercent.
		for pendingTraffic < pendingCapacity {
			nextPendingTraffic := min(pendingTraffic+stepSize, 100)
			steps = append(steps, testStep{
				name:          fmt.Sprintf("Waiting for pending traffic to shift to %d", nextPendingTraffic),
				getValue:      GetPendingTraffic,
				expectedValue: nextPendingTraffic,
			})
			pendingTraffic = nextPendingTraffic

			nextActiveTraffic := max(activeTraffic-stepSize, 0)
			steps = append(steps, testStep{
				name:          fmt.Sprintf("Waiting for active traffic to shift down to %d", nextActiveTraffic),
				getValue:      GetActiveTraffic,
				expectedValue: nextActiveTraffic,
			})
			activeTraffic = nextActiveTraffic
		}

		// Scale down the active cluster's target capacity. The final scale
		// down is when the pending cluster is promoted to active.
		nextActiveCapacity := max(activeCapacity-maxSurge, 0)
		if nextActiveCapacity < activeCapacity && nextActiveCapacity > 0 {
			steps = append(steps, testStep{
				name:          fmt.Sprintf("Waiting for active capacity to scale down to %d", nextActiveCapacity),
				getValue:      GetActiveCapacity,
				expectedValue: nextActiveCapacity,
			})
			activeCapacity = nextActiveCapacity
		}
	}
	return steps
}

const (
	// The lower bound of the RPS for the Locust to reach the steady state.
	locustWarmupRPSThreshold = 400.0
	// The period of time that the RPS must be greater than or equal to the threshold to be considered steady state.
	locustWarmupStableWindowSeconds = 15
	// The maximum duration to wait for the Locust to reach the steady state.
	locustWarmupTimeout = 120 * time.Second
)

// warmupLocust waits for Locust to ramp up and enter the steady state before triggering upgrade.
// Hence, all requests are sent to the old cluster during the warmup period.
//
// The warmup period follows these steps:
// 1. Retrieve the index of the Requests/s column in the stats history file
//   - Retry for cases where the stats history file is not written yet
//
// 2. Query the current RPS from the stats history file
//   - Retry for cases where the query failed
//
// 3. Check if the last RPS is greater than or equal to the threshold for the specified duration
//   - Determine if the Locust has reached the steady state
func warmupLocust(
	test Test,
	locustHeadPod *corev1.Pod,
	rpsThreshold float64,
	stableWindow int,
	timeout time.Duration,
) error {
	// rpsColIdxResult is the result of getting the index of the Requests/s column in the stats history file.
	//   - index: the index of the Requests/s column (the valid index should be >= 0)
	//   - ready: false when the file or column is not ready, a retry is needed
	type rpsColIdxResult struct {
		index int
		ready bool
	}

	// getRpsColIdx gets the index of the Requests/s column in the stats history file.
	getRpsColIdx := func() (rpsColIdxResult, error) {
		stdout, stderr := ExecPodCmd(test, locustHeadPod, common.RayHeadContainer, []string{
			"bash", "-lc", `
latest=$(ls /home/ray/locust_results/test_stats_history.csv 2>/dev/null | head -n 1) || exit 1
head -n1 "$latest"
		`,
		}, true)
		if stderr.Len() != 0 || stdout.Len() == 0 {
			return rpsColIdxResult{ready: false}, nil
		}

		header := strings.TrimSpace(stdout.String())
		cols := strings.Split(header, ",")
		idx := slices.Index(cols, "Requests/s")
		if idx == -1 {
			return rpsColIdxResult{}, fmt.Errorf("Requests/s column not found in stats history file")
		}
		return rpsColIdxResult{
			index: idx,
			ready: true,
		}, nil
	}

	var rpsIdx int
	var haveRpsColIdx bool

	ddl := time.Now().Add(timeout)
	stableCount := 0
	for time.Now().Before(ddl) {
		if !haveRpsColIdx {
			rpsColIdxRes, err := getRpsColIdx()
			if err != nil {
				return err
			}

			if !rpsColIdxRes.ready {
				test.T().Logf("failed to find header in stats history file, retrying in 2 seconds")
				time.Sleep(2 * time.Second)
				continue
			}

			rpsIdx = rpsColIdxRes.index
			haveRpsColIdx = true
			test.T().Logf("found Requests/s column at index: %d", rpsIdx)
		}

		stdout, stderr := ExecPodCmd(test, locustHeadPod, common.RayHeadContainer, []string{
			"bash", "-lc", `
		latest=$(ls /home/ray/locust_results/test_stats_history.csv 2>/dev/null | head -n1) || exit 1
		tail -n1 "$latest"
		`,
		}, true)
		if stderr.Len() != 0 || stdout.Len() == 0 {
			test.T().Logf("failed to query current RPS from Locust, retrying in 2 seconds: %s", stderr.String())
			time.Sleep(2 * time.Second)
			continue
		}

		lastStats := strings.TrimSpace(stdout.String())
		statsSlice := strings.Split(lastStats, ",")
		// Sometimes, we get the last stats with only 4 numbers, which leads to RPS index out of range.
		// This is a temporary workaround. We will fix this in the future.
		if len(statsSlice) <= rpsIdx {
			test.T().Logf("RPS index out of range, retrying in 2 seconds")
			time.Sleep(2 * time.Second)
			continue
		}

		rps := statsSlice[rpsIdx]
		rpsFloat, err := strconv.ParseFloat(rps, 64)
		if err != nil {
			test.T().Logf("failed to parse RPS, retrying in 2 seconds: %s", err.Error())
			time.Sleep(2 * time.Second)
			continue
		}

		if rpsFloat >= rpsThreshold {
			stableCount++
		} else {
			stableCount = 0
		}
		if stableCount >= stableWindow {
			test.T().Logf("Locust has reached the steady state with RPS >= %.2f for %d seconds", rpsThreshold, stableWindow)
			return nil
		}

		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("timeout waiting for Locust to reach the steady state with RPS >= %.2f", rpsThreshold)
}
