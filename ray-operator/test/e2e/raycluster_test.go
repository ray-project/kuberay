package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
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
			WithSpec(newRayClusterSpec().
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
			WithSpec(newRayClusterSpec().
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
			WithSpec(newRayClusterSpec().
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

	rayClusterAC := rayv1ac.RayCluster("raycluster-suspend", namespace.Name).WithSpec(newRayClusterSpec())

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

	rayClusterAC := rayv1ac.RayCluster("raycluster-resource-quota", namespace.Name).WithSpec(newRayClusterSpec())

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
				WithTemplate(headPodTemplateApplyConfiguration().WithFinalizers("test.kuberay.io/finalizers"))).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(2).
				WithMinReplicas(1).
				WithMaxReplicas(5).
				WithGroupName("small-group").
				WithRayStartParams(map[string]string{"num-cpus": "1"}).
				WithTemplate(workerPodTemplateApplyConfiguration().WithFinalizers("test.kuberay.io/finalizers"))))

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
