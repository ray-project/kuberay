package e2e

import (
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

const (
	rayImage   = "rayproject/ray:2.40.0"
	rayVersion = "2.40.0"
)

func TestRayClusterGCSFaultTolerence(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	testScriptAC := newConfigMap(namespace.Name, files(test, "test_detached_actor_1.py", "test_detached_actor_2.py"))
	testScriptCM, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), testScriptAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	redisDM := newRedisDeployment(namespace.Name)

	_, err = test.Client().Core().AppsV1().Deployments(namespace.Name).Apply(test.Ctx(), redisDM, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	redisSvc := newRedisService(namespace.Name)

	_, err = test.Client().Core().CoreV1().Services(namespace.Name).Apply(test.Ctx(), redisSvc, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	test.T().Run("Test Detached Actor", func(_ *testing.T) {
		rayClusterAC := rayv1ac.RayCluster("raycluster-gcsft", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/ft-enabled": "true"}).
		WithSpec(
			newRayClusterSpec(mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](testScriptCM, "/home/ray/samples")).
				WithGcsFaultToleranceOptions(
					rayv1ac.GcsFaultToleranceOptions().WithRedisAddress("redis:6379")).
				WithRayVersion(rayVersion).
				WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
					WithRayStartParams(map[string]string{
						"num-cpus":       "0",
						"redis-password": "5241590000000000",
					}).
					WithTemplate(corev1ac.PodTemplateSpec().
						WithSpec(corev1ac.PodSpec().
							WithContainers(corev1ac.Container().
								WithName("ray-head").
								WithImage(rayImage).
								WithEnv(corev1ac.EnvVar().WithName("RAY_gcs_rpc_server_reconnect_timeout_s").WithValue("20")).
								WithPorts(corev1ac.ContainerPort().WithContainerPort(6379).WithName("redis")).
								WithPorts(corev1ac.ContainerPort().WithContainerPort(8265).WithName("dashboard")).
								WithPorts(corev1ac.ContainerPort().WithContainerPort(10001).WithName("client")).
								WithVolumeMounts(corev1ac.VolumeMount().
									WithMountPath("/home/ray/samples").
									WithName("test-script-configmap"),
								),
							).
							WithVolumes(corev1ac.Volume().
								WithName("test-script-configmap").
								WithConfigMap(corev1ac.ConfigMapVolumeSource().
									WithName("jobs").
									WithItems(corev1ac.KeyToPath().WithKey("test_detached_actor_1.py").WithPath("test_detached_actor_1.py")).
									WithItems(corev1ac.KeyToPath().WithKey("test_detached_actor_2.py").WithPath("test_detached_actor_2.py")),
								),
							),
						),
					),
				).
				WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
					WithRayStartParams(map[string]string{
						"num-cpus": "1",
					}).
					WithGroupName("small-group").
					WithReplicas(1).
					WithMinReplicas(1).
					WithMaxReplicas(2).
					WithTemplate(corev1ac.PodTemplateSpec().
						WithSpec(corev1ac.PodSpec().
							WithContainers(corev1ac.Container().
								WithName("ray-worker").
								WithImage(rayImage).
								WithEnv(corev1ac.EnvVar().WithName("RAY_gcs_rpc_server_reconnect_timeout_s").WithValue("120")).
								WithResources(corev1ac.ResourceRequirements().
									WithLimits(corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("300m"),
									}).
									WithRequests(corev1.ResourceList{
										corev1.ResourceCPU: resource.MustParse("300m"),
									}),
								),
							),
						),
					),
				),
		)
	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)

	g.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	test.T().Logf("Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterProvisioned), MatchCondition(metav1.ConditionTrue, rayv1.AllPodRunningAndReadyFirstTime)))

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
		stdout, stderr := ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"pkill", "gcs_server"})
		t.Logf("pkill gcs_server output - stdout: %s, stderr: %s", stdout.String(), stderr.String())

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
	})
}
