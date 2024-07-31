package e2eautoscaler

import (
	"testing"

	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayClusterAutoscaler(t *testing.T) {
	test := With(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Scripts for creating and terminating detached actors to trigger autoscaling
	scriptsAC := newConfigMap(namespace.Name, "scripts", files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
	scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

	test.T().Run("Create a RayCluster with autoscaling enabled", func(_ *testing.T) {
		rayClusterSpecAC := rayv1ac.RayClusterSpec().
			WithEnableInTreeAutoscaling(true).
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"num-cpus": "0"}).
				WithTemplate(headPodTemplateApplyConfiguration())).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(0).
				WithMinReplicas(0).
				WithMaxReplicas(3).
				WithGroupName("small-group").
				WithRayStartParams(map[string]string{"num-cpus": "1"}).
				WithTemplate(workerPodTemplateApplyConfiguration()))
		rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
			WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		// Wait for RayCluster to become ready and verify the number of available worker replicas.
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
		rayCluster = GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		test.Expect(rayCluster.Status.DesiredWorkerReplicas).To(Equal(int32(0)))

		headPod := GetHeadPod(test, rayCluster)
		test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

		// Create a detached actor, and a worker should be created.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "actor1"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(1))))

		// Create a detached actor, and a worker should be created.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "actor2"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(2))))

		// Terminate a detached actor, and a worker should be deleted.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "actor1"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(1))))

		// Terminate a detached actor, and a worker should be deleted.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "actor2"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))
	})
}

func TestRayClusterAutoscalerWithFakeGPU(t *testing.T) {
	test := With(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Scripts for creating and terminating detached actors to trigger autoscaling
	scriptsAC := newConfigMap(namespace.Name, "scripts", files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
	scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

	test.T().Run("Create a RayCluster with autoscaling enabled", func(_ *testing.T) {
		rayClusterSpecAC := rayv1ac.RayClusterSpec().
			WithEnableInTreeAutoscaling(true).
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"num-cpus": "0"}).
				WithTemplate(headPodTemplateApplyConfiguration())).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(0).
				WithMinReplicas(0).
				WithMaxReplicas(3).
				WithGroupName("gpu-group").
				WithRayStartParams(map[string]string{"num-cpus": "1", "num-gpus": "1"}).
				WithTemplate(workerPodTemplateApplyConfiguration()))
		rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
			WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		// Wait for RayCluster to become ready and verify the number of available worker replicas.
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
		rayCluster = GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		test.Expect(rayCluster.Status.DesiredWorkerReplicas).To(Equal(int32(0)))

		headPod := GetHeadPod(test, rayCluster)
		test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

		// Create a detached gpu actor, and a worker in the "gpu-group" should be created.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "gpu_actor", "--num-gpus=1"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(1))))
		// We don't use real GPU resources of Kubernetes here, therefore we can't test the RayClusterDesiredGPU.
		// We test the Pods count of the "gpu-group" instead.
		test.Expect(GetGroupPods(test, rayCluster, "gpu-group")).To(HaveLen(1))

		// Terminate the gpu detached actor, and the worker should be deleted.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "gpu_actor"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))
	})
}

func TestRayClusterAutoscalerWithFakeSingleHostTPU(t *testing.T) {
	test := With(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Scripts for creating and terminating detached actors to trigger autoscaling
	scriptsAC := newConfigMap(namespace.Name, "scripts", files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
	scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

	test.T().Run("Create a RayCluster with autoscaling enabled", func(_ *testing.T) {
		rayClusterSpecAC := rayv1ac.RayClusterSpec().
			WithEnableInTreeAutoscaling(true).
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"num-cpus": "0"}).
				WithTemplate(headPodTemplateApplyConfiguration())).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(0).
				WithMinReplicas(0).
				WithMaxReplicas(3).
				WithNumOfHosts(1).
				WithGroupName("tpu-group").
				WithRayStartParams(map[string]string{"num-cpus": "1", "resources": `"{\"TPU\": 4}"`}).
				WithTemplate(workerPodTemplateApplyConfiguration()))
		rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
			WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		// Wait for RayCluster to become ready and verify the number of available worker replicas.
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
		rayCluster = GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		test.Expect(rayCluster.Status.DesiredWorkerReplicas).To(Equal(int32(0)))

		headPod := GetHeadPod(test, rayCluster)
		test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

		// Create a detached tpu actor, and 1 worker in the multi-host "tpu-group" should be created.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "tpu_actor", "--custom-resource-name=\"TPU\"", "--num-custom-resources=4"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(1))))
		
		// Each TPU multi-host replica should have 1 workers, so we check for 1 pod in 'tpu-group'.
		test.Expect(GetGroupPods(test, rayCluster, "tpu-group")).To(HaveLen(1))

		// Terminate the TPU detached actos, and the worker group replica should be deleted.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "tpu_actor"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))
	})
}

func TestRayClusterAutoscalerWithFakeMultiHostTPU(t *testing.T) {
	test := With(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Scripts for creating and terminating detached actors to trigger autoscaling
	scriptsAC := newConfigMap(namespace.Name, "scripts", files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
	scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

	test.T().Run("Create a RayCluster with autoscaling enabled", func(_ *testing.T) {
		rayClusterSpecAC := rayv1ac.RayClusterSpec().
			WithEnableInTreeAutoscaling(true).
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"num-cpus": "0"}).
				WithTemplate(headPodTemplateApplyConfiguration())).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(0).
				WithMinReplicas(0).
				WithMaxReplicas(3).
				WithNumOfHosts(4).
				WithGroupName("tpu-group").
				WithRayStartParams(map[string]string{"num-cpus": "1", "resources": `"{\"TPU\": 4}"`}).
				WithTemplate(workerPodTemplateApplyConfiguration()))
		rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
			WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		// Wait for RayCluster to become ready and verify the number of available worker replicas.
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
		rayCluster = GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		test.Expect(rayCluster.Status.DesiredWorkerReplicas).To(Equal(int32(0)))

		headPod := GetHeadPod(test, rayCluster)
		test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

		// Create a detached tpu actor, and 4 workers in the multi-host "tpu-group" should be created.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "tpu_actor_1", "--custom-resource-name=\"TPU\"", "--num-custom-resources=4"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(1))))

		// Each TPU multi-host replica should have 4 workers, so we check for 4 pods in 'tpu-group'.
		test.Expect(GetGroupPods(test, rayCluster, "tpu-group")).To(HaveLen(4))

		// Each TPU multi-host worker should have a task or actor scheduled on it, therefore we create 3 more detached actors
		// to run on each node in the multi-host TPU worker group.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "tpu_actor_2", "--custom-resource-name=\"TPU\"", "--num-custom-resources=4"})
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "tpu_actor_3", "--custom-resource-name=\"TPU\"", "--num-custom-resources=4"})
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "tpu_actor_4", "--custom-resource-name=\"TPU\"", "--num-custom-resources=4"})

		// Each new TPU detached actor should get scheduled to an existing scaled-up worker, so we check that there are still 4 pods in 'tpu-group'.
		test.Expect(GetGroupPods(test, rayCluster, "tpu-group")).To(HaveLen(4))

		// Terminating one TPU detached actor will result in the Ray node becoming idle, causing Ray to scale down the entire multi-host
		// worker group. A new multi-host worker group will then be scaled back up since the remaining detached actors are running.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "tpu_actor_1"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))

		// Terminate the remaining 3 TPU detached actors, and the worker group should be deleted.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "tpu_actor_2"})
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "tpu_actor_3"})
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "tpu_actor_4"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))
		test.Expect(GetGroupPods(test, rayCluster, "tpu-group")).To(HaveLen(0))
	})
}

func TestRayClusterAutoscalerWithCustomResource(t *testing.T) {
	test := With(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Scripts for creating and terminating detached actors to trigger autoscaling
	scriptsAC := newConfigMap(namespace.Name, "scripts", files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
	scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
	test.Expect(err).NotTo(HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

	test.T().Run("Create a RayCluster with autoscaling enabled", func(_ *testing.T) {
		groupName := "custom-resource-group"

		rayClusterSpecAC := rayv1ac.RayClusterSpec().
			WithEnableInTreeAutoscaling(true).
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"num-cpus": "0"}).
				WithTemplate(headPodTemplateApplyConfiguration())).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(0).
				WithMinReplicas(0).
				WithMaxReplicas(3).
				WithGroupName(groupName).
				WithRayStartParams(map[string]string{"num-cpus": "1", "resources": `"{\"CustomResource\": 1}"`}).
				WithTemplate(workerPodTemplateApplyConfiguration()))
		rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
			WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		test.Expect(err).NotTo(HaveOccurred())
		test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		// Wait for RayCluster to become ready and verify the number of available worker replicas.
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
		rayCluster = GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)
		test.Expect(rayCluster.Status.DesiredWorkerReplicas).To(Equal(int32(0)))

		headPod := GetHeadPod(test, rayCluster)
		test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

		// Create a detached custom resource actor, and a worker in the "custom-resource-group" should be created.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "custom_resource_actor", "--num-custom-resources=1"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(1))))
		test.Expect(GetGroupPods(test, rayCluster, groupName)).To(HaveLen(1))

		// Terminate the custom resource detached actor, and the worker should be deleted.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "custom_resource_actor"})
		test.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterDesiredWorkerReplicas, Equal(int32(0))))
	})
}
