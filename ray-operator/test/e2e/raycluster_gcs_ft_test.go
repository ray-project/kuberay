package e2e

import (
	"testing"

	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
)

func TestRayClusterGCSFaultTolerence(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	testScriptAC := newConfigMap(namespace.Name, files(test, "test_detached_actor_1.py", "test_detached_actor_2.py"))

	testScript, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), testScriptAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = test.Client().Core().AppsV1().Deployments(namespace.Name).Apply(
		test.Ctx(),
		appsv1ac.Deployment("redis", namespace.Name).
			WithSpec(appsv1ac.DeploymentSpec().
				WithReplicas(1).
				WithSelector(metav1ac.LabelSelector().WithMatchLabels(map[string]string{"app": "redis"})).
				WithTemplate(corev1ac.PodTemplateSpec().
					WithLabels(map[string]string{"app": "redis"}).
					WithSpec(corev1ac.PodSpec().
						WithContainers(corev1ac.Container().
							WithName("redis").
							WithImage("redis:7.4").
							WithPorts(corev1ac.ContainerPort().WithContainerPort(6379)),
						),
					),
				),
			),
		TestApplyOptions,
	)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = test.Client().Core().CoreV1().Services(namespace.Name).Apply(
		test.Ctx(),
		corev1ac.Service("redis", namespace.Name).
			WithSpec(corev1ac.ServiceSpec().
				WithSelector(map[string]string{"app": "redis"}).
				WithPorts(corev1ac.ServicePort().
					WithPort(6379),
				),
			),
		TestApplyOptions,
	)
	g.Expect(err).NotTo(HaveOccurred())

	rayClusterAC := rayv1ac.RayCluster("raycluster-gcsft", namespace.Name).WithSpec(
		newRayClusterSpec((mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](testScript, "/home/ray/samples"))).WithGcsFaultToleranceOptions(
			rayv1ac.GcsFaultToleranceOptions().
				WithRedisAddress("redis:6379"),
		),
	)

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	// Make sure the RAY_REDIS_ADDRESS env is set on the Head Pod.
	g.Eventually(func(g Gomega) bool {
		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		if rayCluster.Status.Head.PodName != "" {
			headPod, err := test.Client().Core().CoreV1().Pods(namespace.Name).Get(test.Ctx(), rayCluster.Status.Head.PodName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			return utils.EnvVarExists(utils.RAY_REDIS_ADDRESS, headPod.Spec.Containers[utils.RayContainerIndex].Env)
		}
		return false
	}, TestTimeoutMedium).Should(BeTrue())

	test.T().Logf("Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterProvisioned), MatchCondition(metav1.ConditionTrue, rayv1.AllPodRunningAndReadyFirstTime)))

	test.T().Run("Test Detached Actor", func(_ *testing.T) {
		headPod, err := GetHeadPod(test, rayCluster)
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
		HeadPodContainerReady := func(p *corev1.Pod) bool { return p.Status.ContainerStatuses[0].Ready }

		g.Eventually(HeadPod(test, rayCluster), TestTimeoutMedium).
			Should(WithTransform(HeadPodRestartCount, Equal(int32(1))))
		g.Eventually(HeadPod(test, rayCluster), TestTimeoutMedium).
			Should(WithTransform(HeadPodContainerReady, Equal(true)))

		// Pos Status should eventually become Running
		PodState := func(p *corev1.Pod) string { return string(p.Status.Phase) }
		g.Eventually(HeadPod(test, rayCluster)).
			Should(WithTransform(PodState, Equal("Running")))

		headPod, err = GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())

		expectedOutput := "3"
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "samples/test_detached_actor_2.py", rayNamespace, expectedOutput})

		// Test 2: Delete the head Pod
		propagationPolicy := metav1.DeletePropagationBackground
		err = test.Client().Core().CoreV1().Pods(namespace.Name).Delete(test.Ctx(), headPod.Name, metav1.DeleteOptions{
			PropagationPolicy: &propagationPolicy,
		})
		g.Expect(err).NotTo(HaveOccurred())
		// Will get 2 head pods while one is terminating and another is creating, so wait until one is left
		g.Eventually(func() error {
			_, err := GetHeadPod(test, rayCluster)
			return err
		}, TestTimeoutMedium).ShouldNot(HaveOccurred())

		g.Eventually(HeadPod(test, rayCluster)).
			Should(WithTransform(PodState, Equal("Running")))

		// Then get new head pod and run verification
		headPod, err = GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(HaveOccurred())
		expectedOutput = "4"
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "samples/test_detached_actor_2.py", rayNamespace, expectedOutput})
	})
}
