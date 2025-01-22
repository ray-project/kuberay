package v1

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var myRayCluster = &RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
	},
	Spec: RayClusterSpec{
		HeadGroupSpec: HeadGroupSpec{
			RayStartParams: map[string]string{
				"port":                        "6379",
				"object-manager-port":         "12345",
				"node-manager-port":           "12346",
				"object-store-memory":         "100000000",
				"num-cpus":                    "1",
				"dashboard-agent-listen-port": "52365",
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels: map[string]string{
						"groupName": "headgroup",
					},
				},
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
		WorkerGroupSpecs: []WorkerGroupSpec{
			{
				Replicas:    ptr.To[int32](3),
				MinReplicas: ptr.To[int32](0),
				MaxReplicas: ptr.To[int32](10000),
				NumOfHosts:  1,
				GroupName:   "small-group",
				RayStartParams: map[string]string{
					"port":                        "6379",
					"num-cpus":                    "1",
					"dashboard-agent-listen-port": "52365",
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Labels: map[string]string{
							"groupName": "small-group",
						},
					},
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
