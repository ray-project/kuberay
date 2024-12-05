package e2eautoscaler

import (
	"testing"
	"time"

	"github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayClusterAutoscaler(t *testing.T) {
	test := With(t)
	g := gomega.NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Scripts for creating and terminating detached actors to trigger autoscaling
	scriptsAC := newConfigMap(namespace.Name, "scripts", files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
	scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
	g.Expect(err).NotTo(gomega.HaveOccurred())
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
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		// Wait for RayCluster to become ready and verify the number of available worker replicas.
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
		g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

		headPod, err := GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

		// Create a detached actor, and a worker should be created.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "actor1"})
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(1))))

		// Create a detached actor, and a worker should be created.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "actor2"})
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(2))))

		// Terminate a detached actor, and a worker should be deleted.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "actor1"})
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(1))))

		// Terminate a detached actor, and a worker should be deleted.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "actor2"})
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))
	})
}

func TestRayClusterAutoscalerWithFakeGPU(t *testing.T) {
	test := With(t)
	g := gomega.NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Scripts for creating and terminating detached actors to trigger autoscaling
	scriptsAC := newConfigMap(namespace.Name, "scripts", files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
	scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
	g.Expect(err).NotTo(gomega.HaveOccurred())
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
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		// Wait for RayCluster to become ready and verify the number of available worker replicas.
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
		g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

		headPod, err := GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

		// Create a detached gpu actor, and a worker in the "gpu-group" should be created.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "gpu_actor", "--num-gpus=1"})
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(1))))
		// We don't use real GPU resources of Kubernetes here, therefore we can't test the RayClusterDesiredGPU.
		// We test the Pods count of the "gpu-group" instead.
		g.Expect(GetGroupPods(test, rayCluster, "gpu-group")).To(gomega.HaveLen(1))

		// Terminate the gpu detached actor, and the worker should be deleted.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "gpu_actor"})
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))
	})
}

func TestRayClusterAutoscalerWithCustomResource(t *testing.T) {
	test := With(t)
	g := gomega.NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Scripts for creating and terminating detached actors to trigger autoscaling
	scriptsAC := newConfigMap(namespace.Name, "scripts", files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
	scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
	g.Expect(err).NotTo(gomega.HaveOccurred())
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
				WithRayStartParams(map[string]string{"num-cpus": "1", "resources": `'{"CustomResource": 1}'`}).
				WithTemplate(workerPodTemplateApplyConfiguration()))
		rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
			WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		// Wait for RayCluster to become ready and verify the number of available worker replicas.
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
		g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

		headPod, err := GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

		// Create a detached custom resource actor, and a worker in the "custom-resource-group" should be created.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "custom_resource_actor", "--num-custom-resources=1"})
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(1))))
		g.Expect(GetGroupPods(test, rayCluster, groupName)).To(gomega.HaveLen(1))

		// Terminate the custom resource detached actor, and the worker should be deleted.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "custom_resource_actor"})
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))
	})
}

func TestRayClusterAutoscalerNestedRemoteFunction(t *testing.T) {
	test := With(t)
	g := gomega.NewWithT(t)

	const maxReplicas = 2
	const idleTimeOutSec = 5

	// Create a namespace
	namespace := test.NewTestNamespace()
	test.StreamKubeRayOperatorLogs()

	// Scripts for creating and terminating detached actors to trigger autoscaling
	scriptsAC := newConfigMap(namespace.Name, "scripts", files(test, "create_nested_remote_function.py"))
	scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

	rayClusterSpecAC := rayv1ac.RayClusterSpec().
		WithEnableInTreeAutoscaling(true).
		WithRayVersion(GetRayVersion()).
		WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
			WithRayStartParams(map[string]string{"num-cpus": "0"}).
			WithTemplate(headPodTemplateApplyConfiguration())).
		WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
			WithReplicas(0).
			WithMinReplicas(0).
			WithMaxReplicas(maxReplicas).
			WithGroupName("small-group").
			WithRayStartParams(map[string]string{"num-cpus": "1"}).
			WithTemplate(workerPodTemplateApplyConfiguration())).
		WithAutoscalerOptions(rayv1ac.AutoscalerOptions().
			WithIdleTimeoutSeconds(idleTimeOutSec))
	rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
		WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	// Wait for RayCluster to become ready and verify the number of available worker replicas.
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
		Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
	g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

	// Create a number of tasks and wait for their completion, and a worker in the "custom-resource-group" should be created.
	ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_nested_remote_function.py"})

	pods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pods).To(gomega.HaveLen(maxReplicas))

	// Wait for the first child task to finish and afer the idle timeout
	time.Sleep(20 * time.Second)

	// The second child task is still running, so none of the worker pods should be deleted. (node 1: parent blocking, node 2: child 2 running)
	pods, err = GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pods).To(gomega.HaveLen(maxReplicas))

	// After the second child task finishes, all worker pods should be deleted due to the idle timeout.
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
		Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))
}
