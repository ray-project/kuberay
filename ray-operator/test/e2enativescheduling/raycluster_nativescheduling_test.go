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

func TestKubernetesWAS_CreatesWorkloadAndPodGroups(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("native-sched", namespace.Name).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	LogWithTimestamp(test.T(), "Verifying Workload %s/%s exists", namespace.Name, rayCluster.Name)
	workload, err := GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(workload.Spec.ControllerRef).NotTo(BeNil())
	g.Expect(workload.Spec.ControllerRef.APIGroup).To(Equal("ray.io"))
	g.Expect(workload.Spec.ControllerRef.Kind).To(Equal("RayCluster"))
	g.Expect(workload.Spec.ControllerRef.Name).To(Equal(rayCluster.Name))

	g.Expect(workload.Spec.PodGroupTemplates).To(HaveLen(2))
	g.Expect(workload.Spec.PodGroupTemplates[0].Name).To(Equal("head"))
	g.Expect(workload.Spec.PodGroupTemplates[0].SchedulingPolicy.Basic).NotTo(BeNil())
	g.Expect(workload.Spec.PodGroupTemplates[1].Name).To(Equal("worker-small-group"))
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang).NotTo(BeNil())
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount).To(Equal(int32(1)))

	g.Expect(workload.OwnerReferences).To(HaveLen(1))
	g.Expect(workload.OwnerReferences[0].Kind).To(Equal("RayCluster"))
	g.Expect(workload.OwnerReferences[0].Name).To(Equal(rayCluster.Name))
	g.Expect(*workload.OwnerReferences[0].Controller).To(BeTrue())
	g.Expect(workload.Labels[utils.RayClusterLabelKey]).To(Equal(rayCluster.Name))

	LogWithTimestamp(test.T(), "Verifying PodGroups exist")
	headPodGroup, err := GetPodGroup(test, namespace.Name, rayCluster.Name+"-head")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPodGroup.Spec.PodGroupTemplateRef).NotTo(BeNil())
	g.Expect(headPodGroup.Spec.PodGroupTemplateRef.Workload).NotTo(BeNil())
	g.Expect(headPodGroup.Spec.PodGroupTemplateRef.Workload.WorkloadName).To(Equal(rayCluster.Name))
	g.Expect(headPodGroup.Spec.PodGroupTemplateRef.Workload.PodGroupTemplateName).To(Equal("head"))
	g.Expect(headPodGroup.Spec.SchedulingPolicy.Basic).NotTo(BeNil())
	g.Expect(headPodGroup.OwnerReferences).To(HaveLen(1))
	g.Expect(headPodGroup.OwnerReferences[0].Kind).To(Equal("RayCluster"))
	g.Expect(headPodGroup.OwnerReferences[0].Name).To(Equal(rayCluster.Name))
	g.Expect(*headPodGroup.OwnerReferences[0].Controller).To(BeTrue())
	g.Expect(headPodGroup.Labels[utils.RayClusterLabelKey]).To(Equal(rayCluster.Name))

	workerPodGroup, err := GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(workerPodGroup.Spec.PodGroupTemplateRef).NotTo(BeNil())
	g.Expect(workerPodGroup.Spec.PodGroupTemplateRef.Workload).NotTo(BeNil())
	g.Expect(workerPodGroup.Spec.PodGroupTemplateRef.Workload.WorkloadName).To(Equal(rayCluster.Name))
	g.Expect(workerPodGroup.Spec.PodGroupTemplateRef.Workload.PodGroupTemplateName).To(Equal("worker-small-group"))
	g.Expect(workerPodGroup.Spec.SchedulingPolicy.Gang).NotTo(BeNil())
	g.Expect(workerPodGroup.Spec.SchedulingPolicy.Gang.MinCount).To(Equal(int32(1)))
	g.Expect(workerPodGroup.OwnerReferences).To(HaveLen(1))
	g.Expect(workerPodGroup.OwnerReferences[0].Kind).To(Equal("RayCluster"))
	g.Expect(workerPodGroup.OwnerReferences[0].Name).To(Equal(rayCluster.Name))
	g.Expect(workerPodGroup.Labels[utils.RayClusterLabelKey]).To(Equal(rayCluster.Name))

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

func TestKubernetesWAS_PodSchedulingGroup(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("sched-group", namespace.Name).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod.Spec.SchedulerName).To(Equal(corev1.DefaultSchedulerName))
	g.Expect(headPod.Spec.SchedulingGroup).NotTo(BeNil())
	g.Expect(headPod.Spec.SchedulingGroup.PodGroupName).NotTo(BeNil())
	g.Expect(*headPod.Spec.SchedulingGroup.PodGroupName).To(Equal(rayCluster.Name + "-head"))

	workerPods, err := GetWorkerPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(workerPods).NotTo(BeEmpty())
	for _, pod := range workerPods {
		g.Expect(pod.Spec.SchedulerName).To(Equal(corev1.DefaultSchedulerName))
		g.Expect(pod.Spec.SchedulingGroup).NotTo(BeNil())
		g.Expect(pod.Spec.SchedulingGroup.PodGroupName).NotTo(BeNil())
		g.Expect(*pod.Spec.SchedulingGroup.PodGroupName).To(Equal(rayCluster.Name + "-worker-small-group"))
	}
}

func TestKubernetesWAS_MultipleWorkerGroups(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("multi-wg", namespace.Name).
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

	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(3))

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

func TestKubernetesWAS_AutoscalingSkipped(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("autoscale-skip", namespace.Name).
		WithSpec(NewRayClusterSpec().WithEnableInTreeAutoscaling(true))

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s with autoscaling", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to start reconciling", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.HeadPodReady), MatchCondition(metav1.ConditionTrue, rayv1.HeadPodRunningAndReady)))

	_, err = GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(errors.IsNotFound(err)).To(BeTrue(), "expected NotFound for Workload, got: %v", err)

	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod.Spec.SchedulerName).To(Equal(corev1.DefaultSchedulerName))
	g.Expect(headPod.Spec.SchedulingGroup).To(BeNil(), "head pod should not have schedulingGroup when autoscaling is enabled")
}

func TestKubernetesWAS_GangSchedules(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("gang-sched", namespace.Name).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready (gang scheduling)", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	allPods, err := GetAllPods(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(allPods).NotTo(BeEmpty())
	for _, pod := range allPods {
		g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
	}

	_, err = GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Verifying PodGroupScheduled condition on worker PodGroup")
	g.Eventually(PodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group"), TestTimeoutShort).
		Should(WithTransform(func(pg *schedulingv1alpha2.PodGroup) bool {
			return meta.IsStatusConditionTrue(pg.Status.Conditions, schedulingv1alpha2.PodGroupScheduled)
		}, BeTrue()))
}

func TestKubernetesWAS_OwnerReferenceGC(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("gc-test", namespace.Name).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	_, err = GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(2))

	LogWithTimestamp(test.T(), "Deleting RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Delete(test.Ctx(), rayCluster.Name, metav1.DeleteOptions{})
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for Workload to be deleted")
	g.Eventually(func() bool {
		_, err := GetWorkload(test, namespace.Name, rayCluster.Name)
		return errors.IsNotFound(err)
	}, TestTimeoutShort).Should(BeTrue())

	LogWithTimestamp(test.T(), "Waiting for PodGroups to be deleted")
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(BeEmpty())
}

func TestKubernetesWAS_Idempotent(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("idempotent", namespace.Name).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	g.Eventually(Workloads(test, namespace.Name), TestTimeoutShort).Should(HaveLen(1))
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(2))

	LogWithTimestamp(test.T(), "Verifying resource counts remain stable over time")
	g.Consistently(Workloads(test, namespace.Name), 10*time.Second, time.Second).Should(HaveLen(1))
	g.Consistently(PodGroups(test, namespace.Name), 10*time.Second, time.Second).Should(HaveLen(2))
}

func TestKubernetesWAS_SuspendDeletesResources(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("suspend-del", namespace.Name).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	g.Eventually(Workloads(test, namespace.Name), TestTimeoutShort).Should(HaveLen(1))
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(2))

	LogWithTimestamp(test.T(), "Suspending RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	rayClusterAC = rayClusterAC.WithSpec(rayClusterAC.Spec.WithSuspend(true))
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to be suspended", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterSuspended), MatchCondition(metav1.ConditionTrue, string(rayv1.RayClusterSuspended))))

	LogWithTimestamp(test.T(), "Verifying Workload is deleted after suspend")
	g.Eventually(func() bool {
		_, err := GetWorkload(test, namespace.Name, rayCluster.Name)
		return errors.IsNotFound(err)
	}, TestTimeoutShort).Should(BeTrue())

	LogWithTimestamp(test.T(), "Verifying PodGroups are deleted after suspend")
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(BeEmpty())
}

func TestKubernetesWAS_ResumeRecreatesResources(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("resume-rec", namespace.Name).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))
	g.Eventually(Workloads(test, namespace.Name), TestTimeoutShort).Should(HaveLen(1))
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(2))

	workload, err := GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())
	originalWorkloadUID := workload.UID

	LogWithTimestamp(test.T(), "Suspending RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	rayClusterAC = rayClusterAC.WithSpec(rayClusterAC.Spec.WithSuspend(true))
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(StatusCondition(rayv1.RayClusterSuspended), MatchCondition(metav1.ConditionTrue, string(rayv1.RayClusterSuspended))))
	g.Eventually(func() bool {
		_, err := GetWorkload(test, namespace.Name, rayCluster.Name)
		return errors.IsNotFound(err)
	}, TestTimeoutShort).Should(BeTrue())
	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(BeEmpty())

	LogWithTimestamp(test.T(), "Resuming RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
	rayClusterAC = rayClusterAC.WithSpec(rayClusterAC.Spec.WithSuspend(false))
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready after resume", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

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

	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(2))
	_, err = GetPodGroup(test, namespace.Name, rayCluster.Name+"-head")
	g.Expect(err).NotTo(HaveOccurred())
	_, err = GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group")
	g.Expect(err).NotTo(HaveOccurred())

	headPod, err := GetHeadPod(test, rayCluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(headPod.Spec.SchedulingGroup).NotTo(BeNil())
	g.Expect(headPod.Spec.SchedulingGroup.PodGroupName).NotTo(BeNil())
	g.Expect(*headPod.Spec.SchedulingGroup.PodGroupName).To(Equal(rayCluster.Name + "-head"))
}

func TestKubernetesWAS_ScaleUpRecreatesWorkload(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("scale-up", namespace.Name).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s successfully", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	workload, err := GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())
	originalUID := workload.UID
	g.Expect(workload.Spec.PodGroupTemplates).To(HaveLen(2))
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang).NotTo(BeNil())
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount).To(Equal(int32(1)))

	LogWithTimestamp(test.T(), "Scaling up worker replicas from 1 to 3")
	rayClusterAC.Spec.WorkerGroupSpecs[0].WithReplicas(3).WithMinReplicas(3).WithMaxReplicas(3)
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready after scale-up", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(func(inner Gomega) {
		rc, err := GetRayCluster(test, namespace.Name, rayCluster.Name)
		inner.Expect(err).NotTo(HaveOccurred())
		inner.Expect(RayClusterState(rc)).To(Equal(rayv1.Ready))
		inner.Expect(RayClusterDesiredWorkerReplicas(rc)).To(Equal(int32(3)))
	}, TestTimeoutMedium).Should(Succeed())

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

	LogWithTimestamp(test.T(), "Waiting for worker PodGroup to be recreated with updated minCount")
	g.Eventually(func() int32 {
		podGroup, err := GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group")
		if err != nil || podGroup.DeletionTimestamp != nil || podGroup.Spec.SchedulingPolicy.Gang == nil {
			return -1
		}
		return podGroup.Spec.SchedulingPolicy.Gang.MinCount
	}, TestTimeoutShort).Should(Equal(int32(3)))
}

func TestKubernetesWAS_ScaleDownRecreatesWorkload(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("scale-down", namespace.Name).
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

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	workload, err := GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())
	originalUID := workload.UID
	g.Expect(workload.Spec.PodGroupTemplates).To(HaveLen(2))
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang).NotTo(BeNil())
	g.Expect(workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount).To(Equal(int32(3)))

	LogWithTimestamp(test.T(), "Scaling down worker replicas from 3 to 1")
	rayClusterAC.Spec.WorkerGroupSpecs[0].WithReplicas(1).WithMinReplicas(1).WithMaxReplicas(1)
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready after scale-down", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(func(inner Gomega) {
		rc, err := GetRayCluster(test, namespace.Name, rayCluster.Name)
		inner.Expect(err).NotTo(HaveOccurred())
		inner.Expect(RayClusterState(rc)).To(Equal(rayv1.Ready))
		inner.Expect(RayClusterDesiredWorkerReplicas(rc)).To(Equal(int32(1)))
	}, TestTimeoutMedium).Should(Succeed())

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

	LogWithTimestamp(test.T(), "Waiting for worker PodGroup to be recreated with updated minCount")
	g.Eventually(func() int32 {
		podGroup, err := GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group")
		if err != nil || podGroup.DeletionTimestamp != nil || podGroup.Spec.SchedulingPolicy.Gang == nil {
			return -1
		}
		return podGroup.Spec.SchedulingPolicy.Gang.MinCount
	}, TestTimeoutShort).Should(Equal(int32(1)))
}

func TestKubernetesWAS_AddWorkerGroupRecreatesWorkload(t *testing.T) {
	test := With(t)
	g := NewWithT(t)

	namespace := test.NewTestNamespace()

	rayClusterAC := rayv1ac.RayCluster("add-wg", namespace.Name).
		WithSpec(NewRayClusterSpec())

	rayCluster, err := test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())
	LogWithTimestamp(test.T(), "Created RayCluster %s/%s with 1 worker group", rayCluster.Namespace, rayCluster.Name)

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

	workload, err := GetWorkload(test, namespace.Name, rayCluster.Name)
	g.Expect(err).NotTo(HaveOccurred())
	originalUID := workload.UID
	g.Expect(workload.Spec.PodGroupTemplates).To(HaveLen(2))

	LogWithTimestamp(test.T(), "Adding second worker group 'gpu-group' to RayCluster")
	rayClusterAC.Spec.WithWorkerGroupSpecs(rayv1ac.WorkerGroupSpec().
		WithReplicas(2).
		WithMinReplicas(2).
		WithMaxReplicas(2).
		WithGroupName("gpu-group").
		WithRayStartParams(map[string]string{"num-cpus": "1"}).
		WithTemplate(WorkerPodTemplateApplyConfiguration()))
	_, err = test.Client().Ray().RayV1().RayClusters(namespace.Name).Apply(test.Ctx(), rayClusterAC, TestApplyOptions)
	g.Expect(err).NotTo(HaveOccurred())

	LogWithTimestamp(test.T(), "Waiting for RayCluster %s/%s to become ready after adding worker group", rayCluster.Namespace, rayCluster.Name)
	g.Eventually(RayCluster(test, namespace.Name, rayCluster.Name), TestTimeoutMedium).
		Should(WithTransform(RayClusterState, Equal(rayv1.Ready)))

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

	g.Eventually(PodGroups(test, namespace.Name), TestTimeoutShort).Should(HaveLen(3))
	_, err = GetPodGroup(test, namespace.Name, rayCluster.Name+"-head")
	g.Expect(err).NotTo(HaveOccurred())
	_, err = GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-small-group")
	g.Expect(err).NotTo(HaveOccurred())
	_, err = GetPodGroup(test, namespace.Name, rayCluster.Name+"-worker-gpu-group")
	g.Expect(err).NotTo(HaveOccurred())
}
