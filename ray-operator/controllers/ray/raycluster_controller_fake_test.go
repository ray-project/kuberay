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
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	. "github.com/onsi/ginkgo"
	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/scheme"
	"github.com/stretchr/testify/assert"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/utils/pointer"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

var (
	namespaceStr            string
	instanceName            string
	enableInTreeAutoscaling bool
	headGroupNameStr        string
	headGroupServiceAccount string
	groupNameStr            string
	expectReplicaNum        int32
	testPods                []runtime.Object
	testPodsNoHeadIP        []runtime.Object
	testRayCluster          *rayv1alpha1.RayCluster
	headSelector            labels.Selector
	headNodeIP              string
	testServices            []runtime.Object
	workerSelector          labels.Selector
	workersToDelete         []string
)

func setupTest(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	namespaceStr = "default"
	instanceName = "raycluster-sample"
	enableInTreeAutoscaling = true
	headGroupNameStr = "head-group"
	headGroupServiceAccount = "head-service-account"
	groupNameStr = "small-group"
	expectReplicaNum = 3
	workersToDelete = []string{"pod1", "pod2"}
	headNodeIP = "1.2.3.4"
	testPods = []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "headNode",
				Namespace: namespaceStr,
				Labels: map[string]string{
					common.RayNodeLabelKey:      "yes",
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeTypeLabelKey:  string(rayv1alpha1.HeadNode),
					common.RayNodeGroupLabelKey: headGroupNameStr,
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
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: namespaceStr,
				Labels: map[string]string{
					common.RayNodeLabelKey:      "yes",
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeGroupLabelKey: groupNameStr,
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
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: namespaceStr,
				Labels: map[string]string{
					common.RayNodeLabelKey:      "yes",
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeGroupLabelKey: groupNameStr,
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
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod3",
				Namespace: namespaceStr,
				Labels: map[string]string{
					common.RayNodeLabelKey:      "yes",
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeGroupLabelKey: groupNameStr,
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
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod4",
				Namespace: namespaceStr,
				Labels: map[string]string{
					common.RayNodeLabelKey:      "yes",
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeGroupLabelKey: groupNameStr,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-worker",
						Image:   "rayproject/ray:2.5.0",
						Command: []string{"echo"},
						Args:    []string{"Hello Ray"},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod5",
				Namespace: namespaceStr,
				Labels: map[string]string{
					common.RayNodeLabelKey:      "yes",
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeGroupLabelKey: groupNameStr,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-worker",
						Image:   "rayproject/ray:2.5.0",
						Command: []string{"echo"},
						Args:    []string{"Hello Ray"},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
	}
	testPodsNoHeadIP = []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "headNode",
				Namespace: namespaceStr,
				Labels: map[string]string{
					common.RayNodeLabelKey:      "yes",
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeTypeLabelKey:  string(rayv1alpha1.HeadNode),
					common.RayNodeGroupLabelKey: headGroupNameStr,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				PodIP: "",
			},
		},
	}
	testRayCluster = &rayv1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: namespaceStr,
		},
		Spec: rayv1alpha1.RayClusterSpec{
			RayVersion:              "1.0",
			EnableInTreeAutoscaling: &enableInTreeAutoscaling,
			HeadGroupSpec: rayv1alpha1.HeadGroupSpec{
				Replicas: pointer.Int32Ptr(1),
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
								Image:   "rayproject/ray:2.5.0",
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
			WorkerGroupSpecs: []rayv1alpha1.WorkerGroupSpec{
				{
					Replicas:    pointer.Int32Ptr(expectReplicaNum),
					MinReplicas: pointer.Int32Ptr(0),
					MaxReplicas: pointer.Int32Ptr(10000),
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
									Image:   "rayproject/ray:2.5.0",
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
					ScaleStrategy: rayv1alpha1.ScaleStrategy{
						WorkersToDelete: workersToDelete,
					},
				},
			},
		},
	}

	headService, err := common.BuildServiceForHeadPod(*testRayCluster, nil, nil)
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
		common.RayClusterLabelKey,
		selection.Equals,
		instanceReqValue)
	assert.Nil(t, err, "Fail to create requirement")
	groupNameReqValue := []string{groupNameStr}
	groupNameReq, err := labels.NewRequirement(
		common.RayNodeGroupLabelKey,
		selection.Equals,
		groupNameReqValue)
	assert.Nil(t, err, "Fail to create requirement")
	headNameReqValue := []string{headGroupNameStr}
	headNameReq, err := labels.NewRequirement(
		common.RayNodeGroupLabelKey,
		selection.Equals,
		headNameReqValue)
	assert.Nil(t, err, "Fail to create requirement")
	headSelector = labels.NewSelector().Add(*headNameReq)
	workerSelector = labels.NewSelector().Add(*instanceReq).Add(*groupNameReq)
}

// TestReconcile_UnhealthyEvent tests the case where we have unhealthy events
// and we want to update the corresponding pods.
func TestReconcile_UnhealthyEvent(t *testing.T) {
	setupTest(t)

	testPodName := "eventPod"

	// testPodEventName is the name of the event that will be created for testPodName
	// The name of the event is generated by concatenating the pod name and a
	// meaningless random string
	testPodEventName := fmt.Sprintf("%s.15f0c0c5c5c5c5c5", testPodName)

	// add a pod in a different namespace
	newPods := []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testPodName,
				UID:       types.UID(testPodName),
				Namespace: "ns2",
				Labels: map[string]string{
					common.RayNodeLabelKey:      "yes",
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeGroupLabelKey: groupNameStr,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-worker",
						Image:   "rayproject/ray:2.2.0",
						Command: []string{"echo"},
						Args:    []string{"Hello Ray"},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "newPod2",
				UID:       types.UID("newPod2"),
				Namespace: namespaceStr,
				Labels: map[string]string{
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeGroupLabelKey: groupNameStr,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-worker",
						Image:   "rayproject/ray:2.2.0",
						Command: []string{"echo"},
						Args:    []string{"Hello Ray"},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testPodName,
				UID:       types.UID(testPodName),
				Namespace: namespaceStr,
				Labels: map[string]string{
					common.RayNodeLabelKey:      "yes",
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeGroupLabelKey: groupNameStr,
				},
				Annotations: map[string]string{
					common.RayFTEnabledAnnotationKey: "true",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-worker",
						Image:   "rayproject/ray:2.2.0",
						Command: []string{"echo"},
						Args:    []string{"Hello Ray"},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
	}

	testPods = append(testPods, newPods...)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
	ctx := context.Background()

	podList := corev1.PodList{}
	err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))

	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, len(testPods)-1, len(podList.Items), "Init pod list len is wrong")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	// add event for reconcile
	err = fakeClient.Create(ctx, &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testPodEventName,
			Namespace: namespaceStr,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Pod",
			Name:      testPodName,
			Namespace: namespaceStr,
		},
		Reason:  "Unhealthy",
		Type:    "Warning",
		Message: "Readiness probe failed",
	})
	assert.Nil(t, err, "Fail to create event")

	if _, err := testRayClusterReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespaceStr,
			Name:      testPodEventName,
		},
	}); err != nil {
		assert.Nil(t, err, "Fail to reconcile")
	}

	err = fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	assert.Nil(t, err, "Fail to get pod list")

	for _, pod := range podList.Items {
		if pod.Name == testPodName && pod.Namespace == namespaceStr {
			assert.Equal(t, pod.Annotations[common.RayNodeHealthStateAnnotationKey],
				common.PodUnhealthy, "Pod annotation is wrong")
		}
	}

	for _, pod := range podList.Items {
		assert.Equal(t, corev1.PodRunning, pod.Status.Phase, "Pod phase is wrong")
	}
}

func TestReconcile_RemoveWorkersToDelete_RandomDelete(t *testing.T) {
	setupTest(t)

	// TODO (kevin85421): The tests in this file are not independent. As a workaround,
	// I added the assertion to prevent the test logic from being affected by other changes.
	// However, we should refactor the tests in the future.

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup (2) The goal state of the workerGroup is 3 replicas. (3) ENABLE_RANDOM_POD_DELETE is set to true.
	defer os.Unsetenv(common.ENABLE_RANDOM_POD_DELETE)

	assert.Equal(t, 1, len(testRayCluster.Spec.WorkerGroupSpecs), "This test assumes only one worker group.")
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")

	// Pod random deletion is enabled in the following two cases:
	// Case 1: If Autoscaler is disabled, we will always enable random Pod deletion no matter the value of the feature flag.
	// Case 2: If Autoscaler is enabled, we will respect the value of the feature flag. If the feature flag environment variable
	// 		   is not set, we will disable random Pod deletion by default.
	// Here, we enable the Autoscaler and set the feature flag `ENABLE_RANDOM_POD_DELETE` to true to enable random Pod deletion.
	os.Setenv(common.ENABLE_RANDOM_POD_DELETE, "true")
	enableInTreeAutoscaling := true

	tests := map[string]struct {
		workersToDelete []string
		numRandomDelete int
	}{
		// The random Pod deletion (diff < 0) will delete Pod based on the index of the Pod list.
		// That is, it will firstly delete Pod1, then Pod2, then Pod3, then Pod4, then Pod5. Hence,
		// we need to test the different cases of the workersToDelete to make sure both Pod deletion
		// works as expected.
		"Set WorkersToDelete to pod1 and pod2.": {
			// The pod1 and pod2 will be deleted.
			workersToDelete: []string{"pod1", "pod2"},
			numRandomDelete: 0,
		},
		"Set WorkersToDelete to pod3 and pod4": {
			// The pod3 and pod4 will be deleted. If the random Pod deletion is triggered, it will firstly delete pod1 and make the test fail.
			workersToDelete: []string{"pod3", "pod4"},
			numRandomDelete: 0,
		},
		"Set WorkersToDelete to pod1 and pod5": {
			// The pod1 and pod5 will be deleted.
			workersToDelete: []string{"pod1", "pod5"},
			numRandomDelete: 0,
		},
		"Set WorkersToDelete to pod2 and NonExistentPod": {
			// The pod2 will be deleted, and 1 pod will be deleted randomly to meet `expectedNumWorkerPods`.
			workersToDelete: []string{"pod2", "NonExistentPod"},
			numRandomDelete: 1,
		},
		"Set WorkersToDelete to NonExistentPod1 and NonExistentPod1": {
			// Two Pods will be deleted randomly to meet `expectedNumWorkerPods`.
			workersToDelete: []string{"NonExistentPod1", "NonExistentPod2"},
			numRandomDelete: 2,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Initialize a fake client with newScheme and runtimeObjects.
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
			ctx := context.Background()
			podList := corev1.PodList{}
			err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
			assert.Nil(t, err, "Fail to get pod list")
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
			assert.Equal(t, expectedNumWorkersToDelete, len(testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete))

			testRayClusterReconciler := &RayClusterReconciler{
				Client:   fakeClient,
				Recorder: &record.FakeRecorder{},
				Scheme:   scheme.Scheme,
				Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
			}

			err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
			assert.Nil(t, err, "Fail to reconcile Pods")
			err = fakeClient.List(ctx, &podList, &client.ListOptions{
				LabelSelector: workerSelector,
				Namespace:     namespaceStr,
			})
			assert.Nil(t, err, "Fail to get pod list after reconcile")
			assert.Equal(t, expectedNumWorkerPods, len(podList.Items),
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
	defer os.Unsetenv(common.ENABLE_RANDOM_POD_DELETE)

	assert.Equal(t, 1, len(testRayCluster.Spec.WorkerGroupSpecs), "This test assumes only one worker group.")
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")

	// If Autoscaler is enabled, we will respect the value of the feature flag. If the feature flag environment variable
	// is not set, we will disable random Pod deletion by default. Hence, this test will disable random Pod deletion.
	// In this case, the cluster won't achieve the target state (i.e., `expectedNumWorkerPods` worker Pods) in one reconciliation.
	// Instead, the Ray Autoscaler will gradually scale down the cluster in subsequent reconciliations until it reaches the target state.
	os.Unsetenv(common.ENABLE_RANDOM_POD_DELETE)
	enableInTreeAutoscaling := true

	tests := map[string]struct {
		workersToDelete []string
		numNonExistPods int
	}{
		"Set WorkersToDelete to pod2 and pod3.": {
			// The pod2 and pod3 will be deleted. The number of remaining Pods will be 3.
			workersToDelete: []string{"pod2", "pod3"},
			numNonExistPods: 0,
		},
		"Set WorkersToDelete to pod2 and NonExistentPod": {
			// Only pod2 will be deleted. The number of remaining Pods will be 4.
			workersToDelete: []string{"pod2", "NonExistentPod"},
			numNonExistPods: 1,
		},
		"Set WorkersToDelete to NonExistentPod1 and NonExistentPod1": {
			// No Pod will be deleted. The number of remaining Pods will be 5.
			workersToDelete: []string{"NonExistentPod1", "NonExistentPod2"},
			numNonExistPods: 2,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Initialize a fake client with newScheme and runtimeObjects.
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
			ctx := context.Background()
			podList := corev1.PodList{}
			err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
			assert.Nil(t, err, "Fail to get pod list")
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
				Client:   fakeClient,
				Recorder: &record.FakeRecorder{},
				Scheme:   scheme.Scheme,
				Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
			}

			err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
			assert.Nil(t, err, "Fail to reconcile Pods")
			err = fakeClient.List(ctx, &podList, &client.ListOptions{
				LabelSelector: workerSelector,
				Namespace:     namespaceStr,
			})
			assert.Nil(t, err, "Fail to get pod list after reconcile")
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

	assert.Nil(t, err, "Fail to get pod list")

	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	assert.Nil(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})

	assert.Nil(t, err, "Fail to get pod list after reconcile")

	assert.Equal(t, int(localExpectReplicaNum), len(podList.Items),
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
	assert.Equal(t, 1, len(testRayCluster.Spec.WorkerGroupSpecs), "This test assumes only one worker group.")
	testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{}
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")

	// This test makes some assumptions about the testPods object.
	// `testPods` contains 6 pods, including 1 head pod and 5 worker pods.
	assert.Equal(t, 6, len(testPods), "This test assumes the testPods object contains 6 pods.")
	numHeadPods := 1
	oldNumWorkerPods := len(testPods) - numHeadPods

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
	ctx := context.Background()

	// Get the pod list from the fake client.
	podList := corev1.PodList{}
	err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, oldNumWorkerPods+numHeadPods, len(podList.Items), "Init pod list len is wrong")

	// Simulate the deletion of 2 worker Pods. After the deletion, the number of worker Pods should be 3.
	err = fakeClient.Delete(ctx, &podList.Items[3])
	assert.Nil(t, err, "Fail to delete pod")
	err = fakeClient.Delete(ctx, &podList.Items[4])
	assert.Nil(t, err, "Fail to delete pod")

	// Initialize a new RayClusterReconciler.
	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	// Since the desired state of the workerGroup is 3 replicas,
	// the controller will not create or delete any worker Pods.
	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	assert.Nil(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})
	assert.Nil(t, err, "Fail to get pod list after reconcile")
	assert.Equal(t, expectedNumWorkerPods, len(podList.Items),
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
	assert.Equal(t, 1, len(testRayCluster.Spec.WorkerGroupSpecs), "This test assumes only one worker group.")
	testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{}
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")
	testRayCluster.Spec.EnableInTreeAutoscaling = nil

	// This test makes some assumptions about the testPods object.
	// `testPods` contains 6 pods, including 1 head pod and 5 worker pods.
	assert.Equal(t, 6, len(testPods), "This test assumes the testPods object contains 6 pods.")
	numHeadPods := 1
	oldNumWorkerPods := len(testPods) - numHeadPods

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	// Get the pod list from the fake client.
	podList := corev1.PodList{}
	ctx := context.Background()
	err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, oldNumWorkerPods+numHeadPods, len(podList.Items), "Init pod list len is wrong")

	// Simulate the deletion of 2 worker Pods. After the deletion, the number of worker Pods should be 4.
	err = fakeClient.Delete(ctx, &podList.Items[3])
	assert.Nil(t, err, "Fail to delete pod")

	// Initialize a new RayClusterReconciler.
	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	// Since the desired state of the workerGroup is 3 replicas,
	// the controller will delete a worker Pod to reach the goal state.
	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	assert.Nil(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})
	assert.Nil(t, err, "Fail to get pod list after reconcile")

	// The number of worker Pods should be 3.
	assert.Equal(t, expectedNumWorkerPods, len(podList.Items),
		"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, len(podList.Items))
}

func TestReconcile_PodCrash_Diff0_WorkersToDelete_OK(t *testing.T) {
	setupTest(t)

	// TODO (kevin85421): The tests in this file are not independent. As a workaround,
	// I added the assertion to prevent the test logic from being affected by other changes.
	// However, we should refactor the tests in the future.

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup
	// (2) The goal state of the workerGroup is 3 replicas.
	// (3) The workersToDelete has 2 worker Pods (pod3 and pod4). => Simulate the autoscaler scale-down.
	assert.Equal(t, 1, len(testRayCluster.Spec.WorkerGroupSpecs), "This test assumes only one worker group.")
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")
	testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{"pod3", "pod4"}

	// This test makes some assumptions about the testPods object.
	// `testPods` contains 6 pods, including 1 head pod and 5 worker pods.
	assert.Equal(t, 6, len(testPods), "This test assumes the testPods object contains 6 pods.")
	numHeadPods := 1
	oldNumWorkerPods := len(testPods) - numHeadPods

	// Initialize a fake client with newScheme and runtimeObjects.
	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
	ctx := context.Background()

	// Get the pod list from the fake client.
	podList := corev1.PodList{}
	err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, oldNumWorkerPods+numHeadPods, len(podList.Items), "Init pod list len is wrong")

	// Simulate 2 Pod fails. Because the workersToDelete also contains pod3 and pod4, the controller will
	// delete these two Pods. After the deletion, the number of worker Pods should be 3.
	podList.Items[3].Status.Phase = corev1.PodFailed
	podList.Items[4].Status.Phase = corev1.PodFailed
	err = fakeClient.Update(ctx, &podList.Items[3])
	assert.Nil(t, err, "Fail to get update pod status")
	err = fakeClient.Update(ctx, &podList.Items[4])
	assert.Nil(t, err, "Fail to get update pod status")

	// Initialize a new RayClusterReconciler.
	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	// Since the desired state of the workerGroup is 3 replicas, the controller
	// will not create or delete any worker Pods.
	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	assert.Nil(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})
	assert.Nil(t, err, "Fail to get pod list after reconcile")

	// Failed Pods (pod3, pod4) should be deleted because of the workersToDelete.
	// Hence, no failed Pods should exist in `podList`.
	assert.Equal(t, expectedNumWorkerPods, len(podList.Items))
	assert.Equal(t, expectedNumWorkerPods, getNotFailedPodItemNum(podList),
		"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, getNotFailedPodItemNum(podList))
}

func TestReconcile_PodCrash_DiffLess0_OK(t *testing.T) {
	setupTest(t)

	defer os.Unsetenv(common.ENABLE_RANDOM_POD_DELETE)
	// TODO (kevin85421): The tests in this file are not independent. As a workaround,
	// I added the assertion to prevent the test logic from being affected by other changes.
	// However, we should refactor the tests in the future.

	// This test makes some assumptions about the testRayCluster object.
	// (1) 1 workerGroup
	// (2) The goal state of the workerGroup is 3 replicas.
	// (3) The workersToDelete has 1 worker Pod (pod3). => Simulate the autoscaler scale-down.
	assert.Equal(t, 1, len(testRayCluster.Spec.WorkerGroupSpecs), "This test assumes only one worker group.")
	expectedNumWorkerPods := int(*testRayCluster.Spec.WorkerGroupSpecs[0].Replicas)
	assert.Equal(t, 3, expectedNumWorkerPods, "This test assumes the expected number of worker pods is 3.")
	testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{"pod3"}
	enableInTreeAutoscaling := true
	testRayCluster.Spec.EnableInTreeAutoscaling = &enableInTreeAutoscaling

	// This test makes some assumptions about the testPods object.
	// `testPods` contains 6 pods, including 1 head pod and 5 worker pods.
	assert.Equal(t, 6, len(testPods), "This test assumes the testPods object contains 6 pods.")
	numHeadPods := 1
	oldNumWorkerPods := len(testPods) - numHeadPods

	tests := map[string]struct {
		ENABLE_RANDOM_POD_DELETE bool
	}{
		// When Autoscaler is enabled, the random Pod deletion is controleld by the feature flag `ENABLE_RANDOM_POD_DELETE`.
		"Enable random Pod deletion": {
			ENABLE_RANDOM_POD_DELETE: true,
		},
		"Disable random Pod deletion": {
			ENABLE_RANDOM_POD_DELETE: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Initialize a fake client with newScheme and runtimeObjects.
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
			ctx := context.Background()

			// Get the pod list from the fake client.
			podList := corev1.PodList{}
			err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))
			assert.Nil(t, err, "Fail to get pod list")
			assert.Equal(t, oldNumWorkerPods+numHeadPods, len(podList.Items), "Init pod list len is wrong")

			// Simulate 1 Pod fails. Because the workersToDelete also contains pod3, the controller will
			// delete pod3. After the deletion, the number of worker Pods should be 4.
			podList.Items[3].Status.Phase = corev1.PodFailed
			err = fakeClient.Update(ctx, &podList.Items[3])
			assert.Nil(t, err, "Fail to get update pod status")

			// Initialize a new RayClusterReconciler.
			testRayClusterReconciler := &RayClusterReconciler{
				Client:   fakeClient,
				Recorder: &record.FakeRecorder{},
				Scheme:   scheme.Scheme,
				Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
			}

			if tc.ENABLE_RANDOM_POD_DELETE {
				os.Setenv(common.ENABLE_RANDOM_POD_DELETE, "true")
			} else {
				os.Setenv(common.ENABLE_RANDOM_POD_DELETE, "false")
			}
			cluster := testRayCluster.DeepCopy()
			// Case 1: ENABLE_RANDOM_POD_DELETE is true.
			// 	Since the desired state of the workerGroup is 3 replicas, the controller will delete a worker Pod randomly.
			//  After the deletion, the number of worker Pods should be 3.
			// Case 2: ENABLE_RANDOM_POD_DELETE is false.
			//  Only the Pod in the `workersToDelete` will be deleted. After the deletion, the number of worker Pods should be 4.
			err = testRayClusterReconciler.reconcilePods(ctx, cluster)
			assert.Nil(t, err, "Fail to reconcile Pods")

			err = fakeClient.List(ctx, &podList, &client.ListOptions{
				LabelSelector: workerSelector,
				Namespace:     namespaceStr,
			})
			assert.Nil(t, err, "Fail to get pod list after reconcile")

			if tc.ENABLE_RANDOM_POD_DELETE {
				// Case 1: ENABLE_RANDOM_POD_DELETE is true.
				assert.Equal(t, expectedNumWorkerPods, len(podList.Items))
				assert.Equal(t, expectedNumWorkerPods, getNotFailedPodItemNum(podList),
					"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, getNotFailedPodItemNum(podList))
			} else {
				// Case 2: ENABLE_RANDOM_POD_DELETE is false.
				assert.Equal(t, expectedNumWorkerPods+1, len(podList.Items))
				assert.Equal(t, expectedNumWorkerPods+1, getNotFailedPodItemNum(podList),
					"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, getNotFailedPodItemNum(podList))
			}
		})
	}
}

func TestReconcile_PodEvicted_DiffLess0_OK(t *testing.T) {
	setupTest(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()
	ctx := context.Background()

	podList := corev1.PodList{}
	err := fakeClient.List(ctx, &podList, client.InNamespace(namespaceStr))

	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

	// Simulate head pod get evicted.
	podList.Items[0].Status.Phase = corev1.PodFailed
	podList.Items[0].Status.Reason = "Evicted"
	err = fakeClient.Update(ctx, &podList.Items[0])
	assert.Nil(t, err, "Fail to get update pod status")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcilePods(ctx, testRayCluster)
	assert.Nil(t, err, "Fail to reconcile Pods")

	// Filter head pod
	err = fakeClient.List(ctx, &podList, &client.ListOptions{
		LabelSelector: headSelector,
		Namespace:     namespaceStr,
	})

	assert.Nil(t, err, "Fail to get pod list after reconcile")
	assert.Equal(t, 0, len(podList.Items),
		"Evicted head should be deleted after reconcile expect %d actual %d", 0, len(podList.Items))
}

func TestReconcileHeadService(t *testing.T) {
	setupTest(t)

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1alpha1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	cluster := testRayCluster.DeepCopy()
	headService1 := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "head-svc-1",
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				common.RayClusterLabelKey:  cluster.Name,
				common.RayNodeTypeLabelKey: string(rayv1alpha1.HeadNode),
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
		common.RayClusterLabelKey:  cluster.Name,
		common.RayNodeTypeLabelKey: string(rayv1alpha1.HeadNode),
	})

	// Initialize RayCluster reconciler.
	r := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	// Case 1: Head service does not exist.
	err := r.reconcileHeadService(ctx, cluster)
	assert.Nil(t, err, "Fail to reconcile head service")

	// One head service should be created.
	serviceList := corev1.ServiceList{}
	err = fakeClient.List(ctx, &serviceList, &client.ListOptions{
		LabelSelector: headServiceSelector,
		Namespace:     cluster.Namespace,
	})
	assert.Nil(t, err, "Fail to get service list")
	assert.Equal(t, 1, len(serviceList.Items), "Service list len is wrong")

	// Case 2: One head service exists.
	err = r.reconcileHeadService(ctx, cluster)
	assert.Nil(t, err, "Fail to reconcile head service")

	// The namespace should still have only one head service.
	serviceList = corev1.ServiceList{}
	err = fakeClient.List(ctx, &serviceList, &client.ListOptions{
		LabelSelector: headServiceSelector,
		Namespace:     cluster.Namespace,
	})
	assert.Nil(t, err, "Fail to get service list")
	assert.Equal(t, 1, len(serviceList.Items), "Service list len is wrong")

	// Case 3: Two head services exist. This case only happens when users manually create a head service.
	runtimeObjects = []runtime.Object{headService1, headService2}
	fakeClient = clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	serviceList = corev1.ServiceList{}
	err = fakeClient.List(ctx, &serviceList, &client.ListOptions{
		LabelSelector: headServiceSelector,
		Namespace:     cluster.Namespace,
	})
	assert.Nil(t, err, "Fail to get service list")
	assert.Equal(t, 2, len(serviceList.Items), "Service list len is wrong")
	r.Client = fakeClient

	// When there are two head services, the reconciler should report an error.
	err = r.reconcileHeadService(ctx, cluster)
	assert.NotNil(t, err, "Reconciler should report an error when there are two head services")
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
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcileAutoscalerServiceAccount(ctx, testRayCluster)
	assert.Nil(t, err, "Fail to reconcile autoscaler ServiceAccount")

	err = fakeClient.Get(ctx, saNamespacedName, &sa)

	assert.Nil(t, err, "Fail to get head group ServiceAccount after reconciliation")
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
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	// If users specify ServiceAccountName for the head Pod, they need to create a ServiceAccount themselves.
	// However, if KubeRay creates a ServiceAccount for users, the autoscaler may encounter permission issues during
	// zero-downtime rolling updates when RayService is performed. See https://github.com/ray-project/kuberay/pull/1128
	// for more details.
	err := testRayClusterReconciler.reconcileAutoscalerServiceAccount(ctx, cluster)
	assert.NotNil(t, err,
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
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcileAutoscalerServiceAccount(ctx, cluster)
	assert.Nil(t, err)
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
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcileAutoscalerRoleBinding(ctx, testRayCluster)
	assert.Nil(t, err, "Fail to reconcile autoscaler RoleBinding")

	err = fakeClient.Get(ctx, rbNamespacedName, &rb)

	assert.Nil(t, err, "Fail to get autoscaler RoleBinding after reconciliation")
}

func TestReconcile_UpdateClusterReason(t *testing.T) {
	setupTest(t)

	newScheme := runtime.NewScheme()
	_ = rayv1alpha1.AddToScheme(newScheme)

	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(testRayCluster).Build()
	ctx := context.Background()

	namespacedName := types.NamespacedName{
		Name:      instanceName,
		Namespace: namespaceStr,
	}
	cluster := rayv1alpha1.RayCluster{}
	err := fakeClient.Get(ctx, namespacedName, &cluster)
	assert.Nil(t, err, "Fail to get RayCluster")
	assert.Empty(t, cluster.Status.Reason, "Cluster reason should be empty")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}
	reason := "test reason"

	err = testRayClusterReconciler.updateClusterReason(ctx, testRayCluster, reason)
	assert.Nil(t, err, "Fail to update cluster reason")

	err = fakeClient.Get(ctx, namespacedName, &cluster)
	assert.Nil(t, err, "Fail to get RayCluster after updating reason")
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
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	if err := testRayClusterReconciler.updateEndpoints(ctx, testRayCluster); err != nil {
		t.Errorf("updateEndpoints failed: %v", err)
	}

	expected := map[string]string{
		"client":    "10001",
		"dashboard": "8265",
		"metrics":   "8080",
		"redis":     "6379",
		"serve":     "8000",
	}
	assert.Equal(t, expected, testRayCluster.Status.Endpoints, "RayCluster status endpoints not updated")
}

func TestGetHeadPodIP(t *testing.T) {
	setupTest(t)

	extraHeadPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unexpectedExtraHeadNode",
			Namespace: namespaceStr,
			Labels: map[string]string{
				common.RayClusterLabelKey:   instanceName,
				common.RayNodeTypeLabelKey:  string(rayv1alpha1.HeadNode),
				common.RayNodeGroupLabelKey: headGroupNameStr,
			},
		},
	}

	tests := map[string]struct {
		pods         []runtime.Object
		expectedIP   string
		returnsError bool
	}{
		"get expected Pod IP if there's one head node": {
			pods:         testPods,
			expectedIP:   headNodeIP,
			returnsError: false,
		},
		"no error if there's no head node": {
			pods:         []runtime.Object{},
			expectedIP:   "",
			returnsError: false,
		},
		"no error if there's more than one head node": {
			pods:         append(testPods, extraHeadPod),
			expectedIP:   "",
			returnsError: false,
		},
		"no error if head pod ip is not yet set": {
			pods:         testPodsNoHeadIP,
			expectedIP:   "",
			returnsError: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(tc.pods...).Build()

			testRayClusterReconciler := &RayClusterReconciler{
				Client:   fakeClient,
				Recorder: &record.FakeRecorder{},
				Scheme:   scheme.Scheme,
				Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
			}

			ip, err := testRayClusterReconciler.getHeadPodIP(context.TODO(), testRayCluster)

			if tc.returnsError {
				assert.NotNil(t, err, "getHeadPodIP should return error")
			} else {
				assert.Nil(t, err, "getHeadPodIP should not return error")
			}

			assert.Equal(t, tc.expectedIP, ip, "getHeadPodIP returned unexpected IP")
		})
	}
}

func TestGetHeadServiceIP(t *testing.T) {
	setupTest(t)

	headServiceIP := "1.2.3.4"
	headService, err := common.BuildServiceForHeadPod(*testRayCluster, nil, nil)
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

	tests := map[string]struct {
		services     []runtime.Object
		expectedIP   string
		returnsError bool
	}{
		"get expected Service IP if there's one head Service": {
			services:     testServices,
			expectedIP:   headServiceIP,
			returnsError: false,
		},
		"get error if there's no head Service": {
			services:     []runtime.Object{},
			expectedIP:   "",
			returnsError: true,
		},
		"get error if there's more than one head Service": {
			services:     append(testServices, extraHeadService),
			expectedIP:   "",
			returnsError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(tc.services...).Build()

			testRayClusterReconciler := &RayClusterReconciler{
				Client:   fakeClient,
				Recorder: &record.FakeRecorder{},
				Scheme:   scheme.Scheme,
				Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
			}

			ip, err := testRayClusterReconciler.getHeadServiceIP(context.TODO(), testRayCluster)

			if tc.returnsError {
				assert.NotNil(t, err, "getHeadServiceIP should return error")
			} else {
				assert.Nil(t, err, "getHeadServiceIP should not return error")
			}

			assert.Equal(t, tc.expectedIP, ip, "getHeadServiceIP returned unexpected IP")
		})
	}
}

func TestUpdateStatusObservedGeneration(t *testing.T) {
	setupTest(t)

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1alpha1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// To update the status of RayCluster with `r.Status().Update()`,
	// initialize the runtimeObjects with appropriate context. In KubeRay, the `ClusterIP`
	// and `TargetPort` fields are typically set by the cluster's control plane.
	headService, err := common.BuildServiceForHeadPod(*testRayCluster, nil, nil)
	assert.Nil(t, err, "Failed to build head service.")
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
	cluster := rayv1alpha1.RayCluster{}
	err = fakeClient.Get(ctx, namespacedName, &cluster)
	assert.Nil(t, err, "Fail to get RayCluster")
	assert.Equal(t, int64(-1), cluster.Status.ObservedGeneration)
	assert.Equal(t, int64(0), cluster.ObjectMeta.Generation)

	// Initialize RayCluster reconciler.
	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	// Compare the values of `Generation` and `ObservedGeneration` to check if they match.
	newInstance, err := testRayClusterReconciler.calculateStatus(ctx, testRayCluster)
	assert.Nil(t, err)
	err = fakeClient.Get(ctx, namespacedName, &cluster)
	assert.Nil(t, err)
	assert.Equal(t, cluster.ObjectMeta.Generation, newInstance.Status.ObservedGeneration)
}

func TestReconcile_UpdateClusterState(t *testing.T) {
	setupTest(t)

	newScheme := runtime.NewScheme()
	_ = rayv1alpha1.AddToScheme(newScheme)

	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(testRayCluster).Build()
	ctx := context.Background()

	namespacedName := types.NamespacedName{
		Name:      instanceName,
		Namespace: namespaceStr,
	}
	cluster := rayv1alpha1.RayCluster{}
	err := fakeClient.Get(ctx, namespacedName, &cluster)
	assert.Nil(t, err, "Fail to get RayCluster")
	assert.Empty(t, cluster.Status.State, "Cluster state should be empty")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	state := rayv1alpha1.Ready
	err = testRayClusterReconciler.updateClusterState(ctx, testRayCluster, state)
	assert.Nil(t, err, "Fail to update cluster state")

	err = fakeClient.Get(ctx, namespacedName, &cluster)
	assert.Nil(t, err, "Fail to get RayCluster after updating state")
	assert.Equal(t, cluster.Status.State, state, "Cluster state should be updated")
}

func TestInconsistentRayClusterStatus(t *testing.T) {
	newScheme := runtime.NewScheme()
	_ = rayv1alpha1.AddToScheme(newScheme)
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects().Build()
	r := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	// Mock data
	timeNow := metav1.Now()
	oldStatus := rayv1alpha1.RayClusterStatus{
		State:                   rayv1alpha1.Ready,
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
		Head: rayv1alpha1.HeadInfo{
			PodIP:     "10.244.0.6",
			ServiceIP: "10.96.140.249",
		},
		ObservedGeneration: 1,
		Reason:             "test reason",
	}

	// `inconsistentRayClusterStatus` is used to check whether the old and new RayClusterStatus are inconsistent
	// by comparing different fields. If the only differences between the old and new status are the `LastUpdateTime`
	// and `ObservedGeneration` fields (Case 9 and Case 10), the status update will not be triggered.

	// Case 1: `State` is different => return true
	newStatus := oldStatus.DeepCopy()
	newStatus.State = rayv1alpha1.Failed
	assert.True(t, r.inconsistentRayClusterStatus(oldStatus, *newStatus))

	// Case 2: `Reason` is different => return true
	newStatus = oldStatus.DeepCopy()
	newStatus.Reason = "new reason"
	assert.True(t, r.inconsistentRayClusterStatus(oldStatus, *newStatus))

	// Case 3: `AvailableWorkerReplicas` is different => return true
	newStatus = oldStatus.DeepCopy()
	newStatus.AvailableWorkerReplicas = oldStatus.AvailableWorkerReplicas + 1
	assert.True(t, r.inconsistentRayClusterStatus(oldStatus, *newStatus))

	// Case 4: `DesiredWorkerReplicas` is different => return true
	newStatus = oldStatus.DeepCopy()
	newStatus.DesiredWorkerReplicas = oldStatus.DesiredWorkerReplicas + 1
	assert.True(t, r.inconsistentRayClusterStatus(oldStatus, *newStatus))

	// Case 5: `MinWorkerReplicas` is different => return true
	newStatus = oldStatus.DeepCopy()
	newStatus.MinWorkerReplicas = oldStatus.MinWorkerReplicas + 1
	assert.True(t, r.inconsistentRayClusterStatus(oldStatus, *newStatus))

	// Case 6: `MaxWorkerReplicas` is different => return true
	newStatus = oldStatus.DeepCopy()
	newStatus.MaxWorkerReplicas = oldStatus.MaxWorkerReplicas + 1
	assert.True(t, r.inconsistentRayClusterStatus(oldStatus, *newStatus))

	// Case 7: `Endpoints` is different => return true
	newStatus = oldStatus.DeepCopy()
	newStatus.Endpoints["fakeEndpoint"] = "10009"
	assert.True(t, r.inconsistentRayClusterStatus(oldStatus, *newStatus))

	// Case 8: `Head` is different => return true
	newStatus = oldStatus.DeepCopy()
	newStatus.Head.PodIP = "test head pod ip"
	assert.True(t, r.inconsistentRayClusterStatus(oldStatus, *newStatus))

	// Case 9: `LastUpdateTime` is different => return false
	newStatus = oldStatus.DeepCopy()
	newStatus.LastUpdateTime = &metav1.Time{Time: timeNow.Add(time.Hour)}
	assert.False(t, r.inconsistentRayClusterStatus(oldStatus, *newStatus))

	// Case 10: `ObservedGeneration` is different => return false
	newStatus = oldStatus.DeepCopy()
	newStatus.ObservedGeneration = oldStatus.ObservedGeneration + 1
	assert.False(t, r.inconsistentRayClusterStatus(oldStatus, *newStatus))
}

func TestCalculateStatus(t *testing.T) {
	setupTest(t)

	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1alpha1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	headServiceIP := "aaa.bbb.ccc.ddd"
	headService, err := common.BuildServiceForHeadPod(*testRayCluster, nil, nil)
	assert.Nil(t, err, "Failed to build head service.")
	headService.Spec.ClusterIP = headServiceIP
	headPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headNode",
			Namespace: namespaceStr,
			Labels: map[string]string{
				common.RayClusterLabelKey:  instanceName,
				common.RayNodeTypeLabelKey: string(rayv1alpha1.HeadNode),
			},
		},
		Status: corev1.PodStatus{
			PodIP: headNodeIP,
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
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	// Test head information
	newInstance, err := r.calculateStatus(ctx, testRayCluster)
	assert.Nil(t, err)
	assert.Equal(t, headNodeIP, newInstance.Status.Head.PodIP)
	assert.Equal(t, headServiceIP, newInstance.Status.Head.ServiceIP)
}
