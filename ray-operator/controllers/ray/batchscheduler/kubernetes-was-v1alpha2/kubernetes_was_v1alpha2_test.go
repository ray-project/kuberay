package kuberneteswasv1alpha2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha2 "k8s.io/api/scheduling/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

func TestAddMetadataToChildResourceSetsDefaultSchedulerName(t *testing.T) {
	scheduler := &KubernetesWASV1Alpha2Scheduler{}
	parent := &metav1.ObjectMeta{}

	pod := &corev1.Pod{}
	scheduler.AddMetadataToChildResource(context.Background(), parent, pod, "head")
	require.Equal(t, corev1.DefaultSchedulerName, pod.Spec.SchedulerName)

	template := &corev1.PodTemplateSpec{}
	scheduler.AddMetadataToChildResource(context.Background(), parent, template, "worker-group")
	require.Equal(t, corev1.DefaultSchedulerName, template.Spec.SchedulerName)
}

func TestName(t *testing.T) {
	scheduler := &KubernetesWASV1Alpha2Scheduler{}
	require.Equal(t, "kubernetes-was-v1alpha2", scheduler.Name())
}

func TestDoBatchSchedulingOnSubmissionCreatesWorkloadAndPodGroups(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	fakeClient := clientFake.NewClientBuilder().WithScheme(scheme).Build()
	scheduler := &KubernetesWASV1Alpha2Scheduler{cli: fakeClient}
	rayCluster := newTestRayCluster(newWorkerGroup())

	err := scheduler.DoBatchSchedulingOnSubmission(ctx, rayCluster)
	require.NoError(t, err)

	workload := &schedulingv1alpha2.Workload{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: rayCluster.Name, Namespace: rayCluster.Namespace}, workload)
	require.NoError(t, err)
	require.Len(t, workload.Spec.PodGroupTemplates, 2)
	assert.Equal(t, "head", workload.Spec.PodGroupTemplates[0].Name)
	assert.Equal(t, "worker-workers", workload.Spec.PodGroupTemplates[1].Name)
	require.NotNil(t, workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang)
	assert.Equal(t, int32(3), workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount)

	headPodGroup := &schedulingv1alpha2.PodGroup{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster-head", Namespace: rayCluster.Namespace}, headPodGroup)
	require.NoError(t, err)
	assert.Nil(t, headPodGroup.Spec.SchedulingPolicy.Gang)

	workerPodGroup := &schedulingv1alpha2.PodGroup{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster-worker-workers", Namespace: rayCluster.Namespace}, workerPodGroup)
	require.NoError(t, err)
	require.NotNil(t, workerPodGroup.Spec.SchedulingPolicy.Gang)
	assert.Equal(t, "test-cluster", workerPodGroup.Spec.PodGroupTemplateRef.Workload.WorkloadName)
	assert.Equal(t, "worker-workers", workerPodGroup.Spec.PodGroupTemplateRef.Workload.PodGroupTemplateName)
}

func TestDoBatchSchedulingOnSubmissionSkipsAndCleansUpWhenAutoscalingEnabled(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	rayCluster := newTestRayCluster(newWorkerGroup())
	existingWorkload := &schedulingv1alpha2.Workload{ObjectMeta: metav1.ObjectMeta{Name: rayCluster.Name, Namespace: rayCluster.Namespace}}
	existingPodGroup := &schedulingv1alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{
		Name:      "test-cluster-head",
		Namespace: rayCluster.Namespace,
		Labels:    map[string]string{utils.RayClusterLabelKey: rayCluster.Name},
	}}
	fakeClient := clientFake.NewClientBuilder().WithScheme(scheme).WithObjects(existingWorkload, existingPodGroup).Build()
	scheduler := &KubernetesWASV1Alpha2Scheduler{cli: fakeClient}
	enableAutoscaling := true
	rayCluster.Spec.EnableInTreeAutoscaling = &enableAutoscaling

	err := scheduler.DoBatchSchedulingOnSubmission(ctx, rayCluster)
	require.NoError(t, err)

	err = fakeClient.Get(ctx, types.NamespacedName{Name: rayCluster.Name, Namespace: rayCluster.Namespace}, &schedulingv1alpha2.Workload{})
	assert.True(t, apierrors.IsNotFound(err))
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster-head", Namespace: rayCluster.Namespace}, &schedulingv1alpha2.PodGroup{})
	assert.True(t, apierrors.IsNotFound(err))
}

func TestDoBatchSchedulingOnSubmissionSkipsAndCleansUpWhenTooManyWorkerGroups(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	rayCluster := newTestRayCluster(newWorkerGroups(schedulingv1alpha2.WorkloadMaxPodGroupTemplates)...)
	existingWorkload := &schedulingv1alpha2.Workload{ObjectMeta: metav1.ObjectMeta{Name: rayCluster.Name, Namespace: rayCluster.Namespace}}
	existingPodGroup := &schedulingv1alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{
		Name:      "test-cluster-head",
		Namespace: rayCluster.Namespace,
		Labels:    map[string]string{utils.RayClusterLabelKey: rayCluster.Name},
	}}
	fakeClient := clientFake.NewClientBuilder().WithScheme(scheme).WithObjects(existingWorkload, existingPodGroup).Build()
	scheduler := &KubernetesWASV1Alpha2Scheduler{cli: fakeClient}

	err := scheduler.DoBatchSchedulingOnSubmission(ctx, rayCluster)
	require.NoError(t, err)

	err = fakeClient.Get(ctx, types.NamespacedName{Name: rayCluster.Name, Namespace: rayCluster.Namespace}, &schedulingv1alpha2.Workload{})
	assert.True(t, apierrors.IsNotFound(err))
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster-head", Namespace: rayCluster.Namespace}, &schedulingv1alpha2.PodGroup{})
	assert.True(t, apierrors.IsNotFound(err))
}

func TestDoBatchSchedulingOnSubmissionAllowsExactlySevenWorkerGroups(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	fakeClient := clientFake.NewClientBuilder().WithScheme(scheme).Build()
	scheduler := &KubernetesWASV1Alpha2Scheduler{cli: fakeClient}
	rayCluster := newTestRayCluster(newWorkerGroups(schedulingv1alpha2.WorkloadMaxPodGroupTemplates - 1)...)

	err := scheduler.DoBatchSchedulingOnSubmission(ctx, rayCluster)
	require.NoError(t, err)

	workload := &schedulingv1alpha2.Workload{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: rayCluster.Name, Namespace: rayCluster.Namespace}, workload)
	require.NoError(t, err)
	assert.Len(t, workload.Spec.PodGroupTemplates, schedulingv1alpha2.WorkloadMaxPodGroupTemplates)
}

func TestAddMetadataToChildResourceSetsSchedulingGroup(t *testing.T) {
	scheduler := &KubernetesWASV1Alpha2Scheduler{}
	rayCluster := newTestRayCluster(newWorkerGroup())

	headPod := &corev1.Pod{}
	scheduler.AddMetadataToChildResource(context.Background(), rayCluster, headPod, utils.RayNodeHeadGroupLabelValue)
	require.NotNil(t, headPod.Spec.SchedulingGroup)
	require.NotNil(t, headPod.Spec.SchedulingGroup.PodGroupName)
	assert.Equal(t, "test-cluster-head", *headPod.Spec.SchedulingGroup.PodGroupName)
	assert.Equal(t, corev1.DefaultSchedulerName, headPod.Spec.SchedulerName)

	workerPod := &corev1.Pod{}
	scheduler.AddMetadataToChildResource(context.Background(), rayCluster, workerPod, "workers")
	require.NotNil(t, workerPod.Spec.SchedulingGroup)
	require.NotNil(t, workerPod.Spec.SchedulingGroup.PodGroupName)
	assert.Equal(t, "test-cluster-worker-workers", *workerPod.Spec.SchedulingGroup.PodGroupName)
}

func TestAddMetadataToChildResourceSetsTemplateSchedulingGroup(t *testing.T) {
	scheduler := &KubernetesWASV1Alpha2Scheduler{}
	rayCluster := newTestRayCluster(newWorkerGroup())

	template := &corev1.PodTemplateSpec{}
	scheduler.AddMetadataToChildResource(context.Background(), rayCluster, template, "workers")

	require.NotNil(t, template.Spec.SchedulingGroup)
	require.NotNil(t, template.Spec.SchedulingGroup.PodGroupName)
	assert.Equal(t, "test-cluster-worker-workers", *template.Spec.SchedulingGroup.PodGroupName)
	assert.Equal(t, corev1.DefaultSchedulerName, template.Spec.SchedulerName)
}

func TestAddMetadataToChildResourceSkipsSchedulingGroupWhenAutoscalingEnabled(t *testing.T) {
	scheduler := &KubernetesWASV1Alpha2Scheduler{}
	rayCluster := newTestRayCluster(newWorkerGroup())
	enableAutoscaling := true
	rayCluster.Spec.EnableInTreeAutoscaling = &enableAutoscaling

	pod := &corev1.Pod{}
	scheduler.AddMetadataToChildResource(context.Background(), rayCluster, pod, "workers")

	assert.Nil(t, pod.Spec.SchedulingGroup)
	assert.Equal(t, corev1.DefaultSchedulerName, pod.Spec.SchedulerName)
}

func TestAddMetadataToChildResourceSkipsSchedulingGroupWhenTooManyWorkerGroups(t *testing.T) {
	scheduler := &KubernetesWASV1Alpha2Scheduler{}
	rayCluster := newTestRayCluster(newWorkerGroups(schedulingv1alpha2.WorkloadMaxPodGroupTemplates)...)

	pod := &corev1.Pod{}
	scheduler.AddMetadataToChildResource(context.Background(), rayCluster, pod, "group-0")

	assert.Nil(t, pod.Spec.SchedulingGroup)
	assert.Equal(t, corev1.DefaultSchedulerName, pod.Spec.SchedulerName)
}

func TestCleanupOnCompletionDeletesSchedulingResourcesAndFinalizers(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	rayCluster := newTestRayCluster(newWorkerGroup())
	existingWorkload := &schedulingv1alpha2.Workload{ObjectMeta: metav1.ObjectMeta{Name: rayCluster.Name, Namespace: rayCluster.Namespace}}
	existingPodGroup := &schedulingv1alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{
		Name:       "test-cluster-head",
		Namespace:  rayCluster.Namespace,
		Labels:     map[string]string{utils.RayClusterLabelKey: rayCluster.Name},
		Finalizers: []string{podGroupProtectionFinalizer},
	}}
	fakeClient := clientFake.NewClientBuilder().WithScheme(scheme).WithObjects(existingWorkload, existingPodGroup).Build()
	scheduler := &KubernetesWASV1Alpha2Scheduler{cli: fakeClient}

	didCleanup, err := scheduler.CleanupOnCompletion(ctx, rayCluster)
	require.NoError(t, err)
	assert.True(t, didCleanup)

	err = fakeClient.Get(ctx, types.NamespacedName{Name: rayCluster.Name, Namespace: rayCluster.Namespace}, &schedulingv1alpha2.Workload{})
	assert.True(t, apierrors.IsNotFound(err))
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster-head", Namespace: rayCluster.Namespace}, &schedulingv1alpha2.PodGroup{})
	assert.True(t, apierrors.IsNotFound(err))
}

func TestCleanupOnCompletionNotFoundIsNoop(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	fakeClient := clientFake.NewClientBuilder().WithScheme(scheme).Build()
	scheduler := &KubernetesWASV1Alpha2Scheduler{cli: fakeClient}

	didCleanup, err := scheduler.CleanupOnCompletion(ctx, newTestRayCluster(newWorkerGroup()))

	require.NoError(t, err)
	assert.False(t, didCleanup)
}

func TestBuildPodGroupSpecs(t *testing.T) {
	zero := int32(0)
	one := int32(1)
	three := int32(3)
	suspended := true

	tests := []struct {
		name      string
		cluster   *rayv1.RayCluster
		wantNames []string
		wantGang  map[string]int32
		wantBasic []string
	}{
		{
			name:      "head only",
			cluster:   newTestRayCluster(),
			wantNames: []string{"head"},
			wantBasic: []string{"head"},
		},
		{
			name:      "worker group uses gang policy",
			cluster:   newTestRayCluster(newWorkerGroupWithReplicas("workers", 3)),
			wantNames: []string{"head", "worker-workers"},
			wantGang:  map[string]int32{"worker-workers": three},
			wantBasic: []string{"head"},
		},
		{
			name:      "multi-host desired replicas feed min count",
			cluster:   newTestRayCluster(workerGroupWithNumOfHosts("workers", 3, 2)),
			wantNames: []string{"head", "worker-workers"},
			wantGang:  map[string]int32{"worker-workers": 6},
			wantBasic: []string{"head"},
		},
		{
			name: "suspended worker group uses basic policy",
			cluster: newTestRayCluster(rayv1.WorkerGroupSpec{
				GroupName:   "workers",
				NumOfHosts:  1,
				Replicas:    &one,
				MinReplicas: &one,
				MaxReplicas: &one,
				Suspend:     &suspended,
			}),
			wantNames: []string{"head", "worker-workers"},
			wantBasic: []string{"head", "worker-workers"},
		},
		{
			name: "min replicas zero uses basic policy",
			cluster: newTestRayCluster(rayv1.WorkerGroupSpec{
				GroupName:   "workers",
				NumOfHosts:  1,
				Replicas:    &zero,
				MinReplicas: &zero,
				MaxReplicas: &zero,
			}),
			wantNames: []string{"head", "worker-workers"},
			wantBasic: []string{"head", "worker-workers"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs := buildPodGroupSpecs(tt.cluster)
			require.Len(t, specs, len(tt.wantNames))

			byName := make(map[string]schedulingv1alpha2.PodGroupSchedulingPolicy, len(specs))
			for index, spec := range specs {
				assert.Equal(t, tt.wantNames[index], spec.templateName)
				byName[spec.templateName] = spec.schedulingPolicy
			}

			for name, minCount := range tt.wantGang {
				require.NotNil(t, byName[name].Gang, "expected gang policy for %s", name)
				assert.Equal(t, minCount, byName[name].Gang.MinCount)
			}
			for _, name := range tt.wantBasic {
				require.NotNil(t, byName[name].Basic, "expected basic policy for %s", name)
				assert.Nil(t, byName[name].Gang, "expected no gang policy for %s", name)
			}
		})
	}
}

func TestSyncSchedulingResourcesRecreatesStaleWorkload(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	rayCluster := newTestRayCluster(newWorkerGroupWithReplicas("workers", 5))
	existingWorkload := &schedulingv1alpha2.Workload{
		ObjectMeta: metav1.ObjectMeta{Name: rayCluster.Name, Namespace: rayCluster.Namespace},
		Spec: schedulingv1alpha2.WorkloadSpec{PodGroupTemplates: []schedulingv1alpha2.PodGroupTemplate{
			{Name: "head", SchedulingPolicy: schedulingv1alpha2.PodGroupSchedulingPolicy{Basic: &schedulingv1alpha2.BasicSchedulingPolicy{}}},
			{Name: "worker-workers", SchedulingPolicy: schedulingv1alpha2.PodGroupSchedulingPolicy{Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 3}}},
		}},
	}
	existingPodGroup := &schedulingv1alpha2.PodGroup{ObjectMeta: metav1.ObjectMeta{
		Name:      "test-cluster-worker-workers",
		Namespace: rayCluster.Namespace,
		Labels:    map[string]string{utils.RayClusterLabelKey: rayCluster.Name},
	}}
	fakeClient := clientFake.NewClientBuilder().WithScheme(scheme).WithObjects(existingWorkload, existingPodGroup).Build()
	scheduler := &KubernetesWASV1Alpha2Scheduler{cli: fakeClient}

	err := scheduler.DoBatchSchedulingOnSubmission(ctx, rayCluster)
	require.NoError(t, err)

	workload := &schedulingv1alpha2.Workload{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: rayCluster.Name, Namespace: rayCluster.Namespace}, workload)
	require.NoError(t, err)
	require.Len(t, workload.Spec.PodGroupTemplates, 2)
	require.NotNil(t, workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang)
	assert.Equal(t, int32(5), workload.Spec.PodGroupTemplates[1].SchedulingPolicy.Gang.MinCount)

	podGroup := &schedulingv1alpha2.PodGroup{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-cluster-worker-workers", Namespace: rayCluster.Namespace}, podGroup)
	require.NoError(t, err)
	require.NotNil(t, podGroup.Spec.SchedulingPolicy.Gang)
	assert.Equal(t, int32(5), podGroup.Spec.SchedulingPolicy.Gang.MinCount)
}

func TestIsWorkloadStale(t *testing.T) {
	baseCluster := newTestRayCluster(newWorkerGroupWithReplicas("workers", 3))
	baseWorkload, err := (&KubernetesWASV1Alpha2Scheduler{cli: clientFake.NewClientBuilder().WithScheme(newTestScheme(t)).Build()}).buildWorkload(baseCluster)
	require.NoError(t, err)

	tests := []struct {
		name      string
		workload  *schedulingv1alpha2.Workload
		cluster   *rayv1.RayCluster
		wantStale bool
	}{
		{name: "no change", workload: baseWorkload.DeepCopy(), cluster: baseCluster, wantStale: false},
		{name: "worker group added", workload: baseWorkload.DeepCopy(), cluster: newTestRayCluster(newWorkerGroupWithReplicas("workers", 3), newWorkerGroupWithReplicas("gpu", 1)), wantStale: true},
		{name: "worker group removed", workload: baseWorkload.DeepCopy(), cluster: newTestRayCluster(), wantStale: true},
		{name: "worker group renamed", workload: baseWorkload.DeepCopy(), cluster: newTestRayCluster(newWorkerGroupWithReplicas("renamed", 3)), wantStale: true},
		{name: "replica count changed", workload: baseWorkload.DeepCopy(), cluster: newTestRayCluster(newWorkerGroupWithReplicas("workers", 5)), wantStale: true},
		{name: "num hosts changed", workload: baseWorkload.DeepCopy(), cluster: newTestRayCluster(workerGroupWithNumOfHosts("workers", 3, 2)), wantStale: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantStale, isWorkloadStale(tt.workload, tt.cluster))
		})
	}
}

func TestSchedulingPoliciesMatch(t *testing.T) {
	tests := []struct {
		name string
		a    schedulingv1alpha2.PodGroupSchedulingPolicy
		b    schedulingv1alpha2.PodGroupSchedulingPolicy
		want bool
	}{
		{name: "both empty", want: true},
		{name: "both basic", a: schedulingv1alpha2.PodGroupSchedulingPolicy{Basic: &schedulingv1alpha2.BasicSchedulingPolicy{}}, b: schedulingv1alpha2.PodGroupSchedulingPolicy{Basic: &schedulingv1alpha2.BasicSchedulingPolicy{}}, want: true},
		{name: "same gang", a: schedulingv1alpha2.PodGroupSchedulingPolicy{Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 3}}, b: schedulingv1alpha2.PodGroupSchedulingPolicy{Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 3}}, want: true},
		{name: "different gang", a: schedulingv1alpha2.PodGroupSchedulingPolicy{Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 3}}, b: schedulingv1alpha2.PodGroupSchedulingPolicy{Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 5}}, want: false},
		{name: "basic vs gang", a: schedulingv1alpha2.PodGroupSchedulingPolicy{Basic: &schedulingv1alpha2.BasicSchedulingPolicy{}}, b: schedulingv1alpha2.PodGroupSchedulingPolicy{Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: 1}}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, schedulingPoliciesMatch(tt.a, tt.b))
		})
	}
}

func TestSchedulingV1alpha2Available(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantErr     bool
		errContains string
	}{
		{
			name: "API available returns resource list",
			handler: func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/apis/scheduling.k8s.io/v1alpha2" {
					writer.Header().Set("Content-Type", "application/json")
					resourceList := metav1.APIResourceList{
						GroupVersion: "scheduling.k8s.io/v1alpha2",
						APIResources: []metav1.APIResource{
							{Name: "workloads", Kind: "Workload", Namespaced: true},
							{Name: "podgroups", Kind: "PodGroup", Namespaced: true},
						},
					}
					require.NoError(t, json.NewEncoder(writer).Encode(resourceList))
					return
				}
				http.NotFound(writer, request)
			},
		},
		{
			name: "API available returns empty resource list",
			handler: func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/apis/scheduling.k8s.io/v1alpha2" {
					writer.Header().Set("Content-Type", "application/json")
					require.NoError(t, json.NewEncoder(writer).Encode(metav1.APIResourceList{GroupVersion: "scheduling.k8s.io/v1alpha2"}))
					return
				}
				http.NotFound(writer, request)
			},
		},
		{
			name: "API not available returns 404",
			handler: func(writer http.ResponseWriter, request *http.Request) {
				http.NotFound(writer, request)
			},
			wantErr:     true,
			errContains: "scheduling.k8s.io/v1alpha2 API is not available",
		},
		{
			name: "API not available returns server error",
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				http.Error(writer, "internal server error", http.StatusInternalServerError)
			},
			wantErr:     true,
			errContains: "scheduling.k8s.io/v1alpha2 API is not available",
		},
		{
			name: "different group version does not satisfy v1alpha2",
			handler: func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/apis/scheduling.k8s.io/v1" {
					writer.Header().Set("Content-Type", "application/json")
					require.NoError(t, json.NewEncoder(writer).Encode(metav1.APIResourceList{GroupVersion: "scheduling.k8s.io/v1"}))
					return
				}
				http.NotFound(writer, request)
			},
			wantErr:     true,
			errContains: "scheduling.k8s.io/v1alpha2 API is not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			err := schedulingV1alpha2Available(&rest.Config{Host: server.URL})
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSchedulingV1alpha2AvailableAllowsNilConfig(t *testing.T) {
	require.NoError(t, schedulingV1alpha2Available(nil))
}

func TestSchedulingV1alpha2AvailableUnreachableServer(t *testing.T) {
	err := schedulingV1alpha2Available(&rest.Config{Host: "http://127.0.0.1:1"})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "scheduling.k8s.io/v1alpha2 API is not available") || strings.Contains(err.Error(), "connection refused"))
}

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, rayv1.AddToScheme(scheme))
	require.NoError(t, schedulingv1alpha2.AddToScheme(scheme))
	return scheme
}

func newTestRayCluster(workerGroups ...rayv1.WorkerGroupSpec) *rayv1.RayCluster {
	return &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec:    rayv1.HeadGroupSpec{Template: corev1.PodTemplateSpec{}},
			WorkerGroupSpecs: workerGroups,
		},
	}
}

func newWorkerGroup() rayv1.WorkerGroupSpec {
	return newWorkerGroupWithReplicas("workers", 3)
}

func newWorkerGroupWithReplicas(groupName string, replicas int32) rayv1.WorkerGroupSpec {
	return rayv1.WorkerGroupSpec{
		GroupName:   groupName,
		NumOfHosts:  1,
		Replicas:    &replicas,
		MinReplicas: &replicas,
		MaxReplicas: &replicas,
		Template:    corev1.PodTemplateSpec{},
	}
}

func workerGroupWithNumOfHosts(groupName string, replicas int32, numOfHosts int32) rayv1.WorkerGroupSpec {
	workerGroup := newWorkerGroupWithReplicas(groupName, replicas)
	workerGroup.NumOfHosts = numOfHosts
	return workerGroup
}

func newWorkerGroups(count int) []rayv1.WorkerGroupSpec {
	workerGroups := make([]rayv1.WorkerGroupSpec, 0, count)
	for index := range count {
		workerGroups = append(workerGroups, newWorkerGroupWithReplicas(fmt.Sprintf("group-%d", index), 1))
	}
	return workerGroups
}
