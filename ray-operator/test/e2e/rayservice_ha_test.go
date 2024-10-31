package e2e

import (
	"testing"

	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayService(t *testing.T) {
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
	test.T().Run("Static RayService", func(_ *testing.T) {
		rayServiceAC := rayv1ac.RayService("static-raysvc", namespace.Name).
			WithSpec(rayv1ac.RayServiceSpec().
				WithServeConfigV2(`
proxy_location: EveryNode
applications:
- name: no_ops
  route_prefix: /
  import_path: microbenchmarks.no_ops:app_builder
  args:
    num_forwards: 0
  runtime_env:
    working_dir: https://github.com/ray-project/serve_workloads/archive/a2e2405f3117f1b4134b6924b5f44c4ff0710c00.zip
  deployments:
  - name: NoOp
    num_replicas: 2
    max_replicas_per_node: 1
    ray_actor_options:
      num_cpus: 1
`).
				WithRayClusterSpec(newRayClusterSpec()))

		rayService, err := test.Client().Ray().RayV1().RayServices(namespace.Name).Apply(test.Ctx(), rayServiceAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayService %s/%s successfully", rayService.Namespace, rayService.Name)

		test.T().Logf("Waiting for RayService %s/%s to running", rayService.Namespace, rayService.Name)
		g.Eventually(RayService(test, rayService.Namespace, rayService.Name), TestTimeoutMedium).
			Should(WithTransform(RayServiceStatus, Equal(rayv1.Running)))

		KubectlApplyYAML(test, "testdata/locust-cluster.const-rate.yaml", namespace.Name)
		locustCluster, err := GetRayCluster(test, namespace.Name, "locust-cluster")
		g.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created Locust RayCluster %s/%s successfully", locustCluster.Namespace, locustCluster.Name)

		// Wait for RayCluster to become ready and verify the number of available worker replicas.
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
			"python", "/locust-runner/locust_runner.py", "-f", "/locustfile/locustfile.py", "--host", "http://static-raysvc-serve-svc:8000",
		})
	})
}
