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
	"testing"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"

	. "github.com/onsi/ginkgo"
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
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
	testRayCluster          *rayiov1alpha1.RayCluster
	headSelector            labels.Selector
	headNodeIP              string
	testServices            []runtime.Object
	workerSelector          labels.Selector
	workersToDelete         []string
)

func setupTest(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	// To verify the failure logic, you can change this PrioritizeWorkersToDelete to false.
	PrioritizeWorkersToDelete = true

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
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeTypeLabelKey:  string(rayiov1alpha1.HeadNode),
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
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeGroupLabelKey: groupNameStr,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-worker",
						Image:   "rayproject/ray:2.3.0",
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
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeGroupLabelKey: groupNameStr,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "ray-worker",
						Image:   "rayproject/ray:2.3.0",
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
					common.RayClusterLabelKey:   instanceName,
					common.RayNodeTypeLabelKey:  string(rayiov1alpha1.HeadNode),
					common.RayNodeGroupLabelKey: headGroupNameStr,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				PodIP: "",
			},
		},
	}
	testRayCluster = &rayiov1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: namespaceStr,
		},
		Spec: rayiov1alpha1.RayClusterSpec{
			RayVersion:              "1.0",
			EnableInTreeAutoscaling: &enableInTreeAutoscaling,
			HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
				ServiceType: "ClusterIP",
				Replicas:    pointer.Int32Ptr(1),
				RayStartParams: map[string]string{
					"port":                "6379",
					"object-manager-port": "12345",
					"node-manager-port":   "12346",
					"object-store-memory": "100000000",
					"num-cpus":            "1",
				},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: headGroupServiceAccount,
						Containers: []corev1.Container{
							{
								Name:    "ray-head",
								Image:   "rayproject/ray:2.3.0",
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
			WorkerGroupSpecs: []rayiov1alpha1.WorkerGroupSpec{
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
									Image:   "rayproject/ray:2.3.0",
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
					ScaleStrategy: rayiov1alpha1.ScaleStrategy{
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
	dashboardService, err := common.BuildDashboardService(*testRayCluster)
	if err != nil {
		t.Errorf("failed to build dashboard service: %v", err)
	}
	for i, port := range dashboardService.Spec.Ports {
		headService.Spec.Ports[i].TargetPort = intstr.IntOrString{IntVal: port.Port}
	}
	testServices = []runtime.Object{
		headService,
		dashboardService,
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

func tearDown(t *testing.T) {
	PrioritizeWorkersToDelete = false
}

func TestReconcile_RemoveWorkersToDelete_OK(t *testing.T) {
	setupTest(t)
	defer tearDown(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	podList := corev1.PodList{}
	err := fakeClient.List(context.Background(), &podList, client.InNamespace(namespaceStr))

	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcilePods(testRayCluster)
	assert.Nil(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(context.Background(), &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})

	assert.Nil(t, err, "Fail to get pod list after reconcile")

	assert.Equal(t, int(expectReplicaNum), len(podList.Items),
		"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, len(podList.Items))
}

func TestReconcile_RandomDelete_OK(t *testing.T) {
	setupTest(t)
	defer tearDown(t)

	var localExpectReplicaNum int32 = 2
	testRayCluster.Spec.WorkerGroupSpecs[0].Replicas = &localExpectReplicaNum

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	podList := corev1.PodList{}
	err := fakeClient.List(context.Background(), &podList, client.InNamespace(namespaceStr))

	assert.Nil(t, err, "Fail to get pod list")

	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcilePods(testRayCluster)
	assert.Nil(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(context.Background(), &podList, &client.ListOptions{
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
	defer tearDown(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	podList := corev1.PodList{}
	err := fakeClient.List(context.Background(), &podList, client.InNamespace(namespaceStr))

	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

	// Simulate 2 pod container got deleted.
	err = fakeClient.Delete(context.Background(), &podList.Items[3])
	assert.Nil(t, err, "Fail to delete pod")
	err = fakeClient.Delete(context.Background(), &podList.Items[4])
	assert.Nil(t, err, "Fail to delete pod")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcilePods(testRayCluster)
	assert.Nil(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(context.Background(), &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})

	assert.Nil(t, err, "Fail to get pod list after reconcile")

	assert.Equal(t, int(expectReplicaNum), len(podList.Items),
		"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, len(podList.Items))

	for i := 0; i < len(podList.Items); i++ {
		if contains(workersToDelete, podList.Items[i].Name) {
			t.Errorf("WorkersToDelete is not actually deleted, %s", podList.Items[i].Name)
		}
	}
}

func TestReconcile_PodDeleted_DiffLess0_OK(t *testing.T) {
	setupTest(t)
	defer tearDown(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	podList := corev1.PodList{}
	err := fakeClient.List(context.Background(), &podList, client.InNamespace(namespaceStr))

	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

	// Simulate 1 pod container got deleted.
	err = fakeClient.Delete(context.Background(), &podList.Items[3])
	assert.Nil(t, err, "Fail to delete pod")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcilePods(testRayCluster)
	assert.Nil(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(context.Background(), &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})

	assert.Nil(t, err, "Fail to get pod list after reconcile")

	assert.Equal(t, int(expectReplicaNum), len(podList.Items),
		"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, len(podList.Items))

	for i := 0; i < len(podList.Items); i++ {
		if contains(workersToDelete, podList.Items[i].Name) {
			t.Errorf("WorkersToDelete is not actually deleted, %s", podList.Items[i].Name)
		}
	}
}

func TestReconcile_PodDCrash_Diff0_OK(t *testing.T) {
	setupTest(t)
	defer tearDown(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	podList := corev1.PodList{}
	err := fakeClient.List(context.Background(), &podList, client.InNamespace(namespaceStr))

	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

	// Simulate 2 pod container crash.
	podList.Items[3].Status.Phase = corev1.PodFailed
	podList.Items[4].Status.Phase = corev1.PodFailed
	err = fakeClient.Update(context.Background(), &podList.Items[3])
	assert.Nil(t, err, "Fail to get update pod status")
	err = fakeClient.Update(context.Background(), &podList.Items[4])
	assert.Nil(t, err, "Fail to get update pod status")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcilePods(testRayCluster)
	assert.Nil(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(context.Background(), &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})

	assert.Nil(t, err, "Fail to get pod list after reconcile")

	assert.Equal(t, int(expectReplicaNum), getNotFailedPodItemNum(podList),
		"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, getNotFailedPodItemNum(podList))

	for i := 0; i < len(podList.Items); i++ {
		if contains(workersToDelete, podList.Items[i].Name) {
			t.Errorf("WorkersToDelete is not actually deleted, %s", podList.Items[i].Name)
		}
	}
}

func TestReconcile_PodDCrash_DiffLess0_OK(t *testing.T) {
	setupTest(t)
	defer tearDown(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	podList := corev1.PodList{}
	err := fakeClient.List(context.Background(), &podList, client.InNamespace(namespaceStr))

	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

	// Simulate 1 pod container crash.
	podList.Items[3].Status.Phase = corev1.PodFailed
	err = fakeClient.Update(context.Background(), &podList.Items[3])
	assert.Nil(t, err, "Fail to get update pod status")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcilePods(testRayCluster)
	assert.Nil(t, err, "Fail to reconcile Pods")

	err = fakeClient.List(context.Background(), &podList, &client.ListOptions{
		LabelSelector: workerSelector,
		Namespace:     namespaceStr,
	})

	assert.Nil(t, err, "Fail to get pod list after reconcile")

	assert.Equal(t, int(expectReplicaNum), getNotFailedPodItemNum(podList),
		"Replica number is wrong after reconcile expect %d actual %d", expectReplicaNum, getNotFailedPodItemNum(podList))

	for i := 0; i < len(podList.Items); i++ {
		if contains(workersToDelete, podList.Items[i].Name) {
			t.Errorf("WorkersToDelete is not actually deleted, %s", podList.Items[i].Name)
		}
	}
}

func TestReconcile_PodEvicted_DiffLess0_OK(t *testing.T) {
	setupTest(t)
	defer tearDown(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	podList := corev1.PodList{}
	err := fakeClient.List(context.Background(), &podList, client.InNamespace(namespaceStr))

	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

	// Simulate head pod get evicted.
	podList.Items[0].Status.Phase = corev1.PodFailed
	podList.Items[0].Status.Reason = "Evicted"
	err = fakeClient.Update(context.Background(), &podList.Items[0])
	assert.Nil(t, err, "Fail to get update pod status")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcilePods(testRayCluster)
	assert.Nil(t, err, "Fail to reconcile Pods")

	// Filter head pod
	err = fakeClient.List(context.Background(), &podList, &client.ListOptions{
		LabelSelector: headSelector,
		Namespace:     namespaceStr,
	})

	assert.Nil(t, err, "Fail to get pod list after reconcile")
	assert.Equal(t, 0, len(podList.Items),
		"Evicted head should be deleted after reconcile expect %d actual %d", 0, len(podList.Items))
}

func TestReconcile_UpdateLocalWorkersToDelete_OK(t *testing.T) {
	setupTest(t)
	defer tearDown(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	podList := corev1.PodList{}
	err := fakeClient.List(context.Background(), &podList, client.InNamespace(namespaceStr))

	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

	// Simulate 1 pod container crash.
	podList.Items[1].Status.Phase = corev1.PodFailed
	runningPodsItems := append(podList.Items[:1], podList.Items[2:]...)

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	testWorker := testRayCluster.Spec.WorkerGroupSpecs[0]

	// this should remove `pod1` from testWorker.ScaleStrategy.WorkersToDelete
	testRayClusterReconciler.updateLocalWorkersToDelete(&testWorker, runningPodsItems)

	assert.Len(t, testWorker.ScaleStrategy.WorkersToDelete, 1, "updateLocalWorkersToDelete does not update WorkersToDelete properly")
	assert.Equal(t, testWorker.ScaleStrategy.WorkersToDelete[0], "pod2")
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
	defer tearDown(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	saNamespacedName := types.NamespacedName{
		Name:      headGroupServiceAccount,
		Namespace: namespaceStr,
	}
	sa := corev1.ServiceAccount{}
	err := fakeClient.Get(context.Background(), saNamespacedName, &sa)

	assert.True(t, k8serrors.IsNotFound(err), "Head group service account should not exist yet")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcileAutoscalerServiceAccount(testRayCluster)
	assert.Nil(t, err, "Fail to reconcile autoscaler ServiceAccount")

	err = fakeClient.Get(context.Background(), saNamespacedName, &sa)

	assert.Nil(t, err, "Fail to get head group ServiceAccount after reconciliation")
}

func TestReconcile_AutoscalerRoleBinding(t *testing.T) {
	setupTest(t)
	defer tearDown(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	rbNamespacedName := types.NamespacedName{
		Name:      instanceName,
		Namespace: namespaceStr,
	}
	rb := rbacv1.RoleBinding{}
	err := fakeClient.Get(context.Background(), rbNamespacedName, &rb)

	assert.True(t, k8serrors.IsNotFound(err), "autoscaler RoleBinding should not exist yet")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	err = testRayClusterReconciler.reconcileAutoscalerRoleBinding(testRayCluster)
	assert.Nil(t, err, "Fail to reconcile autoscaler RoleBinding")

	err = fakeClient.Get(context.Background(), rbNamespacedName, &rb)

	assert.Nil(t, err, "Fail to get autoscaler RoleBinding after reconciliation")
}

func TestReconcile_UpdateClusterReason(t *testing.T) {
	setupTest(t)
	defer tearDown(t)
	newScheme := runtime.NewScheme()
	_ = rayiov1alpha1.AddToScheme(newScheme)

	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(testRayCluster).Build()

	namespacedName := types.NamespacedName{
		Name:      instanceName,
		Namespace: namespaceStr,
	}
	cluster := rayiov1alpha1.RayCluster{}
	err := fakeClient.Get(context.Background(), namespacedName, &cluster)
	assert.Nil(t, err, "Fail to get RayCluster")
	assert.Empty(t, cluster.Status.Reason, "Cluster reason should be empty")

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}
	reason := "test reason"

	err = testRayClusterReconciler.updateClusterReason(testRayCluster, reason)
	assert.Nil(t, err, "Fail to update cluster reason")

	err = fakeClient.Get(context.Background(), namespacedName, &cluster)
	assert.Nil(t, err, "Fail to get RayCluster after updating reason")
	assert.Equal(t, cluster.Status.Reason, reason, "Cluster reason should be updated")
}

func TestUpdateEndpoints(t *testing.T) {
	setupTest(t)
	defer tearDown(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testServices...).Build()

	testRayClusterReconciler := &RayClusterReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayCluster"),
	}

	if err := testRayClusterReconciler.updateEndpoints(testRayCluster); err != nil {
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
	defer tearDown(t)

	extraHeadPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unexpectedExtraHeadNode",
			Namespace: namespaceStr,
			Labels: map[string]string{
				common.RayClusterLabelKey:   instanceName,
				common.RayNodeTypeLabelKey:  string(rayiov1alpha1.HeadNode),
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

			ip, err := testRayClusterReconciler.getHeadPodIP(testRayCluster)

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
	defer tearDown(t)

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

			ip, err := testRayClusterReconciler.getHeadServiceIP(testRayCluster)

			if tc.returnsError {
				assert.NotNil(t, err, "getHeadServiceIP should return error")
			} else {
				assert.Nil(t, err, "getHeadServiceIP should not return error")
			}

			assert.Equal(t, tc.expectedIP, ip, "getHeadServiceIP returned unexpected IP")
		})
	}
}
