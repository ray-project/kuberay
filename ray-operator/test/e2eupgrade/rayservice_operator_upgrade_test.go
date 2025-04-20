package e2eupgrade

import (
	"fmt"
	"os/exec"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	e2e "github.com/ray-project/kuberay/ray-operator/test/e2erayservice"
	"github.com/ray-project/kuberay/ray-operator/test/sampleyaml"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestZeroDowntimeUpgradeAfterOperatorUpgrade(t *testing.T) {
	// buildkite e2eupgrade installs the previous supported release of KubeRay.
	// This test installs a RayService, checks the connectivity of the RayService,
	// upgrades the Ray operator to the latest release, initiates a zero-downtime upgrade of
	// the RayService, while ensuring throughout the RayService can handle requests.

	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-sample"

	// Get the upgrade version from environment
	upgradeVersion := GetKubeRayUpgradeVersion()

	// Create RayService custom resource
	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(e2e.RayServiceSampleYamlApplyConfiguration())
	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	// Check RayService using deprecated ServiceStatus from previous release
	test.T().Logf("Waiting for RayService %s/%s to be running", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(RayServiceStatus, Equal(rayv1.Running)))

	// Get the latest RayService
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	// Create a curl Pod to query RayService
	curlPodName := "curl-pod"
	curlContainerName := "curl-container"

	test.T().Logf("Creating curl pod %s/%s", namespace.Name, curlPodName)
	curlPod, err := CreateCurlPod(test, curlPodName, curlContainerName, namespace.Name)
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(func(g Gomega) *corev1.Pod {
		updatedCurlPod, err := test.Client().Core().CoreV1().Pods(curlPod.Namespace).Get(test.Ctx(), curlPod.Name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		return updatedCurlPod
	}, TestTimeoutShort).Should(WithTransform(sampleyaml.IsPodRunningAndReady, BeTrue()))
	test.T().Logf("Curl pod %s/%s is running and ready", namespace.Name, curlPodName)

	// Validate RayService is able to serve requests
	test.T().Logf("Sending requests to the RayService to make sure it is ready to serve requests")
	g.Expect(requestRayService(test, rayService, curlPod, curlContainerName)).To(Equal("6, 15 pizzas please!"))

	// Validate RayService serve service correctly configured
	svcName := utils.GenerateServeServiceName(rayService.Name)
	test.T().Logf("Checking that the K8s serve service %s has exactly one endpoint because the cluster only has a head Pod", svcName)
	endpoints, err := test.Client().Core().CoreV1().Endpoints(namespace.Name).Get(test.Ctx(), svcName, metav1.GetOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(endpoints.Subsets).To(HaveLen(1))
	g.Expect(endpoints.Subsets[0].Addresses).To(HaveLen(1))

	// Upgrade KubeRay operator to latest version and replace CRDs
	test.T().Logf("Upgrading the KubeRay operator to the latest release")
	cmd := exec.Command("kubectl", "replace", "-k", fmt.Sprintf("github.com/ray-project/kuberay/ray-operator/config/crd?ref=%s", upgradeVersion)) //nolint:gosec // required for upgrade
	err = cmd.Run()
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(cmd, TestTimeoutShort).Should(WithTransform(ProcessStateSuccess, BeTrue()))
	cmd = exec.Command("helm", "upgrade", "kuberay-operator", "kuberay/kuberay-operator", "--version", upgradeVersion)
	err = cmd.Run()
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(cmd, TestTimeoutShort).Should(WithTransform(ProcessStateSuccess, BeTrue()))

	// Validate RayService is able to serve requests during the upgrade
	test.T().Logf("Sending requests to the RayService to make sure it is ready to serve requests")
	g.Consistently(requestRayService, "30s", "1s").WithArguments(test, rayService, curlPod, curlContainerName).Should(Equal("6, 15 pizzas please!"))

	// Trigger a zero-downtime upgrade of the RayService
	test.T().Logf("Upgrading the RayService to trigger a zero downtime upgrade")
	rayService, err = GetRayService(test, namespace.Name, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Env = append(
		rayService.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{
			Name:  "RAY_SERVE_PROXY_MIN_DRAINING_PERIOD_S",
			Value: "30",
		},
	)
	rayService, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	// Validate RayService performs upgrade and is reachable
	test.T().Logf("Waiting for RayService %s/%s UpgradeInProgress condition to be true", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeTrue()))

	test.T().Logf("Waiting for RayService %s/%s UpgradeInProgress condition to be false", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeFalse()))
	test.T().Logf("Waiting for RayService %s/%s Ready condition to be true because there is an endpoint", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceReady, BeTrue()))

	test.T().Logf("Sending requests to the RayService to make sure it is ready to serve requests")
	g.Expect(requestRayService(test, rayService, curlPod, curlContainerName)).To(Equal("6, 15 pizzas please!"))
}
