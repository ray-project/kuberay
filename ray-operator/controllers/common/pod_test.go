package common

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/utils"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var instance = &rayiov1alpha1.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
	},
	Spec: rayiov1alpha1.RayClusterSpec{
		RayVersion: "1.0.0",
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
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels: map[string]string{
						"ray.io/cluster": "raycluster-sample",
						"ray.io/group":   "headgroup",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name:    "ray-head",
							Image:   "rayproject/autoscaler",
							Command: []string{"python"},
							Args:    []string{"/opt/code.py"},
							Env: []corev1.EnvVar{
								corev1.EnvVar{
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
			rayiov1alpha1.WorkerGroupSpec{
				Replicas:    pointer.Int32Ptr(3),
				MinReplicas: pointer.Int32Ptr(0),
				MaxReplicas: pointer.Int32Ptr(10000),
				GroupName:   "small-group",
				RayStartParams: map[string]string{
					"port":           "6379",
					"redis-password": "LetMeInRay",
					"num-cpus":       "1",
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Labels: map[string]string{
							"ray.io/cluster": "raycluster-sample",
							"ray.io/group":   "small-group",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							corev1.Container{
								Name:    "ray-worker",
								Image:   "rayproject/autoscaler",
								Command: []string{"echo"},
								Args:    []string{"Hello Ray"},
								Env: []corev1.EnvVar{
									corev1.EnvVar{
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
		},
	},
}

func TestBuildPod(t *testing.T) {
	podName := strings.ToLower(instance.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(instance.Name)
	podTemplateSpec := DefaultHeadPodTemplate(*instance, instance.Spec.HeadGroupSpec, podName, svcName)
	pod := BuildPod(podTemplateSpec, rayiov1alpha1.HeadNode, instance.Spec.HeadGroupSpec.RayStartParams, svcName)

	actualResult := pod.Labels[RayClusterLabelKey]
	expectedResult := instance.Name
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[RayNodeTypeLabelKey]
	expectedResult = string(rayiov1alpha1.HeadNode)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[RayNodeGroupLabelKey]
	expectedResult = "headgroup"
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	//testing worker pod
	worker := instance.Spec.WorkerGroupSpecs[0]
	podName = instance.Name + DashSymbol + string(rayiov1alpha1.WorkerNode) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)
	podTemplateSpec = DefaultWorkerPodTemplate(*instance, worker, podName, svcName)
	pod = BuildPod(podTemplateSpec, rayiov1alpha1.WorkerNode, worker.RayStartParams, svcName)

	expectedResult = fmt.Sprintf("%s:6379", svcName)
	actualResult = instance.Spec.WorkerGroupSpecs[0].RayStartParams["address"]

	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}
