package e2eincrementalupgrade

import (
	"fmt"
	"net/http"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayServiceSuspendDuringIncrementalUpgrade(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.RayServiceIncrementalUpgrade, true)

	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()
	rayServiceName := "suspend-incremental-rayservice"

	stepSize := new(int32(100))
	interval := new(int32(1))
	maxSurge := new(int32(100))

	rayService, httpRoute, _ := bootstrapIncrementalRayService(
		test, g, namespace.Name, rayServiceName, stepSize, interval, maxSurge, defaultIncrementalUpgradeServeConfigV2)
	gatewayName := fmt.Sprintf("%s-gateway", rayServiceName)
	// Curl through the backing Istio Service's cluster DNS rather than the
	// LoadBalancer-assigned external IP. The DNS handle is stable across
	// Gateway recreation (Istio names the backing Deployment + Service
	// `<gateway-name>-istio` in the Gateway's namespace), so the assertions
	// after Spec.Suspend cycles through don't need to re-resolve a possibly-
	// new external IP. The traffic still flows through the Gateway envoy
	// pods and exercises the HTTPRoute / weighted-backend routing.
	gatewayHost := fmt.Sprintf("%s-istio.%s.svc.cluster.local", gatewayName, namespace.Name)

	// Sanity-check that the service is wired up through the Gateway before
	// touching Spec.Suspend.
	curlPod, err := CreateCurlPod(g, test, CurlPodName, CurlContainerName, namespace.Name)
	g.Expect(err).NotTo(HaveOccurred())
	stdout, _ := CurlRayServiceGateway(test, gatewayHost, curlPod, CurlContainerName, http.MethodPost, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))

	// Trigger an incremental upgrade so both active and pending RayClusters
	// exist when Spec.Suspend kicks in.
	LogWithTimestamp(test.T(), "Triggering incremental upgrade so both active and pending clusters exist before suspend")
	g.Eventually(incrementalUpgrade(test, namespace.Name, rayServiceName), TestTimeoutShort).Should(Succeed())
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceUpgrading, BeTrue()))
	g.Eventually(func() string {
		rs, err := GetRayService(test, namespace.Name, rayServiceName)
		if err != nil {
			return ""
		}
		return rs.Status.PendingServiceStatus.RayClusterName
	}, TestTimeoutMedium).ShouldNot(BeEmpty())

	LogWithTimestamp(test.T(), "Setting Spec.Suspend=true while incremental upgrade is in progress")
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec.Suspend = true
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for the Suspended condition to be True")
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceSuspended, BeTrue()))

	LogWithTimestamp(test.T(), "Gateway and HTTPRoute must be deleted alongside RayClusters and Services")
	g.Eventually(func(gg Gomega) {
		_, err := GetGateway(test, namespace.Name, gatewayName)
		gg.Expect(errors.IsNotFound(err)).To(BeTrue(), "Gateway %s should be deleted; got err=%v", gatewayName, err)

		_, err = GetHTTPRoute(test, namespace.Name, httpRoute.Name)
		gg.Expect(errors.IsNotFound(err)).To(BeTrue(), "HTTPRoute %s should be deleted; got err=%v", httpRoute.Name, err)

		rcList, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).List(test.Ctx(), metav1.ListOptions{
			LabelSelector: utils.RayOriginatedFromCRNameLabelKey + "=" + rayServiceName,
		})
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(rcList.Items).To(BeEmpty(), "All RayClusters owned by the RayService should be deleted")

		svcList, err := test.Client().Core().CoreV1().Services(namespace.Name).List(test.Ctx(), metav1.ListOptions{
			LabelSelector: utils.RayOriginatedFromCRNameLabelKey + "=" + rayServiceName,
		})
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(svcList.Items).To(BeEmpty(), "All Services owned by the RayService should be deleted")
	}, TestTimeoutMedium).Should(Succeed())

	LogWithTimestamp(test.T(), "Setting Spec.Suspend=false; the controller must recreate Gateway, HTTPRoute, RayCluster, and Services")
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec.Suspend = false
	_, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).
		Should(WithTransform(IsRayServiceSuspended, BeFalse()))

	// Gateway and HTTPRoute must come back, and the resumed Gateway must
	// serve traffic again so we know the network path is fully restored
	// rather than just the K8s objects re-existing.
	LogWithTimestamp(test.T(), "Waiting for Gateway %s/%s to be ready again", namespace.Name, gatewayName)
	g.Eventually(Gateway(test, namespace.Name, gatewayName), TestTimeoutMedium).
		Should(WithTransform(utils.IsGatewayReady, BeTrue()))

	LogWithTimestamp(test.T(), "Waiting for HTTPRoute %s/%s to be ready again", namespace.Name, httpRoute.Name)
	g.Eventually(func() (bool, error) {
		gw, err := GetGateway(test, namespace.Name, gatewayName)
		if err != nil {
			return false, err
		}
		route, err := GetHTTPRoute(test, namespace.Name, httpRoute.Name)
		if err != nil {
			return false, err
		}
		return utils.IsHTTPRouteReady(gw, route), nil
	}, TestTimeoutMedium).Should(BeTrue())

	// Resume runs against the upgraded spec — incrementalUpgrade() bumped
	// MangoStand price 3→4 via the API before Spec.Suspend was flipped, so
	// (MANGO, 2) -> 8, not 6. This also doubles as a check that resume
	// applied the upgraded spec rather than reviving the pre-upgrade state.
	LogWithTimestamp(test.T(), "Verifying the resumed RayService serves traffic through the recreated Gateway with the upgraded spec")
	curlCmd := []string{
		"curl", "-sS", "--connect-timeout", "3", "--max-time", "5",
		"-X", "POST",
		"-H", "Connection: close",
		"-H", "Content-Type: application/json",
		fmt.Sprintf("http://%s/fruit", gatewayHost),
		"-d", `["MANGO", 2]`,
	}
	g.Eventually(func(gg Gomega) {
		stdout, _, err := ExecPodCmdWithError(test, curlPod, CurlContainerName, curlCmd)
		gg.Expect(err).NotTo(HaveOccurred())
		gg.Expect(stdout.String()).To(Equal("8"))
	}, TestTimeoutMedium).Should(Succeed())
}
