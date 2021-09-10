package v1alpha1

import (
	"testing"

	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var myRayCluster = &RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
	},
	Spec: RayClusterSpec{
		RayVersion: "1.0",
		HeadService: v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "head-svc",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Name: "redis", Port: int32(6379)}},
				// Use a headless service, meaning that the DNS record for the service will
				// point directly to the head node pod's IP address.
				ClusterIP: corev1.ClusterIPNone,
				// This selector must match the label of the head node.
				Selector: map[string]string{
					"identifier": "raycluster-sample-head",
				},
			},
		},
		HeadGroupSpec: HeadGroupSpec{
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
		WorkerGroupsSpec: []WorkerGroupSpec{
			WorkerGroupSpec{
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

func TestMarshalling(t *testing.T) {
	// marshal successfully
	_, err := json.Marshal(&myRayCluster)
	if err != nil {
		t.Fatalf("Expected `%v` but got `%v`", nil, err)
	}
}
