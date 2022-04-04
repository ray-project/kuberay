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

package controllers

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo"
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/common"
	"github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/scheme"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// +kubebuilder:scaffold:imports
)

var (
	namespaceStr     string
	instanceName     string
	headGroupNameStr string
	groupNameStr     string
	expectReplicaNum int32
	testPods         []runtime.Object
	testRayCluster   *rayiov1alpha1.RayCluster
	workerSelector   labels.Selector
)

func setupTest(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	namespaceStr = "default"
	instanceName = "raycluster-sample"
	headGroupNameStr = "head-group"
	groupNameStr = "small-group"
	expectReplicaNum = 3
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
			Status: corev1.PodStatus{
				Phase: v1.PodRunning,
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
			Status: corev1.PodStatus{
				Phase: v1.PodRunning,
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
			Status: corev1.PodStatus{
				Phase: v1.PodRunning,
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
			Status: corev1.PodStatus{
				Phase: v1.PodRunning,
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
			Status: corev1.PodStatus{
				Phase: v1.PodRunning,
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
			Status: corev1.PodStatus{
				Phase: v1.PodRunning,
			},
		},
	}
	testRayCluster = &rayiov1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: namespaceStr,
		},
		Spec: rayiov1alpha1.RayClusterSpec{
			RayVersion: "1.0",
			HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
				ServiceType: "ClusterIP",
				Replicas:    pointer.Int32Ptr(1),
				RayStartParams: map[string]string{
					"port":                "6379",
					"object-manager-port": "12345",
					"node-manager-port":   "12346",
					"object-store-memory": "100000000",
					"redis-password":      "LetMeInRay",
					"num-cpus":            "1",
				},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:    "ray-head",
								Image:   "rayproject/autoscaler",
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
						"port":           "6379",
						"redis-password": "LetMeInRay",
						"num-cpus":       "1",
					},
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:    "ray-worker",
									Image:   "rayproject/autoscaler",
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
						WorkersToDelete: []string{
							"pod1",
							"pod2",
						},
					},
				},
			},
		},
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
		if contains(testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete, podList.Items[i].Name) {
			t.Fatalf("WorkersToDelete is not actually deleted, %s", podList.Items[i].Name)
		}
	}
}

func TestReconcile_PodCrash_Fail(t *testing.T) {
	setupTest(t)
	defer tearDown(t)

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	podList := corev1.PodList{}
	err := fakeClient.List(context.Background(), &podList, client.InNamespace(namespaceStr))

	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

	// Simulate 2 pod container crash.
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
		if contains(testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete, podList.Items[i].Name) {
			t.Logf("WorkersToDelete is not actually deleted, %s", podList.Items[i].Name)
		}
	}
}

func TestReconcile_PodCrash_OK(t *testing.T) {
	setupTest(t)
	defer tearDown(t)

	PrioritizeWorkersToDelete = true

	fakeClient := clientFake.NewClientBuilder().WithRuntimeObjects(testPods...).Build()

	podList := corev1.PodList{}
	err := fakeClient.List(context.Background(), &podList, client.InNamespace(namespaceStr))

	assert.Nil(t, err, "Fail to get pod list")
	assert.Equal(t, len(testPods), len(podList.Items), "Init pod list len is wrong")

	// Simulate 2 pod container crash.
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
		if contains(testRayCluster.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete, podList.Items[i].Name) {
			t.Errorf("WorkersToDelete is not actually deleted, %s", podList.Items[i].Name)
		}
	}
}

func contains(slice []string, item string) bool {
	set := make(map[string]struct{}, len(slice))
	for _, s := range slice {
		set[s] = struct{}{}
	}

	_, ok := set[item]
	return ok
}
