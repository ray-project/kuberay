package e2e

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/test/sampleyaml"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestOldHeadPodFailDuringUpgrade(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	rayServiceName := "rayservice-sample"

	rayServiceAC := rayv1ac.RayService(rayServiceName, namespace.Name).WithSpec(rayServiceSampleYamlApplyConfiguration())

	rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	test.T().Logf("Waiting for RayService %s/%s to running", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(IsRayServiceReady, BeTrue()))

	// Get the latest RayService
	rayService, err = GetRayService(test, namespace.Name, rayServiceName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayService).NotTo(BeNil())

	curlPodName := "curl-pod"
	curlContainerName := "curl"

	test.T().Logf("Creating curl pod %s/%s", namespace.Name, curlPodName)

	curlPod, err := CreateCurlPod(test, curlPodName, curlContainerName, namespace.Name)
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(func(g Gomega) *corev1.Pod {
		updatedCurlPod, err := test.Client().Core().CoreV1().Pods(curlPod.Namespace).Get(test.Ctx(), curlPod.Name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		return updatedCurlPod
	}, TestTimeoutShort).Should(WithTransform(sampleyaml.IsPodRunningAndReady, BeTrue()))
	test.T().Logf("Curl pod %s/%s is running and ready", namespace.Name, curlPodName)

	test.T().Logf("Sending requests to the RayService to make sure it is ready to serve requests")
	stdout, _ := curlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))
	stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
	g.Expect(stdout.String()).To(Equal("15 pizzas please!"))

	svcName := utils.GenerateServeServiceName(rayService.Name)
	test.T().Logf("Checking that the K8s serve service %s has exactly one endpoint because the cluster only has a head Pod", svcName)
	endpoints, err := test.Client().Core().CoreV1().Endpoints(namespace.Name).Get(test.Ctx(), svcName, metav1.GetOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(endpoints.Subsets).To(HaveLen(1))
	g.Expect(endpoints.Subsets[0].Addresses).To(HaveLen(1))
	headPodName := endpoints.Subsets[0].Addresses[0].TargetRef.Name

	test.T().Logf("Upgrading the RayService to trigger a zero downtime upgrade")
	rayService, err = GetRayService(test, namespace.Name, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())
	rayService.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.InitContainers = append(
		rayService.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.InitContainers,
		corev1.Container{
			Name:    "sleep",
			Image:   rayService.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Image,
			Command: []string{"/bin/bash"},
			// Intentionally make the new pending cluster sleep for a while to give us
			// time to capture the iptables manipulation below.
			Args: []string{"-c", "sleep 30"},
		},
	)
	rayService, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	test.T().Logf("Using iptables to make the ProxyActor(8000 port) fail to receive requests on the old head Pod")
	headPodPatch := map[string]interface{}{
		"spec": map[string]interface{}{
			"ephemeralContainers": []corev1.EphemeralContainer{
				{
					EphemeralContainerCommon: corev1.EphemeralContainerCommon{
						Name:    "proxy-actor-drop",
						Image:   "istio/iptables",
						Command: []string{"iptables"},
						Args:    []string{"-A", "INPUT", "-p", "tcp", "--dport", "8000", "-j", "DROP"},
						SecurityContext: &corev1.SecurityContext{
							Privileged: ptr.To(true),
							RunAsUser:  ptr.To(int64(0)),
						},
					},
				},
			},
		},
	}
	patchBytes, err := json.Marshal(headPodPatch)
	g.Expect(err).NotTo(HaveOccurred())
	headPod, err := test.Client().Core().CoreV1().Pods(namespace.Name).Patch(test.Ctx(), headPodName, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{}, "ephemeralcontainers")
	g.Expect(err).NotTo(HaveOccurred())

	test.T().Logf("Checking that the old Head Pod's label `ray.io/serve` is `true`")
	g.Expect(headPod.Labels[utils.RayClusterServingServiceLabelKey]).To(Equal("true"))

	test.T().Logf("Checking that the old Head Pod's label `ray.io/serve` is `false` because it is not healthy")
	g.Eventually(func(g Gomega) string {
		headPod, err := test.Client().Core().CoreV1().Pods(namespace.Name).Get(test.Ctx(), headPodName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		return headPod.Labels[utils.RayClusterServingServiceLabelKey]
	}, TestTimeoutShort).Should(Equal("false"))

	test.T().Logf("Waiting for RayService %s/%s UpgradeInProgress condition to be true", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeTrue()))
	test.T().Logf("Waiting for RayService %s/%s Ready condition to be false because there is no endpoint", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceReady, BeFalse()))

	test.T().Logf("Checking that the K8s serve service eventually has 1 endpoint and the endpoint is not the old head Pod")
	g.Eventually(func(g Gomega) {
		endpoints, err = test.Client().Core().CoreV1().Endpoints(namespace.Name).Get(test.Ctx(), svcName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(endpoints.Subsets).To(HaveLen(1))
		g.Expect(endpoints.Subsets[0].Addresses).To(HaveLen(1))
		g.Expect(endpoints.Subsets[0].Addresses[0].TargetRef.Name).NotTo(Equal(headPodName))
	}, TestTimeoutMedium).Should(Succeed())

	test.T().Logf("Waiting for RayService %s/%s UpgradeInProgress condition to be false", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceUpgrading, BeFalse()))
	test.T().Logf("Waiting for RayService %s/%s Ready condition to be true because there is an endpoint", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutShort).Should(WithTransform(IsRayServiceReady, BeTrue()))

	test.T().Logf("Sending requests to the RayService to make sure it is ready to serve requests")
	stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
	g.Expect(stdout.String()).To(Equal("6"))
	stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
	g.Expect(stdout.String()).To(Equal("15 pizzas please!"))
}
