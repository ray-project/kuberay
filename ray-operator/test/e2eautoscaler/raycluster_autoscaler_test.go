package e2eautoscaler

import (
	"fmt"
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

var tests = []struct {
	HeadPodTemplateGetter   func() *corev1ac.PodTemplateSpecApplyConfiguration
	WorkerPodTemplateGetter func() *corev1ac.PodTemplateSpecApplyConfiguration
	name                    string
}{
	{
		HeadPodTemplateGetter:   headPodTemplateApplyConfiguration,
		WorkerPodTemplateGetter: workerPodTemplateApplyConfiguration,
		name:                    "Create a RayCluster with autoscaling enabled",
	},
	{
		HeadPodTemplateGetter:   headPodTemplateApplyConfigurationV2,
		WorkerPodTemplateGetter: workerPodTemplateApplyConfigurationV2,
		name:                    "Create a RayCluster with autoscaler v2 enabled",
	},
}

func TestRayClusterAutoscaler(t *testing.T) {
	for _, tc := range tests {
		test := With(t)
		g := gomega.NewWithT(t)

		// Create a namespace
		namespace := test.NewTestNamespace()

		// Scripts for creating and terminating detached actors to trigger autoscaling
		scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
		scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

		test.T().Run(tc.name, func(_ *testing.T) {
			rayClusterSpecAC := rayv1ac.RayClusterSpec().
				WithEnableInTreeAutoscaling(true).
				WithRayVersion(GetRayVersion()).
				WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
					WithRayStartParams(map[string]string{"num-cpus": "0"}).
					WithTemplate(tc.HeadPodTemplateGetter())).
				WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
					WithReplicas(0).
					WithMinReplicas(0).
					WithMaxReplicas(3).
					WithGroupName("small-group").
					WithRayStartParams(map[string]string{"num-cpus": "1"}).
					WithTemplate(tc.WorkerPodTemplateGetter()))
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
}

func TestRayClusterAutoscalerWithFakeGPU(t *testing.T) {
	for _, tc := range tests {

		test := With(t)
		g := gomega.NewWithT(t)

		// Create a namespace
		namespace := test.NewTestNamespace()

		// Scripts for creating and terminating detached actors to trigger autoscaling
		scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
		scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

		test.T().Run(tc.name, func(_ *testing.T) {
			rayClusterSpecAC := rayv1ac.RayClusterSpec().
				WithEnableInTreeAutoscaling(true).
				WithRayVersion(GetRayVersion()).
				WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
					WithRayStartParams(map[string]string{"num-cpus": "0"}).
					WithTemplate(tc.HeadPodTemplateGetter())).
				WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
					WithReplicas(0).
					WithMinReplicas(0).
					WithMaxReplicas(3).
					WithGroupName("gpu-group").
					WithRayStartParams(map[string]string{"num-cpus": "1", "num-gpus": "1"}).
					WithTemplate(tc.WorkerPodTemplateGetter()))
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
}

func TestRayClusterAutoscalerWithCustomResource(t *testing.T) {
	for _, tc := range tests {

		test := With(t)
		g := gomega.NewWithT(t)

		// Create a namespace
		namespace := test.NewTestNamespace()

		// Scripts for creating and terminating detached actors to trigger autoscaling
		scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
		scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

		test.T().Run(tc.name, func(_ *testing.T) {
			groupName := "custom-resource-group"

			rayClusterSpecAC := rayv1ac.RayClusterSpec().
				WithEnableInTreeAutoscaling(true).
				WithRayVersion(GetRayVersion()).
				WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
					WithRayStartParams(map[string]string{"num-cpus": "0"}).
					WithTemplate(tc.HeadPodTemplateGetter())).
				WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
					WithReplicas(0).
					WithMinReplicas(0).
					WithMaxReplicas(3).
					WithGroupName(groupName).
					WithRayStartParams(map[string]string{"num-cpus": "1", "resources": `'{"CustomResource": 1}'`}).
					WithTemplate(tc.WorkerPodTemplateGetter()))
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
}

func TestRayClusterAutoscalerWithDesiredState(t *testing.T) {
	for _, tc := range tests {

		test := With(t)
		g := gomega.NewWithT(t)

		const maxReplica = 3
		// Set the scale down window to a large enough value, so scale down could be disabled to avoid test flakiness.
		const scaleDownWaitSec = 3600

		// Create a namespace
		namespace := test.NewTestNamespace()

		// Scripts for creating and terminating detached actors to trigger autoscaling
		scriptsAC := newConfigMap(namespace.Name, files(test, "create_concurrent_tasks.py"))
		scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

		test.T().Run(tc.name, func(_ *testing.T) {
			groupName := "custom-resource-group"
			rayClusterSpecAC := rayv1ac.RayClusterSpec().
				WithEnableInTreeAutoscaling(true).
				WithRayVersion(GetRayVersion()).
				WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
					WithRayStartParams(map[string]string{"num-cpus": "0"}).
					WithTemplate(tc.HeadPodTemplateGetter())).
				WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
					WithReplicas(0).
					WithMinReplicas(0).
					WithMaxReplicas(maxReplica).
					WithGroupName(groupName).
					WithRayStartParams(map[string]string{"num-cpus": "1", "resources": `'{"CustomResource": 1}'`}).
					WithTemplate(tc.WorkerPodTemplateGetter())).
				WithAutoscalerOptions(rayv1ac.AutoscalerOptions().
					WithIdleTimeoutSeconds(scaleDownWaitSec))
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
			ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_concurrent_tasks.py"})

			// Scale down has been disabled, after ray script execution completion the cluster is expected to have max replica's number of pods.
			pods, err := GetWorkerPods(test, rayCluster)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(pods).To(gomega.HaveLen(maxReplica))
		})

	}
}

func TestRayClusterAutoscalerMinReplicasUpdate(t *testing.T) {
	for _, tc := range tests {

		test := With(t)
		g := gomega.NewWithT(t)

		// Create a namespace
		namespace := test.NewTestNamespace()

		// Script for creating detached actors to trigger autoscaling
		scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_actor.py"))
		scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

		test.T().Run(tc.name, func(_ *testing.T) {
			groupName := "test-group"

			rayClusterSpecAC := rayv1ac.RayClusterSpec().
				WithEnableInTreeAutoscaling(true).
				WithRayVersion(GetRayVersion()).
				WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
					WithRayStartParams(map[string]string{"num-cpus": "0"}).
					WithTemplate(tc.HeadPodTemplateGetter())).
				WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
					WithReplicas(1).
					WithMinReplicas(0).
					WithMaxReplicas(5).
					WithGroupName(groupName).
					WithRayStartParams(map[string]string{"num-cpus": "1"}).
					WithTemplate(tc.WorkerPodTemplateGetter()))
			rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
				WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

			rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			// Wait for RayCluster to become ready
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
			g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(1))))

			// Update minReplicas from 0 to 2
			rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Get(test.Ctx(), rayCluster.Name, metav1.GetOptions{})
			g.Expect(err).NotTo(gomega.HaveOccurred())
			rayCluster.Spec.WorkerGroupSpecs[0].MinReplicas = ptr.To(int32(2))
			rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Update(test.Ctx(), rayCluster, metav1.UpdateOptions{})
			g.Expect(err).NotTo(gomega.HaveOccurred())
			test.T().Logf("Updated RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			// Verify that KubeRay creates an additional Pod
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(2))))

			// Create detached actors to trigger autoscaling to 5 Pods
			headPod, err := GetHeadPod(test, rayCluster)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

			for i := 0; i < 5; i++ {
				ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", fmt.Sprintf("actor%d", i)})
			}

			// Verify that the Autoscaler scales up to 5 Pods
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(5))))

			// Check that replicas is set to 5
			g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(GetRayClusterWorkerGroupReplicaSum, gomega.Equal(int32(5))))
		})
	}
}

func TestRayClusterAutoscalerV2IdleTimeout(t *testing.T) {
	// Only test with the V2 Autoscaler
	tc := tests[1]

	test := With(t)
	g := gomega.NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	idleTimeoutShort := int32(10)
	idleTimeoutLong := int32(30)
	timeoutBuffer := int32(20) // Additional wait time to allow for scale down operation

	// Script for creating detached actors to trigger autoscaling
	scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
	scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	test.T().Logf("Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

	test.T().Run(tc.name, func(_ *testing.T) {
		groupName1 := "short-idle-timeout-group"
		groupName2 := "long-idle-timeout-group"
		rayClusterSpecAC := rayv1ac.RayClusterSpec().
			WithEnableInTreeAutoscaling(true).
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"num-cpus": "0"}).
				WithTemplate(tc.HeadPodTemplateGetter())).
			WithWorkerGroupSpecs(
				rayv1ac.WorkerGroupSpec().
					WithReplicas(0).
					WithMinReplicas(0).
					WithMaxReplicas(1).
					WithIdleTimeoutSeconds(idleTimeoutShort).
					WithGroupName(groupName1).
					WithRayStartParams(map[string]string{"num-cpus": "1"}).
					WithTemplate(tc.WorkerPodTemplateGetter()),
				rayv1ac.WorkerGroupSpec().
					WithReplicas(0).
					WithMinReplicas(0).
					WithMaxReplicas(1).
					WithIdleTimeoutSeconds(idleTimeoutLong).
					WithGroupName(groupName2).
					WithRayStartParams(map[string]string{"num-cpus": "2"}).
					WithTemplate(tc.WorkerPodTemplateGetter()),
			)
		rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
			WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		// Wait for RayCluster to become ready
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
		g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

		headPod, err := GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		test.T().Logf("Found head pod %s/%s", headPod.Namespace, headPod.Name)

		// Deploy one detached actor on each worker group. This is guaranteed by setting `maxReplicas` and specifying respective num-cpus.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "actor-long-timeout", "--num-cpus=2"})
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "actor-short-timeout", "--num-cpus=1"})
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(2))))
		g.Expect(GetGroupPods(test, rayCluster, groupName1)).To(gomega.HaveLen(1))
		g.Expect(GetGroupPods(test, rayCluster, groupName2)).To(gomega.HaveLen(1))

		// Terminate the first detached actor, and the worker should be marked idle after ~10 seconds.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "actor-short-timeout"})
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), time.Duration(idleTimeoutShort+timeoutBuffer)*time.Second).
			Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(1))))

		// Terminate the second detached actor, and the worker should be marked idle after ~30 seconds.
		ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "actor-long-timeout"})
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), time.Duration(idleTimeoutLong+timeoutBuffer)*time.Second).
			Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))
	})
}
