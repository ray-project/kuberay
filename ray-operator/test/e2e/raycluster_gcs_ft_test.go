package e2e

import (
	"os/exec"
	"testing"

	. "github.com/onsi/gomega"

	// metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// "k8s.io/utils/ptr"

	// k8serrors "k8s.io/apimachinery/pkg/api/errors"
	corev1 "k8s.io/api/core/v1"

	// "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayClusterGCSFaultTolerence(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	test.T().Log("Creating Cluster for GCSFaultTolerence testing.")
	yamlFilePath := "testdata/ray-cluster.ray-ft.yaml"
	rayClusterFromYaml := DeserializeRayClusterYAML(test, yamlFilePath)
	KubectlApplyYAML(test, yamlFilePath, namespace.Name)

	rayCluster, err := GetRayCluster(test, namespace.Name, rayClusterFromYaml.Name)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rayCluster).NotTo(BeNil())

	test.T().Logf("Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	test.T().Run("Test Detached Actor", func(t *testing.T) {
		headPod, err := GetHeadPod(test, rayClusterFromYaml)
		g.Expect(err).NotTo(HaveOccurred())

		test.T().Logf("HeadPod Name: %s", headPod.Name)

		rayNamespace := "testing-ray-namespace"
		test.T().Logf("Ray namespace: %s", rayNamespace)

		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "samples/test_detached_actor_1.py", rayNamespace})

		// [Test 1: Kill GCS process to "restart" the head Pod]
		// become running and ready, the RayCluster still needs tens of seconds
		// Hence, `test_detached_actor_2.py` will retry until a Ray client
		// connection succeeds.
		// Assert is implement in python, so no furthur handling needed here, and so are other ExecPodCmd
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"pkill", "gcs_server"})

		// Restart count should eventually become 1, not creating a new pod
		HeadPodRestartCount := func(p *corev1.Pod) int32 { return p.Status.ContainerStatuses[0].RestartCount }
		g.Eventually(HeadPod(test, rayCluster)).
			Should(WithTransform(HeadPodRestartCount, Equal(int32(1))))

		// Pos Status should eventually become Running
		PodState := func(p *corev1.Pod) string { return string(p.Status.Phase) }
		g.Eventually(HeadPod(test, rayCluster)).
			Should(WithTransform(PodState, Equal("Running")))

		headPod, err = GetHeadPod(test, rayClusterFromYaml)
		g.Expect(err).NotTo(HaveOccurred())

		expectedOutput := "3"
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "samples/test_detached_actor_2.py", rayNamespace, expectedOutput})

		// Test 2: Delete the head Pod
		kubectlCmd := exec.CommandContext(test.Ctx(), "kubectl", "delete", "pod", headPod.Name, "-n", namespace.Name)
		kubectlCmd.Run()

		// Will get 2 head pods while one is terminating and another is creating, so wait until one is left
		g.Eventually(func() error {
			_, err := GetHeadPod(test, rayClusterFromYaml)
			return err
		}, TestTimeoutMedium).ShouldNot(HaveOccurred())

		headPod, err = GetHeadPod(test, rayClusterFromYaml)
		g.Expect(err).NotTo(HaveOccurred())
		expectedOutput = "4"
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "samples/test_detached_actor_2.py", rayNamespace, expectedOutput})
	})

}
