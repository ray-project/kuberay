package e2eautoscaler

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"

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
		t.Run(tc.name, func(t *testing.T) {
			test := With(t)
			g := gomega.NewWithT(t)

			// Create a namespace
			namespace := test.NewTestNamespace()

			// Scripts for creating and terminating detached actors to trigger autoscaling
			scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
			scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

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
			LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			// Wait for RayCluster to become ready and verify the number of available worker replicas.
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
			g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

			headPod, err := GetHeadPod(test, rayCluster)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

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
		t.Run(tc.name, func(t *testing.T) {
			test := With(t)
			g := gomega.NewWithT(t)

			// Create a namespace
			namespace := test.NewTestNamespace()

			// Scripts for creating and terminating detached actors to trigger autoscaling
			scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
			scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

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
			LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			// Wait for RayCluster to become ready and verify the number of available worker replicas.
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
			g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

			headPod, err := GetHeadPod(test, rayCluster)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

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
		t.Run(tc.name, func(t *testing.T) {
			test := With(t)
			g := gomega.NewWithT(t)

			// Create a namespace
			namespace := test.NewTestNamespace()

			// Scripts for creating and terminating detached actors to trigger autoscaling
			scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
			scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

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
			LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)
			// Wait for RayCluster to become ready and verify the number of available worker replicas.
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
			g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

			headPod, err := GetHeadPod(test, rayCluster)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

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
		t.Run(tc.name, func(t *testing.T) {
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
			LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

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
			LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			// Wait for RayCluster to become ready and verify the number of available worker replicas.
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
			g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

			headPod, err := GetHeadPod(test, rayCluster)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

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
		t.Run(tc.name, func(t *testing.T) {
			test := With(t)
			g := gomega.NewWithT(t)

			// Create a namespace
			namespace := test.NewTestNamespace()

			// Script for creating detached actors to trigger autoscaling
			scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_actor.py"))
			scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

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
			LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

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
			LogWithTimestamp(test.T(), "Updated RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			// Verify that KubeRay creates an additional Pod
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(2))))

			// Create detached actors to trigger autoscaling to 5 Pods
			headPod, err := GetHeadPod(test, rayCluster)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

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

func TestRayClusterAutoscalerMaxReplicasUpdate(t *testing.T) {
	replicaTests := []struct {
		name             string
		initialMax       int32
		updatedMax       int32
		expectedReplicas int32
		actorCount       int
	}{
		{
			name:       "Scale up maxReplicas from 3 to 5",
			initialMax: 3,
			updatedMax: 5,
		},
		{
			name:       "Scale down maxReplicas from 3 to 1",
			initialMax: 3,
			updatedMax: 1,
		},
	}

	for _, tc := range tests {
		for _, rtc := range replicaTests {
			t.Run(fmt.Sprintf("%s(%s)", tc.name, rtc.name), func(t *testing.T) {
				test := With(t)
				g := gomega.NewWithT(t)

				namespace := test.NewTestNamespace()

				scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_actor.py"))
				scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
				g.Expect(err).NotTo(gomega.HaveOccurred())

				groupName := "test-group"

				rayClusterSpecAC := rayv1ac.RayClusterSpec().
					WithEnableInTreeAutoscaling(true).
					WithRayVersion(GetRayVersion()).
					WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
						WithRayStartParams(map[string]string{"num-cpus": "0"}).
						WithTemplate(tc.HeadPodTemplateGetter())).
					WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
						WithReplicas(1).
						WithMinReplicas(1).
						WithMaxReplicas(rtc.initialMax).
						WithGroupName(groupName).
						WithRayStartParams(map[string]string{"num-cpus": "1"}).
						WithTemplate(tc.WorkerPodTemplateGetter()))
				rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
					WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

				rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
				g.Expect(err).NotTo(gomega.HaveOccurred())

				// Wait for RayCluster to become ready and verify the number of available initial worker replicas (1)
				g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
					Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
				g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(1))))

				headPod, err := GetHeadPod(test, rayCluster)
				g.Expect(err).NotTo(gomega.HaveOccurred())

				// Create detached actors
				for i := 0; i < int(max(rtc.updatedMax, rtc.initialMax)); i++ {
					ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", fmt.Sprintf("actor%d", i)})
				}

				g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutLong).
					Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(rtc.initialMax)))
				// Verify that the Autoscaler scales up/down to initialMax Pod count
				g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).
					To(gomega.WithTransform(GetRayClusterWorkerGroupReplicaSum, gomega.Equal(rtc.initialMax)))

				// Update maxReplicas
				rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Get(test.Ctx(), rayCluster.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(gomega.HaveOccurred())
				rayCluster.Spec.WorkerGroupSpecs[0].MaxReplicas = ptr.To(rtc.updatedMax)
				rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Update(test.Ctx(), rayCluster, metav1.UpdateOptions{})
				g.Expect(err).NotTo(gomega.HaveOccurred())

				// Check that replicas is set to the updatedMax
				g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutLong).
					Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(rtc.updatedMax)))

				// Verify that the Autoscaler scales up/down to updatedMax Pod count
				g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutShort).
					Should(gomega.WithTransform(GetRayClusterWorkerGroupReplicaSum, gomega.Equal(rtc.updatedMax)))
			})
		}
	}
}

func TestRayClusterAutoscalerV2IdleTimeout(t *testing.T) {
	// Only test with the V2 Autoscaler
	tc := tests[1]

	t.Run(tc.name, func(t *testing.T) {
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
		LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

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
		LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		// Wait for RayCluster to become ready
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
		g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

		headPod, err := GetHeadPod(test, rayCluster)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

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

// This test verifies that the autoscaler can still trigger GPU nodes for CPU tasks when no CPU-only worker group is defined.
func TestRayClusterAutoscalerGPUNodesForCPUTasks(t *testing.T) {
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			test := With(t)
			g := gomega.NewWithT(t)

			// Create a namespace
			namespace := test.NewTestNamespace()

			// Scripts for creating and terminating detached actors to trigger autoscaling
			scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
			scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

			groupName := "gpu-group"

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
					// This group has GPU resources
					WithRayStartParams(map[string]string{"num-cpus": "1", "num-gpus": "1"}).
					WithTemplate(tc.WorkerPodTemplateGetter()))

			rayClusterAC := rayv1ac.RayCluster("ray-cluster-gpu-for-cpu", namespace.Name).
				WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

			rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			// Wait for RayCluster to become ready and verify the number of available worker replicas
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
			g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

			headPod, err := GetHeadPod(test, rayCluster)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

			// Create a detached actor that only needs CPU resources
			ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "cpu_actor", "--num-cpus=1"})

			// Verify that the autoscaler creates a GPU node for this CPU-only task
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(1))))

			// Verify that the created node is from the GPU worker group
			g.Expect(GetGroupPods(test, rayCluster, groupName)).To(gomega.HaveLen(1))

			// Terminate the actor, and the worker should be deleted
			ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "cpu_actor"})
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))
		})
	}
}

// This test verifies that the autoscaler does not remove idle nodes required by an upcoming placement group.
// The following is how the test works with the do_not_remove_idles_for_pg.py script:
// 1. We create a placement group `pg1` with a bundle [{"CPU": 1}].
// 2. Autoscaler should scale up 1 worker Pod (`worker1`).
// 3. We remove `pg1`.
// 4. We create a placement group `pg2` with bundles [{"CPU": 1}, {"CPU": 1}].
// 5. Autoscaler should scale up the second worker Pod (`worker2`), but it needs at least 15 seconds to be up and running due to the injected init container.
// 6. We verify that `worker1` should not be terminated, although it is idle for more than the `IdleTimeoutSeconds`, which is 6 seconds.
func TestRayClusterAutoscalerDoNotRemoveIdlesForPlacementGroup(t *testing.T) {
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			test := With(t)
			g := gomega.NewWithT(t)

			// Create a namespace
			namespace := test.NewTestNamespace()

			scriptsAC := newConfigMap(namespace.Name, files(test, "do_not_remove_idles_for_pg.py"))
			scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

			workerTemplate := tc.WorkerPodTemplateGetter()
			workerTemplate.Spec.WithInitContainers(corev1ac.Container().
				WithName("init-sleep").
				WithImage(GetRayImage()).
				// delay the worker startup to make sure it takes longer than the IdleTimeoutSeconds, which is 6 seconds,
				// and longer than the default autoscaler update interval of 5 seconds.
				WithCommand("bash", "-c", "sleep 15"))

			rayClusterSpecAC := rayv1ac.RayClusterSpec().
				WithEnableInTreeAutoscaling(true).
				WithRayVersion(GetRayVersion()).
				WithAutoscalerOptions(rayv1ac.AutoscalerOptions().
					WithIdleTimeoutSeconds(6)).
				WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
					WithRayStartParams(map[string]string{"num-cpus": "0"}).
					WithTemplate(tc.HeadPodTemplateGetter())).
				WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
					WithReplicas(0).
					WithMinReplicas(0).
					WithMaxReplicas(2).
					WithGroupName("short-idle-group").
					WithRayStartParams(map[string]string{"num-cpus": "1"}).
					WithTemplate(workerTemplate))

			rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
				WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

			rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			// Wait for RayCluster to become ready and verify there is no worker replica.
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
			g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

			headPod, err := GetHeadPod(test, rayCluster)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

			// Run the test script. It should exit without error.
			ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/do_not_remove_idles_for_pg.py"})
		})
	}
}

// This test verifies that the autoscaler can launch nodes to fulfill ray.autoscaler.sdk.request_resources from the user program.
func TestRayClusterAutoscalerSDKRequestResources(t *testing.T) {
	for _, tc := range tests {
		t.Run("Test ray.autoscaler.sdk.request_resources", func(t *testing.T) {
			test := With(t)
			g := gomega.NewWithT(t)

			// Create a namespace
			namespace := test.NewTestNamespace()

			// Mount the call_request_resources.py script as a ConfigMap
			scriptsAC := newConfigMap(namespace.Name, files(test, "call_request_resources.py"))
			scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

			groupName := "request-group"

			rayClusterSpecAC := rayv1ac.RayClusterSpec().
				WithEnableInTreeAutoscaling(true).
				WithRayVersion(GetRayVersion()).
				WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
					WithRayStartParams(map[string]string{"num-cpus": "0"}).
					WithTemplate(tc.HeadPodTemplateGetter())).
				WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
					WithReplicas(0).
					WithMinReplicas(0).
					WithMaxReplicas(5).
					WithGroupName(groupName).
					WithRayStartParams(map[string]string{"num-cpus": "1"}).
					WithTemplate(tc.WorkerPodTemplateGetter()))
			rayClusterAC := rayv1ac.RayCluster("ray-cluster-sdk", namespace.Name).
				WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

			rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			// Wait for RayCluster to become ready
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
			g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

			headPod, err := GetHeadPod(test, rayCluster)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

			// Trigger resource request via ray.autoscaler.sdk.request_resources
			ExecPodCmd(test, headPod, common.RayHeadContainer, []string{
				"python", "/home/ray/test_scripts/call_request_resources.py", "--num-cpus=3",
			})

			// Autoscaler should create 3 workers
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.BeNumerically("==", 3)))
		})
	}
}

// This test verifies that a new worker node can be launched in a newly added worker group.
func TestRayClusterAutoscalerAddNewWorkerGroup(t *testing.T) {
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			test := With(t)
			g := gomega.NewWithT(t)

			// Create a namespace
			namespace := test.NewTestNamespace()

			// Mount the create_detached_actor.py and terminate_detached_actor.py scripts as a ConfigMap
			scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_actor.py", "terminate_detached_actor.py"))
			scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

			cpuGroup := "cpu-group"
			gpuGroup := "gpu-group"

			rayClusterSpecAC := rayv1ac.RayClusterSpec().
				WithEnableInTreeAutoscaling(true).
				WithAutoscalerOptions(rayv1ac.AutoscalerOptions().
					WithIdleTimeoutSeconds(10)).
				WithRayVersion(GetRayVersion()).
				WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
					WithRayStartParams(map[string]string{"num-cpus": "0"}).
					WithTemplate(tc.HeadPodTemplateGetter())).
				WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
					WithReplicas(0).
					WithMinReplicas(0).
					WithMaxReplicas(3).
					WithGroupName(cpuGroup).
					WithRayStartParams(map[string]string{"num-cpus": "1"}).
					WithTemplate(tc.WorkerPodTemplateGetter()))
			rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
				WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

			rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			// Wait for RayCluster to become ready
			g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
				Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
			g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

			headPod, err := GetHeadPod(test, rayCluster)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

			// Create a CPU-only detached actor, and a worker should be created.
			ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "cpu_actor", "--num-cpus=1"})
			g.Eventually(GroupPods(test, rayCluster, cpuGroup), TestTimeoutMedium).Should(gomega.HaveLen(1))

			// Update the CPU worker group to have 1 replica to avoid overwriting the existing worker group
			// when adding the new GPU worker group.
			//
			// TODO(kevin85421): Autoscaler V2 will get stuck forever if the CPU worker group's replicas are overwritten
			// from 1 to 0. Ideally, Autoscaler V2 should still work no matter whether `Replicas` is overwritten or not.
			//
			// (1) Create a CPU detached actor
			// (2) Autoscaler updates the CPU worker group's `Replicas` to 1
			// (3) Add a GPU worker group and overwrite the CPU worker group's `Replicas` to 0
			// (4) At this point, the CPU worker group's `Replicas` is 0 in CR, but the CPU worker Pod still exists
			// (5) Create a GPU detached actor, and the GPU worker group's `Replicas` will be updated to 1
			// (6) Create a CPU detached actor, and the CPU worker group's `Replicas` will be updated to 1 instead of 2
			rayClusterAC.Spec.WorkerGroupSpecs[0].WithReplicas(1)

			// Add a GPU worker group
			rayClusterAC.Spec.WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(0).
				WithMinReplicas(0).
				WithMaxReplicas(3).
				WithGroupName(gpuGroup).
				WithRayStartParams(map[string]string{"num-cpus": "0", "num-gpus": "1"}).
				WithTemplate(tc.WorkerPodTemplateGetter()))

			rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			LogWithTimestamp(test.T(), "Updated RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

			// Create a GPU-only detached actor, and a worker should be created.
			ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "gpu_actor", "--num-gpus=1", "--num-cpus=0"})
			g.Eventually(GroupPods(test, rayCluster, gpuGroup), TestTimeoutMedium).Should(gomega.HaveLen(1))

			// Create a CPU-only detached actor, and a worker should be created.
			ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/create_detached_actor.py", "cpu_actor_2", "--num-cpus=1"})
			g.Eventually(GroupPods(test, rayCluster, cpuGroup), TestTimeoutMedium).Should(gomega.HaveLen(2))

			// Terminate the GPU-only detached actor, and a worker should be deleted.
			ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/terminate_detached_actor.py", "gpu_actor"})
			g.Eventually(GroupPods(test, rayCluster, gpuGroup), TestTimeoutMedium).Should(gomega.BeEmpty())
		})
	}
}

func TestRayClusterAutoscalerPlacementGroup(t *testing.T) {
	for _, tc := range tests {
		for _, setting := range []struct {
			workerGroupRayStartParams   map[string]string
			createPlacementGroupCMD     []string
			expectedWorkerGroupReplicas int
			workerGroupMaxReplicas      int32
		}{
			{
				workerGroupRayStartParams:   map[string]string{"num-cpus": "2"},
				workerGroupMaxReplicas:      3,
				createPlacementGroupCMD:     []string{"python", "/home/ray/test_scripts/create_detached_placement_group.py", "--num-cpus-per-bundle=1", "--num-bundles=2", "--strategy=STRICT_PACK"},
				expectedWorkerGroupReplicas: 1,
			},
			{
				workerGroupRayStartParams:   map[string]string{"num-cpus": "2"},
				workerGroupMaxReplicas:      3,
				createPlacementGroupCMD:     []string{"python", "/home/ray/test_scripts/create_detached_placement_group.py", "--num-cpus-per-bundle=1", "--num-bundles=2", "--strategy=PACK"},
				expectedWorkerGroupReplicas: 1,
			},
			{
				workerGroupRayStartParams:   map[string]string{"num-cpus": "2"},
				workerGroupMaxReplicas:      3,
				createPlacementGroupCMD:     []string{"python", "/home/ray/test_scripts/create_detached_placement_group.py", "--num-cpus-per-bundle=1", "--num-bundles=2", "--strategy=STRICT_SPREAD"},
				expectedWorkerGroupReplicas: 2,
			},
			{
				workerGroupRayStartParams:   map[string]string{"num-cpus": "1"},
				workerGroupMaxReplicas:      3,
				createPlacementGroupCMD:     []string{"python", "/home/ray/test_scripts/create_detached_placement_group.py", "--num-cpus-per-bundle=1", "--num-bundles=2", "--strategy=SPREAD"},
				expectedWorkerGroupReplicas: 2,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				test := With(t)
				g := gomega.NewWithT(t)

				// Create a namespace
				namespace := test.NewTestNamespace()

				// Mount the scripts as a ConfigMap
				scriptsAC := newConfigMap(namespace.Name, files(test, "create_detached_placement_group.py", "check_placement_group_ready.py"))
				scripts, err := test.Client().Core().CoreV1().ConfigMaps(namespace.Name).Apply(test.Ctx(), scriptsAC, TestApplyOptions)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				LogWithTimestamp(test.T(), "Created ConfigMap %s/%s successfully", scripts.Namespace, scripts.Name)

				rayClusterSpecAC := rayv1ac.RayClusterSpec().
					WithEnableInTreeAutoscaling(true).
					WithAutoscalerOptions(rayv1ac.AutoscalerOptions().
						WithIdleTimeoutSeconds(10)).
					WithRayVersion(GetRayVersion()).
					WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
						WithRayStartParams(map[string]string{"num-cpus": "0"}).
						WithTemplate(tc.HeadPodTemplateGetter())).
					WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
						WithReplicas(0).
						WithMinReplicas(0).
						WithMaxReplicas(setting.workerGroupMaxReplicas).
						WithGroupName("cpu-group").
						WithRayStartParams(setting.workerGroupRayStartParams).
						WithTemplate(tc.WorkerPodTemplateGetter()))
				rayClusterAC := rayv1ac.RayCluster("ray-cluster", namespace.Name).
					WithSpec(apply(rayClusterSpecAC, mountConfigMap[rayv1ac.RayClusterSpecApplyConfiguration](scripts, "/home/ray/test_scripts")))

				rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

				// Wait for RayCluster to become ready
				g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
					Should(gomega.WithTransform(RayClusterState, gomega.Equal(rayv1.Ready)))
				g.Expect(GetRayCluster(test, rayCluster.Namespace, rayCluster.Name)).To(gomega.WithTransform(RayClusterDesiredWorkerReplicas, gomega.Equal(int32(0))))

				headPod, err := GetHeadPod(test, rayCluster)
				g.Expect(err).NotTo(gomega.HaveOccurred())
				LogWithTimestamp(test.T(), "Found head pod %s/%s", headPod.Namespace, headPod.Name)

				// Create a detached placement group, and workers should be created.
				ExecPodCmd(test, headPod, common.RayHeadContainer, setting.createPlacementGroupCMD)
				g.Eventually(GroupPods(test, rayCluster, "cpu-group"), TestTimeoutMedium).Should(gomega.HaveLen(setting.expectedWorkerGroupReplicas))

				// check if the placement group is ready.
				ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/check_placement_group_ready.py"})

				// Delete those workers.
				pods, err := GroupPods(test, rayCluster, "cpu-group")()
				g.Expect(err).NotTo(gomega.HaveOccurred())
				for _, pod := range pods {
					err := test.Client().Core().CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
					g.Expect(err).NotTo(gomega.HaveOccurred())
				}

				// The same number of workers should come back.
				g.Eventually(GroupPods(test, rayCluster, "cpu-group"), TestTimeoutMedium).Should(
					gomega.WithTransform(func(latestPods []corev1.Pod) []corev1.Pod {
						return slices.DeleteFunc(latestPods, func(pod corev1.Pod) bool {
							for _, old := range pods {
								if pod.Name == old.Name {
									return true
								}
							}
							return false
						})
					}, gomega.HaveLen(setting.expectedWorkerGroupReplicas)))

				// check if the placement group is ready again.
				ExecPodCmd(test, headPod, common.RayHeadContainer, []string{"python", "/home/ray/test_scripts/check_placement_group_ready.py"})
			})
		}
	}
}
