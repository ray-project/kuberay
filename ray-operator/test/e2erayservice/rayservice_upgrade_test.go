package e2e

import (
	"encoding/json"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/test/sampleyaml"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayServiceUpgrade(t *testing.T) {
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
		Should(WithTransform(RayServiceStatus, Equal(rayv1.Running)))

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

	// test the default curl result
	g.Eventually(func(g Gomega) {
		// curl /fruit
		stdout, _ := curlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
		g.Expect(stdout.String()).To(Equal("6"))
		// curl /calc
		stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
		g.Expect(stdout.String()).To(Equal("15 pizzas please!"))
	}, TestTimeoutShort).Should(Succeed())

	// After the above curl test, now the RayService is ready.
	// Get the Endpoints object for the later usage.
	svcName, err := utils.GenerateHeadServiceName(utils.RayServiceCRD, rayService.Spec.RayClusterSpec, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())
	endpoints, err := test.Client().Core().CoreV1().Endpoints(namespace.Name).Get(test.Ctx(), svcName, metav1.GetOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(endpoints.Subsets).To(HaveLen(1))
	g.Expect(endpoints.Subsets[0].Addresses).To(HaveLen(1))

	// upgrade the cluster and the serve application
	// Parse ServeConfigV2 and replace the string in the simplest way to update it.
	rayService, err = GetRayService(test, namespace.Name, rayService.Name)
	g.Expect(err).NotTo(HaveOccurred())

	serveConfig := rayService.Spec.ServeConfigV2
	serveConfig = strings.Replace(serveConfig, "price: 3", "price: 4", -1)
	serveConfig = strings.Replace(serveConfig, "factor: 5", "factor: 3", -1)

	// modify InitContainers to trigger a zero downtime upgrade.
	rayService.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.InitContainers = append(
		rayService.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.InitContainers,
		corev1.Container{
			Name:    "sleep",
			Image:   rayService.Spec.RayClusterSpec.HeadGroupSpec.Template.Spec.Containers[0].Image,
			Command: []string{"/bin/bash"},
			// intentionally make the new pending cluster sleep for a while for us
			// to have time to capture the below iptables manipulation.
			Args: []string{"-c", "sleep 30"},
		},
	)
	rayService.Spec.ServeConfigV2 = serveConfig
	rayService, err = test.Client().Ray().RayV1().RayServices(namespace.Name).Update(test.Ctx(), rayService, metav1.UpdateOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	// use iptables to make the ProxyActor(8000 port) fail on the old Head.
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
							Privileged: ptr.To[bool](true),
							RunAsUser:  ptr.To[int64](0),
						},
					},
				},
			},
		},
	}
	patchBytes, err := json.Marshal(headPodPatch)
	g.Expect(err).NotTo(HaveOccurred())
	headPodName := endpoints.Subsets[0].Addresses[0].TargetRef.Name
	headPod, err := test.Client().Core().CoreV1().Pods(namespace.Name).Patch(test.Ctx(), headPodName, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{}, "ephemeralcontainers")
	g.Expect(err).NotTo(HaveOccurred())
	// the "ray.io/serve" label on the old Head should be "true" first,
	g.Expect(headPod.Labels[utils.RayClusterServingServiceLabelKey]).To(Equal("true"))
	// then be turned to "false" and it should also not be in the new Endpoints.
	g.Eventually(func(g Gomega) string {
		headPod, err := test.Client().Core().CoreV1().Pods(namespace.Name).Get(test.Ctx(), headPodName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		return headPod.Labels[utils.RayClusterServingServiceLabelKey]
	}, TestTimeoutShort).Should(Equal("false"))
	g.Eventually(func(g Gomega) {
		endpoints, err = test.Client().Core().CoreV1().Endpoints(namespace.Name).Get(test.Ctx(), svcName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(endpoints.Subsets).To(HaveLen(1))
		g.Expect(endpoints.Subsets[0].Addresses).To(HaveLen(1))
		g.Expect(endpoints.Subsets[0].Addresses[0].TargetRef.Name).NotTo(Equal(headPodName))
	}, TestTimeoutShort).Should(Succeed())

	// Test the upgraded Ray Serve application.
	g.Eventually(func(g Gomega) {
		// curl /fruit
		stdout, _ := curlRayServicePod(test, rayService, curlPod, curlContainerName, "/fruit", `["MANGO", 2]`)
		g.Expect(stdout.String()).To(Equal("8"))
		// curl /calc
		stdout, _ = curlRayServicePod(test, rayService, curlPod, curlContainerName, "/calc", `["MUL", 3]`)
		g.Expect(stdout.String()).To(Equal("9 pizzas please!"))
	}, TestTimeoutShort).Should(Succeed())
}
