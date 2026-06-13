package e2enativescheduling

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha2 "k8s.io/api/scheduling/v1alpha2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayv1ac "github.com/ray-project/kuberay/ray-operator/pkg/client/applyconfiguration/ray/v1"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestNativeScheduling_CreatesWorkloadAndPodGroups(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("native-sched", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	// Wait for cluster to become ready.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify Workload exists with correct spec.
	LogWithTimestamp(test.T(), "Verifying Workload %s/%s exists", namespace.Name, rayCluster.Name)
	workload, err := GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(workload.Spec.ControllerRef).NotTo(BeNil())
	g.Expect(workload.Spec.ControllerRef.APIGroup).To(Equal("ray.io"))
	g.Expect(workload.Spec.ControllerRef.Kind).To(Equal("RayCluster"))
	g.Expect(workload.Spec.ControllerRef.Name).To(Equal(rayCluster.Name))

	// Workload should have 2 PodGroupTemplates: head + 1 worker group.
	g.Expect(workload.Spec.PodGroupTemplates).To(HaveLen(2))
	g.Expect(workload.Spec.PodGroupTemplates[0].Name).To(Equal("head"))
	g.Expect(workload.Spec.PodGroupTemplates[0].SchedulingPolicy.Basic).NotTo(BeNil())
	g.Expect(workload.Spec.PodGroupTemplates[1].Name).To(Equal("worker-small-group"))
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang).NotTo(BeNil())
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount).To(Equal(int32(1)))

	// Verify ownerReference points to the RayCluster.
	g.Expect(workload.OwnerReferences).To(HaveLen(1))
	g.Expect(workload.OwnerReferences[0].Kind).To(Equal("RayCluster"))
	g.Expect(workload.OwnerReferences[0].Name).To(Equal(rayCluster.Name))
	g.Expect(*workload.OwnerReferences[0].Controller).To(BeTrue())

	// Verify labels on Workload.
	g.Expect(workload.Labels[utils.RayClusterLabelKey]).To(Equal(rayCluster.Name))

	// Verify PodGroups exist.
	LogWithTimestamp(test.T(), "Verifying PodGroups exist")
	headPG, err := GetPodGroup(test, namespace.Name, rayCluster.Name+"-head")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPG.Spec.PodGroupTemplateRef).NotTo(BeNil())
	g.Expect(headPG.Spec.PodGroupTemplateRef.Workload).NotTo(BeNil())
	g.Expect(headPG.Spec.PodGroupTemplateRef.Workload.WorkloadName).To(Equal(rayCluster.Name))
	g.Expect(headPG.Spec.PodGroupTemplateRef.Workload.PodGroupTemplateName).To(Equal("head"))
	g.Expect(headPG.Spec.SchedulingPolicy.Basic).NotTo(BeNil())

	// Verify PodGroup ownerReference and labels.
	g.Expect(headPG.OwnerReferences).To(HaveLen(1))
	g.Expect(headPG.OwnerReferences[0].Kind).To(Equal("RayCluster"))
	g.Expect(headPG.OwnerReferences[0].Name).To(Equal(rayCluster.Name))
	g.Expect(*headPG.OwnerReferences[0].Controller).To(BeTrue())
	g.Expect(headPG.Labels[utils.RayClusterLabelKey]).To(Equal(rayCluster.Name))

	workerPG, err := GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(workerPG.Spec.PodGroupTemplateRef).NotTo(BeNil())
	g.Expect(workerPG.Spec.PodGroupTemplateRef.Workload).NotTo(BeNil())
	g.Expect(workerPG.Spec.PodGroupTemplateRef.Workload.WorkloadName).To(Equal(rayCluster.Name))
	g.Expect(workerPG.Spec.PodGroupTemplateRef.Workload.PodGroupTemplateName).To(Equal("worker-small-group"))
	g.Expect(workerPG.Spec.SchedulingPolicy.Gang).NotTo(BeNil())
	g.Expect(workerPG.Spec.SchedulingPolicy.Gang.MinCount).To(Equal(int32(1)))
	g.Expect(workerPG.OwnerReferences).To(HaveLen(1))
	g.Expect(workerPG.OwnerReferences[0].Kind).To(Equal("RayCluster"))
	g.Expect(workerPG.OwnerReferences[0].Name).To(Equal(rayCluster.Name))
	g.Expect(workerPG.Labels[utils.RayClusterLabelKey]).To(Equal(rayCluster.Name))

	// Verify the scheduler processed the PodGroups by checking PodGroupScheduled condition.
	LogWithTimestamp(test.T(), "Verifying PodGroupScheduled condition on PodGroups")
	g.Eventually(PodGroup(test, namespace.Name, rayCluster.Name+"-head"), TestTimeoutShort).
		Should(WithTransform(func(pg *schedulingv1alpha2.PodGroup) bool {
			return meta.IsStatusConditionTrue(pg.Status.Conditions, schedulingv1alpha2.PodGroupScheduled)
		}, BeTrue()))
	g.Eventually(PodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group"), TestTimeoutShort).
		Should(WithTransform(func(pg *schedulingv1alpha2.PodGroup) bool {
			return meta.IsStatusConditionTrue(pg.Status.Conditions, schedulingv1alpha2.PodGroupScheduled)
		}, BeTrue()))
}

func TestNativeScheduling_PodSchedulingGroup(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("sched-group", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify head pod has schedulingGroup set.
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod.Spec.SchedulingGroup).NotTo(BeNil())
	g.Expect(headPod.Spec.SchedulingGroup.PodGroupName).NotTo(BeNil())
	g.Expect(*headPod.Spec.SchedulingGroup.PodGroupName).To(Equal(rayCluster.Name + "-head"))

	// Verify worker pods have schedulingGroup set.
	workerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(workerPods).NotTo(BeEmpty())
	for _, pod := range workerPods {
		g.Expect(pod.Spec.SchedulingGroup).NotTo(BeNil())
		g.Expect(pod.Spec.SchedulingGroup.PodGroupName).NotTo(BeNil())
		g.Expect(*pod.Spec.SchedulingGroup.PodGroupName).To(Equal(rayCluster.Name + "-worker-small-group"))
	}
}

func TestNativeScheduling_MultipleWorkerGroups(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("multi-wg", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(rayv1ac.RayClusterSpec().
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
				WithTemplate(HeadPodTemplateApplyConfiguration())).
			WithWorkerGroupSpecs(
				rayv1ac.WorkerGroupSpec().
					WithReplicas(1).
					WithMinReplicas(1).
					WithMaxReplicas(1).
					WithGroupName("group-a").
					WithRayStartParams(map[string]string{"num-cpus": "1"}).
					WithTemplate(WorkerPodTemplateApplyConfiguration()),
				rayv1ac.WorkerGroupSpec().
					WithReplicas(2).
					WithMinReplicas(2).
					WithMaxReplicas(2).
					WithGroupName("group-b").
					WithRayStartParams(map[string]string{"num-cpus": "1"}).
					WithTemplate(WorkerPodTemplateApplyConfiguration()),
			))

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s with 2 worker groups", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify Workload has 3 PodGroupTemplates: head + 2 worker groups.
	workload, err := GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(workload.Spec.PodGroupTemplates).To(HaveLen(3))
	g.Expect(workload.Spec.PodGroupTemplates[0].Name).To(Equal("head"))
	g.Expect(workload.Spec.PodGroupTemplates[1].Name).To(Equal("worker-group-a"))
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang).NotTo(BeNil())
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount).To(Equal(int32(1)))
	g.Expect(workload.Spec.PodGroupTemplates[2].Name).To(Equal("worker-group-b"))
	g.Expect(workload.Spec.PodGroupTemplates[2].SchedulingPolicy.Gang).NotTo(BeNil())
	g.Expect(workload.Spec.PodGroupTemplates[2].SchedulingPolicy.Gang.MinCount).To(Equal(int32(2)))

	// Verify 3 PodGroups exist (head + group-a + group-b).
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(3))

	pgGroupA, err := GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-group-a")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pgGroupA.Spec.SchedulingPolicy.Gang).NotTo(BeNil())
	g.Expect(pgGroupA.Spec.SchedulingPolicy.Gang.MinCount).To(Equal(int32(1)))

	pgGroupB, err := GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-group-b")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pgGroupB.Spec.SchedulingPolicy.Gang).NotTo(BeNil())
	g.Expect(pgGroupB.Spec.SchedulingPolicy.Gang.MinCount).To(Equal(int32(2)))

	// Verify pods in each worker group reference the correct PodGroup.
	LogWithTimestamp(test.T(), "Verifying per-group pod schedulingGroup references")
	allWorkerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	for _, pod := range allWorkerPods {
		group := pod.Labels[utils.RayNodeGroupLabelKey]
		g.Expect(pod.Spec.SchedulingGroup).NotTo(BeNil(), "pod %s missing schedulingGroup", pod.Name)
		g.Expect(pod.Spec.SchedulingGroup.PodGroupName).NotTo(BeNil(), "pod %s missing podGroupName", pod.Name)
		g.Expect(*pod.Spec.SchedulingGroup.PodGroupName).To(Equal(rayCluster.Name+"-worker-"+group),
			"pod %s has wrong podGroupName", pod.Name)
	}
}

func TestNativeScheduling_Events(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("sched-events", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify CreatedWorkload event was emitted.
	g.Eventually(GetEvents(test, namespace.Name, rayCluster.Name, "CreatedWorkload"), TestTimeoutShort).
		ShouldNot(BeEmpty())

	// Verify CreatedPodGroup events were emitted (head + worker).
	g.Eventually(GetEvents(test, namespace.Name, rayCluster.Name, "CreatedPodGroup"), TestTimeoutShort).
		Should(HaveLen(2))
}

func TestNativeScheduling_NoAnnotation(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	// Create a RayCluster without the native scheduling annotation.
	rayClusterAC := rayv1ac.RayCluster("no-annotation", namespace.Name).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s without native scheduling annotation", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify no Workload was created.
	_, err = GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(errors.IsNotFound(err)).To(BeTrue(), "expected NotFound for Workload, got: %v", err)

	// Verify no PodGroups were created.
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(BeEmpty())

	// Verify head pod does not have schedulingGroup set.
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod.Spec.SchedulingGroup).To(BeNil())
}

func TestNativeScheduling_AutoscalingSkipped(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	// Create a RayCluster with autoscaling + native scheduling annotation.
	rayClusterAC := rayv1ac.RayCluster("autoscale-skip", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(NewRayClusterSpec().WithEnableInTreeAutoscaling(true))

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s with autoscaling + native scheduling", rayCluster.Namespace, rayCluster.Name)

	// Wait for the cluster to start reconciling (HeadPodReady condition appears).
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to start reconciling", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.HeadPodReady), MatchCondition(metav1.ConditionTrue, rayv1.HeadPodRunningAndReady)))

	// Verify WorkloadSchedulingSkipped warning event was emitted.
	g.Eventually(GetEvents(test, namespace.Name, rayCluster.Name, "WorkloadSchedulingSkipped"), TestTimeoutShort).
		ShouldNot(BeEmpty())

	// Verify no Workload was created.
	_, err = GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(errors.IsNotFound(err)).To(BeTrue(), "expected NotFound for Workload, got: %v", err)

	// Verify head pod does not have schedulingGroup set (autoscaling skipped native scheduling).
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod.Spec.SchedulingGroup).To(BeNil(), "head pod should not have schedulingGroup when autoscaling is enabled")
}

func TestNativeScheduling_GangSchedules(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("gang-sched", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	// Wait for the cluster to become ready — this validates that the scheduler
	// processes the gang and all pods in the gang become Running.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready (gang scheduling)", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify all pods are Running.
	allPods, err := GetAllPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(allPods).NotTo(BeEmpty())
	for _, pod := range allPods {
		g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
	}

	// Verify Workload and PodGroups were created.
	_, err = GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the scheduler's gang plugin processed the PodGroups.
	// PodGroupScheduled=True confirms the gang constraint was evaluated and satisfied,
	// not just that pods happened to schedule independently.
	LogWithTimestamp(test.T(), "Verifying PodGroupScheduled condition on worker PodGroup")
	g.Eventually(PodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group"), TestTimeoutShort).
		Should(WithTransform(func(pg *schedulingv1alpha2.PodGroup) bool {
			return meta.IsStatusConditionTrue(pg.Status.Conditions, schedulingv1alpha2.PodGroupScheduled)
		}, BeTrue()))
}

func TestNativeScheduling_OwnerReferenceGC(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("gc-test", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	// Wait for cluster to become ready so Workload and PodGroups are created.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify Workload and PodGroups exist before deletion.
	_, err = GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(2))

	// Delete the RayCluster.
	LogWithTimestamp(test.T(), "Deleting RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify Workload is garbage collected.
	LogWithTimestamp(test.T(), "Waiting for Workload to be garbage collected")
	g.Eventually(func() bool {
		_, err := GetWorkload(test, namespace.Name, rayCluster.Name)
		return errors.IsNotFound(err)
	}, TestTimeoutShort).Should(BeTrue())

	// Verify PodGroups are garbage collected.
	LogWithTimestamp(test.T(), "Waiting for PodGroups to be garbage collected")
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(BeEmpty())
}

func TestNativeScheduling_Idempotent(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("idempotent", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify exactly 1 Workload and 2 PodGroups exist.
	g.Eventually(Workloads(test, namespace.Name), TestTimeoutShort).Should(HaveLen(1))
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(2))

	// Wait and verify the count stays stable (no duplicates created by re-reconciliation).
	LogWithTimestamp(test.T(), "Verifying resource counts remain stable over time")
	g.Consistently(Workloads(test, namespace.Name), 10*time.Second, time.Second).Should(HaveLen(1))
	g.Consistently(PodGroups(test, namespace.Name), 10*time.Second, time.Second).Should(HaveLen(2))
}

func TestNativeScheduling_SuspendDeletesResources(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("suspend-del", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	// Wait for cluster to become ready so Workload and PodGroups are created.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify Workload and PodGroups exist before suspend.
	g.Eventually(Workloads(test, namespace.Name), TestTimeoutShort).Should(HaveLen(1))
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(2))

	// Verify WorkloadScheduled condition is True before suspend.
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutShort).
		Should(WithTransform(StatusCondition(rayv1.RayClusterWorkloadScheduled), MatchCondition(metav1.ConditionTrue, rayv1.WorkloadReady)))

	// Suspend the cluster.
	LogWithTimestamp(test.T(), "Suspending RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	rayClusterAC = rayClusterAC.WithSpec(rayClusterAC.Spec.WithSuspend(true))
	rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	// Wait for the cluster to be suspended.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to be suspended", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterSuspended), MatchCondition(metav1.ConditionTrue, string(rayv1.RayClusterSuspended))))

	// Verify Workload is deleted after suspend.
	LogWithTimestamp(test.T(), "Verifying Workload is deleted after suspend")
	g.Eventually(func() bool {
		_, err := GetWorkload(test, namespace.Name, rayCluster.Name)
		return errors.IsNotFound(err)
	}, TestTimeoutShort).Should(BeTrue())

	// Verify PodGroups are deleted after suspend.
	LogWithTimestamp(test.T(), "Verifying PodGroups are deleted after suspend")
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(BeEmpty())

	// Verify WorkloadScheduled condition is removed after suspend.
	LogWithTimestamp(test.T(), "Verifying WorkloadScheduled condition is removed after suspend")
	g.Eventually(func(gg Gomega) {
		cluster, err := GetRayCluster(test, namespace.Name, rayCluster.Name)
		gg.Expect(err).NotTo(HaveOccurred())
		cond := meta.FindStatusCondition(cluster.Status.Conditions, string(rayv1.RayClusterWorkloadScheduled))
		gg.Expect(cond).To(BeNil(), "WorkloadScheduled condition should be removed when suspended")
	}, TestTimeoutShort).Should(Succeed())

	// Verify DeletedWorkload and DeletedPodGroup events were emitted.
	g.Eventually(GetEvents(test, namespace.Name, rayCluster.Name, "DeletedWorkload"), TestTimeoutShort).
		ShouldNot(BeEmpty())
	g.Eventually(GetEvents(test, namespace.Name, rayCluster.Name, "DeletedPodGroup"), TestTimeoutShort).
		ShouldNot(BeEmpty())
}

func TestNativeScheduling_ResumeRecreatesResources(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("resume-rec", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	// Wait for cluster to become ready.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	g.Eventually(Workloads(test, namespace.Name), TestTimeoutShort).Should(HaveLen(1))
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(2))

	// Capture the original Workload UID to verify it changes after resume.
	workload, err := GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())
	originalWorkloadUID := workload.UID

	// Suspend the cluster.
	LogWithTimestamp(test.T(), "Suspending RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	rayClusterAC = rayClusterAC.WithSpec(rayClusterAC.Spec.WithSuspend(true))
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	// Wait for suspend to complete and resources to be deleted.
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterSuspended), MatchCondition(metav1.ConditionTrue, string(rayv1.RayClusterSuspended))))
	g.Eventually(func() bool {
		_, err := GetWorkload(test, namespace.Name, rayCluster.Name)
		return errors.IsNotFound(err)
	}, TestTimeoutShort).Should(BeTrue())
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(BeEmpty())

	// Resume the cluster.
	LogWithTimestamp(test.T(), "Resuming RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	rayClusterAC = rayClusterAC.WithSpec(rayClusterAC.Spec.WithSuspend(false))
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	// Wait for cluster to become ready again.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready after resume", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify Workload is recreated with correct spec and a new UID (proving it was deleted and recreated).
	LogWithTimestamp(test.T(), "Verifying Workload is recreated after resume")
	g.Eventually(func(inner Gomega) {
		w, err := GetWorkload(test, namespace.Name, rayCluster.Name)
		inner.Expect(err).NotTo(HaveOccurred())
		inner.Expect(w.UID).NotTo(Equal(originalWorkloadUID), "Workload should have a new UID after resume")
		inner.Expect(w.Spec.PodGroupTemplates).To(HaveLen(2))
		inner.Expect(w.Spec.PodGroupTemplates[0].Name).To(Equal("head"))
		inner.Expect(w.Spec.PodGroupTemplates[1].Name).To(Equal("worker-small-group"))
		inner.Expect(w.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang).NotTo(BeNil())
		inner.Expect(w.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount).To(Equal(int32(1)))
	}, TestTimeoutShort).Should(Succeed())

	// Verify PodGroups are recreated.
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(2))

	_, err = GetPodGroup(test, namespace.Name, rayCluster.Name+"-head")
	g.Expect(err).NotTo(HaveOccurred())
	_, err = GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group")
	g.Expect(err).NotTo(HaveOccurred())

	// Verify WorkloadScheduled condition is True after resume.
	LogWithTimestamp(test.T(), "Verifying WorkloadScheduled condition is True after resume")
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutShort).
		Should(WithTransform(StatusCondition(rayv1.RayClusterWorkloadScheduled), MatchCondition(metav1.ConditionTrue, rayv1.WorkloadReady)))

	// Verify new pods get schedulingGroup set.
	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod.Spec.SchedulingGroup).NotTo(BeNil())
	g.Expect(headPod.Spec.SchedulingGroup.PodGroupName).NotTo(BeNil())
	g.Expect(*headPod.Spec.SchedulingGroup.PodGroupName).To(Equal(rayCluster.Name + "-head"))

	workerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(workerPods).NotTo(BeEmpty())
	for _, pod := range workerPods {
		g.Expect(pod.Spec.SchedulingGroup).NotTo(BeNil())
		g.Expect(pod.Spec.SchedulingGroup.PodGroupName).NotTo(BeNil())
		g.Expect(*pod.Spec.SchedulingGroup.PodGroupName).To(Equal(rayCluster.Name + "-worker-small-group"))
	}
}

func TestNativeScheduling_ScaleUpRecreatesWorkload(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("scale-up", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	// Wait for cluster to become ready.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Record the original Workload UID.
	workload, err := GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())
	originalUID := workload.UID
	g.Expect(workload.Spec.PodGroupTemplates).To(HaveLen(2))
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang).NotTo(BeNil())
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount).To(Equal(int32(1)))

	// Scale up workers from 1 to 3.
	LogWithTimestamp(test.T(), "Scaling up worker replicas from 1 to 3")
	rayClusterAC.Spec.WorkerGroupSpecs[0].WithReplicas(3).WithMinReplicas(3).WithMaxReplicas(3)
	rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	// Wait for the cluster to become ready with new replicas.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready after scale-up", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(func(inner Gomega) {
		rc, err := GetRayCluster(test, namespace.Name, rayCluster.Name)
		inner.Expect(err).NotTo(HaveOccurred())
		inner.Expect(RayClusterState(rc)).To(Equal(rayv1.Ready))
		inner.Expect(RayClusterDesiredWorkerReplicas(rc)).To(Equal(int32(3)))
	}, TestTimeoutMedium).Should(Succeed())

	// Verify the Workload was recreated (different UID) with updated minCount.
	// Both checks are in a single Eventually to avoid a race where the UID passes
	// but the spec is read from a stale object.
	LogWithTimestamp(test.T(), "Verifying Workload was recreated with updated minCount")
	g.Eventually(func(inner Gomega) {
		w, err := GetWorkload(test, namespace.Name, rayCluster.Name)
		inner.Expect(err).NotTo(HaveOccurred())
		inner.Expect(w.UID).NotTo(Equal(originalUID), "Workload should have been recreated with a new UID")
		inner.Expect(w.Spec.PodGroupTemplates).To(HaveLen(2))
		inner.Expect(w.Spec.PodGroupTemplates[1].Name).To(Equal("worker-small-group"))
		inner.Expect(w.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang).NotTo(BeNil())
		inner.Expect(w.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount).To(Equal(int32(3)))
	}, TestTimeoutShort).Should(Succeed())

	// Verify PodGroups are recreated with updated minCount.
	// PodGroups may be delayed by scheduler finalizer removal during the delete-then-create flow,
	// so we use Eventually to wait for the reconciler to successfully recreate them.
	LogWithTimestamp(test.T(), "Waiting for worker PodGroup to be recreated with updated minCount")
	g.Eventually(func() int32 {
		pg, err := GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group")
		if err != nil || pg.DeletionTimestamp != nil {
			return -1
		}
		if pg.Spec.SchedulingPolicy.Gang == nil {
			return -1
		}
		return pg.Spec.SchedulingPolicy.Gang.MinCount
	}, TestTimeoutShort).Should(Equal(int32(3)))
}

func TestNativeScheduling_ScaleDownRecreatesWorkload(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	// Start with 3 replicas so we can scale down.
	rayClusterAC := rayv1ac.RayCluster("scale-down", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(rayv1ac.RayClusterSpec().
			WithRayVersion(GetRayVersion()).
			WithHeadGroupSpec(rayv1ac.HeadGroupSpec().
				WithRayStartParams(map[string]string{"dashboard-host": "0.0.0.0"}).
				WithTemplate(HeadPodTemplateApplyConfiguration())).
			WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
				WithReplicas(3).
				WithMinReplicas(3).
				WithMaxReplicas(3).
				WithGroupName("small-group").
				WithRayStartParams(map[string]string{"num-cpus": "1"}).
				WithTemplate(WorkerPodTemplateApplyConfiguration())))

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s with 3 replicas", rayCluster.Namespace, rayCluster.Name)

	// Wait for cluster to become ready.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Record the original Workload UID and verify minCount=3.
	workload, err := GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())
	originalUID := workload.UID
	g.Expect(workload.Spec.PodGroupTemplates).To(HaveLen(2))
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang).NotTo(BeNil())
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount).To(Equal(int32(3)))

	// Scale down workers from 3 to 1.
	LogWithTimestamp(test.T(), "Scaling down worker replicas from 3 to 1")
	rayClusterAC.Spec.WorkerGroupSpecs[0].WithReplicas(1).WithMinReplicas(1).WithMaxReplicas(1)
	rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	// Wait for the cluster to become ready with new replicas.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready after scale-down", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(func(inner Gomega) {
		rc, err := GetRayCluster(test, namespace.Name, rayCluster.Name)
		inner.Expect(err).NotTo(HaveOccurred())
		inner.Expect(RayClusterState(rc)).To(Equal(rayv1.Ready))
		inner.Expect(RayClusterDesiredWorkerReplicas(rc)).To(Equal(int32(1)))
	}, TestTimeoutMedium).Should(Succeed())

	// Verify the Workload was recreated (different UID) with updated minCount=1.
	LogWithTimestamp(test.T(), "Verifying Workload was recreated with updated minCount")
	g.Eventually(func(inner Gomega) {
		w, err := GetWorkload(test, namespace.Name, rayCluster.Name)
		inner.Expect(err).NotTo(HaveOccurred())
		inner.Expect(w.UID).NotTo(Equal(originalUID), "Workload should have a new UID after scale-down")
		inner.Expect(w.Spec.PodGroupTemplates).To(HaveLen(2))
		inner.Expect(w.Spec.PodGroupTemplates[1].Name).To(Equal("worker-small-group"))
		inner.Expect(w.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang).NotTo(BeNil())
		inner.Expect(w.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount).To(Equal(int32(1)))
	}, TestTimeoutShort).Should(Succeed())

	// Verify worker PodGroup is recreated with updated minCount.
	LogWithTimestamp(test.T(), "Waiting for worker PodGroup to be recreated with updated minCount")
	g.Eventually(func() int32 {
		pg, err := GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group")
		if err != nil || pg.DeletionTimestamp != nil {
			return -1
		}
		if pg.Spec.SchedulingPolicy.Gang == nil {
			return -1
		}
		return pg.Spec.SchedulingPolicy.Gang.MinCount
	}, TestTimeoutShort).Should(Equal(int32(1)))
}

func TestNativeScheduling_AddWorkerGroupRecreatesWorkload(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("add-wg", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s with 1 worker group", rayCluster.Namespace, rayCluster.Name)

	// Wait for cluster to become ready.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Record the original Workload UID and verify 2 PodGroupTemplates (head + 1 worker group).
	workload, err := GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())
	originalUID := workload.UID
	g.Expect(workload.Spec.PodGroupTemplates).To(HaveLen(2))

	// Add a second worker group.
	LogWithTimestamp(test.T(), "Adding second worker group 'gpu-group' to RayCluster")
	rayClusterAC.Spec.WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
		WithReplicas(2).
		WithMinReplicas(2).
		WithMaxReplicas(2).
		WithGroupName("gpu-group").
		WithRayStartParams(map[string]string{"num-cpus": "1"}).
		WithTemplate(WorkerPodTemplateApplyConfiguration()))
	rayCluster, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	// Wait for the cluster to become ready with the new worker group.
	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready after adding worker group", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify the Workload was recreated with 3 PodGroupTemplates (head + 2 worker groups).
	LogWithTimestamp(test.T(), "Verifying Workload was recreated with 3 PodGroupTemplates")
	g.Eventually(func(inner Gomega) {
		w, err := GetWorkload(test, namespace.Name, rayCluster.Name)
		inner.Expect(err).NotTo(HaveOccurred())
		inner.Expect(w.UID).NotTo(Equal(originalUID), "Workload should have a new UID after adding worker group")
		inner.Expect(w.Spec.PodGroupTemplates).To(HaveLen(3))
		inner.Expect(w.Spec.PodGroupTemplates[0].Name).To(Equal("head"))
		inner.Expect(w.Spec.PodGroupTemplates[1].Name).To(Equal("worker-small-group"))
		inner.Expect(w.Spec.PodGroupTemplates[2].Name).To(Equal("worker-gpu-group"))
		inner.Expect(w.Spec.PodGroupTemplates[2].SchedulingPolicy.Gang).NotTo(BeNil())
		inner.Expect(w.Spec.PodGroupTemplates[2].SchedulingPolicy.Gang.MinCount).To(Equal(int32(2)))
	}, TestTimeoutShort).Should(Succeed())

	// Verify 3 PodGroups exist (head + 2 worker groups).
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(3))

	_, err = GetPodGroup(test, namespace.Name, rayCluster.Name+"-head")
	g.Expect(err).NotTo(HaveOccurred())
	_, err = GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group")
	g.Expect(err).NotTo(HaveOccurred())
	_, err = GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-gpu-group")
	g.Expect(err).NotTo(HaveOccurred())
}

func TestNativeScheduling_WorkloadScheduledCondition(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("cond-test", namespace.Name).
		WithAnnotations(map[string]string{"ray.io/native-workload-scheduling": "true"}).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)

	// Wait for cluster to become ready.
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify the WorkloadScheduled condition is True/WorkloadReady.
	LogWithTimestamp(test.T(), "Verifying WorkloadScheduled condition is True")
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutShort).
		Should(WithTransform(StatusCondition(rayv1.RayClusterWorkloadScheduled), SatisfyAll(
			WithTransform(func(c metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
			WithTransform(func(c metav1.Condition) string { return c.Reason }, Equal(rayv1.WorkloadReady)),
		)))
}

func TestNativeScheduling_WorkloadScheduledConditionAbsentWhenDisabled(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	// Create a RayCluster without the native scheduling annotation.
	rayClusterAC := rayv1ac.RayCluster("no-cond", namespace.Name).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s without native scheduling annotation", rayCluster.Namespace, rayCluster.Name)

	// Wait for cluster to become ready.
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	// Verify no WorkloadScheduled condition is set, and it stays absent over time.
	LogWithTimestamp(test.T(), "Verifying WorkloadScheduled condition is absent")
	g.Consistently(func(gg Gomega) {
		cluster, err := GetRayCluster(test, namespace.Name, rayCluster.Name)
		gg.Expect(err).NotTo(HaveOccurred())
		cond := meta.FindStatusCondition(cluster.Status.Conditions, string(rayv1.RayClusterWorkloadScheduled))
		gg.Expect(cond).To(BeNil(), "WorkloadScheduled condition should not be set without annotation")
	}, 10*time.Second, time.Second).Should(Succeed())
}
