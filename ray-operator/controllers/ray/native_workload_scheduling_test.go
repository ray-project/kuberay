package ray

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha2 "k8s.io/api/scheduling/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/expectations"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = rayv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = schedulingv1alpha2.AddToScheme(s)
	return s
}

func newTestRayCluster(workerGroups ...rayv1.WorkerGroupSpec) *rayv1.RayCluster {
	return &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
			UID:       types.UID("test-uid"),
			Annotations: map[string]string{
				NativeWorkloadSchedulingAnnotation: "true",
			},
		},
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				RayStartParams: map[string]string{
					"port":     "6379",
					"num-cpus": "1",
				},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "ray-head", Image: "rayproject/ray:latest"}},
					},
				},
			},
			WorkerGroupSpecs: workerGroups,
		},
	}
}

func newWorkerGroup(name string, replicas int32) rayv1.WorkerGroupSpec {
	return rayv1.WorkerGroupSpec{
		GroupName:   name,
		Replicas:    new(replicas),
		MinReplicas: new(replicas),
		MaxReplicas: new(replicas),
		NumOfHosts:  1,
		RayStartParams: map[string]string{
			"port":     "6379",
			"num-cpus": "1",
		},
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "ray-worker", Image: "rayproject/ray:latest"}},
			},
		},
	}
}

func newReconciler(fakeClient client.Client, s *runtime.Scheme, recorder record.EventRecorder, opts ...RayClusterReconcilerOptions) *RayClusterReconciler {
	var options RayClusterReconcilerOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	return &RayClusterReconciler{
		Client:                     fakeClient,
		Scheme:                     s,
		Recorder:                   recorder,
		options:                    options,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}
}

// --- Reconcile behavior tests ---

func TestReconcileNativeWorkloadScheduling_CreatesWorkloadAndPodGroups(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))
	ctx := context.Background()

	err := r.reconcileNativeWorkloadScheduling(ctx, cluster)
	require.NoError(t, err)

	// Verify Workload was created.
	workload := &schedulingv1alpha2.Workload{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster", Namespace: "default"}, workload)
	require.NoError(t, err)
	assert.Equal(t, "test-cluster", workload.Name)
	assert.Len(t, workload.Spec.PodGroupTemplates, 2) // head + 1 worker group

	// Verify head PodGroup was created.
	headPG := &schedulingv1alpha2.PodGroup{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster-head", Namespace: "default"}, headPG)
	require.NoError(t, err)

	// Verify worker PodGroup was created.
	workerPG := &schedulingv1alpha2.PodGroup{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster-worker-workers", Namespace: "default"}, workerPG)
	require.NoError(t, err)
}

func TestReconcileNativeWorkloadScheduling_MultipleWorkerGroups(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(
		newWorkerGroup("cpu-workers", 2),
		newWorkerGroup("gpu-workers", 4),
		newWorkerGroup("tpu-workers", 1),
	)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))
	ctx := context.Background()

	err := r.reconcileNativeWorkloadScheduling(ctx, cluster)
	require.NoError(t, err)

	// Verify Workload has 4 templates (1 head + 3 worker groups).
	workload := &schedulingv1alpha2.Workload{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster", Namespace: "default"}, workload)
	require.NoError(t, err)
	assert.Len(t, workload.Spec.PodGroupTemplates, 4)

	// Verify all 4 PodGroups exist.
	for _, pgName := range []string{"test-cluster-head", "test-cluster-worker-cpu-workers", "test-cluster-worker-gpu-workers", "test-cluster-worker-tpu-workers"} {
		pg := &schedulingv1alpha2.PodGroup{}
		err = fakeClient.Get(ctx, types.NamespacedName{Name: pgName, Namespace: "default"}, pg)
		require.NoError(t, err, "PodGroup %s should exist", pgName)
	}
}

func TestReconcileNativeWorkloadScheduling_Idempotent(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(20))
	ctx := context.Background()

	// Call twice — second call should succeed (AlreadyExists is a no-op).
	err := r.reconcileNativeWorkloadScheduling(ctx, cluster)
	require.NoError(t, err)
	err = r.reconcileNativeWorkloadScheduling(ctx, cluster)
	require.NoError(t, err)

	// Verify still only one Workload.
	workloadList := &schedulingv1alpha2.WorkloadList{}
	err = fakeClient.List(ctx, workloadList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	assert.Len(t, workloadList.Items, 1)

	// Verify still only 2 PodGroups (head + worker).
	pgList := &schedulingv1alpha2.PodGroupList{}
	err = fakeClient.List(ctx, pgList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	assert.Len(t, pgList.Items, 2)
}

func TestReconcileNativeWorkloadScheduling_SkipsWhenAnnotationMissing(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	cluster.Annotations = nil // No annotation
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))
	ctx := context.Background()

	err := r.reconcileNativeWorkloadScheduling(ctx, cluster)
	require.NoError(t, err)

	// No Workloads should be created.
	workloadList := &schedulingv1alpha2.WorkloadList{}
	err = fakeClient.List(ctx, workloadList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	assert.Empty(t, workloadList.Items)
}

func TestReconcileNativeWorkloadScheduling_SkipsWhenFeatureGateDisabled(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, false)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))
	ctx := context.Background()

	err := r.reconcileNativeWorkloadScheduling(ctx, cluster)
	require.NoError(t, err)

	// No Workloads should be created.
	workloadList := &schedulingv1alpha2.WorkloadList{}
	err = fakeClient.List(ctx, workloadList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	assert.Empty(t, workloadList.Items)
}

func TestReconcileNativeWorkloadScheduling_SkipsWhenAutoscalingEnabled(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	cluster.Spec.EnableInTreeAutoscaling = new(true)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	fakeRecorder := record.NewFakeRecorder(10)
	r := newReconciler(fakeClient, s, fakeRecorder)
	ctx := context.Background()

	err := r.reconcileNativeWorkloadScheduling(ctx, cluster)
	require.NoError(t, err)

	// No Workloads should be created.
	workloadList := &schedulingv1alpha2.WorkloadList{}
	err = fakeClient.List(ctx, workloadList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	assert.Empty(t, workloadList.Items)

	// Should have emitted a warning event.
	assert.Len(t, fakeRecorder.Events, 1)
}

func TestReconcileNativeWorkloadScheduling_SkipsWhenBatchSchedulerConfigured(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	fakeRecorder := record.NewFakeRecorder(10)
	// Create a SchedulerManager with a real non-default batch scheduler (yunikorn).
	batchMgr, err := batchscheduler.NewSchedulerManager(context.Background(),
		configapi.Configuration{BatchScheduler: "yunikorn"}, nil, nil)
	require.NoError(t, err)
	r := newReconciler(fakeClient, s, fakeRecorder, RayClusterReconcilerOptions{
		BatchSchedulerManager: batchMgr,
	})
	ctx := context.Background()

	err = r.reconcileNativeWorkloadScheduling(ctx, cluster)
	require.NoError(t, err)

	// No Workloads should be created.
	workloadList := &schedulingv1alpha2.WorkloadList{}
	err = fakeClient.List(ctx, workloadList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	assert.Empty(t, workloadList.Items)

	// Should have emitted a warning event.
	assert.Len(t, fakeRecorder.Events, 1)
}

func TestReconcileNativeWorkloadScheduling_FailsWhenMoreThan7WorkerGroups(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	workers := make([]rayv1.WorkerGroupSpec, 8)
	for i := range workers {
		workers[i] = newWorkerGroup("workers-"+string(rune('a'+i)), 1)
	}
	cluster := newTestRayCluster(workers...)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	fakeRecorder := record.NewFakeRecorder(10)
	r := newReconciler(fakeClient, s, fakeRecorder)
	ctx := context.Background()

	err := r.reconcileNativeWorkloadScheduling(ctx, cluster)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeding the maximum of 7")

	// No Workloads should be created.
	workloadList := &schedulingv1alpha2.WorkloadList{}
	err = fakeClient.List(ctx, workloadList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	assert.Empty(t, workloadList.Items)
}

// --- Workload construction tests ---

func TestBuildWorkload_HeadTemplateUsesBasicPolicy(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	workload, err := r.buildWorkload(cluster)
	require.NoError(t, err)

	// First template should be "head" with BasicSchedulingPolicy.
	require.Len(t, workload.Spec.PodGroupTemplates, 2)
	headTemplate := workload.Spec.PodGroupTemplates[0]
	assert.Equal(t, "head", headTemplate.Name)
	assert.NotNil(t, headTemplate.SchedulingPolicy.Basic)
	assert.Nil(t, headTemplate.SchedulingPolicy.Gang)
}

func TestBuildWorkload_WorkerTemplateUsesGangPolicy(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	workload, err := r.buildWorkload(cluster)
	require.NoError(t, err)

	// Second template should be "worker-workers" with GangSchedulingPolicy.
	require.Len(t, workload.Spec.PodGroupTemplates, 2)
	workerTemplate := workload.Spec.PodGroupTemplates[1]
	assert.Equal(t, "worker-workers", workerTemplate.Name)
	assert.Nil(t, workerTemplate.SchedulingPolicy.Basic)
	require.NotNil(t, workerTemplate.SchedulingPolicy.Gang)
	assert.Equal(t, int32(3), workerTemplate.SchedulingPolicy.Gang.MinCount)
}

func TestBuildWorkload_MinCountMatchesDesiredReplicas(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(
		newWorkerGroup("small", 2),
		newWorkerGroup("large", 5),
	)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	workload, err := r.buildWorkload(cluster)
	require.NoError(t, err)
	require.Len(t, workload.Spec.PodGroupTemplates, 3)

	assert.Equal(t, int32(2), workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount)
	assert.Equal(t, int32(5), workload.Spec.PodGroupTemplates[2].SchedulingPolicy.Gang.MinCount)
}

func TestBuildWorkload_MinCountWithNumOfHosts2(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	wg := newWorkerGroup("multi-host", 3)
	wg.NumOfHosts = 2
	cluster := newTestRayCluster(wg)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	workload, err := r.buildWorkload(cluster)
	require.NoError(t, err)
	require.Len(t, workload.Spec.PodGroupTemplates, 2)

	// GetWorkerGroupDesiredReplicas multiplies by NumOfHosts: 3 * 2 = 6
	require.NotNil(t, workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang)
	assert.Equal(t, int32(6), workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount)
}

func TestBuildWorkload_SuspendedWorkerGroupMinCount0(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	wg := newWorkerGroup("suspended-group", 3)
	wg.Suspend = new(true)
	cluster := newTestRayCluster(wg)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	workload, err := r.buildWorkload(cluster)
	require.NoError(t, err)
	require.Len(t, workload.Spec.PodGroupTemplates, 2)

	// Suspended group should use BasicSchedulingPolicy, not gang with minCount=0.
	workerTemplate := workload.Spec.PodGroupTemplates[1]
	assert.NotNil(t, workerTemplate.SchedulingPolicy.Basic)
	assert.Nil(t, workerTemplate.SchedulingPolicy.Gang)
}

func TestBuildWorkload_MinReplicas0UsesBasicPolicy(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	wg := newWorkerGroup("zero-min", 0)
	wg.MinReplicas = new(int32(0))
	wg.Replicas = new(int32(0))
	cluster := newTestRayCluster(wg)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	workload, err := r.buildWorkload(cluster)
	require.NoError(t, err)
	require.Len(t, workload.Spec.PodGroupTemplates, 2)

	workerTemplate := workload.Spec.PodGroupTemplates[1]
	assert.NotNil(t, workerTemplate.SchedulingPolicy.Basic)
	assert.Nil(t, workerTemplate.SchedulingPolicy.Gang)
}

// --- OwnerReference tests ---

func TestBuildWorkload_OwnerReference(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	workload, err := r.buildWorkload(cluster)
	require.NoError(t, err)

	require.Len(t, workload.OwnerReferences, 1)
	ownerRef := workload.OwnerReferences[0]
	assert.Equal(t, "ray.io/v1", ownerRef.APIVersion)
	assert.Equal(t, "RayCluster", ownerRef.Kind)
	assert.Equal(t, "test-cluster", ownerRef.Name)
	assert.Equal(t, cluster.UID, ownerRef.UID)
	assert.True(t, *ownerRef.Controller)
	assert.True(t, *ownerRef.BlockOwnerDeletion)
}

func TestBuildPodGroup_OwnerReference(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	policy := schedulingv1alpha2.PodGroupSchedulingPolicy{
		Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 3},
	}
	pg, err := r.buildPodGroup(cluster, "worker-workers", policy)
	require.NoError(t, err)

	require.Len(t, pg.OwnerReferences, 1)
	ownerRef := pg.OwnerReferences[0]
	assert.Equal(t, "ray.io/v1", ownerRef.APIVersion)
	assert.Equal(t, "RayCluster", ownerRef.Kind)
	assert.Equal(t, "test-cluster", ownerRef.Name)
	assert.Equal(t, cluster.UID, ownerRef.UID)
	assert.True(t, *ownerRef.Controller)
	assert.True(t, *ownerRef.BlockOwnerDeletion)
}

// --- PodGroup construction tests ---

func TestBuildPodGroup_TemplateRef(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster()
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	policy := schedulingv1alpha2.PodGroupSchedulingPolicy{
		Basic: &schedulingv1alpha2.BasicSchedulingPolicy{},
	}
	pg, err := r.buildPodGroup(cluster, "head", policy)
	require.NoError(t, err)

	require.NotNil(t, pg.Spec.PodGroupTemplateRef)
	require.NotNil(t, pg.Spec.PodGroupTemplateRef.Workload)
	assert.Equal(t, "test-cluster", pg.Spec.PodGroupTemplateRef.Workload.WorkloadName)
	assert.Equal(t, "head", pg.Spec.PodGroupTemplateRef.Workload.PodGroupTemplateName)
}

func TestBuildPodGroup_PolicyCopied(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster()
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	gangPolicy := schedulingv1alpha2.PodGroupSchedulingPolicy{
		Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 5},
	}
	pg, err := r.buildPodGroup(cluster, "worker-gpu", gangPolicy)
	require.NoError(t, err)

	require.NotNil(t, pg.Spec.SchedulingPolicy.Gang)
	assert.Equal(t, int32(5), pg.Spec.SchedulingPolicy.Gang.MinCount)
	assert.Nil(t, pg.Spec.SchedulingPolicy.Basic)
}

// --- Workload spec tests ---

func TestBuildWorkload_ControllerRef(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	workload, err := r.buildWorkload(cluster)
	require.NoError(t, err)

	require.NotNil(t, workload.Spec.ControllerRef)
	assert.Equal(t, "ray.io", workload.Spec.ControllerRef.APIGroup)
	assert.Equal(t, "RayCluster", workload.Spec.ControllerRef.Kind)
	assert.Equal(t, "test-cluster", workload.Spec.ControllerRef.Name)
}

func TestBuildWorkload_Labels(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	workload, err := r.buildWorkload(cluster)
	require.NoError(t, err)

	assert.Equal(t, "test-cluster", workload.Labels[utils.RayClusterLabelKey])
}

func TestBuildPodGroup_Labels(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster()
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))

	policy := schedulingv1alpha2.PodGroupSchedulingPolicy{
		Basic: &schedulingv1alpha2.BasicSchedulingPolicy{},
	}
	pg, err := r.buildPodGroup(cluster, "head", policy)
	require.NoError(t, err)

	assert.Equal(t, "test-cluster", pg.Labels[utils.RayClusterLabelKey])
}

// --- Pod scheduling group tests ---

func TestSetSchedulingGroup_HeadPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "head-pod"},
		Spec:       corev1.PodSpec{},
	}
	setSchedulingGroup(pod, podGroupName("my-cluster", "head"))

	require.NotNil(t, pod.Spec.SchedulingGroup)
	require.NotNil(t, pod.Spec.SchedulingGroup.PodGroupName)
	assert.Equal(t, "my-cluster-head", *pod.Spec.SchedulingGroup.PodGroupName)
}

func TestSetSchedulingGroup_WorkerPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-pod"},
		Spec:       corev1.PodSpec{},
	}
	setSchedulingGroup(pod, podGroupName("my-cluster", "worker-gpu-workers"))

	require.NotNil(t, pod.Spec.SchedulingGroup)
	require.NotNil(t, pod.Spec.SchedulingGroup.PodGroupName)
	assert.Equal(t, "my-cluster-worker-gpu-workers", *pod.Spec.SchedulingGroup.PodGroupName)
}

func TestSetSchedulingGroup_Idempotent(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "head-pod"},
		Spec:       corev1.PodSpec{},
	}

	// Set once, then set again with same value — should be stable.
	setSchedulingGroup(pod, "my-cluster-head")
	require.NotNil(t, pod.Spec.SchedulingGroup)
	assert.Equal(t, "my-cluster-head", *pod.Spec.SchedulingGroup.PodGroupName)

	setSchedulingGroup(pod, "my-cluster-head")
	assert.Equal(t, "my-cluster-head", *pod.Spec.SchedulingGroup.PodGroupName)
}

// --- Naming tests ---

func TestPodGroupName(t *testing.T) {
	tests := []struct {
		clusterName  string
		templateName string
		expected     string
	}{
		{"my-cluster", "head", "my-cluster-head"},
		{"my-cluster", "worker-gpu-workers", "my-cluster-worker-gpu-workers"},
		{"ray-cluster-1", "worker-cpu", "ray-cluster-1-worker-cpu"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, podGroupName(tt.clusterName, tt.templateName))
		})
	}
}

// --- isNativeWorkloadSchedulingEnabled tests ---

func TestIsNativeWorkloadSchedulingEnabled(t *testing.T) {
	tests := []struct {
		name        string
		featureGate bool
		annotation  string
		expected    bool
	}{
		{"both enabled", true, "true", true},
		{"gate on, annotation missing", true, "", false},
		{"gate off, annotation on", false, "true", false},
		{"both off", false, "", false},
		{"gate on, annotation wrong value", true, "false", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, tt.featureGate)
			cluster := &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NativeWorkloadSchedulingAnnotation: tt.annotation,
					},
				},
			}
			if tt.annotation == "" {
				cluster.Annotations = nil
			}
			assert.Equal(t, tt.expected, isNativeWorkloadSchedulingEnabled(cluster))
		})
	}
}

func TestShouldSetSchedulingGroup(t *testing.T) {
	tests := []struct {
		name           string
		featureGate    bool
		annotation     string
		autoscaling    bool
		batchSched     bool
		tooManyWorkers bool
		expected       bool
	}{
		{"enabled without autoscaling", true, "true", false, false, false, true},
		{"enabled with autoscaling", true, "true", true, false, false, false},
		{"disabled", false, "true", false, false, false, false},
		{"no annotation", true, "", false, false, false, false},
		{"batch scheduler configured", true, "true", false, true, false, false},
		{"too many worker groups", true, "true", false, false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, tt.featureGate)
			cluster := &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NativeWorkloadSchedulingAnnotation: tt.annotation,
					},
				},
			}
			if tt.annotation == "" {
				cluster.Annotations = nil
			}
			if tt.autoscaling {
				cluster.Spec.EnableInTreeAutoscaling = new(true)
			}
			if tt.tooManyWorkers {
				for i := 0; i < 8; i++ {
					cluster.Spec.WorkerGroupSpecs = append(cluster.Spec.WorkerGroupSpecs,
						newWorkerGroup(fmt.Sprintf("wg-%d", i), 1))
				}
			}

			s := newTestScheme()
			fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
			fakeRecorder := record.NewFakeRecorder(10)
			var opts RayClusterReconcilerOptions
			if tt.batchSched {
				batchMgr, err := batchscheduler.NewSchedulerManager(context.Background(),
					configapi.Configuration{BatchScheduler: "yunikorn"}, nil, nil)
				require.NoError(t, err)
				opts = RayClusterReconcilerOptions{
					BatchSchedulerManager: batchMgr,
				}
			}
			r := newReconciler(fakeClient, s, fakeRecorder, opts)
			assert.Equal(t, tt.expected, r.shouldSetSchedulingGroup(cluster))
		})
	}
}

// --- buildPodGroupSpecs direct tests ---

func TestBuildPodGroupSpecs_HeadOnly(t *testing.T) {
	cluster := newTestRayCluster()
	specs := buildPodGroupSpecs(cluster)

	require.Len(t, specs, 1)
	assert.Equal(t, "head", specs[0].templateName)
	assert.NotNil(t, specs[0].schedulingPolicy.Basic)
	assert.Nil(t, specs[0].schedulingPolicy.Gang)
}

func TestBuildPodGroupSpecs_WorkerGroupPolicies(t *testing.T) {
	tests := []struct {
		name         string
		workerGroup  rayv1.WorkerGroupSpec
		expectGang   bool
		expectMinCnt int32
	}{
		{
			name:         "active worker group uses gang policy",
			workerGroup:  newWorkerGroup("active", 3),
			expectGang:   true,
			expectMinCnt: 3,
		},
		{
			name: "suspended worker group uses basic policy",
			workerGroup: func() rayv1.WorkerGroupSpec {
				wg := newWorkerGroup("suspended", 3)
				wg.Suspend = new(true)
				return wg
			}(),
			expectGang: false,
		},
		{
			name: "zero replicas uses basic policy",
			workerGroup: func() rayv1.WorkerGroupSpec {
				wg := newWorkerGroup("zero", 0)
				wg.MinReplicas = new(int32(0))
				wg.Replicas = new(int32(0))
				return wg
			}(),
			expectGang: false,
		},
		{
			name: "multi-host multiplies replicas",
			workerGroup: func() rayv1.WorkerGroupSpec {
				wg := newWorkerGroup("multi", 2)
				wg.NumOfHosts = 3
				return wg
			}(),
			expectGang:   true,
			expectMinCnt: 6, // 2 * 3
		},
		{
			name: "replicas clamped to max",
			workerGroup: func() rayv1.WorkerGroupSpec {
				wg := newWorkerGroup("clamped", 10)
				wg.MaxReplicas = new(int32(5))
				return wg
			}(),
			expectGang:   true,
			expectMinCnt: 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := newTestRayCluster(tt.workerGroup)
			specs := buildPodGroupSpecs(cluster)

			require.Len(t, specs, 2)
			// First is always head.
			assert.Equal(t, "head", specs[0].templateName)

			workerSpec := specs[1]
			assert.Equal(t, "worker-"+tt.workerGroup.GroupName, workerSpec.templateName)
			if tt.expectGang {
				require.NotNil(t, workerSpec.schedulingPolicy.Gang)
				assert.Nil(t, workerSpec.schedulingPolicy.Basic)
				assert.Equal(t, tt.expectMinCnt, workerSpec.schedulingPolicy.Gang.MinCount)
			} else {
				assert.NotNil(t, workerSpec.schedulingPolicy.Basic)
				assert.Nil(t, workerSpec.schedulingPolicy.Gang)
			}
		})
	}
}

func TestBuildPodGroupSpecs_MultipleWorkerGroups(t *testing.T) {
	cluster := newTestRayCluster(
		newWorkerGroup("cpu", 2),
		newWorkerGroup("gpu", 4),
	)
	specs := buildPodGroupSpecs(cluster)

	require.Len(t, specs, 3)
	assert.Equal(t, "head", specs[0].templateName)
	assert.Equal(t, "worker-cpu", specs[1].templateName)
	assert.Equal(t, int32(2), specs[1].schedulingPolicy.Gang.MinCount)
	assert.Equal(t, "worker-gpu", specs[2].templateName)
	assert.Equal(t, int32(4), specs[2].schedulingPolicy.Gang.MinCount)
}

// --- Boundary condition tests ---

func TestReconcileNativeWorkloadScheduling_Exactly7WorkerGroups(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	workers := make([]rayv1.WorkerGroupSpec, 7)
	for i := range workers {
		workers[i] = newWorkerGroup("workers-"+strconv.Itoa(i), 1)
	}
	cluster := newTestRayCluster(workers...)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(20))
	ctx := context.Background()

	err := r.reconcileNativeWorkloadScheduling(ctx, cluster)
	require.NoError(t, err)

	// Should create 1 Workload with 8 templates (1 head + 7 workers).
	workload := &schedulingv1alpha2.Workload{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster", Namespace: "default"}, workload)
	require.NoError(t, err)
	assert.Len(t, workload.Spec.PodGroupTemplates, 8)

	// Should create 8 PodGroups (1 head + 7 workers).
	pgList := &schedulingv1alpha2.PodGroupList{}
	err = fakeClient.List(ctx, pgList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	assert.Len(t, pgList.Items, 8)
}

// --- Error path tests ---

func TestBuildWorkload_SetControllerReferenceError(t *testing.T) {
	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	// Use a scheme that doesn't know about RayCluster — SetControllerReference will fail.
	badScheme := runtime.NewScheme()
	_ = schedulingv1alpha2.AddToScheme(badScheme)
	fakeClient := clientFake.NewClientBuilder().WithScheme(badScheme).Build()
	r := newReconciler(fakeClient, badScheme, record.NewFakeRecorder(10))

	workload, err := r.buildWorkload(cluster)
	require.Error(t, err)
	assert.Nil(t, workload)
}

func TestBuildPodGroup_SetControllerReferenceError(t *testing.T) {
	cluster := newTestRayCluster()
	badScheme := runtime.NewScheme()
	_ = schedulingv1alpha2.AddToScheme(badScheme)
	fakeClient := clientFake.NewClientBuilder().WithScheme(badScheme).Build()
	r := newReconciler(fakeClient, badScheme, record.NewFakeRecorder(10))

	policy := schedulingv1alpha2.PodGroupSchedulingPolicy{
		Basic: &schedulingv1alpha2.BasicSchedulingPolicy{},
	}
	pg, err := r.buildPodGroup(cluster, "head", policy)
	require.Error(t, err)
	assert.Nil(t, pg)
}

func TestReconcileNativeWorkloadScheduling_WorkloadCreateFailure(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*schedulingv1alpha2.Workload); ok {
					return fmt.Errorf("simulated API server error")
				}
				return c.Create(ctx, obj, opts...)
			},
		}).Build()
	fakeRecorder := record.NewFakeRecorder(10)
	r := newReconciler(fakeClient, s, fakeRecorder)
	ctx := context.Background()

	err := r.reconcileNativeWorkloadScheduling(ctx, cluster)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated API server error")

	// Should have emitted a FailedToCreateWorkload event.
	assert.Len(t, fakeRecorder.Events, 1)
	event := <-fakeRecorder.Events
	assert.Contains(t, event, string(FailedToCreateWorkload))
}

func TestReconcileNativeWorkloadScheduling_PodGroupCreateFailure(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				if _, ok := obj.(*schedulingv1alpha2.PodGroup); ok {
					return fmt.Errorf("simulated PodGroup creation error")
				}
				return c.Create(ctx, obj, opts...)
			},
		}).Build()
	fakeRecorder := record.NewFakeRecorder(10)
	r := newReconciler(fakeClient, s, fakeRecorder)
	ctx := context.Background()

	err := r.reconcileNativeWorkloadScheduling(ctx, cluster)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated PodGroup creation error")

	// Workload should have been created successfully.
	workload := &schedulingv1alpha2.Workload{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster", Namespace: "default"}, workload)
	require.NoError(t, err)

	// Should have emitted CreatedWorkload + FailedToCreatePodGroup events.
	assert.Len(t, fakeRecorder.Events, 2)
}

// --- Controller integration tests: schedulingGroup on pods ---

func TestCreateHeadPod_SetsSchedulingGroup(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))
	ctx := context.Background()

	err := r.createHeadPod(ctx, *cluster, "")
	require.NoError(t, err)

	podList := &corev1.PodList{}
	err = fakeClient.List(ctx, podList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)

	pod := podList.Items[0]
	require.NotNil(t, pod.Spec.SchedulingGroup, "head pod should have schedulingGroup set")
	assert.Equal(t, "test-cluster-head", *pod.Spec.SchedulingGroup.PodGroupName)
}

func TestCreateWorkerPod_SetsSchedulingGroup(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	worker := newWorkerGroup("gpu-workers", 3)
	cluster := newTestRayCluster(worker)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))
	ctx := context.Background()

	err := r.createWorkerPod(ctx, *cluster, worker)
	require.NoError(t, err)

	podList := &corev1.PodList{}
	err = fakeClient.List(ctx, podList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)

	pod := podList.Items[0]
	require.NotNil(t, pod.Spec.SchedulingGroup, "worker pod should have schedulingGroup set")
	assert.Equal(t, "test-cluster-worker-gpu-workers", *pod.Spec.SchedulingGroup.PodGroupName)
}

func TestCreateWorkerPodWithIndex_SetsSchedulingGroup(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	worker := newWorkerGroup("tpu-workers", 2)
	cluster := newTestRayCluster(worker)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))
	ctx := context.Background()

	err := r.createWorkerPodWithIndex(ctx, *cluster, worker, "replica-0", 0, 0)
	require.NoError(t, err)

	podList := &corev1.PodList{}
	err = fakeClient.List(ctx, podList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)

	pod := podList.Items[0]
	require.NotNil(t, pod.Spec.SchedulingGroup, "worker pod should have schedulingGroup set")
	assert.Equal(t, "test-cluster-worker-tpu-workers", *pod.Spec.SchedulingGroup.PodGroupName)
}

func TestCreateHeadPod_NoSchedulingGroupWhenDisabled(t *testing.T) {
	// Feature gate enabled but annotation missing
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	cluster := newTestRayCluster(newWorkerGroup("workers", 3))
	delete(cluster.Annotations, NativeWorkloadSchedulingAnnotation)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))
	ctx := context.Background()

	err := r.createHeadPod(ctx, *cluster, "")
	require.NoError(t, err)

	podList := &corev1.PodList{}
	err = fakeClient.List(ctx, podList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)

	pod := podList.Items[0]
	assert.Nil(t, pod.Spec.SchedulingGroup, "head pod should not have schedulingGroup when annotation is missing")
}

func TestCreateWorkerPod_NoSchedulingGroupWhenFeatureGateDisabled(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, false)

	worker := newWorkerGroup("workers", 3)
	cluster := newTestRayCluster(worker)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))
	ctx := context.Background()

	err := r.createWorkerPod(ctx, *cluster, worker)
	require.NoError(t, err)

	podList := &corev1.PodList{}
	err = fakeClient.List(ctx, podList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)

	pod := podList.Items[0]
	assert.Nil(t, pod.Spec.SchedulingGroup, "worker pod should not have schedulingGroup when feature gate is disabled")
}

func TestCreateWorkerPodWithIndex_NoSchedulingGroupWhenDisabled(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.NativeWorkloadScheduling, true)

	worker := newWorkerGroup("tpu-workers", 2)
	cluster := newTestRayCluster(worker)
	delete(cluster.Annotations, NativeWorkloadSchedulingAnnotation)
	s := newTestScheme()
	fakeClient := clientFake.NewClientBuilder().WithScheme(s).Build()
	r := newReconciler(fakeClient, s, record.NewFakeRecorder(10))
	ctx := context.Background()

	err := r.createWorkerPodWithIndex(ctx, *cluster, worker, "replica-0", 0, 0)
	require.NoError(t, err)

	podList := &corev1.PodList{}
	err = fakeClient.List(ctx, podList, &client.ListOptions{Namespace: "default"})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)

	pod := podList.Items[0]
	assert.Nil(t, pod.Spec.SchedulingGroup, "worker pod should not have schedulingGroup when annotation is missing")
}
