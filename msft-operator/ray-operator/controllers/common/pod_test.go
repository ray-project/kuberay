package common

import (
	"fmt"
	rayiov1alpha1 "ray-operator/api/v1alpha1"
	"ray-operator/controllers/utils"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
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
		HeadService: v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "head-svc",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Ports:     []corev1.ServicePort{{Name: "redis", Port: int32(6379)}},
				ClusterIP: corev1.ClusterIPNone,
				Selector: map[string]string{
					"identifier": "raycluster-sample-head",
				},
			},
		},
		HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
			Replicas: pointer.Int32Ptr(1),
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
						"rayCluster": "raycluster-sample",
						"groupName":  "headgroup",
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
		WorkerGroupsSpec: []rayiov1alpha1.WorkerGroupSpec{
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
							"rayCluster": "raycluster-sample",
							"groupName":  "small-group",
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
	podType := rayiov1alpha1.HeadNode
	podName := strings.ToLower(instance.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	podConf := DefaultHeadPodConfig(*instance, podType, podName, instance.Spec.HeadService.Name)
	svcName := instance.Spec.HeadService.Name

	pod := BuildPod(podConf, rayiov1alpha1.HeadNode, instance.Spec.HeadGroupSpec.RayStartParams, svcName)

	actualResult := pod.Labels["identifier"]
	expectedResult := fmt.Sprintf("%s-%s", instance.Name, podType)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	//testing worker pod
	worker := instance.Spec.WorkerGroupsSpec[0]
	podType = rayiov1alpha1.WorkerNode
	podName = instance.Name + DashSymbol + string(podType) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)
	podConf = DefaultWorkerPodConfig(*instance, worker, podType, podName, instance.Spec.HeadService.Name)
	pod = BuildPod(podConf, rayiov1alpha1.WorkerNode, worker.RayStartParams, svcName)

	expectedResult = fmt.Sprintf("%s:6379", instance.Spec.HeadService.Name)
	actualResult = instance.Spec.WorkerGroupsSpec[0].RayStartParams["address"]

	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}
