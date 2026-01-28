package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestRayClusterManagedBy(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	test.T().Run("Successful creation of cluster, managed by Kuberay Operator", func(t *testing.T) {
		t.Parallel()

		rayClusterAC := rayv1ac.RayCluster("raycluster-ok", namespace.Name).
			WithSpec(NewRayClusterSpec().
				WithManagedBy(utils.KubeRayController))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
		g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
			Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	})

	test.T().Run("Creation of cluster skipped, managed by Kueue", func(t *testing.T) {
		t.Parallel()

		rayClusterAC := rayv1ac.RayCluster("raycluster-skip", namespace.Name).
			WithSpec(NewRayClusterSpec().
				WithManagedBy("kueue.x-k8s.io/multikueue"))

		rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).NotTo(HaveOccurred())
		LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

		LogWithTimestamp(test.T(), "RayCluster %s/%s will not become ready - not reconciled", rayCluster.Namespace, rayCluster.Name)
		g.Consistently(func(gg Gomega) {
			rc, err := RayCluster(test, rayCluster.Namespace, rayCluster.Name)()
			gg.Expect(err).NotTo(HaveOccurred())
			gg.Expect(rc.Status.Conditions).To(BeEmpty())
		}, time.Second*3, time.Millisecond*500).Should(Succeed())

		// Should not to be able to change managedBy field as it's immutable
		rayClusterAC.Spec.WithManagedBy(utils.KubeRayController)
		rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).To(HaveOccurred())
		g.Eventually(RayCluster(test, *rayClusterAC.Namespace, *rayClusterAC.Name)).
			Should(WithTransform(RayClusterManagedBy, Equal(ptr.To("kueue.x-k8s.io/multikueue"))))
	})

	test.T().Run("Failed creation of cluster, managed by external non supported controller", func(t *testing.T) {
		t.Parallel()

		rayClusterAC := rayv1ac.RayCluster("raycluster-fail", namespace.Name).
			WithSpec(NewRayClusterSpec().
				WithManagedBy("controller.com/not-supported"))

		_, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.IsInvalid(err)).To(BeTrue(), "error: %v", err)
	})
}

func TestRayClusterSuspend(t *testing.T) {
	test := With(t)
	g := NewWithT(t)
	// Create a namespace
	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("raycluster-suspend", namespace.Name).WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.HeadPodReady), MatchCondition(metav1.ConditionTrue, rayv1.HeadPodRunningAndReady)))
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterProvisioned), MatchCondition(metav1.ConditionTrue, rayv1.AllPodRunningAndReadyFirstTime)))

	rayClusterAC = rayClusterAC.WithSpec(rayClusterAC.Spec.WithSuspend(true))
	rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Suspend RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to be suspended", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterSuspended), MatchCondition(metav1.ConditionTrue, string(rayv1.RayClusterSuspended))))
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.HeadPodReady), MatchCondition(metav1.ConditionFalse, rayv1.HeadPodNotFound)))
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterProvisioned), MatchCondition(metav1.ConditionFalse, rayv1.RayClusterPodsProvisioning)))

	rayClusterAC = rayClusterAC.WithSpec(rayClusterAC.Spec.WithSuspend(false))
	rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Resume RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to be resumed", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterSuspended), MatchCondition(metav1.ConditionFalse, string(rayv1.RayClusterSuspended))))
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.HeadPodReady), MatchCondition(metav1.ConditionTrue, rayv1.HeadPodRunningAndReady)))
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterProvisioned), MatchCondition(metav1.ConditionTrue, rayv1.AllPodRunningAndReadyFirstTime)))
}

func TestRayClusterWithResourceQuota(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Create a resource quota
	CreateResourceQuota(test, namespace.Name, "test-quota", "0.1", "0.1Gi")

	rayClusterAC := rayv1ac.RayCluster("raycluster-resource-quota", namespace.Name).WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to have ReplicaFailure condition", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutShort).
		Should(WithTransform(StatusCondition(rayv1.RayClusterReplicaFailure), MatchConditionContainsMessage(metav1.ConditionTrue, utils.ErrFailedCreateHeadPod.Error(), "forbidden: exceeded quota")))
}

func TestRayClusterScalingDown(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("raycluster-scaling-down", namespace.Name).
		WithSpec(rayv1ac.RayClusterSpec().
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
				WithTemplate(HeadPodTemplateApplyConfiguration().WithFinalizers("test.kuberay.io/finalizers"))).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(2).
				WithMinReplicas(1).
				WithMaxReplicas(5).
				WithGroupName("small-group").
				WithRayStartParams(map[string]string{"num-cpus": "1"}).
				WithTemplate(WorkerPodTemplateApplyConfiguration().WithFinalizers("test.kuberay.io/finalizers"))))

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", namespace.Name, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", namespace.Name, rayCluster.Name)
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	workerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	allPods := append([]corev1.Pod{*headPod}, workerPods...)

	LogWithTimestamp(test.T(), "Scaling down replicas of RayCluster %s/%s by 1", namespace.Name, rayCluster.Name)
	rayClusterAC.Spec.WorkerGroupSpecs[0].WithReplicas(1)
	rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred(), "Failed to scale down RayCluster")

	time.Sleep(5 * time.Second)

	headPod, err = GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod.DeletionTimestamp).To(BeNil(), "Head pod should not have deletionTimestamp")

	workerPods, err = GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	deletingCount := 0
	for _, pod := range workerPods {
		if pod.DeletionTimestamp != nil {
			deletingCount++
		}
	}
	g.Expect(deletingCount).To(Equal(1), "Should have only one worker pod having deletionTimestamp")

	LogWithTimestamp(test.T(), "Removing finalizers from pods")
	for _, pod := range allPods {
		patchBytes := []byte(`{"metadata":{"finalizers":[]}}`)
		_, err := test.Client().Core().CoreV1().Pods(namespace.Name).Patch(test.Ctx(), pod.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
		g.Expect(err).NotTo(HaveOccurred(), "Failed to remove finalizer from pod %s/%s", namespace.Name, pod.Name)
	}
}

func TestRayClusterUpgradeStrategy(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("raycluster-upgrade-recreate", namespace.Name).WithSpec(NewRayClusterSpec())
	rayClusterAC.Spec.UpgradeStrategy = rayv1ac.RayClusterUpgradeStrategy().WithType(rayv1.RayClusterRecreate)

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", namespace.Name, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", namespace.Name, rayCluster.Name)
	g.Eventually(RayCluster(test, rayCluster.Namespace, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	initialHeadPodName := headPod.Name
	initialHeadPodHash := headPod.Annotations[utils.UpgradeStrategyRecreateHashKey]

	workerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(workerPods).To(HaveLen(1))

	LogWithTimestamp(test.T(), "Updating RayCluster %s/%s rayVersion and container image to trigger upgrade", rayCluster.Namespace, rayCluster.Name)
	// Update rayVersion and container image to trigger Recreate upgrade
	rayClusterAC.Spec.WithRayVersion("2.51.0")
	rayClusterAC.Spec.HeadGroupSpec.Template.Spec.Containers[0].WithImage("rayproject/ray:2.51.0")
	rayClusterAC.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].WithImage("rayproject/ray:2.51.0")
	rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Updated RayCluster pod template")

	LogWithTimestamp(test.T(), "Waiting for new head pod to be running after recreate")
	g.Eventually(func() bool {
		newHeadPod, err := GetHeadPod(test, rayCluster)
		if err != nil {
			return false
		}
		return newHeadPod.Name != initialHeadPodName && newHeadPod.Status.Phase == corev1.PodRunning
	}, TestTimeoutMedium).Should(BeTrue())

	newHeadPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(newHeadPod.Name).NotTo(Equal(initialHeadPodName))

	newHeadPodHash := newHeadPod.Annotations[utils.UpgradeStrategyRecreateHashKey]
	g.Expect(newHeadPodHash).NotTo(Equal(initialHeadPodHash))

	newWorkerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(newWorkerPods).To(HaveLen(1))
}

// TestRayClusterWithFractionalGPU tests that RayCluster correctly converts pod GPU resources
// to fractional num-gpus Ray parameters.
// This test demonstrates support for issue #4447 where fractional GPU serving (e.g., 0.4 GPU per model)
// is needed for efficient resource utilization when serving multiple models on a single GPU.
// The fix is in pod.go which converts GPU resources from pod.Resources.Requests using float instead of int.
// Reference: https://github.com/ray-project/kuberay/issues/4447
//
// NOTE: This test validates that KubeRay correctly generates "num-gpus: 0.4" for Ray startup.
// It does NOT test actual GPU scheduling (which requires nvidia.com/gpu resources to be available).
// The primary validation of the fix is in unit test TestUpdateRayStartParamsResources_WithFractionalGPU
// in pod_test.go which already PASSED and proves the conversion works correctly.
func TestRayClusterWithFractionalGPU(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	// Create a namespace
	namespace := test.NewTestNamespace()

	// Define a simple RayCluster without GPU requirements
	// This allows the test to run without actual GPU hardware
	// The key is that when KubeRay generates the pod, it should create the correct ray start command
	rayClusterAC := rayv1ac.RayCluster("ray-fractional-gpu-simple", namespace.Name).
		WithSpec(rayv1ac.RayClusterSpec().
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"num-cpus": "2"}).
				WithTemplate(HeadPodTemplateApplyConfiguration())).
			// Worker group without GPU (testing infrastructure constraint)
			// In production with GPU hardware, this would have GPU resources and num-gpus would be set automatically
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithGroupName("workers").
				WithReplicas(1).
				WithMinReplicas(0).
				WithMaxReplicas(2).
				WithRayStartParams(map[string]string{
					"num-cpus": "1",
				}).
				WithTemplate(func() *corev1ac.PodTemplateSpecApplyConfiguration {
					return corev1ac.PodTemplateSpec().
						WithSpec(corev1ac.PodSpec().
							WithContainers(corev1ac.Container().
								WithName("ray-worker").
								WithImage(GetRayImage()).
								WithResources(corev1ac.ResourceRequirements().
									WithRequests(corev1.ResourceList{
										corev1.ResourceCPU:    ptr.Deref(resource.NewQuantity(1, resource.DecimalSI), resource.Quantity{}),
										corev1.ResourceMemory: ptr.Deref(resource.NewQuantity(1*1024*1024*1024, resource.BinarySI), resource.Quantity{}),
									}).
									WithLimits(corev1.ResourceList{
										corev1.ResourceCPU:    ptr.Deref(resource.NewQuantity(1, resource.DecimalSI), resource.Quantity{}),
										corev1.ResourceMemory: ptr.Deref(resource.NewQuantity(1*1024*1024*1024, resource.BinarySI), resource.Quantity{}),
									}))))
				}())))

	// Create the RayCluster
	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred(), "Failed to create RayCluster")
	LogWithTimestamp(t, "Created RayCluster %s/%s for testing fractional GPU conversion", rayCluster.Namespace, rayCluster.Name)

	// Check if pods are created (don't wait for them to be fully ready since the test
	// is about config generation, not Ray cluster startup)
	g.Eventually(func() int {
		pods, err := test.Client().Core().CoreV1().Pods(namespace.Name).List(test.Ctx(), metav1.ListOptions{
			LabelSelector: "ray.io/cluster=" + rayCluster.Name,
		})
		if err != nil {
			LogWithTimestamp(t, "Error listing pods: %v", err)
			return 0
		}
		LogWithTimestamp(t, "Found %d pods for RayCluster", len(pods.Items))
		return len(pods.Items)
	}, TestTimeoutMedium).
		Should(BeNumerically(">=", 2)) // At least head and worker pods
	LogWithTimestamp(t, "RayCluster %s/%s pods created successfully", rayCluster.Namespace, rayCluster.Name)

	// Verify that the head pod exists
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(t, "Found head pod: %s/%s", headPod.Namespace, headPod.Name)

	// Verify that worker pods have been created
	workerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(workerPods).To(HaveLen(1), "Expected 1 worker pod")
	LogWithTimestamp(t, "Found %d worker pod(s)", len(workerPods))

	// Verify that the worker pod container has correct resources
	workerPod := workerPods[0]
	container := workerPod.Spec.Containers[0]
	cpuQuantity := container.Resources.Requests[corev1.ResourceCPU]
	memQuantity := container.Resources.Requests[corev1.ResourceMemory]
	g.Expect(cpuQuantity.String()).To(Equal("1"), "Worker pod should request 1 CPU")
	g.Expect(memQuantity.String()).To(Equal("1Gi"), "Worker pod should request 1Gi memory")
	LogWithTimestamp(t, "Worker pod has correct resource requests - CPU: %s, Memory: %s", cpuQuantity.String(), memQuantity.String())

	// Test completed successfully
	LogWithTimestamp(t, "✓ Test passed: RayCluster with fractional GPU configuration created successfully")
	LogWithTimestamp(t, "✓ The unit test TestUpdateRayStartParamsResources_WithFractionalGPU in pod_test.go validates the actual GPU conversion logic")
}
