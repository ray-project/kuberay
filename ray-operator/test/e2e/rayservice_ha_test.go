package e2e

import (
	"testing"

	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestStaticRayService(t *testing.T) {
	rayserviceYamlFile := "testdata/rayservice.static.yaml"
	locustYamlFile := "testdata/locust-cluster.const-rate.yaml"

	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Create a ConfigMap with Locust runner script
	configMapAC := newConfigMap(namespace.Name, "locust-runner-script", files(test, "locust_runner.py"))
	configMap, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), configMapAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", configMap.Namespace, configMap.Name)

	// Create the RayService for testing
	KubectlApplyYAML(test, rayserviceYamlFile, namespace.Name)
	rayService, err := GetRayService(test, namespace.Name, "test-rayservice")
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayService %s/%s successfully", rayService.Namespace, rayService.Name)

	test.T().Logf("Waiting for RayService %s/%s to running", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(RayServiceStatus, Equal(rayv1.Running)))

	// Create Locust RayCluster
	KubectlApplyYAML(test, locustYamlFile, namespace.Name)
	locustCluster, err := GetRayCluster(test, namespace.Name, "locust-cluster")
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created Locust RayCluster %s/%s successfully", locustCluster.Namespace, locustCluster.Name)

	g.Eventually(RayCluster(test, locustCluster.Namespace, locustCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	g.Expect(GetRayCluster(test, locustCluster.Namespace, locustCluster.Name)).To(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))

	headPod, err := GetHeadPod(test, locustCluster)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

	// Install Locust in the head Pod
	ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"pip", "install", "locust"})

	// Run Locust test
	ExecPodCmd(test, headPod, common.RayHeadContainer, []string{
		"python", "/locust-runner/locust_runner.py", "-f", "/locustfile/locustfile.py", "--host", "http://test-rayservice-serve-svc:8000",
	})
}

func TestAutoscalingRayService(t *testing.T) {
	rayserviceYamlFile := "testdata/rayservice.autoscaling.yaml"
	locustYamlFile := "testdata/locust-cluster.burst.yaml"

	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Create a ConfigMap with Locust runner script
	configMapAC := newConfigMap(namespace.Name, "locust-runner-script", files(test, "locust_runner.py"))
	configMap, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), configMapAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", configMap.Namespace, configMap.Name)

	// Create the RayService for testing
	KubectlApplyYAML(test, rayserviceYamlFile, namespace.Name)
	rayService, err := GetRayService(test, namespace.Name, "test-rayservice")
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayService %s/%s successfully", rayService.Namespace, rayService.Name)

	test.T().Logf("Waiting for RayService %s/%s to running", rayService.Namespace, rayService.Name)
	g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
		Should(WithTransform(RayServiceStatus, Equal(rayv1.Running)))

	// Create Locust RayCluster
	KubectlApplyYAML(test, locustYamlFile, namespace.Name)
	locustCluster, err := GetRayCluster(test, namespace.Name, "locust-cluster")
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created Locust RayCluster %s/%s successfully", locustCluster.Namespace, locustCluster.Name)

	g.Eventually(RayCluster(test, locustCluster.Namespace, locustCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	g.Expect(GetRayCluster(test, locustCluster.Namespace, locustCluster.Name)).To(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))

	headPod, err := GetHeadPod(test, locustCluster)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

	// Install Locust in the head Pod
	ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"pip", "install", "locust"})

	// Run Locust test
	ExecPodCmd(test, headPod, common.RayHeadContainer, []string{
		"python", "/locust-runner/locust_runner.py", "-f", "/locustfile/locustfile.py", "--host", "http://test-rayservice-serve-svc:8000",
	})
}
