/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ray

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/expectations"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/scheme"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	"github.com/ray-project/kuberay/ray-operator/test/support"

	. "github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

var (
	namespaceStr            string
	instanceName            string
	enableInTreeAutoscaling bool
	headGroupNameStr        string
	groupNameStr            string
	expectReplicaNum        int32
	expectNumOfHostNum      int32
	testPods                []runtime.Object
	testPodsNoHeadIP        []runtime.Object
	testRayCluster          *rayv1.RayCluster
	headSelector            labels.Selector
	headNodeIP              string
	headNodeName            string
	testServices            []runtime.Object
	workerSelector          labels.Selector
	workersToDelete         []string
)

const (
	// MultiKueueController represents the vaue of the MultiKueue controller
	MultiKueueController = "kueue.x-k8s.io/multikueue"
)

func setupTest(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	namespaceStr = "default"
	instanceName = "raycluster-sample"
	enableInTreeAutoscaling = true
	headGroupNameStr = "head-group"
	groupNameStr = "small-group"
	expectReplicaNum = 3
	expectNumOfHostNum = 1
	workersToDelete = []string{"pod1", "pod2"}
	headNodeIP = "1.2.3.4"
	headNodeName = "headNode"
	testPods = []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      headNodeName,
				Namespace: namespaceStr,
				Labels: map[string]string{
					utils.RayNodeLabelKey:      "yes",
					utils.RayClusterLabelKey:   instanceName,
					utils.RayNodeTypeLabelKey:  string(rayv1.HeadNode),
					utils.RayNodeGroupLabelKey: headGroupNameStr,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-head",
						Image:   "rayproject/autoscaler",
						Command: []string{"python"},
						Args:    []string{"/opt/code.py"},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				PodIP: headNodeIP,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "ray-head",
						State: corev1.ContainerState{},
					},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: namespaceStr,
				Labels: map[string]string{
					utils.RayNodeLabelKey:      "yes",
					utils.RayClusterLabelKey:   instanceName,
					utils.RayNodeGroupLabelKey: groupNameStr,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-worker",
						Image:   "rayproject/autoscaler",
						Command: []string{"echo"},
						Args:    []string{"Hello Ray"},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "ray-worker",
						State: corev1.ContainerState{},
					},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: namespaceStr,
				Labels: map[string]string{
					utils.RayNodeLabelKey:      "yes",
					utils.RayClusterLabelKey:   instanceName,
					utils.RayNodeGroupLabelKey: groupNameStr,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-worker",
						Image:   "rayproject/autoscaler",
						Command: []string{"echo"},
						Args:    []string{"Hello Ray"},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "ray-worker",
						State: corev1.ContainerState{},
					},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod3",
				Namespace: namespaceStr,
				Labels: map[string]string{
					utils.RayNodeLabelKey:      "yes",
					utils.RayClusterLabelKey:   instanceName,
					utils.RayNodeGroupLabelKey: groupNameStr,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-worker",
						Image:   "rayproject/autoscaler",
						Command: []string{"echo"},
						Args:    []string{"Hello Ray"},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "ray-worker",
						State: corev1.ContainerState{},
					},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod4",
				Namespace: namespaceStr,
				Labels: map[string]string{
					utils.RayNodeLabelKey:      "yes",
					utils.RayClusterLabelKey:   instanceName,
					utils.RayNodeGroupLabelKey: groupNameStr,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-worker",
						Image:   support.GetRayImage(),
						Command: []string{"echo"},
						Args:    []string{"Hello Ray"},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "ray-worker",
						State: corev1.ContainerState{},
					},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod5",
				Namespace: namespaceStr,
				Labels: map[string]string{
					utils.RayNodeLabelKey:      "yes",
					utils.RayClusterLabelKey:   instanceName,
					utils.RayNodeGroupLabelKey: groupNameStr,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-worker",
						Image:   support.GetRayImage(),
						Command: []string{"echo"},
						Args:    []string{"Hello Ray"},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "ray-worker",
						State: corev1.ContainerState{},
					},
				},
			},
		},
	}
	testPodsNoHeadIP = []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      headNodeName,
				Namespace: namespaceStr,
				Labels: map[string]string{
					utils.RayNodeLabelKey:      "yes",
					utils.RayClusterLabelKey:   instanceName,
					utils.RayNodeTypeLabelKey:  string(rayv1.HeadNode),
					utils.RayNodeGroupLabelKey: headGroupNameStr,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				PodIP: "",
			},
		},
	}
	testRayCluster = &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: namespaceStr,
		},
		Spec: rayv1.RayClusterSpec{
			EnableInTreeAutoscaling: &enableInTreeAutoscaling,
			HeadGroupSpec: rayv1.HeadGroupSpec{
				RayStartParams: map[string]string{
					"port":                "6379",
					"object-manager-port": "12345",
					"node-manager-port":   "12346",
					"object-store-memory": "100000000",
					"num-cpus":            "1",
				},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:    "ray-head",
								Image:   support.GetRayImage(),
								Command: []string{"python"},
								Args:    []string{"/opt/code.py"},
								Env: []corev1.EnvVar{
									{
										Name: "MY_POD_IP",
										ValueFrom: &corev1.EnvVarSource{
											FieldRef: &corev1.ObjectFieldSelector{
												FieldPath: "status.podIP",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{
					Replicas:    ptr.To[int32](expectReplicaNum),
					MinReplicas: ptr.To[int32](0),
					MaxReplicas: ptr.To[int32](10000),
					NumOfHosts:  expectNumOfHostNum,
					GroupName:   groupNameStr,
					RayStartParams: map[string]string{
						"port":     "6379",
						"num-cpus": "1",
					},
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:    "ray-worker",
									Image:   support.GetRayImage(),
									Command: []string{"echo"},
									Args:    []string{"Hello Ray"},
									Env: []corev1.EnvVar{
										{
											Name: "MY_POD_IP",
											ValueFrom: &corev1.EnvVarSource{
												FieldRef: &corev1.ObjectFieldSelector{
													FieldPath: "status.podIP",
												},
											},
										},
									},
								},
							},
						},
					},
					ScaleStrategy: rayv1.ScaleStrategy{
						WorkersToDelete: workersToDelete,
					},
				},
			},
		},
	}

	headService, err := common.BuildServiceForHeadPod(context.Background(), *testRayCluster, nil, nil)
	if err != nil {
		t.Errorf("failed to build head service: %v", err)
	}
	// K8s automatically sets TargetPort to the same value as Port. So we mimic that behavior here.
	for i, port := range headService.Spec.Ports {
		headService.Spec.Ports[i].TargetPort = intstr.IntOrString{IntVal: port.Port}
	}
	testServices = []runtime.Object{
		headService,
	}

	instanceReqValue := []string{instanceName}
	instanceReq, err := labels.NewRequirement(
		utils.RayClusterLabelKey,
		selection.Equals,
		instanceReqValue)
	require.NoError(t, err, "Fail to create requirement")
	groupNameReqValue := []string{groupNameStr}
	groupNameReq, err := labels.NewRequirement(
		utils.RayNodeGroupLabelKey,
		selection.Equals,
		groupNameReqValue)
	require.NoError(t, err, "Fail to create requirement")
	headNameReqValue := []string{headGroupNameStr}
	headNameReq, err := labels.NewRequirement(
		utils.RayNodeGroupLabelKey,
		selection.Equals,
		headNameReqValue)
	require.NoError(t, err, "Fail to create requirement")
	headSelector = labels.NewSelector().Add(*headNameReq)
	workerSelector = labels.NewSelector().Add(*instanceReq).Add(*groupNameReq)
}

func TestReconcile_RemoveWorkersToDelete_RandomDelete(t *testing.T) {
	setupTest(t)

	// TODO (kevin85421): The tests in this file are not independent. As a workaround,
	// I added the assertion to prevent the test logic from being affected by other changes.
	// However, we should refactor the tests in the future.

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup (2) The goal state of the workerGroup is 3 replicas. (3) ENABLE_RANDOM_POD_DELETE is set to true.
	defer os.Unsetenv(utils.ENABLE_RANDOM_POD_DELETE)

	assert.Len(t, testRayCluster.Spec.WorkerGroupSpecs, 1, "This test assumes only one worker group.")
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")

	// Pod random deletion is enabled in the following two cases:
	// Case 1: If Autoscaler is disabled, we will always enable random Pod deletion no matter the value of the feature flag.
	// Case 2: If Autoscaler is enabled, we will respect the value of the feature flag. If the feature flag environment variable
	// 		   is not set, we will disable random Pod deletion by default.
	// Here, we enable the Autoscaler and set the feature flag `ENABLE_RANDOM_POD_DELETE` to true to enable random Pod deletion.
	os.Setenv(utils.ENABLE_RANDOM_POD_DELETE, "true")
	enableInTreeAutoscaling := true

	tests := []struct {
		name            string
		workersToDelete []string
		numRandomDelete int
	}{
		// The random Pod deletion (diff < 0) will delete Pod based on the index of the Pod list.
		// That is, it will firstly delete Pod1, then Pod2, then Pod3, then Pod4, then Pod5. Hence,
		// we need to test the different cases of the workersToDelete to make sure both Pod deletion
		// works as expected.
		{
			name: "Set WorkersToDelete to pod1 and pod2.",
			// The pod1 and pod2 will be deleted.
			workersToDelete: []string{"pod1", "pod2"},
			numRandomDelete: 0,
		},
		{
			name: "Set WorkersToDelete to pod3 and pod4",
			// The pod3 and pod4 will be deleted. If the random Pod deletion is triggered, it will firstly delete pod1 and make the test fail.
			workersToDelete: []string{"pod3", "pod4"},
			numRandomDelete: 0,
		},
		{
			name: "Set WorkersToDelete to pod1 and pod5",
			// The pod1 and pod5 will be deleted.
			workersToDelete: []string{"pod1", "pod5"},
			numRandomDelete: 0,
		},
		{
			name: "Set WorkersToDelete to pod2 and NonExistentPod",
			// The pod2 will be deleted, and 1 pod will be deleted randomly to meet `expectedNumWorkerPods`.
			workersToDelete: []string{"pod2", "NonExistentPod"},
			numRandomDelete: 1,
		},
		{
			name: "Set WorkersToDelete to NonExistentPod1 and NonExistentPod1",
			// Two Pods will be deleted randomly to meet `expectedNumWorkerPods`.
			workersToDelete: []string{"NonExistentPod1", "NonExistentPod2"},
			numRandomDelete: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Initialize a fake client with newScheme and runtimeObjects.
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
			ctx := context.Background()
			podList := corev1.PodList{}
			err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
			require.NoError(t, err, "Fail to get pod list")
			numAllPods := len(podList.Items)
			numWorkerPods := numAllPods - 1 // -1 for the head pod
			assert.Equal(t, len(testPods), numAllPods, "Init pod list len is wrong")

			// Sanity check
			testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = tc.workersToDelete
			testRayCluster.Spec.EnableInTreeAutoscaling = &enableInTreeAutoscaling
			expectedNumWorkersToDelete := numWorkerPods - expectedNumWorkerPods
			nonExistentPodSet := make(map[string]struct{})
			for _, podName := range testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete {
				exist := false
				for _, pod := range podList.Items {
					if pod.Name == podName {
						exist = true
						break
					}
				}
				if !exist {
					nonExistentPodSet[podName] = struct{}{}
				}
			}

			// Simulate the Ray Autoscaler attempting to scale down.
			assert.Len(t, testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete, expectedNumWorkersToDelete)
			testRayClusterReconciler := &RayClusterReconciler{
				Client:                     fakeClient,
				Recorder:                   &record.FakeRecorder{},
				Scheme:                     scheme.Scheme,
				rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
			}

			err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
			require.NoError(t, err, "Fail to reconcile Pods")
			err = fakeClient.List(ctx, &podList, &client.ListOptions{
				LabelSelector: workerSelector,
				Namespace:     namespaceStr,
			})
			require.NoError(t, err, "Fail to get pod list after reconcile")
			assert.Len(t, podList.Items, expectedNumWorkerPods,
				"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, len(podList.Items))

			// Check if the workersToDelete are deleted.
			for _, pod := range podList.Items {
				if contains(tc.workersToDelete, pod.Name) {
					t.Fatalf("WorkersToDelete is not actually deleted, %s", pod.Name)
				}
			}
			numRandomDelete := expectedNumWorkersToDelete - (len(tc.workersToDelete) - len(nonExistentPodSet))
			assert.Equal(t, tc.numRandomDelete, numRandomDelete)
		})
	}
}

func TestReconcile_RemoveWorkersToDelete_NoRandomDelete(t *testing.T) {
	setupTest(t)

	// TODO (kevin85421): The tests in this file are not independent. As a workaround,
	// I added the assertion to prevent the test logic from being affected by other changes.
	// However, we should refactor the tests in the future.

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup (2) The goal state of the workerGroup is 3 replicas. (3) Disable random Pod deletion.
	defer os.Unsetenv(utils.ENABLE_RANDOM_POD_DELETE)

	assert.Len(t, testRayCluster.Spec.WorkerGroupSpecs, 1, "This test assumes only one worker group.")
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")

	// If Autoscaler is enabled, we will respect the value of the feature flag. If the feature flag environment variable
	// is not set, we will disable random Pod deletion by default. Hence, this test will disable random Pod deletion.
	// In this case, the cluster won't achieve the target state (i.e., `expectedNumWorkerPods` worker Pods) in one reconciliation.
	// Instead, the Ray Autoscaler will gradually scale down the cluster in subsequent reconciliations until it reaches the target state.
	os.Unsetenv(utils.ENABLE_RANDOM_POD_DELETE)
	enableInTreeAutoscaling := true

	tests := []struct {
		name            string
		workersToDelete []string
		numNonExistPods int
	}{
		{
			name: "Set WorkersToDelete to pod2 and pod3.",
			// The pod2 and pod3 will be deleted. The number of remaining Pods will be 3.
			workersToDelete: []string{"pod2", "pod3"},
			numNonExistPods: 0,
		},
		{
			name: "Set WorkersToDelete to pod2 and NonExistentPod",
			// Only pod2 will be deleted. The number of remaining Pods will be 4.
			workersToDelete: []string{"pod2", "NonExistentPod"},
			numNonExistPods: 1,
		},
		{
			name: "Set WorkersToDelete to NonExistentPod1 and NonExistentPod1",
			// No Pod will be deleted. The number of remaining Pods will be 5.
			workersToDelete: []string{"NonExistentPod1", "NonExistentPod2"},
			numNonExistPods: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Initialize a fake client with newScheme and runtimeObjects.
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
			ctx := context.Background()
			podList := corev1.PodList{}
			err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
			require.NoError(t, err, "Fail to get pod list")
			numAllPods := len(podList.Items)
			numWorkerPods := numAllPods - 1 // -1 for the head pod
			assert.Equal(t, len(testPods), numAllPods, "Init pod list len is wrong")

			// Sanity check
			testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = tc.workersToDelete
			testRayCluster.Spec.EnableInTreeAutoscaling = &enableInTreeAutoscaling

			// Because we disable random Pod deletion, only the Pods in the `workersToDelete` will be deleted.
			expectedNumWorkersToDelete := numWorkerPods - expectedNumWorkerPods - tc.numNonExistPods

			// Simulate the Ray Autoscaler attempting to scale down.
			assert.Equal(t, expectedNumWorkersToDelete, len(testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete)-tc.numNonExistPods)

			testRayClusterReconciler := &RayClusterReconciler{
				Client:                     fakeClient,
				Recorder:                   &record.FakeRecorder{},
				Scheme:                     scheme.Scheme,
				rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
			}

			err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
			require.NoError(t, err, "Fail to reconcile Pods")
			err = fakeClient.List(ctx, &podList, &client.ListOptions{
				LabelSelector: workerSelector,
				Namespace:     namespaceStr,
			})
			require.NoError(t, err, "Fail to get pod list after reconcile")
			assert.Equal(t, expectedNumWorkersToDelete, numWorkerPods-len(podList.Items))

			// Check if the workersToDelete are deleted.
			for _, pod := range podList.Items {
				if contains(tc.workersToDelete, pod.Name) {
					t.Fatalf("WorkersToDelete is not actually deleted, %s", pod.Name)
				}
			}
		})
	}
}

func TestReconcile_RandomDelete_OK(t *testing.T) {
	setupTest(t)

	// Pod random deletion is enabled in the following two cases:
	// Case 1: If Autoscaler is disabled, we will always enable random Pod deletion no matter the value of the feature flag.
	// Case 2: If Autoscaler is enabled, we will respect the value of the feature flag. If the feature flag environment variable
	// 		   is not set, we will disable random Pod deletion by default.
	// Here, we disable the Autoscaler to enable random Pod deletion.
	testRayCluster.Spec.EnableInTreeAutoscaling = nil

	var localExpectReplicaNum int32 = 2
	testRayCluster.Spec.WorkerGroupSpecs[0].Replicas = &localExpectReplicaNum

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
	ctx := context.Background()

	podList := corev1.PodList{}
	err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))

	require.NoError(t, err, "Fail to get pod list")

	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")
	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     scheme.Scheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	require.NoError(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})

	require.NoError(t, err, "Fail to get pod list after reconcile")

	assert.Len(t, podList.Items, int(localExpectReplicaNum),
		"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, len(podList.Items))

	for i := 0; i < len(podList.Items); i++ {
		if contains(workersToDelete, podList.Items[i].Name) {
			t.Fatalf("WorkersToDelete is not actually deleted, %s", podList.Items[i].Name)
		}
	}
}

func TestReconcile_PodDeleted_Diff0_OK(t *testing.T) {
	setupTest(t)

	// TODO (kevin85421): The tests in this file are not independent. As a workaround,
	// I added the assertion to prevent the test logic from being affected by other changes.
	// However, we should refactor the tests in the future.

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup (2) The goal state of the workerGroup is 3 replicas. (3) Set the workersToDelete to empty.
	assert.Len(t, testRayCluster.Spec.WorkerGroupSpecs, 1, "This test assumes only one worker group.")
	testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{}
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")

	// This test makes some assumptions about the testPods object.
	// `testPods` contains 6 pods, including 1 head pod and 5 worker pods.
	assert.Len(t, testPods, 6, "This test assumes the testPods object contains 6 pods.")
	numHeadPods := 1
	oldNumWorkerPods := len(testPods) - numHeadPods

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
	ctx := context.Background()

	// Get the pod list from the fake client.
	podList := corev1.PodList{}
	err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	require.NoError(t, err, "Fail to get pod list")
	assert.Len(t, podList.Items, oldNumWorkerPods+numHeadPods, "Init pod list len is wrong")

	// Simulate the deletion of 2 worker Pods. After the deletion, the number of worker Pods should be 3.
	err = fakeClient.Delete(ctx, &podList.Items[3])
	require.NoError(t, err, "Fail to delete pod")
	err = fakeClient.Delete(ctx, &podList.Items[4])
	require.NoError(t, err, "Fail to delete pod")

	// Initialize a new RayClusterReconciler.
	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     scheme.Scheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	// Since the desired state of the workerGroup is 3 replicas,
	// the controller will not create or delete any worker Pods.
	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	require.NoError(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})
	require.NoError(t, err, "Fail to get pod list after reconcile")
	assert.Len(t, podList.Items, expectedNumWorkerPods,
		"Replica number is wrong after reconcile expect %d actual %d", expectedNumWorkerPods, len(podList.Items))
}

func TestReconcile_PodDeleted_DiffLess0_OK(t *testing.T) {
	setupTest(t)

	// TODO (kevin85421): The tests in this file are not independent. As a workaround,
	// I added the assertion to prevent the test logic from being affected by other changes.
	// However, we should refactor the tests in the future.

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup (2) The goal state of the workerGroup is 3 replicas. (3) Set the workersToDelete to empty.
	// (4) Disable Autoscaler.
	assert.Len(t, testRayCluster.Spec.WorkerGroupSpecs, 1, "This test assumes only one worker group.")
	testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{}
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")
	testRayCluster.Spec.EnableInTreeAutoscaling = nil

	// This test makes some assumptions about the testPods object.
	// `testPods` contains 6 pods, including 1 head pod and 5 worker pods.
	assert.Len(t, testPods, 6, "This test assumes the testPods object contains 6 pods.")
	numHeadPods := 1
	oldNumWorkerPods := len(testPods) - numHeadPods

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	// Get the pod list from the fake client.
	podList := corev1.PodList{}
	ctx := context.Background()
	err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	require.NoError(t, err, "Fail to get pod list")
	assert.Len(t, podList.Items, oldNumWorkerPods+numHeadPods, "Init pod list len is wrong")

	// Simulate the deletion of 1 worker Pod. After the deletion, the number of worker Pods should be 4.
	err = fakeClient.Delete(ctx, &podList.Items[3])
	require.NoError(t, err, "Fail to delete pod")

	// Initialize a new RayClusterReconciler.
	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     scheme.Scheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	// Since the desired state of the workerGroup is 3 replicas, the controller
	// will delete a worker Pod randomly to reach the goal state.
	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	require.NoError(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})
	require.NoError(t, err, "Fail to get pod list after reconcile")

	// The number of worker Pods should be 3.
	assert.Len(t, podList.Items, expectedNumWorkerPods,
		"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, len(podList.Items))
}

func TestReconcile_Diff0_WorkersToDelete_OK(t *testing.T) {
	setupTest(t)

	// TODO (kevin85421): The tests in this file are not independent. As a workaround,
	// I added the assertion to prevent the test logic from being affected by other changes.
	// However, we should refactor the tests in the future.

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup
	// (2) The goal state of the workerGroup is 3 replicas.
	// (3) The workersToDelete has 2 worker Pods (pod3 and pod4). => Simulate the autoscaler scale-down.
	assert.Len(t, testRayCluster.Spec.WorkerGroupSpecs, 1, "This test assumes only one worker group.")
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")
	testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{"pod3", "pod4"}

	// This test makes some assumptions about the testPods object.
	// `testPods` contains 6 pods, including 1 head pod and 5 worker pods.
	assert.Len(t, testPods, 6, "This test assumes the testPods object contains 6 pods.")
	numHeadPods := 1
	oldNumWorkerPods := len(testPods) - numHeadPods

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
	ctx := context.Background()

	// Get the pod list from the fake client.
	podList := corev1.PodList{}
	err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	require.NoError(t, err, "Fail to get pod list")
	assert.Len(t, podList.Items, oldNumWorkerPods+numHeadPods, "Init pod list len is wrong")

	// Initialize a new RayClusterReconciler.
	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     scheme.Scheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	// Pod3 and Pod4 should be deleted because of the workersToDelete.
	// Hence, no failed Pods should exist in `podList`.
	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	require.NoError(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})
	require.NoError(t, err, "Fail to get pod list after reconcile")

	assert.Len(t, podList.Items, expectedNumWorkerPods)
	assert.Equal(t, expectedNumWorkerPods, getNotFailedPodItemNum(podList),
		"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, getNotFailedPodItemNum(podList))
}

func TestReconcile_PodCrash_DiffLess0_OK(t *testing.T) {
	setupTest(t)

	defer os.Unsetenv(utils.ENABLE_RANDOM_POD_DELETE)
	// TODO (kevin85421): The tests in this file are not independent. As a workaround,
	// I added the assertion to prevent the test logic from being affected by other changes.
	// However, we should refactor the tests in the future.

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup
	// (2) The goal state of the workerGroup is 3 replicas.
	// (3) The workersToDelete has 1 worker Pod (pod3). => Simulate the autoscaler scale-down.
	assert.Len(t, testRayCluster.Spec.WorkerGroupSpecs, 1, "This test assumes only one worker group.")
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")
	testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{"pod3"}
	enableInTreeAutoscaling := true
	testRayCluster.Spec.EnableInTreeAutoscaling = &enableInTreeAutoscaling

	// This test makes some assumptions about the testPods object.
	// `testPods` contains 6 pods, including 1 head pod and 5 worker pods.
	assert.Len(t, testPods, 6, "This test assumes the testPods object contains 6 pods.")
	numHeadPods := 1
	oldNumWorkerPods := len(testPods) - numHeadPods

	tests := []struct {
		name                  string
		enableRandomPodDelete bool
	}{
		// When Autoscaler is enabled, the random Pod deletion is controleld by the feature flag `enableRandomPodDelete`.
		{
			name:                  "Enable random Pod deletion",
			enableRandomPodDelete: true,
		},
		{
			name:                  "Disable random Pod deletion",
			enableRandomPodDelete: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Initialize a fake client with newScheme and runtimeObjects.
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
			ctx := context.Background()

			// Get the pod list from the fake client.
			podList := corev1.PodList{}
			err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
			require.NoError(t, err, "Fail to get pod list")
			assert.Len(t, podList.Items, oldNumWorkerPods+numHeadPods, "Init pod list len is wrong")

			// Initialize a new RayClusterReconciler.
			testRayClusterReconciler := &RayClusterReconciler{
				Client:                     fakeClient,
				Recorder:                   &record.FakeRecorder{},
				Scheme:                     scheme.Scheme,
				rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
			}

			if tc.enableRandomPodDelete {
				os.Setenv(utils.ENABLE_RANDOM_POD_DELETE, "true")
			} else {
				os.Setenv(utils.ENABLE_RANDOM_POD_DELETE, "false")
			}
			cluster := testRayCluster.DeepCopy()
			// Case 1: enableRandomPodDelete is true.
			// 	Since the desired state of the workerGroup is 3 replicas, the controller will delete a worker Pod randomly.
			//  After the deletion, the number of worker Pods should be 3.
			// Case 2: enableRandomPodDelete is false.
			//  Only the Pod in the `workersToDelete` will be deleted. After the deletion, the number of worker Pods should be 4.
			err = testRayClusterReconciler.reconcilePods(ctx, cluster)
			require.NoError(t, err, "Fail to reconcile Pods")

			err = fakeClient.List(ctx, &podList, &client.ListOptions{
				LabelSelector: workerSelector,
				Namespace:     namespaceStr,
			})
			require.NoError(t, err, "Fail to get pod list after reconcile")

			if tc.enableRandomPodDelete {
				// Case 1: enableRandomPodDelete is true.
				assert.Len(t, podList.Items, expectedNumWorkerPods)
				assert.Equal(t, expectedNumWorkerPods, getNotFailedPodItemNum(podList),
					"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, getNotFailedPodItemNum(podList))
			} else {
				// Case 2: enableRandomPodDelete is false.
				assert.Len(t, podList.Items, expectedNumWorkerPods+1)
				assert.Equal(t, expectedNumWorkerPods+1, getNotFailedPodItemNum(podList),
					"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, getNotFailedPodItemNum(podList))
			}
		})
	}
}

func TestReconcile_PodEvicted_DiffLess0_OK(t *testing.T) {
	setupTest(t)

	tests := []struct {
		name          string
		restartPolicy corev1.RestartPolicy
	}{
		{
			name:          "Pod with RestartPolicyAlways",
			restartPolicy: corev1.RestartPolicyAlways,
		},
		{
			name:          "Pod with RestartPolicyOnFailure",
			restartPolicy: corev1.RestartPolicyOnFailure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := clientFake.NewClientBuilder().
				WithRuntimeObjects(testPods...).
				Build()
			ctx := context.Background()
			podList := corev1.PodList{}
			err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))

			require.NoError(t, err, "Fail to get pod list")
			assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

			// Simulate head pod get evicted.
			podList.Items[0].Spec.RestartPolicy = tc.restartPolicy
			err = fakeClient.Update(ctx, &podList.Items[0])
			require.NoError(t, err, "Fail to update head Pod restart policy")
			podList.Items[0].Status.Phase = corev1.PodFailed
			err = fakeClient.Status().Update(ctx, &podList.Items[0])
			require.NoError(t, err, "Fail to update head Pod status")

			testRayClusterReconciler := &RayClusterReconciler{
				Client:                     fakeClient,
				Recorder:                   &record.FakeRecorder{},
				Scheme:                     scheme.Scheme,
				rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
			}

			err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
			// The head Pod with the status `Failed` will be deleted, and the function will return an
			// error to requeue the request with a short delay. If the function returns nil, the controller
			// will requeue the request after RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV (default: 300) seconds.
			require.Error(t, err)

			// Filter head pod
			err = fakeClient.List(ctx, &podList, &client.ListOptions{
				LabelSelector: headSelector,
				Namespace:     namespaceStr,
			})

			require.NoError(t, err, "Fail to get pod list after reconcile")
			assert.Empty(t, podList.Items,
				"Evicted head should be deleted after reconcile. Actual number of items: %d", len(podList.Items))
		})
	}
}

func TestReconcileHeadService(t *testing.T) {
	setupTest(t)

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	cluster := testRayCluster.DeepCopy()
	headService1 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "head-svc-1",
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				utils.RayClusterLabelKey:  cluster.Name,
				utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
				utils.RayIDLabelKey:       utils.CheckLabel(utils.GenerateIdentifier(cluster.Name, rayv1.HeadNode)),
			},
		},
	}
	headService2 := headService1.DeepCopy()
	headService2.Name = "head-svc-2"

	// Initialize a fake client with newScheme and runtimeObjects.
	runtimeObjects := []runtime.Object{cluster}
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	ctx := context.TODO()
	headServiceSelector := labels.SelectorFromSet(map[string]string{
		utils.RayClusterLabelKey:  cluster.Name,
		utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
		utils.RayIDLabelKey:       utils.CheckLabel(utils.GenerateIdentifier(cluster.Name, rayv1.HeadNode)),
	})

	// Initialize RayCluster reconciler.
	r := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     scheme.Scheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	// Case 1: Head service does not exist.
	err := r.reconcileHeadService(ctx, cluster)
	require.NoError(t, err, "Fail to reconcile head service")

	// One head service should be created.
	serviceList := corev1.ServiceList{}
	err = fakeClient.List(ctx, &serviceList, &client.ListOptions{
		LabelSelector: headServiceSelector,
		Namespace:     cluster.Namespace,
	})
	require.NoError(t, err, "Fail to get service list")
	assert.Len(t, serviceList.Items, 1, "Service list len is wrong")

	// Case 2: One head service exists.
	err = r.reconcileHeadService(ctx, cluster)
	require.NoError(t, err, "Fail to reconcile head service")

	// The namespace should still have only one head service.
	serviceList = corev1.ServiceList{}
	err = fakeClient.List(ctx, &serviceList, &client.ListOptions{
		LabelSelector: headServiceSelector,
		Namespace:     cluster.Namespace,
	})
	require.NoError(t, err, "Fail to get service list")
	assert.Len(t, serviceList.Items, 1, "Service list len is wrong")

	// Case 3: Two head services exist. This case only happens when users manually create a head service.
	runtimeObjects = []runtime.Object{headService1, headService2}
	fakeClient = clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	serviceList = corev1.ServiceList{}
	err = fakeClient.List(ctx, &serviceList, &client.ListOptions{
		LabelSelector: headServiceSelector,
		Namespace:     cluster.Namespace,
	})
	require.NoError(t, err, "Fail to get service list")
	assert.Len(t, serviceList.Items, 2, "Service list len is wrong")
	r.Client = fakeClient

	// When there are two head services, the reconciler should report an error.
	err = r.reconcileHeadService(ctx, cluster)
	require.Error(t, err, "Reconciler should report an error when there are two head services")
}

func TestReconcileHeadlessService(t *testing.T) {
	setupTest(t)

	// Specify a multi-host worker group
	testRayCluster.Spec.WorkerGroupSpecs[0].NumOfHosts = 4

	// Mock data
	cluster := testRayCluster.DeepCopy()

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Initialize a fake client with newScheme and runtimeObjects.
	runtimeObjects := []runtime.Object{cluster}
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	ctx := context.TODO()

	// Initialize RayCluster reconciler.
	r := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     scheme.Scheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	headlessServiceSelector := labels.SelectorFromSet(map[string]string{
		utils.RayClusterHeadlessServiceLabelKey: cluster.Name,
	})

	// Case 1: Headless service does not exist.
	err := r.reconcileHeadlessService(ctx, cluster)
	require.NoError(t, err, "Fail to reconcile head service")

	// One headless service should be created.
	serviceList := corev1.ServiceList{}
	err = fakeClient.List(ctx, &serviceList, &client.ListOptions{
		LabelSelector: headlessServiceSelector,
		Namespace:     cluster.Namespace,
	})
	expectedName := cluster.Name + utils.DashSymbol + utils.HeadlessServiceSuffix
	require.NoError(t, err, "Fail to get service list")
	assert.Len(t, serviceList.Items, 1, "Service list len is wrong")
	assert.Equal(t, expectedName, serviceList.Items[0].ObjectMeta.Name, "Headless Service name is wrong, expected %s actual %s", expectedName, serviceList.Items[0].ObjectMeta.Name)
	assert.Equal(t, "None", serviceList.Items[0].Spec.ClusterIP, "Created service is not a headless service, ClusterIP is not None")

	// Case 2: Headless service already exists, nothing should be done
	err = r.reconcileHeadlessService(ctx, cluster)
	require.NoError(t, err, "Fail to reconcile head service")

	// The namespace should still have only one headless service.
	serviceList = corev1.ServiceList{}
	err = fakeClient.List(ctx, &serviceList, &client.ListOptions{
		LabelSelector: headlessServiceSelector,
		Namespace:     cluster.Namespace,
	})
	require.NoError(t, err, "Fail to get service list")
	assert.Len(t, serviceList.Items, 1, "Service list len is wrong")
}

func contains(slice []string, item string) bool {
	set := make(map[string]struct{}, len(slice))
	for _, s := range slice {
		set[s] = struct{}{}
	}

	_, ok := set[item]
	return ok
}

func getNotFailedPodItemNum(podList corev1.PodList) int {
	count := 0
	for _, aPod := range podList.Items {
		if aPod.Status.Phase != corev1.PodFailed {
			count++
		}
	}

	return count
}

func TestReconcile_AutoscalerServiceAccount(t *testing.T) {
	setupTest(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
	ctx := context.Background()
	saNamespacedName := types.NamespacedName{
		Name:      utils.GetHeadGroupServiceAccountName(testRayCluster),
		Namespace: namespaceStr,
	}
	sa := corev1.ServiceAccount{}
	err := fakeClient.Get(ctx, saNamespacedName, &sa)

	assert.True(t, k8serrors.IsNotFound(err), "Head group service account should not exist yet")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     scheme.Scheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	err = testRayClusterReconciler.reconcileAutoscalerServiceAccount(ctx, testRayCluster)
	require.NoError(t, err, "Fail to reconcile autoscaler ServiceAccount")

	err = fakeClient.Get(ctx, saNamespacedName, &sa)

	require.NoError(t, err, "Fail to get head group ServiceAccount after reconciliation")
}

func TestReconcile_Autoscaler_ServiceAccountName(t *testing.T) {
	setupTest(t)

	// Specify a ServiceAccountName for the head Pod
	myServiceAccount := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-sa",
			Namespace: namespaceStr,
		},
	}
	cluster := testRayCluster.DeepCopy()
	cluster.Spec.HeadGroupSpec.Template.Spec.ServiceAccountName = myServiceAccount.Name

	// Case 1: There is no ServiceAccount "my-sa" in the Kubernetes cluster
	runtimeObjects := []runtime.Object{}
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(runtimeObjects...).Build()
	ctx := context.Background()

	// Initialize the reconciler
	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     scheme.Scheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	// If users specify ServiceAccountName for the head Pod, they need to create a ServiceAccount themselves.
	// However, if KubeRay creates a ServiceAccount for users, the autoscaler may encounter permission issues during
	// zero-downtime rolling updates when RayService is performed. See https://github.com/ray-project/kuberay/pull/1128
	// for more details.
	err := testRayClusterReconciler.reconcileAutoscalerServiceAccount(ctx, cluster)
	require.Error(t, err,
		"When users specify ServiceAccountName for the head Pod, they need to create a ServiceAccount themselves. "+
			"If the ServiceAccount does not exist, the reconciler should return an error. However, err is nil.")

	// Case 2: There is a ServiceAccount "my-sa" in the Kubernetes cluster
	runtimeObjects = []runtime.Object{&myServiceAccount}
	fakeClient = clientFake.NewClientBuilder().WithRuntimeObjects(runtimeObjects...).Build()

	// Initialize the reconciler
	testRayClusterReconciler = &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	err = testRayClusterReconciler.reconcileAutoscalerServiceAccount(ctx, cluster)
	require.NoError(t, err)
}

func TestReconcile_AutoscalerRoleBinding(t *testing.T) {
	setupTest(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
	ctx := context.Background()

	rbNamespacedName := types.NamespacedName{
		Name:      instanceName,
		Namespace: namespaceStr,
	}
	rb := rbacv1.RoleBinding{}
	err := fakeClient.Get(ctx, rbNamespacedName, &rb)

	assert.True(t, k8serrors.IsNotFound(err), "autoscaler RoleBinding should not exist yet")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     scheme.Scheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	err = testRayClusterReconciler.reconcileAutoscalerRoleBinding(ctx, testRayCluster)
	require.NoError(t, err, "Fail to reconcile autoscaler RoleBinding")

	err = fakeClient.Get(ctx, rbNamespacedName, &rb)

	require.NoError(t, err, "Fail to get autoscaler RoleBinding after reconciliation")
}

func TestReconcile_UpdateClusterReason(t *testing.T) {
	setupTest(t)

	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)

	fakeClient := clientFake.NewClientBuilder().
		WithScheme(newScheme).
		WithObjects(testRayCluster).
		WithStatusSubresource(testRayCluster).
		Build()
	ctx := context.Background()

	namespacedName := types.NamespacedName{
		Name:      instanceName,
		Namespace: namespaceStr,
	}
	cluster := rayv1.RayCluster{}
	err := fakeClient.Get(ctx, namespacedName, &cluster)
	require.NoError(t, err, "Fail to get RayCluster")
	assert.Empty(t, cluster.Status.Reason, "Cluster reason should be empty")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     scheme.Scheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}
	reason := "test reason"

	newTestRayCluster := testRayCluster.DeepCopy()
	newTestRayCluster.Status.Reason = reason
	inconsistent, err := testRayClusterReconciler.updateRayClusterStatus(ctx, testRayCluster, newTestRayCluster)
	require.NoError(t, err, "Fail to update cluster reason")
	assert.True(t, inconsistent)

	err = fakeClient.Get(ctx, namespacedName, &cluster)
	require.NoError(t, err, "Fail to get RayCluster after updating reason")
	assert.Equal(t, cluster.Status.Reason, reason, "Cluster reason should be updated")
}

func TestUpdateEndpoints(t *testing.T) {
	setupTest(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testServices...).Build()
	ctx := context.Background()
	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	if err := testRayClusterReconciler.updateEndpoints(ctx, testRayCluster); err != nil {
		t.Errorf("updateEndpoints failed: %v", err)
	}

	expected := map[string]string{
		"client":     "10001",
		"dashboard":  "8265",
		"metrics":    "8080",
		"gcs-server": "6379",
		"serve":      "8000",
	}
	assert.Equal(t, expected, testRayCluster.Status.Endpoints, "RayCluster status endpoints not updated")
}

func TestGetHeadPodIPAndNameFromGetRayClusterHeadPod(t *testing.T) {
	setupTest(t)

	extraHeadPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unexpectedExtraHeadNode",
			Namespace: namespaceStr,
			Labels: map[string]string{
				utils.RayClusterLabelKey:   instanceName,
				utils.RayNodeTypeLabelKey:  string(rayv1.HeadNode),
				utils.RayNodeGroupLabelKey: headGroupNameStr,
			},
		},
	}

	tests := []struct {
		name         string
		expectedIP   string
		expectedName string
		pods         []runtime.Object
		returnsError bool
	}{
		{
			name:         "get expected Pod IP if there's one head node",
			pods:         testPods,
			expectedIP:   headNodeIP,
			expectedName: headNodeName,
			returnsError: false,
		},
		{
			name:         "no error if there's no head node",
			pods:         []runtime.Object{},
			expectedIP:   "",
			returnsError: false,
		},
		{
			name:         "no error if there's more than one head node",
			pods:         append(testPods, extraHeadPod),
			expectedIP:   "",
			returnsError: true,
		},
		{
			name:         "no error if head pod ip is not yet set",
			pods:         testPodsNoHeadIP,
			expectedIP:   "",
			expectedName: headNodeName,
			returnsError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(tc.pods...).Build()

			ip, name := "", ""
			headPod, err := common.GetRayClusterHeadPod(context.TODO(), fakeClient, testRayCluster)
			if headPod != nil {
				ip = headPod.Status.PodIP
				name = headPod.Name
			}

			if tc.returnsError {
				require.Error(t, err, "GetRayClusterHeadPod should return error")
			} else {
				require.NoError(t, err, "GetRayClusterHeadPod should not return error")
			}

			assert.Equal(t, tc.expectedIP, ip, "GetRayClusterHeadPod returned unexpected IP")
			assert.Equal(t, tc.expectedName, name, "GetRayClusterHeadPod returned unexpected name")
		})
	}
}

func TestGetHeadServiceIPAndName(t *testing.T) {
	setupTest(t)

	headServiceIP := "1.2.3.4"
	headService, err := common.BuildServiceForHeadPod(context.Background(), *testRayCluster, nil, nil)
	if err != nil {
		t.Errorf("failed to build head service: %v", err)
	}
	headService.Spec.ClusterIP = headServiceIP
	testServices = []runtime.Object{
		headService,
	}

	extraHeadService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unexpectedExtraHeadService",
			Namespace: namespaceStr,
			Labels:    common.HeadServiceLabels(*testRayCluster),
		},
	}

	tests := []struct {
		name         string
		expectedIP   string
		expectedName string
		services     []runtime.Object
		returnsError bool
	}{
		{
			name:         "get expected Service IP if there's one head Service",
			services:     testServices,
			expectedIP:   headServiceIP,
			expectedName: headService.Name,
			returnsError: false,
		},
		{
			name:         "get error if there's no head Service",
			services:     []runtime.Object{},
			expectedIP:   "",
			expectedName: "",
			returnsError: true,
		},
		{
			name:         "get error if there's more than one head Service",
			services:     append(testServices, extraHeadService),
			expectedIP:   "",
			expectedName: "",
			returnsError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(tc.services...).Build()
			testRayClusterReconciler := &RayClusterReconciler{
				Client:                     fakeClient,
				Recorder:                   &record.FakeRecorder{},
				Scheme:                     scheme.Scheme,
				rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
			}

			ip, name, err := testRayClusterReconciler.getHeadServiceIPAndName(context.TODO(), testRayCluster)
			if tc.returnsError {
				require.Error(t, err, "getHeadServiceIPAndName should return error")
			} else {
				require.NoError(t, err, "getHeadServiceIPAndName should not return error")
			}

			assert.Equal(t, tc.expectedIP, ip, "getHeadServiceIPAndName returned unexpected IP")
			assert.Equal(t, tc.expectedName, name, "getHeadServiceIPAndName returned unexpected name")
		})
	}
}

func TestGetHeadServiceIPAndNameOnHeadlessService(t *testing.T) {
	setupTest(t)

	headService, err := common.BuildServiceForHeadPod(context.Background(), *testRayCluster, nil, nil)
	if err != nil {
		t.Errorf("failed to build head service: %v", err)
	}
	assert.Equal(t, corev1.ClusterIPNone, headService.Spec.ClusterIP, "BuildServiceForHeadPod returned unexpected ClusterIP")

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(headService).WithRuntimeObjects(testPods...).Build()

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	ip, name, err := testRayClusterReconciler.getHeadServiceIPAndName(context.TODO(), testRayCluster)

	require.NoError(t, err)
	assert.Equal(t, headNodeIP, ip, "getHeadServiceIPAndName returned unexpected IP")
	assert.Equal(t, headService.Name, name, "getHeadServiceIPAndName returned unexpected name")
}

func TestUpdateStatusObservedGeneration(t *testing.T) {
	setupTest(t)

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// To update the status of RayCluster with `r.Status().Update()`,
	// initialize the runtimeObjects with appropriate context. In KubeRay, the `ClusterIP`
	// and `TargetPort` fields are typically set by the cluster's control plane.
	headService, err := common.BuildServiceForHeadPod(context.Background(), *testRayCluster, nil, nil)
	require.NoError(t, err, "Failed to build head service.")
	headService.Spec.ClusterIP = headNodeIP
	for i, port := range headService.Spec.Ports {
		headService.Spec.Ports[i].TargetPort = intstr.IntOrString{IntVal: port.Port}
	}
	runtimeObjects := append(testPods, headService, testRayCluster)

	// To facilitate testing, we set an impossible value for ObservedGeneration.
	// Note that ObjectMeta's `Generation` and `ResourceVersion` don't behave properly in the fake client.
	// [Ref] https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.5/pkg/client/fake
	testRayCluster.Status.ObservedGeneration = -1

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	ctx := context.Background()

	// Verify the initial values of `Generation` and `ObservedGeneration`.
	namespacedName := types.NamespacedName{
		Name:      instanceName,
		Namespace: namespaceStr,
	}
	cluster := rayv1.RayCluster{}
	err = fakeClient.Get(ctx, namespacedName, &cluster)
	require.NoError(t, err, "Fail to get RayCluster")
	assert.Equal(t, int64(-1), cluster.Status.ObservedGeneration)
	assert.Equal(t, int64(0), cluster.ObjectMeta.Generation)

	// Initialize RayCluster reconciler.
	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     scheme.Scheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	// Compare the values of `Generation` and `ObservedGeneration` to check if they match.
	newInstance, err := testRayClusterReconciler.calculateStatus(ctx, testRayCluster, nil)
	require.NoError(t, err)
	err = fakeClient.Get(ctx, namespacedName, &cluster)
	require.NoError(t, err)
	assert.Equal(t, cluster.ObjectMeta.Generation, newInstance.Status.ObservedGeneration)
}

func TestReconcile_UpdateClusterState(t *testing.T) {
	setupTest(t)

	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)

	fakeClient := clientFake.NewClientBuilder().
		WithScheme(newScheme).
		WithObjects(testRayCluster).
		WithStatusSubresource(testRayCluster).
		Build()
	ctx := context.Background()

	namespacedName := types.NamespacedName{
		Name:      instanceName,
		Namespace: namespaceStr,
	}
	cluster := rayv1.RayCluster{}
	err := fakeClient.Get(ctx, namespacedName, &cluster)
	require.NoError(t, err, "Fail to get RayCluster")
	assert.Empty(t, cluster.Status.State, "Cluster state should be empty") //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288

	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     newScheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	state := rayv1.Ready
	newTestRayCluster := testRayCluster.DeepCopy()
	newTestRayCluster.Status.State = state //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
	inconsistent, err := testRayClusterReconciler.updateRayClusterStatus(ctx, testRayCluster, newTestRayCluster)
	require.NoError(t, err, "Fail to update cluster state")
	assert.True(t, inconsistent)

	err = fakeClient.Get(ctx, namespacedName, &cluster)
	require.NoError(t, err, "Fail to get RayCluster after updating state")
	assert.Equal(t, cluster.Status.State, state, "Cluster state should be updated") //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
}

func TestInconsistentRayClusterStatus(t *testing.T) {
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects().Build()
	r := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	// Mock data
	timeNow := metav1.Now()
	oldStatus := rayv1.RayClusterStatus{
		State:                   rayv1.Ready,
		ReadyWorkerReplicas:     1,
		AvailableWorkerReplicas: 1,
		DesiredWorkerReplicas:   1,
		MinWorkerReplicas:       1,
		MaxWorkerReplicas:       10,
		LastUpdateTime:          &timeNow,
		Endpoints: map[string]string{
			"client":    "10001",
			"dashboard": "8265",
			"gcs":       "6379",
			"metrics":   "8080",
		},
		Head: rayv1.HeadInfo{
			PodIP:     "10.244.0.6",
			ServiceIP: "10.96.140.249",
		},
		ObservedGeneration: 1,
		Reason:             "test reason",
	}

	// `inconsistentRayClusterStatus` is used to check whether the old and new RayClusterStatus are inconsistent
	// by comparing different fields. If the only differences between the old and new status are the `LastUpdateTime`
	// and `ObservedGeneration` fields, the status update will not be triggered.
	ctx := context.Background()

	testCases := []struct {
		modifyStatus func(*rayv1.RayClusterStatus)
		name         string
		expectResult bool
	}{
		{
			name: "State is updated, expect result to be true",
			modifyStatus: func(newStatus *rayv1.RayClusterStatus) {
				newStatus.State = rayv1.Suspended //nolint:staticcheck // Still need to check State even though it is deprecated, delete this no lint after this field is removed.
			},
			expectResult: true,
		},
		{
			name: "Reason is updated, expect result to be true",
			modifyStatus: func(newStatus *rayv1.RayClusterStatus) {
				newStatus.Reason = "new reason"
			},
			expectResult: true,
		},
		{
			name: "ReadyWorkerReplicas is updated, expect result to be true",
			modifyStatus: func(newStatus *rayv1.RayClusterStatus) {
				newStatus.ReadyWorkerReplicas = oldStatus.ReadyWorkerReplicas + 1
			},
			expectResult: true,
		},
		{
			name: "AvailableWorkerReplicas is updated, expect result to be true",
			modifyStatus: func(newStatus *rayv1.RayClusterStatus) {
				newStatus.AvailableWorkerReplicas = oldStatus.AvailableWorkerReplicas + 1
			},
			expectResult: true,
		},
		{
			name: "DesiredWorkerReplicas is updated, expect result to be true",
			modifyStatus: func(newStatus *rayv1.RayClusterStatus) {
				newStatus.DesiredWorkerReplicas = oldStatus.DesiredWorkerReplicas + 1
			},
			expectResult: true,
		},
		{
			name: "MinWorkerReplicas is updated, expect result to be true",
			modifyStatus: func(newStatus *rayv1.RayClusterStatus) {
				newStatus.MinWorkerReplicas = oldStatus.MinWorkerReplicas + 1
			},
			expectResult: true,
		},
		{
			name: "MaxWorkerReplicas is updated, expect result to be true",
			modifyStatus: func(newStatus *rayv1.RayClusterStatus) {
				newStatus.MaxWorkerReplicas = oldStatus.MaxWorkerReplicas + 1
			},
			expectResult: true,
		},
		{
			name: "Endpoints is updated, expect result to be true",
			modifyStatus: func(newStatus *rayv1.RayClusterStatus) {
				newStatus.Endpoints["fakeEndpoint"] = "10009"
			},
			expectResult: true,
		},
		{
			name: "Head.PodIP is updated, expect result to be true",
			modifyStatus: func(newStatus *rayv1.RayClusterStatus) {
				newStatus.Head.PodIP = "test head pod ip"
			},
			expectResult: true,
		},
		{
			name: "RayClusterReplicaFailure is updated, expect result to be true",
			modifyStatus: func(newStatus *rayv1.RayClusterStatus) {
				meta.SetStatusCondition(&newStatus.Conditions, metav1.Condition{Type: string(rayv1.RayClusterReplicaFailure), Status: metav1.ConditionTrue})
			},
			expectResult: true,
		},
		{
			name: "LastUpdateTime is updated, expect result to be false",
			modifyStatus: func(newStatus *rayv1.RayClusterStatus) {
				newStatus.LastUpdateTime = &metav1.Time{Time: timeNow.Add(time.Hour)}
			},
			expectResult: false,
		},
		{
			name: "ObservedGeneration is updated, expect result to be false",
			modifyStatus: func(newStatus *rayv1.RayClusterStatus) {
				newStatus.ObservedGeneration = oldStatus.ObservedGeneration + 1
			},
			expectResult: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			newStatus := oldStatus.DeepCopy()
			testCase.modifyStatus(newStatus)
			result := r.inconsistentRayClusterStatus(ctx, oldStatus, *newStatus)
			assert.Equal(t, testCase.expectResult, result)
		})
	}
}

func TestCalculateStatus(t *testing.T) {
	setupTest(t)
	assert.True(t, features.Enabled(features.RayClusterStatusConditions))

	// disable feature gate for the following tests
	features.SetFeatureGateDuringTest(t, features.RayClusterStatusConditions, false)

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	headServiceIP := "aaa.bbb.ccc.ddd"
	headService, err := common.BuildServiceForHeadPod(context.Background(), *testRayCluster, nil, nil)
	require.NoError(t, err, "Failed to build head service.")
	headService.Spec.ClusterIP = headServiceIP
	podReadyStatus := corev1.PodStatus{
		PodIP: headNodeIP,
		Phase: corev1.PodRunning,
		Conditions: []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			},
		},
	}
	headLabel := map[string]string{
		utils.RayClusterLabelKey:  instanceName,
		utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
	}
	workerLabel := map[string]string{
		utils.RayClusterLabelKey:  instanceName,
		utils.RayNodeTypeLabelKey: string(rayv1.WorkerNode),
	}
	headPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headNode",
			Namespace: namespaceStr,
			Labels:    headLabel,
		},
		Status: podReadyStatus,
	}
	runtimeObjects := []runtime.Object{headPod, headService}
	for i := int32(0); i < expectReplicaNum; i++ {
		runtimeObjects = append(runtimeObjects, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "workerNode-" + strconv.Itoa(int(i)),
				Namespace: namespaceStr,
				Labels:    workerLabel,
			},
			Status: podReadyStatus,
		})
	}

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	ctx := context.Background()

	// Initialize a RayCluster reconciler.
	r := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	// Test head information
	newInstance, err := r.calculateStatus(ctx, testRayCluster, nil)
	require.NoError(t, err)
	assert.Equal(t, headNodeIP, newInstance.Status.Head.PodIP)
	assert.Equal(t, headServiceIP, newInstance.Status.Head.ServiceIP)
	assert.Equal(t, headService.Name, newInstance.Status.Head.ServiceName)
	assert.NotNil(t, newInstance.Status.StateTransitionTimes, "Cluster state transition timestamp should be created")
	assert.Equal(t, newInstance.Status.LastUpdateTime, newInstance.Status.StateTransitionTimes[rayv1.Ready])

	// Test reconcilePodsErr with the feature gate disabled
	newInstance, err = r.calculateStatus(ctx, testRayCluster, errors.Join(utils.ErrFailedCreateHeadPod, errors.New("invalid")))
	require.NoError(t, err)
	assert.Empty(t, newInstance.Status.Conditions)

	// enable feature gate for the following tests
	features.SetFeatureGateDuringTest(t, features.RayClusterStatusConditions, true)

	// Test CheckRayHeadRunningAndReady with head pod running and ready
	newInstance, _ = r.calculateStatus(ctx, testRayCluster, nil)
	assert.True(t, meta.IsStatusConditionPresentAndEqual(newInstance.Status.Conditions, string(rayv1.HeadPodReady), metav1.ConditionTrue))

	// Test CheckRayHeadRunningAndReady with head pod not ready
	headPod.Status.Conditions = []corev1.PodCondition{
		{
			Type:   corev1.PodReady,
			Status: corev1.ConditionFalse,
		},
	}
	runtimeObjects = []runtime.Object{headPod, headService}
	fakeClient = clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	r.Client = fakeClient
	newInstance, _ = r.calculateStatus(ctx, testRayCluster, nil)
	assert.True(t, meta.IsStatusConditionPresentAndEqual(newInstance.Status.Conditions, string(rayv1.HeadPodReady), metav1.ConditionFalse))

	// Test CheckRayHeadRunningAndReady with head pod not running
	headPod.Status.Phase = corev1.PodFailed
	runtimeObjects = []runtime.Object{headPod, headService}
	fakeClient = clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	r.Client = fakeClient
	newInstance, _ = r.calculateStatus(ctx, testRayCluster, nil)
	assert.True(t, meta.IsStatusConditionPresentAndEqual(newInstance.Status.Conditions, string(rayv1.HeadPodReady), metav1.ConditionFalse))

	// Test reconcilePodsErr with the feature gate enabled
	newInstance, err = r.calculateStatus(ctx, testRayCluster, errors.Join(utils.ErrFailedCreateHeadPod, errors.New("invalid")))
	require.NoError(t, err)
	assert.True(t, meta.IsStatusConditionPresentAndEqual(newInstance.Status.Conditions, string(rayv1.RayClusterReplicaFailure), metav1.ConditionTrue))
}

// TestCalculateStatusWithoutDesiredReplicas tests that the cluster CR should not be marked as Ready if
// DesiredWorkerReplicas > 0 and DesiredWorkerReplicas != ReadyWorkerReplicas
func TestCalculateStatusWithoutDesiredReplicas(t *testing.T) {
	setupTest(t)

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	headServiceIP := "aaa.bbb.ccc.ddd"
	headService, err := common.BuildServiceForHeadPod(context.Background(), *testRayCluster, nil, nil)
	require.NoError(t, err, "Failed to build head service.")
	headService.Spec.ClusterIP = headServiceIP
	headPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headNode",
			Namespace: namespaceStr,
			Labels: map[string]string{
				utils.RayClusterLabelKey:  instanceName,
				utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
			},
		},
		Status: corev1.PodStatus{
			PodIP: headNodeIP,
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	runtimeObjects := []runtime.Object{headPod, headService}

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	ctx := context.Background()

	// Initialize a RayCluster reconciler.
	r := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	newInstance, err := r.calculateStatus(ctx, testRayCluster, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, newInstance.Status.DesiredWorkerReplicas)
	assert.NotEqual(t, newInstance.Status.DesiredWorkerReplicas, newInstance.Status.ReadyWorkerReplicas)
	assert.Equal(t, newInstance.Status.State, rayv1.ClusterState("")) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
	assert.Empty(t, newInstance.Status.Reason)
	assert.Nil(t, newInstance.Status.StateTransitionTimes)
}

// TestCalculateStatusWithSuspendedWorkerGroups tests that the cluster CR should be marked as Ready without workers
// and all desired resources are not counted with suspended workers
func TestCalculateStatusWithSuspendedWorkerGroups(t *testing.T) {
	setupTest(t)

	testRayCluster.Spec.WorkerGroupSpecs[0].Suspend = ptr.To[bool](true)
	testRayCluster.Spec.WorkerGroupSpecs[0].MinReplicas = ptr.To[int32](100)
	testRayCluster.Spec.WorkerGroupSpecs[0].MaxReplicas = ptr.To[int32](100)
	testRayCluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("100m"),
		corev1.ResourceMemory: resource.MustParse("100Mi"),
	}

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	headServiceIP := "aaa.bbb.ccc.ddd"
	headService, err := common.BuildServiceForHeadPod(context.Background(), *testRayCluster, nil, nil)
	require.NoError(t, err, "Failed to build head service.")
	headService.Spec.ClusterIP = headServiceIP
	headPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headNode",
			Namespace: namespaceStr,
			Labels: map[string]string{
				utils.RayClusterLabelKey:  instanceName,
				utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
			},
		},
		Status: corev1.PodStatus{
			PodIP: headNodeIP,
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	runtimeObjects := []runtime.Object{headPod, headService}

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	ctx := context.Background()

	// Initialize a RayCluster reconciler.
	r := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	newInstance, err := r.calculateStatus(ctx, testRayCluster, nil)
	require.NoError(t, err)
	assert.Zero(t, newInstance.Status.DesiredWorkerReplicas)
	assert.Zero(t, newInstance.Status.MinWorkerReplicas)
	assert.Zero(t, newInstance.Status.MaxWorkerReplicas)
	assert.Zero(t, newInstance.Status.DesiredCPU)
	assert.Zero(t, newInstance.Status.DesiredMemory)
	assert.Equal(t, rayv1.Ready, newInstance.Status.State) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
	assert.NotNil(t, newInstance.Status.StateTransitionTimes)
}

// TestCalculateStatusWithReconcileErrorBackAndForth tests that the cluster CR should not be marked as Ready if reconcileErr != nil
// and the Ready state should not be removed after being Ready even if reconcileErr != nil
func TestCalculateStatusWithReconcileErrorBackAndForth(t *testing.T) {
	setupTest(t)

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	headServiceIP := "aaa.bbb.ccc.ddd"
	headService, err := common.BuildServiceForHeadPod(context.Background(), *testRayCluster, nil, nil)
	require.NoError(t, err, "Failed to build head service.")
	headService.Spec.ClusterIP = headServiceIP
	podReadyStatus := corev1.PodStatus{
		PodIP: headNodeIP,
		Phase: corev1.PodRunning,
		Conditions: []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			},
		},
	}
	headLabel := map[string]string{
		utils.RayClusterLabelKey:  instanceName,
		utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
	}
	workerLabel := map[string]string{
		utils.RayClusterLabelKey:  instanceName,
		utils.RayNodeTypeLabelKey: string(rayv1.WorkerNode),
	}
	headPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headNode",
			Namespace: namespaceStr,
			Labels:    headLabel,
		},
		Status: podReadyStatus,
	}
	runtimeObjects := []runtime.Object{headPod, headService}
	for i := int32(0); i < expectReplicaNum; i++ {
		runtimeObjects = append(runtimeObjects, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "workerNode-" + strconv.Itoa(int(i)),
				Namespace: namespaceStr,
				Labels:    workerLabel,
			},
			Status: podReadyStatus,
		})
	}

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	ctx := context.Background()

	// Initialize a RayCluster reconciler.
	r := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	// Test head information with a reconcile error
	newInstance, err := r.calculateStatus(ctx, testRayCluster, errors.New("invalid"))
	require.NoError(t, err)
	assert.NotZero(t, newInstance.Status.DesiredWorkerReplicas)
	// Note that even if there are DesiredWorkerReplicas ready, we don't mark CR to be Ready state due to the reconcile error.
	assert.Equal(t, newInstance.Status.DesiredWorkerReplicas, newInstance.Status.ReadyWorkerReplicas)
	assert.Equal(t, rayv1.ClusterState(""), newInstance.Status.State) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
	assert.Empty(t, newInstance.Status.Reason)
	assert.Nil(t, newInstance.Status.StateTransitionTimes)

	// Test head information without a reconcile error
	newInstance, err = r.calculateStatus(ctx, newInstance, nil)
	require.NoError(t, err)
	assert.NotZero(t, newInstance.Status.DesiredWorkerReplicas)
	assert.Equal(t, newInstance.Status.DesiredWorkerReplicas, newInstance.Status.ReadyWorkerReplicas)
	assert.Equal(t, rayv1.Ready, newInstance.Status.State) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
	assert.Empty(t, newInstance.Status.Reason)
	assert.NotNil(t, newInstance.Status.StateTransitionTimes)
	assert.NotNil(t, newInstance.Status.StateTransitionTimes[rayv1.Ready])
	t1 := newInstance.Status.StateTransitionTimes[rayv1.Ready]

	// Test head information with a reconcile error again
	newInstance, err = r.calculateStatus(ctx, newInstance, errors.New("invalid2"))
	require.NoError(t, err)
	assert.NotZero(t, newInstance.Status.DesiredWorkerReplicas)
	assert.Equal(t, newInstance.Status.DesiredWorkerReplicas, newInstance.Status.ReadyWorkerReplicas)
	assert.Equal(t, rayv1.Ready, newInstance.Status.State) //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
	assert.Empty(t, newInstance.Status.Reason)
	assert.NotNil(t, newInstance.Status.StateTransitionTimes)
	assert.NotNil(t, newInstance.Status.StateTransitionTimes[rayv1.Ready])
	assert.Equal(t, t1, newInstance.Status.StateTransitionTimes[rayv1.Ready]) // no change to StateTransitionTimes
}

func TestRayClusterProvisionedCondition(t *testing.T) {
	setupTest(t)
	assert.True(t, features.Enabled(features.RayClusterStatusConditions))

	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	ReadyStatus := corev1.PodStatus{
		Phase: corev1.PodRunning,
		Conditions: []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			},
		},
	}

	UnReadyStatus := corev1.PodStatus{
		Phase: corev1.PodRunning,
		Conditions: []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionFalse,
			},
		},
	}

	headPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headNode",
			Namespace: namespaceStr,
			Labels: map[string]string{
				utils.RayClusterLabelKey:  instanceName,
				utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
			},
		},
	}

	workerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workerNode",
			Namespace: namespaceStr,
			Labels: map[string]string{
				utils.RayClusterLabelKey:   instanceName,
				utils.RayNodeTypeLabelKey:  string(rayv1.WorkerNode),
				utils.RayNodeGroupLabelKey: groupNameStr,
			},
		},
	}

	runtimeObjects := append([]runtime.Object{headPod, workerPod}, testServices...)
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	ctx := context.Background()
	r := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	// Initially, neither head Pod nor worker Pod are ready. The RayClusterProvisioned condition should not be present.
	headPod.Status = UnReadyStatus
	workerPod.Status = UnReadyStatus
	_ = fakeClient.Status().Update(ctx, headPod)
	_ = fakeClient.Status().Update(ctx, workerPod)
	testRayCluster, _ = r.calculateStatus(ctx, testRayCluster, nil)
	rayClusterProvisionedCondition := meta.FindStatusCondition(testRayCluster.Status.Conditions, string(rayv1.RayClusterProvisioned))
	assert.Equal(t, metav1.ConditionFalse, rayClusterProvisionedCondition.Status)
	assert.Equal(t, rayv1.RayClusterPodsProvisioning, rayClusterProvisionedCondition.Reason)

	// After a while, all Ray Pods are ready for the first time, RayClusterProvisioned condition should be added and set to True.
	headPod.Status = ReadyStatus
	workerPod.Status = ReadyStatus
	_ = fakeClient.Status().Update(ctx, headPod)
	_ = fakeClient.Status().Update(ctx, workerPod)
	testRayCluster, _ = r.calculateStatus(ctx, testRayCluster, nil)
	rayClusterProvisionedCondition = meta.FindStatusCondition(testRayCluster.Status.Conditions, string(rayv1.RayClusterProvisioned))
	assert.Equal(t, metav1.ConditionTrue, rayClusterProvisionedCondition.Status)
	assert.Equal(t, rayv1.AllPodRunningAndReadyFirstTime, rayClusterProvisionedCondition.Reason)

	// After a while, worker Pod fails readiness, but since RayClusterProvisioned focuses solely on whether all Ray Pods are ready for the first time,
	// RayClusterProvisioned condition should still be True.
	workerPod.Status = UnReadyStatus
	_ = fakeClient.Status().Update(ctx, workerPod)
	testRayCluster, _ = r.calculateStatus(ctx, testRayCluster, nil)
	rayClusterProvisionedCondition = meta.FindStatusCondition(testRayCluster.Status.Conditions, string(rayv1.RayClusterProvisioned))
	assert.Equal(t, metav1.ConditionTrue, rayClusterProvisionedCondition.Status)
	assert.Equal(t, rayv1.AllPodRunningAndReadyFirstTime, rayClusterProvisionedCondition.Reason)

	// After a while, head Pod also fails readiness, RayClusterProvisioned condition should still be true.
	headPod.Status = UnReadyStatus
	_ = fakeClient.Status().Update(ctx, headPod)
	testRayCluster, _ = r.calculateStatus(ctx, testRayCluster, nil)
	rayClusterProvisionedCondition = meta.FindStatusCondition(testRayCluster.Status.Conditions, string(rayv1.RayClusterProvisioned))
	assert.Equal(t, metav1.ConditionTrue, rayClusterProvisionedCondition.Status)
	assert.Equal(t, rayv1.AllPodRunningAndReadyFirstTime, rayClusterProvisionedCondition.Reason)
}

func TestStateTransitionTimes_NoStateChange(t *testing.T) {
	setupTest(t)

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	headServiceIP := "aaa.bbb.ccc.ddd"
	headService, err := common.BuildServiceForHeadPod(context.Background(), *testRayCluster, nil, nil)
	require.NoError(t, err, "Failed to build head service.")
	headService.Spec.ClusterIP = headServiceIP
	// headService.Spec.cont
	headPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headNode",
			Namespace: namespaceStr,
			Labels: map[string]string{
				utils.RayClusterLabelKey:  instanceName,
				utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
			},
		},
		Status: corev1.PodStatus{
			PodIP: headNodeIP,
			Phase: corev1.PodRunning,
		},
	}
	runtimeObjects := []runtime.Object{headPod, headService}

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	ctx := context.Background()

	// Initialize a RayCluster reconciler.
	r := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	preUpdateTime := metav1.Now()
	testRayCluster.Status.State = rayv1.Ready //nolint:staticcheck // https://github.com/ray-project/kuberay/pull/2288
	testRayCluster.Status.StateTransitionTimes = map[rayv1.ClusterState]*metav1.Time{rayv1.Ready: &preUpdateTime}
	newInstance, err := r.calculateStatus(ctx, testRayCluster, nil)
	require.NoError(t, err)
	assert.Equal(t, preUpdateTime, *newInstance.Status.StateTransitionTimes[rayv1.Ready], "Cluster state transition timestamp should not be updated")
}

func Test_TerminatedWorkers_NoAutoscaler(t *testing.T) {
	setupTest(t)

	// TODO (kevin85421): The tests in this file are not independent. As a workaround,
	// I added the assertion to prevent the test logic from being affected by other changes.
	// However, we should refactor the tests in the future.

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup
	// (2) The goal state of the workerGroup is 3 replicas.
	// (3) Set the `WorkersToDelete` field to an empty slice.
	// (4) Disable autoscaling.
	assert.Len(t, testRayCluster.Spec.WorkerGroupSpecs, 1, "This test assumes only one worker group.")
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")
	testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{}
	testRayCluster.Spec.EnableInTreeAutoscaling = nil

	// This test makes some assumptions about the testPods object.
	// `testPods` contains 6 pods, including 1 head pod and 5 worker pods.
	assert.Len(t, testPods, 6, "This test assumes the testPods object contains 6 pods.")
	numHeadPods := 1
	oldNumWorkerPods := len(testPods) - numHeadPods

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
	ctx := context.Background()

	// Get the pod list from the fake client.
	podList := corev1.PodList{}
	err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	require.NoError(t, err, "Fail to get pod list")
	assert.Len(t, podList.Items, oldNumWorkerPods+numHeadPods, "Init pod list len is wrong")

	// Make sure all worker Pods are running.
	for _, pod := range podList.Items {
		pod.Status.Phase = corev1.PodRunning
		err = fakeClient.Status().Update(ctx, &pod)
		require.NoError(t, err, "Fail to update pod status")
	}

	// Initialize a new RayClusterReconciler.
	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     scheme.Scheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	// Since the desired state of the workerGroup is 3 replicas, the controller
	// will delete 2 worker Pods.
	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	require.NoError(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})
	require.NoError(t, err, "Fail to get Pod list after reconcile")
	assert.Len(t, podList.Items, expectedNumWorkerPods)

	// Update 1 worker Pod to Failed (a terminate state) state.
	podList.Items[0].Status.Phase = corev1.PodFailed
	err = fakeClient.Status().Update(ctx, &podList.Items[0])
	require.NoError(t, err, "Fail to update Pod status")

	// Reconcile again, and the Failed worker Pod should be deleted even if the goal state of the workerGroup specifies 3 replicas.
	// The function will return an error to requeue the request after a brief delay. Moreover, if there are unhealthy worker
	// Pods to be deleted, the controller won't create new worker Pods during the same reconcile loop. As a result, the number of worker
	// Pods will be (expectedNumWorkerPods - 1) after the reconcile loop.
	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	require.Error(t, err)
	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})
	require.NoError(t, err, "Fail to get Pod list after reconcile")
	assert.Len(t, podList.Items, expectedNumWorkerPods-1)

	// Reconcile again, and the controller will create a new worker Pod to reach the goal state of the workerGroup.
	// Note that the status of new worker Pod created by the fake client is empty, so we need to set all worker
	// Pods to running state manually to avoid the new Pod being deleted in the next `reconcilePods` call.
	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	require.NoError(t, err)
	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})
	require.NoError(t, err, "Fail to get Pod list after reconcile")
	assert.Len(t, podList.Items, expectedNumWorkerPods)
	for _, pod := range podList.Items {
		pod.Status.Phase = corev1.PodRunning
		err = fakeClient.Status().Update(ctx, &pod)
		require.NoError(t, err, "Fail to update pod status")
	}

	// Update 1 worker Pod to Succeeded (a terminate state) state.
	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})
	require.NoError(t, err, "Fail to get Pod list after reconcile")
	podList.Items[0].Status.Phase = corev1.PodSucceeded
	err = fakeClient.Status().Update(ctx, &podList.Items[0])
	require.NoError(t, err, "Fail to update Pod status")

	// Reconcile again, and the Succeeded worker Pod should be deleted even if the goal state of the workerGroup specifies 3 replicas.
	// The function will return an error to requeue the request after a brief delay. Moreover, if there are unhealthy worker
	// Pods to be deleted, the controller won't create new worker Pods during the same reconcile loop. As a result, the number of worker
	// Pods will be (expectedNumWorkerPods - 1) after the reconcile loop.
	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	require.Error(t, err)
	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})
	require.NoError(t, err, "Fail to get Pod list after reconcile")
	assert.Len(t, podList.Items, expectedNumWorkerPods-1)

	// Reconcile again, and the controller will create a new worker Pod to reach the goal state of the workerGroup.
	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	require.NoError(t, err)
	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})
	require.NoError(t, err, "Fail to get Pod list after reconcile")
	assert.Len(t, podList.Items, expectedNumWorkerPods)
}

func Test_TerminatedHead_RestartPolicy(t *testing.T) {
	setupTest(t)

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Only one head Pod and no worker Pods in the RayCluster.
	runtimeObjects := testPods[0:1]
	cluster := testRayCluster.DeepCopy()
	cluster.Spec.WorkerGroupSpecs = nil
	fakeClient := clientFake.NewClientBuilder().
		WithScheme(newScheme).
		WithRuntimeObjects(runtimeObjects...).
		Build()
	ctx := context.Background()

	// Get the pod list from the fake client.
	podList := corev1.PodList{}
	err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	require.NoError(t, err, "Fail to get pod list")
	assert.Len(t, podList.Items, 1)
	assert.Equal(t, "headNode", podList.Items[0].Name)

	// Make sure the head Pod's restart policy is `Always` and status is `Failed`.
	// I have not observed this combination in practice, but no Kubernetes documentation
	// explicitly forbids it.
	podList.Items[0].Spec.RestartPolicy = corev1.RestartPolicyAlways
	err = fakeClient.Update(ctx, &podList.Items[0])
	require.NoError(t, err)
	podList.Items[0].Status.Phase = corev1.PodFailed
	err = fakeClient.Status().Update(ctx, &podList.Items[0])
	require.NoError(t, err)

	// Initialize a new RayClusterReconciler.
	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     newScheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	// The head Pod will be deleted regardless restart policy.
	err = testRayClusterReconciler.reconcilePods(ctx, cluster)
	require.Error(t, err)
	err = fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	require.NoError(t, err, "Fail to get pod list")
	assert.Empty(t, podList.Items)

	// The new head Pod will be created in this reconcile loop.
	err = testRayClusterReconciler.reconcilePods(ctx, cluster)
	require.NoError(t, err)
	err = fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	require.NoError(t, err, "Fail to get pod list")
	assert.Len(t, podList.Items, 1)

	// Make sure the head Pod's restart policy is `Never` and status is `Running`.
	podList.Items[0].Spec.RestartPolicy = corev1.RestartPolicyNever
	err = fakeClient.Update(ctx, &podList.Items[0])
	require.NoError(t, err)
	podList.Items[0].Status.Phase = corev1.PodRunning
	podList.Items[0].Status.ContainerStatuses = append(podList.Items[0].Status.ContainerStatuses,
		corev1.ContainerStatus{
			Name: podList.Items[0].Spec.Containers[utils.RayContainerIndex].Name,
			State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{},
			},
		})
	err = fakeClient.Status().Update(ctx, &podList.Items[0])
	require.NoError(t, err)

	// The head Pod will be deleted and the controller will return an error
	// instead of creating a new head Pod in the same reconcile loop.
	err = testRayClusterReconciler.reconcilePods(ctx, cluster)
	require.Error(t, err)
	err = fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	require.NoError(t, err, "Fail to get pod list")
	assert.Empty(t, podList.Items)

	// The new head Pod will be created in this reconcile loop.
	err = testRayClusterReconciler.reconcilePods(ctx, cluster)
	require.NoError(t, err)
	err = fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	require.NoError(t, err, "Fail to get pod list")
	assert.Len(t, podList.Items, 1)
}

func Test_RunningPods_RayContainerTerminated(t *testing.T) {
	setupTest(t)

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Only one head Pod and no worker Pods in the RayCluster.
	runtimeObjects := testPods[0:1]
	cluster := testRayCluster.DeepCopy()
	cluster.Spec.WorkerGroupSpecs = nil
	fakeClient := clientFake.NewClientBuilder().
		WithScheme(newScheme).
		WithRuntimeObjects(runtimeObjects...).
		WithStatusSubresource(cluster).
		Build()
	ctx := context.Background()

	// Get the pod list from the fake client.
	podList := corev1.PodList{}
	err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	require.NoError(t, err, "Fail to get pod list")
	assert.Len(t, podList.Items, 1)
	assert.Equal(t, "headNode", podList.Items[0].Name)

	// Make sure the head Pod's restart policy is `Never`, the Pod status is `Running`,
	// and the Ray container has terminated. The next `reconcilePods` call will delete
	// the head Pod and will not create a new one in the same reconciliation loop.
	podList.Items[0].Spec.RestartPolicy = corev1.RestartPolicyNever
	err = fakeClient.Update(ctx, &podList.Items[0])
	require.NoError(t, err)

	podList.Items[0].Status.Phase = corev1.PodRunning
	podList.Items[0].Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name: podList.Items[0].Spec.Containers[utils.RayContainerIndex].Name,
			State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{},
			},
		},
	}
	err = fakeClient.Status().Update(ctx, &podList.Items[0])
	require.NoError(t, err)

	// Initialize a new RayClusterReconciler.
	testRayClusterReconciler := &RayClusterReconciler{
		Client:                     fakeClient,
		Recorder:                   &record.FakeRecorder{},
		Scheme:                     newScheme,
		rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
	}

	// The head Pod will be deleted and the controller will return an error
	// instead of creating a new head Pod in the same reconcile loop.
	err = testRayClusterReconciler.reconcilePods(ctx, cluster)
	require.Error(t, err)
	err = fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	require.NoError(t, err, "Fail to get pod list")
	assert.Empty(t, podList.Items)

	// The new head Pod will be created in this reconcile loop.
	err = testRayClusterReconciler.reconcilePods(ctx, cluster)
	require.NoError(t, err)
	err = fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	require.NoError(t, err, "Fail to get pod list")
	assert.Len(t, podList.Items, 1)
}

func Test_ShouldDeletePod(t *testing.T) {
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "ray-head",
				},
			},
		},
	}
	tests := []struct {
		name            string
		restartPolicy   corev1.RestartPolicy
		phase           corev1.PodPhase
		containerStatus []corev1.ContainerStatus
		shouldDelete    bool
	}{
		{
			// The restart policy is `Always` and the Pod is in a terminate state.
			// The expected behavior is that the controller will delete the Pod regardless of the restart policy.
			name:          "restartPolicy=Always, phase=PodFailed, shouldDelete=true",
			restartPolicy: corev1.RestartPolicyAlways,
			phase:         corev1.PodFailed,
			shouldDelete:  true,
		},
		{
			// The restart policy is `Always`, the Pod is not in a terminate state,
			// and the Ray container has not terminated. The expected behavior is that the
			// controller will not delete the Pod.
			name:          "restartPolicy=Always, phase=PodRunning, ray-head=running, shouldDelete=false",
			restartPolicy: corev1.RestartPolicyAlways,
			phase:         corev1.PodRunning,
			containerStatus: []corev1.ContainerStatus{
				{
					Name: "ray-head",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
			shouldDelete: false,
		},
		{
			// The restart policy is `Always`, the Pod is not in a terminate state,
			// and the Ray container has terminated. The expected behavior is that the controller
			// will not delete the Pod because the restart policy is `Always`.
			name:          "restartPolicy=Always, phase=PodRunning, ray-head=terminated, shouldDelete=false",
			restartPolicy: corev1.RestartPolicyAlways,
			phase:         corev1.PodRunning,
			containerStatus: []corev1.ContainerStatus{
				{
					Name: "ray-head",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{},
					},
				},
			},
			shouldDelete: false,
		},
		{
			// The restart policy is `Never` and the Pod is in a terminate state.
			// The expected behavior is that the controller will delete the Pod.
			name:          "restartPolicy=Never, phase=PodFailed, shouldDelete=true",
			restartPolicy: corev1.RestartPolicyNever,
			phase:         corev1.PodFailed,
			shouldDelete:  true,
		},
		{
			// The restart policy is `Never` and the Pod terminated successfully.
			// The expected behavior is that the controller will delete the Pod.
			name:          "restartPolicy=Never, phase=PodSucceeded, shouldDelete=true",
			restartPolicy: corev1.RestartPolicyNever,
			phase:         corev1.PodSucceeded,
			shouldDelete:  true,
		},
		{
			// The restart policy is set to `Never`, the Pod is not in a terminated state, and
			// the Ray container has not terminated. The expected behavior is that the controller will not
			// delete the Pod.
			name:          "restartPolicy=Never, phase=PodRunning, ray-head=running, shouldDelete=false",
			restartPolicy: corev1.RestartPolicyNever,
			phase:         corev1.PodRunning,
			containerStatus: []corev1.ContainerStatus{
				{
					Name: "ray-head",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
			shouldDelete: false,
		},
		{
			// The restart policy is set to `Never`, the Pod is not in a terminated state, and
			// the Ray container has terminated. The expected behavior is that the controller will delete
			// the Pod.
			name:          "restartPolicy=Never, phase=PodRunning, ray-head=terminated, shouldDelete=true",
			restartPolicy: corev1.RestartPolicyNever,
			phase:         corev1.PodRunning,
			containerStatus: []corev1.ContainerStatus{
				{
					Name: "ray-head",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{},
					},
				},
			},
			shouldDelete: true,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			pod.Spec.RestartPolicy = testCase.restartPolicy
			pod.Status.Phase = testCase.phase
			pod.Status.ContainerStatuses = testCase.containerStatus

			shouldDelete, _ := shouldDeletePod(pod, rayv1.HeadNode)
			assert.EqualValues(
				t, shouldDelete, testCase.shouldDelete,
				"unexpected value of shouldDelete",
			)
		})
	}
}

func Test_RedisCleanupFeatureFlag(t *testing.T) {
	setupTest(t)

	defer os.Unsetenv(utils.ENABLE_GCS_FT_REDIS_CLEANUP)

	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Prepare a RayCluster with the GCS FT enabled and Autoscaling disabled.
	gcsFTEnabledCluster := testRayCluster.DeepCopy()
	if gcsFTEnabledCluster.Annotations == nil {
		gcsFTEnabledCluster.Annotations = make(map[string]string)
	}
	gcsFTEnabledCluster.Annotations[utils.RayFTEnabledAnnotationKey] = "true"
	gcsFTEnabledCluster.Spec.EnableInTreeAutoscaling = nil
	ctx := context.Background()

	// The KubeRay operator environment variable `ENABLE_GCS_FT_REDIS_CLEANUP` is used to enable/disable
	// the GCS FT Redis cleanup feature. If the feature flag is not set, the GCS FT Redis cleanup feature
	// is enabled by default.
	tests := []struct {
		name                    string
		enableGCSFTRedisCleanup string
		expectedNumFinalizers   int
	}{
		{
			name:                    "Enable GCS FT Redis cleanup",
			enableGCSFTRedisCleanup: "true",
			expectedNumFinalizers:   1,
		},
		{
			name:                    "Disable GCS FT Redis cleanup",
			enableGCSFTRedisCleanup: "false",
			expectedNumFinalizers:   0,
		},
		{
			name:                    "Feature flag is not set",
			enableGCSFTRedisCleanup: "unset",
			expectedNumFinalizers:   1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.enableGCSFTRedisCleanup == "unset" {
				os.Unsetenv(utils.ENABLE_GCS_FT_REDIS_CLEANUP)
			} else {
				os.Setenv(utils.ENABLE_GCS_FT_REDIS_CLEANUP, tc.enableGCSFTRedisCleanup)
			}

			cluster := gcsFTEnabledCluster.DeepCopy()
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(newScheme).
				WithObjects(cluster).
				WithStatusSubresource(cluster).
				Build()

			// Initialize the reconciler
			testRayClusterReconciler := &RayClusterReconciler{
				Client:                     fakeClient,
				Recorder:                   &record.FakeRecorder{},
				Scheme:                     newScheme,
				rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
			}

			rayClusterList := rayv1.RayClusterList{}
			err := fakeClient.List(ctx, &rayClusterList, client.InNamespace(namespaceStr))
			require.NoError(t, err, "Fail to get RayCluster list")
			assert.Len(t, rayClusterList.Items, 1)
			assert.Empty(t, rayClusterList.Items[0].Finalizers)

			_, err = testRayClusterReconciler.rayClusterReconcile(ctx, cluster)
			if tc.enableGCSFTRedisCleanup == "false" {
				require.NoError(t, err)
				podList := corev1.PodList{}
				err = fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
				require.NoError(t, err)
				assert.NotEmpty(t, podList.Items)
			} else {
				// Add the GCS FT Redis cleanup finalizer to the RayCluster.
				require.NoError(t, err)
			}

			// Check the RayCluster's finalizer
			rayClusterList = rayv1.RayClusterList{}
			err = fakeClient.List(ctx, &rayClusterList, client.InNamespace(namespaceStr))
			require.NoError(t, err, "Fail to get RayCluster list")
			assert.Len(t, rayClusterList.Items, 1)
			assert.Len(t, rayClusterList.Items[0].Finalizers, tc.expectedNumFinalizers)
			if tc.expectedNumFinalizers > 0 {
				assert.True(t, controllerutil.ContainsFinalizer(&rayClusterList.Items[0], utils.GCSFaultToleranceRedisCleanupFinalizer))

				// No Pod should be created before adding the GCS FT Redis cleanup finalizer.
				podList := corev1.PodList{}
				err = fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
				require.NoError(t, err, "Fail to get Pod list")
				assert.Empty(t, podList.Items)

				// Reconcile the RayCluster again. The controller should create Pods.
				_, err = testRayClusterReconciler.rayClusterReconcile(ctx, cluster)
				require.NoError(t, err)

				err = fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
				require.NoError(t, err, "Fail to get Pod list")
				assert.NotEmpty(t, podList.Items)
			}
		})
	}
}

func TestEvents_RedisCleanup(t *testing.T) {
	setupTest(t)
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)
	_ = batchv1.AddToScheme(newScheme)

	// Prepare a RayCluster with the GCS FT enabled and Autoscaling disabled.
	gcsFTEnabledCluster := testRayCluster.DeepCopy()
	if gcsFTEnabledCluster.Annotations == nil {
		gcsFTEnabledCluster.Annotations = make(map[string]string)
	}
	gcsFTEnabledCluster.Annotations[utils.RayFTEnabledAnnotationKey] = "true"
	gcsFTEnabledCluster.Spec.EnableInTreeAutoscaling = nil

	// Add the Redis cleanup finalizer to the RayCluster and modify the RayCluster's DeleteTimestamp to trigger the Redis cleanup.
	controllerutil.AddFinalizer(gcsFTEnabledCluster, utils.GCSFaultToleranceRedisCleanupFinalizer)
	now := metav1.Now()
	gcsFTEnabledCluster.DeletionTimestamp = &now
	errInjected := errors.New("random error")

	tests := []struct {
		fakeClientFn func(client.Object) client.Client
		errInjected  error
		message      string
	}{
		{
			fakeClientFn: func(obj client.Object) client.Client {
				return clientFake.NewClientBuilder().
					WithScheme(newScheme).
					WithRuntimeObjects([]runtime.Object{obj}...).
					Build()
			},
			errInjected: nil,
			message:     "Created Redis cleanup Job",
		},
		{
			fakeClientFn: func(obj client.Object) client.Client {
				return clientFake.NewClientBuilder().
					WithScheme(newScheme).
					WithRuntimeObjects([]runtime.Object{obj}...).
					WithInterceptorFuncs(interceptor.Funcs{
						Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
							return errInjected
						},
					}).
					Build()
			},
			errInjected: errInjected,
			message:     "Failed to create Redis cleanup Job",
		},
	}

	for _, tc := range tests {
		t.Run(tc.message, func(t *testing.T) {
			cluster := gcsFTEnabledCluster.DeepCopy()
			ctx := context.Background()

			fakeClient := tc.fakeClientFn(cluster)

			// Buffer length of 100 is arbitrary here. We should have only 1 event generated, but we keep 100
			// if that isn't the case in the future. If this test starts timing out because of a full
			// channel, this is probably the reason, and we should change our approach or increase buffer length.
			recorder := record.NewFakeRecorder(100)

			testRayClusterReconciler := &RayClusterReconciler{
				Client:   fakeClient,
				Recorder: recorder,
				Scheme:   newScheme,
			}

			_, err := testRayClusterReconciler.rayClusterReconcile(ctx, cluster)
			require.ErrorIs(t, err, tc.errInjected)

			var foundEvent bool
			var events []string
			for len(recorder.Events) > 0 {
				event := <-recorder.Events
				if strings.Contains(event, tc.message) {
					foundEvent = true
					break
				}
				events = append(events, event)
			}
			assert.Truef(t, foundEvent, "Expected event to be generated for redis cleanup job creation, got events: %s", strings.Join(events, "\n"))
		})
	}
}

func Test_RedisCleanup(t *testing.T) {
	setupTest(t)
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)
	_ = batchv1.AddToScheme(newScheme)

	// Prepare a RayCluster with the GCS FT enabled and Autoscaling disabled.
	gcsFTEnabledCluster := testRayCluster.DeepCopy()
	if gcsFTEnabledCluster.Annotations == nil {
		gcsFTEnabledCluster.Annotations = make(map[string]string)
	}
	gcsFTEnabledCluster.Annotations[utils.RayFTEnabledAnnotationKey] = "true"
	gcsFTEnabledCluster.Spec.EnableInTreeAutoscaling = nil

	// Add the Redis cleanup finalizer to the RayCluster and modify the RayCluster's DeleteTimestamp to trigger the Redis cleanup.
	controllerutil.AddFinalizer(gcsFTEnabledCluster, utils.GCSFaultToleranceRedisCleanupFinalizer)
	now := metav1.Now()
	gcsFTEnabledCluster.DeletionTimestamp = &now

	// TODO (kevin85421): Create a constant variable in constant.go for the head group name.
	const headGroupName = utils.RayNodeHeadGroupLabelValue

	headPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headNode",
			Namespace: gcsFTEnabledCluster.Namespace,
			Labels: map[string]string{
				utils.RayClusterLabelKey:   gcsFTEnabledCluster.Name,
				utils.RayNodeTypeLabelKey:  string(rayv1.HeadNode),
				utils.RayNodeGroupLabelKey: headGroupName,
			},
		},
	}
	workerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workerNode",
			Namespace: gcsFTEnabledCluster.Namespace,
			Labels: map[string]string{
				utils.RayClusterLabelKey:   gcsFTEnabledCluster.Name,
				utils.RayNodeTypeLabelKey:  string(rayv1.WorkerNode),
				utils.RayNodeGroupLabelKey: gcsFTEnabledCluster.Spec.WorkerGroupSpecs[0].GroupName,
			},
		},
	}

	tests := []struct {
		name            string
		hasHeadPod      bool
		hasWorkerPod    bool
		expectedNumJobs int
	}{
		{
			name:            "Both head and worker Pods are not terminated",
			hasHeadPod:      true,
			hasWorkerPod:    true,
			expectedNumJobs: 0,
		},
		{
			name:            "Only head Pod is terminated",
			hasHeadPod:      false,
			hasWorkerPod:    true,
			expectedNumJobs: 1,
		},
		{
			name:            "Only worker Pod is terminated",
			hasHeadPod:      true,
			hasWorkerPod:    false,
			expectedNumJobs: 0,
		},
		{
			name:            "Both head and worker Pods are terminated",
			hasHeadPod:      false,
			hasWorkerPod:    false,
			expectedNumJobs: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := gcsFTEnabledCluster.DeepCopy()
			runtimeObjects := []runtime.Object{cluster}
			if tc.hasHeadPod {
				runtimeObjects = append(runtimeObjects, headPod.DeepCopy())
			}
			if tc.hasWorkerPod {
				runtimeObjects = append(runtimeObjects, workerPod.DeepCopy())
			}
			ctx := context.Background()

			fakeClient := clientFake.NewClientBuilder().
				WithScheme(newScheme).
				WithRuntimeObjects(runtimeObjects...).
				WithStatusSubresource(cluster).
				Build()
			if tc.hasHeadPod {
				headPods := corev1.PodList{}
				err := fakeClient.List(ctx, &headPods, client.InNamespace(namespaceStr),
					client.MatchingLabels{
						utils.RayClusterLabelKey:   cluster.Name,
						utils.RayNodeGroupLabelKey: headGroupName,
						utils.RayNodeTypeLabelKey:  string(rayv1.HeadNode),
					})
				require.NoError(t, err)
				assert.Len(t, headPods.Items, 1)
			}
			if tc.hasWorkerPod {
				workerPods := corev1.PodList{}
				err := fakeClient.List(ctx, &workerPods, client.InNamespace(namespaceStr),
					client.MatchingLabels{
						utils.RayClusterLabelKey:   cluster.Name,
						utils.RayNodeGroupLabelKey: cluster.Spec.WorkerGroupSpecs[0].GroupName,
						utils.RayNodeTypeLabelKey:  string(rayv1.WorkerNode),
					})
				require.NoError(t, err)
				assert.Len(t, workerPods.Items, 1)
			}

			testRayClusterReconciler := &RayClusterReconciler{
				Client:   fakeClient,
				Recorder: &record.FakeRecorder{},
				Scheme:   newScheme,
			}

			// Check Job
			jobList := batchv1.JobList{}
			err := fakeClient.List(ctx, &jobList, client.InNamespace(namespaceStr))
			require.NoError(t, err, "Fail to get Job list")
			assert.Empty(t, jobList.Items)

			_, err = testRayClusterReconciler.rayClusterReconcile(ctx, cluster)
			require.NoError(t, err)

			// Check Job
			jobList = batchv1.JobList{}
			err = fakeClient.List(ctx, &jobList, client.InNamespace(namespaceStr))
			require.NoError(t, err, "Fail to get Job list")
			assert.Len(t, jobList.Items, tc.expectedNumJobs)

			if tc.expectedNumJobs > 0 {
				// Check RayCluster's finalizer
				rayClusterList := rayv1.RayClusterList{}
				err = fakeClient.List(ctx, &rayClusterList, client.InNamespace(namespaceStr))
				require.NoError(t, err, "Fail to get RayCluster list")
				assert.Len(t, rayClusterList.Items, 1)
				assert.True(t, controllerutil.ContainsFinalizer(&rayClusterList.Items[0], utils.GCSFaultToleranceRedisCleanupFinalizer))
				assert.Equal(t, int64(300), *jobList.Items[0].Spec.ActiveDeadlineSeconds)

				// Simulate the Job succeeded.
				job := jobList.Items[0]
				job.Status.Succeeded = 1
				job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
				err = fakeClient.Status().Update(ctx, &job)
				require.NoError(t, err, "Fail to update Job status")

				// Reconcile the RayCluster again. The controller should remove the finalizer and the RayCluster will be deleted.
				// See https://github.com/kubernetes-sigs/controller-runtime/blob/release-0.11/pkg/client/fake/client.go#L308-L310 for more details.
				_, err = testRayClusterReconciler.rayClusterReconcile(ctx, cluster)
				require.NoError(t, err, "Fail to reconcile RayCluster")
				err = fakeClient.List(ctx, &rayClusterList, client.InNamespace(namespaceStr))
				require.NoError(t, err, "Fail to get RayCluster list")
				assert.Empty(t, rayClusterList.Items)
			}
		})
	}
}

func TestReconcile_Replicas_Optional(t *testing.T) {
	setupTest(t)

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup (2) disable autoscaling
	assert.Len(t, testRayCluster.Spec.WorkerGroupSpecs, 1, "This test assumes only one worker group.")

	// Disable autoscaling so that the random Pod deletion is enabled.
	testRayCluster.Spec.EnableInTreeAutoscaling = ptr.To(false)
	testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{}

	tests := []struct {
		replicas        *int32
		minReplicas     *int32
		maxReplicas     *int32
		name            string
		desiredReplicas int
	}{
		{
			// If `Replicas` is nil, the controller will set the desired state of the workerGroup to `MinReplicas` Pods.
			// [Note]: It is not possible for `Replicas` to be nil in practice because it has a default value in the CRD.
			replicas:        nil,
			minReplicas:     ptr.To[int32](1),
			maxReplicas:     ptr.To[int32](10000),
			name:            "Replicas is nil",
			desiredReplicas: 1,
		},
		{
			// If `Replicas` is smaller than `MinReplicas`, the controller will set the desired state of the workerGroup to `MinReplicas` Pods.
			replicas:        ptr.To[int32](0),
			minReplicas:     ptr.To[int32](1),
			maxReplicas:     ptr.To[int32](10000),
			name:            "Replicas is smaller than MinReplicas",
			desiredReplicas: 1,
		},
		{
			// If `Replicas` is larger than `MaxReplicas`, the controller will set the desired state of the workerGroup to `MaxReplicas` Pods.
			replicas:        ptr.To[int32](4),
			minReplicas:     ptr.To[int32](1),
			maxReplicas:     ptr.To[int32](3),
			name:            "Replicas is larger than MaxReplicas",
			desiredReplicas: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := testRayCluster.DeepCopy()
			cluster.Spec.WorkerGroupSpecs[0].Replicas = tc.replicas
			cluster.Spec.WorkerGroupSpecs[0].MinReplicas = tc.minReplicas
			cluster.Spec.WorkerGroupSpecs[0].MaxReplicas = tc.maxReplicas

			// This test makes some assumptions about the testPods object.
			// `testPods` contains 6 pods, including 1 head pod and 5 worker pods.
			assert.Len(t, testPods, 6, "This test assumes the testPods object contains 6 pods.")
			numHeadPods := 1
			oldNumWorkerPods := len(testPods) - numHeadPods

			// Initialize a fake client with newScheme and runtimeObjects.
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
			ctx := context.Background()

			// Get the pod list from the fake client.
			podList := corev1.PodList{}
			err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
			require.NoError(t, err, "Fail to get pod list")
			assert.Len(t, podList.Items, oldNumWorkerPods+numHeadPods, "Init pod list len is wrong")

			// Initialize a new RayClusterReconciler.
			testRayClusterReconciler := &RayClusterReconciler{
				Client:                     fakeClient,
				Recorder:                   &record.FakeRecorder{},
				Scheme:                     scheme.Scheme,
				rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
			}

			// Since the desired state of the workerGroup is 1 replica,
			// the controller will delete 4 worker Pods.
			err = testRayClusterReconciler.reconcilePods(ctx, cluster)
			require.NoError(t, err, "Fail to reconcile Pods")

			err = fakeClient.List(ctx, &podList, &client.ListOptions{
				LabelSelector: workerSelector,
				Namespace:     namespaceStr,
			})
			require.NoError(t, err, "Fail to get pod list after reconcile")
			assert.Len(t, podList.Items, tc.desiredReplicas,
				"Replica number is wrong after reconcile expect %d actual %d", tc.desiredReplicas, len(podList.Items))
		})
	}
}

func TestReconcile_Multihost_Replicas(t *testing.T) {
	setupTest(t)

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup (2) disable autoscaling
	assert.Len(t, testRayCluster.Spec.WorkerGroupSpecs, 1, "This test assumes only one worker group.")

	// Disable autoscaling so that the random Pod deletion is enabled.
	// Set `NumOfHosts` to 4 to specify multi-host group
	testRayCluster.Spec.EnableInTreeAutoscaling = ptr.To(false)
	testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{}
	testRayCluster.Spec.WorkerGroupSpecs[0].NumOfHosts = 4

	tests := []struct {
		replicas        *int32
		minReplicas     *int32
		maxReplicas     *int32
		name            string
		desiredReplicas int
		numOfHosts      int
	}{
		{
			// If `Replicas` is nil, the controller will set the desired state of the workerGroup to `MinReplicas`*`NumOfHosts` Pods.
			replicas:        nil,
			minReplicas:     ptr.To[int32](1),
			maxReplicas:     ptr.To[int32](10000),
			name:            "Replicas is nil",
			desiredReplicas: 1,
			numOfHosts:      4,
		},
		{
			// If `Replicas` is smaller than `MinReplicas`, the controller will set the desired state of the workerGroup to `MinReplicas`*`NumOfHosts` Pods.
			replicas:        ptr.To[int32](0),
			minReplicas:     ptr.To[int32](1),
			maxReplicas:     ptr.To[int32](10000),
			name:            "Replicas is smaller than MinReplicas",
			desiredReplicas: 1,
			numOfHosts:      4,
		},
		{
			// If `Replicas` is larger than `MaxReplicas`, the controller will set the desired state of the workerGroup to `MaxReplicas`*`NumOfHosts` Pods.
			replicas:        ptr.To[int32](4),
			minReplicas:     ptr.To[int32](1),
			maxReplicas:     ptr.To[int32](3),
			name:            "Replicas is larger than MaxReplicas",
			desiredReplicas: 3,
			numOfHosts:      4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := testRayCluster.DeepCopy()
			cluster.Spec.WorkerGroupSpecs[0].Replicas = tc.replicas
			cluster.Spec.WorkerGroupSpecs[0].MinReplicas = tc.minReplicas
			cluster.Spec.WorkerGroupSpecs[0].MaxReplicas = tc.maxReplicas

			// This test makes some assumptions about the testPods object.
			// `testPods` contains 6 pods, including 1 head pod and 5 worker pods.
			assert.Len(t, testPods, 6, "This test assumes the testPods object contains 6 pods.")
			numHeadPods := 1
			oldNumWorkerPods := len(testPods) - numHeadPods

			// Initialize a fake client with newScheme and runtimeObjects.
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
			ctx := context.Background()

			// Get the pod list from the fake client.
			podList := corev1.PodList{}
			err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
			require.NoError(t, err, "Fail to get pod list")
			assert.Len(t, podList.Items, oldNumWorkerPods+numHeadPods, "Init pod list len is wrong")

			// Initialize a new RayClusterReconciler.
			testRayClusterReconciler := &RayClusterReconciler{
				Client:                     fakeClient,
				Recorder:                   &record.FakeRecorder{},
				Scheme:                     scheme.Scheme,
				rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
			}

			// Since the desired state of the workerGroup is 1 replica,
			// the controller will delete 4 worker Pods.
			err = testRayClusterReconciler.reconcilePods(ctx, cluster)
			require.NoError(t, err, "Fail to reconcile Pods")

			err = fakeClient.List(ctx, &podList, &client.ListOptions{
				LabelSelector: workerSelector,
				Namespace:     namespaceStr,
			})
			require.NoError(t, err, "Fail to get pod list after reconcile")
			assert.Len(t, podList.Items, tc.desiredReplicas*tc.numOfHosts,
				"Pod list is wrong after reconcile expect %d actual %d", tc.desiredReplicas*tc.numOfHosts, len(podList.Items))
		})
	}
}

func TestReconcile_NumOfHosts(t *testing.T) {
	setupTest(t)

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup (2) disable autoscaling
	assert.Len(t, testRayCluster.Spec.WorkerGroupSpecs, 1, "This test assumes only one worker group.")

	// Disable autoscaling so that the random Pod deletion is enabled.
	// Set `Replicas` to 1 and clear `WorkersToDelete`
	testRayCluster.Spec.EnableInTreeAutoscaling = ptr.To(false)
	testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{}
	testRayCluster.Spec.WorkerGroupSpecs[0].Replicas = ptr.To[int32](1)

	tests := []struct {
		replicas   *int32
		name       string
		numOfHosts int32
	}{
		{
			// If `NumOfHosts` is 1, the controller will set the desired state of the workerGroup to `Replicas` Pods.
			replicas:   ptr.To[int32](1),
			name:       "NumOfHosts is 1",
			numOfHosts: 1,
		},
		{
			// If `NumOfHosts` is larger than 1, the controller will set the desired state of the workerGroup to `NumOfHosts` Pods.
			replicas:   ptr.To[int32](1),
			name:       "NumOfHosts is larger than 1",
			numOfHosts: 4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := testRayCluster.DeepCopy()
			cluster.Spec.WorkerGroupSpecs[0].NumOfHosts = tc.numOfHosts

			// Initialize a fake client with newScheme and runtimeObjects.
			// The fake client will start with 1 head pod and 0 worker pods.
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods[0]).Build()
			ctx := context.Background()

			// Get the pod list from the fake client.
			podList := corev1.PodList{}
			err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
			require.NoError(t, err, "Fail to get pod list")
			assert.Len(t, podList.Items, 1, "Init pod list len is wrong")

			// Initialize a new RayClusterReconciler.
			testRayClusterReconciler := &RayClusterReconciler{
				Client:                     fakeClient,
				Recorder:                   &record.FakeRecorder{},
				Scheme:                     scheme.Scheme,
				rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
			}

			err = testRayClusterReconciler.reconcilePods(ctx, cluster)
			require.NoError(t, err, "Fail to reconcile Pods")

			err = fakeClient.List(ctx, &podList, &client.ListOptions{
				LabelSelector: workerSelector,
				Namespace:     namespaceStr,
			})
			require.NoError(t, err, "Fail to get pod list after reconcile")
			if tc.numOfHosts > 1 {
				assert.Len(t, podList.Items, int(tc.numOfHosts),
					"Number of worker pods is wrong after reconcile expect %d actual %d", int(tc.numOfHosts), len(podList.Items)-1)
			} else {
				assert.Len(t, podList.Items, int(*tc.replicas),
					"Replica number is wrong after reconcile expect %d actual %d", int(*tc.replicas), len(podList.Items))
			}
		})
	}
}

func TestSumGPUs(t *testing.T) {
	nvidiaGPUResourceName := corev1.ResourceName("nvidia.com/gpu")
	googleTPUResourceName := corev1.ResourceName("google.com/tpu")

	tests := []struct {
		name     string
		input    map[corev1.ResourceName]resource.Quantity
		expected resource.Quantity
	}{
		{
			name: "no GPUs specified",
			input: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU: resource.MustParse("1"),
			},
			expected: resource.MustParse("0"),
		},
		{
			name: "one GPU type specified",
			input: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("1"),
				nvidiaGPUResourceName: resource.MustParse("1"),
				googleTPUResourceName: resource.MustParse("1"),
			},
			expected: resource.MustParse("1"),
		},
		{
			name: "multiple GPUs specified",
			input: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:                 resource.MustParse("1"),
				nvidiaGPUResourceName:              resource.MustParse("3"),
				corev1.ResourceName("foo.bar/gpu"): resource.MustParse("2"),
				googleTPUResourceName:              resource.MustParse("1"),
			},
			expected: resource.MustParse("5"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := sumGPUs(tc.input)
			assert.True(t, tc.expected.Equal(result), "GPU number is wrong")
		})
	}
}

func TestDeleteAllPods(t *testing.T) {
	newScheme := runtime.NewScheme()
	_ = corev1.AddToScheme(newScheme)
	ns := "tmp-ns"
	ts := metav1.Now()
	filter := map[string]string{"app": "tmp"}

	p1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "alive",
			Namespace: ns,
			Labels:    filter,
		},
	}
	p2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleted",
			Namespace:         ns,
			Labels:            filter,
			DeletionTimestamp: &ts,
			Finalizers:        []string{"tmp"},
		},
	}
	p3 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other",
			Namespace: ns,
			Labels:    map[string]string{"app": "other"},
		},
	}

	fakeClient := clientFake.NewClientBuilder().
		WithScheme(newScheme).
		WithRuntimeObjects(p1, p2, p3).
		Build()

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   newScheme,
	}
	ctx := context.Background()
	// The first `deleteAllPods` function call should delete the "alive" Pod.
	pods, err := testRayClusterReconciler.deleteAllPods(ctx, common.AssociationOptions{client.InNamespace(ns), client.MatchingLabels(filter)})
	require.NoError(t, err)
	assert.Len(t, pods.Items, 2)
	assert.Subset(t, []string{"alive", "deleted"}, []string{pods.Items[0].Name, pods.Items[1].Name})
	// The second `deleteAllPods` function call should delete no Pods because none are active.
	pods, err = testRayClusterReconciler.deleteAllPods(ctx, common.AssociationOptions{client.InNamespace(ns), client.MatchingLabels(filter)})
	require.NoError(t, err)
	assert.Len(t, pods.Items, 1)
	assert.Equal(t, "deleted", pods.Items[0].Name)
	// Make sure that the above `deleteAllPods` calls didn't remove other Pods.
	pods = corev1.PodList{}
	err = fakeClient.List(ctx, &pods, client.InNamespace(ns))
	require.NoError(t, err)
	assert.Len(t, pods.Items, 2)
	assert.Subset(t, []string{"deleted", "other"}, []string{pods.Items[0].Name, pods.Items[1].Name})
}

func TestEvents_FailedPodCreation(t *testing.T) {
	tests := []struct {
		errInject error
		// simulate is responsible for simulating pod deletions in different scenarios.
		simulate   func(ctx context.Context, t *testing.T, podList corev1.PodList, client client.WithWatch)
		name       string
		failureMsg string
		podType    string
	}{
		{
			errInject: utils.ErrFailedCreateWorkerPod,
			simulate: func(ctx context.Context, t *testing.T, podList corev1.PodList, client client.WithWatch) {
				// Simulate the deletion of 3 worker Pods. After the deletion, the number of worker Pods should be 3.
				err := client.Delete(ctx, &podList.Items[2])
				require.NoError(t, err, "Fail to delete pod")
				err = client.Delete(ctx, &podList.Items[3])
				require.NoError(t, err, "Fail to delete pod")
				err = client.Delete(ctx, &podList.Items[4])
				require.NoError(t, err, "Fail to delete pod")
			},
			name:       "failure event for failed worker pod creation",
			failureMsg: "Failed to create worker Pod",
			podType:    "worker",
		},
		{
			errInject: utils.ErrFailedCreateHeadPod,
			simulate: func(ctx context.Context, t *testing.T, podList corev1.PodList, client client.WithWatch) {
				// Simulate the deletion of head pod
				err := client.Delete(ctx, &podList.Items[0])
				require.NoError(t, err, "Fail to delete pod")
			},
			name:       "failure event for failed head pod creation",
			failureMsg: "Failed to create head Pod",
			podType:    "head",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTest(t)

			// TODO (kevin85421): The tests in this file are not independent. As a workaround,
			// I added the assertion to prevent the test logic from being affected by other changes.
			// However, we should refactor the tests in the future.

			// This test makes some assumptions about the testRayCluster object.
			// (1) 1 workerGroup (2) The goal state of the workerGroup is 3 replicas. (3) Set the workersToDelete to empty.
			assert.Len(t, testRayCluster.Spec.WorkerGroupSpecs, 1, "This test assumes only one worker group.")
			testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{}
			expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
			assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")

			// This test makes some assumptions about the testPods object.
			// `testPods` contains 6 pods, including 1 head pod and 5 worker pods.
			assert.Len(t, testPods, 6, "This test assumes the testPods object contains 6 pods.")
			numHeadPods := 1
			oldNumWorkerPods := len(testPods) - numHeadPods

			// Initialize a fake client with newScheme and runtimeObjects.
			// We create a fake client with an interceptor for Create() in order to simulate a failure for pod creation.
			// We return utils.ErrFailedCreateWorkerPod here because we deleted a worker pod in the previous step, so
			// an attempt to reconcile that will take place.
			fakeClient := clientFake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return test.errInject
				},
			}).WithRuntimeObjects(testPods...).Build()
			ctx := context.Background()

			// Get the pod list from the fake client.
			podList := corev1.PodList{}
			err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
			require.NoError(t, err, "Fail to get pod list")
			assert.Len(t, podList.Items, oldNumWorkerPods+numHeadPods, "Init pod list len is wrong")

			test.simulate(ctx, t, podList, fakeClient)

			// Buffer length of 100 is arbitrary here. We should have only 1 event genereated, but we keep 100
			// if that isn't the case in the future. If this test starts timining out because of a full
			// channel, this is probably the reason and we should change our approach or increase buffer length.
			recorder := record.NewFakeRecorder(100)

			// Initialize a new RayClusterReconciler.
			testRayClusterReconciler := &RayClusterReconciler{
				Client:                     fakeClient,
				Recorder:                   recorder,
				Scheme:                     scheme.Scheme,
				rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
			}

			// Since the desired state of the workerGroup is 3 replicas,
			// the controller will try to create one worker pod.
			err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
			// We should get an error here because of simulating a pod creation failure.
			require.Error(t, err, "unexpected error")

			var foundFailureEvent bool
			events := []string{}
			for len(recorder.Events) > 0 {
				event := <-recorder.Events
				if strings.Contains(event, test.failureMsg) {
					foundFailureEvent = true
					break
				}
				events = append(events, event)
			}

			assert.Truef(t, foundFailureEvent, "Expected event to be generated for %s pod creation failure, got events: %s", test.podType, strings.Join(events, "\n"))
		})
	}
}

func Test_ReconcileManagedBy(t *testing.T) {
	setupTest(t)
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)
	_ = batchv1.AddToScheme(newScheme)

	tests := []struct {
		managedBy       *string
		name            string
		shouldReconcile bool
	}{
		{
			managedBy:       nil,
			name:            "ManagedBy field not set",
			shouldReconcile: true,
		},
		{
			managedBy:       ptr.To(utils.KubeRayController),
			name:            "ManagedBy field to RayOperator",
			shouldReconcile: true,
		},
		{
			managedBy: ptr.To(""),
			name:      "ManagedBy field empty",
		},
		{
			managedBy: ptr.To(MultiKueueController),
			name:      "ManagedBy field to external allowed controller",
		},
		{
			managedBy: ptr.To("controller.com/invalid"),
			name:      "ManagedBy field to external not allowed controller",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cluster := testRayCluster.DeepCopy()
			cluster.Spec.EnableInTreeAutoscaling = ptr.To(false)
			cluster.Status = rayv1.RayClusterStatus{}
			cluster.Spec.ManagedBy = tc.managedBy
			runtimeObjects := []runtime.Object{cluster}
			fakeClient := clientFake.NewClientBuilder().
				WithScheme(newScheme).
				WithRuntimeObjects(runtimeObjects...).
				WithStatusSubresource(cluster).
				Build()
			testRayClusterReconciler := &RayClusterReconciler{
				Client:                     fakeClient,
				Recorder:                   &record.FakeRecorder{},
				Scheme:                     newScheme,
				rayClusterScaleExpectation: expectations.NewRayClusterScaleExpectation(fakeClient),
			}

			result, err := testRayClusterReconciler.rayClusterReconcile(ctx, cluster)
			require.NoError(t, err)
			if tc.shouldReconcile {
				// finish with requeue due to detected incosistency
				assert.InDelta(t, result.RequeueAfter.Seconds(), DefaultRequeueDuration.Seconds(), 1e-6)
			} else {
				// skip reconciliation
				assert.InDelta(t, result.RequeueAfter.Seconds(), time.Duration(0).Seconds(), 1e-6)
			}
		})
	}
}
