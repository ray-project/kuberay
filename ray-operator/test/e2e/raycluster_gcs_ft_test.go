package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

const (
	redisPassword = "5241590000000000"
)

func TestRayClusterGCSFaultTolerence(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	testScriptAC := newConfigMap(namespace.Name, files(test, "test_detached_actor_1.py", "test_detached_actor_2.py"))
	testScript, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), testScriptAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	test.T().Run("Test Detached Actor", func(_ *testing.T) {
		checkRedisDBSize := deployRedis(test, namespace.Name, redisPassword)
		defer g.Eventually(checkRedisDBSize, time.Second*30, time.Second).Should(BeEquivalentTo("0"))

		rayClusterSpecAC := rayv1ac.RayClusterSpec().
			WithGcsFaultToleranceOptions(
				rayv1ac.GcsFaultToleranceOptions().
					WithRedisAddress("redis:6379").
					WithRedisPassword(rayv1ac.RedisCredential().WithValue(redisPassword)),
			).
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{
					"num-cpus": "0",
				}).
				WithTemplate(headPodTemplateApplyConfiguration()),
			).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithRayStartParams(map[string]string{
					"num-cpus": "1",
				}).
				WithGroupName("small-group").
				WithReplicas(1).
				WithMinReplicas(1).
				WithMaxReplicas(2).
				WithTemplate(workerPodTemplateApplyConfiguration()),
			)
		rayClusterAC := rayv1ac.RayCluster("raycluster-gcsft", namespace.Name).
			WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](testScript, "/home/ray/samples")))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)

		g.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		test.T().Logf("Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
		g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutLong).
			Should(WithTransform(StatusCondition(rayv1.RayClusterProvisioned), MatchCondition(metav1.ConditionTrue, rayv1.AllPodRunningAndReadyFirstTime)))

		headPod, err := GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())

		test.T().Logf("HeadPod Name: %s", headPod.Name)

		rayNamespace := "testing-ray-namespace"
		test.T().Logf("Ray namespace: %s", rayNamespace)

		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "samples/test_detached_actor_1.py", rayNamespace})

		// [Test 1: Kill GCS process to "restart" the head Pod]
		// Assertion is implement in python, so no furthur handling needed here, and so are other ExecPodCmd
		stdout, stderr := ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"pkill", "gcs_server"})
		t.Logf("pkill gcs_server output - stdout: %s, stderr: %s", stdout.String(), stderr.String())

		// Restart count should eventually become 1, not creating a new pod
		HeadPodRestartCount := func(p *corev1.Pod) int32 { return p.Status.ContainerStatuses[0].RestartCount }
		HeadPodContainerReady := func(p *corev1.Pod) bool { return p.Status.ContainerStatuses[0].Ready }

		g.Eventually(HeadPod(test, rayCluster), TestTimeoutMedium).
			Should(WithTransform(HeadPodRestartCount, Equal(int32(1))))
		g.Eventually(HeadPod(test, rayCluster), TestTimeoutMedium).
			Should(WithTransform(HeadPodContainerReady, Equal(true)))

		// Pod Status should eventually become Running
		PodState := func(p *corev1.Pod) string { return string(p.Status.Phase) }
		g.Eventually(HeadPod(test, rayCluster)).
			Should(WithTransform(PodState, Equal("Running")))

		headPod, err = GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())

		expectedOutput := "3"
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "samples/test_detached_actor_2.py", rayNamespace, expectedOutput})

		// Test 2: Delete the head Pod
		err = test.Client().Core().CoreV1().Pods(namespace.Name).Delete(test.Ctx(), headPod.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())

		testPodNameChanged := func(p *corev1.Pod) bool { return p.Name != headPod.Name }
		g.Eventually(HeadPod(test, rayCluster), TestTimeoutMedium).
			Should(WithTransform(testPodNameChanged, Equal(true)))

		g.Eventually(HeadPod(test, rayCluster), TestTimeoutMedium).
			Should(WithTransform(PodState, Equal("Running")))

		headPod, _ = GetHeadPod(test, rayCluster)
		expectedOutput = "4"

		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "samples/test_detached_actor_2.py", rayNamespace, expectedOutput})

		err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
		g.Expect(err).NotTo(HaveOccurred())
	})
}
