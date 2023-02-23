package v1alpha1

import (
	"encoding/json"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var expectedRayJob = RayJob{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "rayjob-sample",
		Namespace: "default",
	},
	Spec: RayJobSpec{
		Entrypoint: "echo hello",
		Metadata: map[string]string{
			"owner": "userA",
		},
		RayClusterSpec: &RayClusterSpec{
			RayVersion: "1.12.1",
			HeadGroupSpec: HeadGroupSpec{
				ServiceType: corev1.ServiceTypeClusterIP,
				Replicas:    pointer.Int32Ptr(1),
				RayStartParams: map[string]string{
					"port":                "6379",
					"object-store-memory": "100000000",
					"dashboard-host":      "0.0.0.0",
					"num-cpus":            "1",
					"node-ip-address":     "127.0.0.1",
					"block":               "true",
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"rayCluster": "raycluster-sample",
							"groupName":  "headgroup",
						},
						Annotations: map[string]string{
							"key": "value",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Image: "rayproject/ray:2.3.0",
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
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("2Gi"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("2Gi"),
									},
								},
								Ports: []corev1.ContainerPort{
									{
										Name:          "gcs-server",
										ContainerPort: 6379,
									},
									{
										Name:          "dashboard",
										ContainerPort: 8265,
									},
									{
										Name:          "head",
										ContainerPort: 10001,
									},
								},
							},
						},
					},
				},
			},
			WorkerGroupSpecs: []WorkerGroupSpec{
				{
					Replicas:    pointer.Int32Ptr(3),
					MinReplicas: pointer.Int32Ptr(0),
					MaxReplicas: pointer.Int32Ptr(10000),
					GroupName:   "small-group",
					RayStartParams: map[string]string{
						"port":     "6379",
						"num-cpus": "1",
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
				},
			},
		},
	},
	Status: RayJobStatus{
		JobId: "raysubmit_HY2gtJmY1zjysYbm",
	},
}

var testRayJobJSON = `{
    "metadata": {
        "name": "rayjob-sample",
        "namespace": "default",
        "creationTimestamp": null
    },
    "spec": {
        "entrypoint": "echo hello",
        "metadata": {
            "owner":"userA"
        },
        "rayClusterSpec": {
            "headGroupSpec": {
                "serviceType": "ClusterIP",
                "replicas": 1,
                "rayStartParams": {
                    "block": "true",
                    "dashboard-host": "0.0.0.0",
                    "node-ip-address": "127.0.0.1",
                    "num-cpus": "1",
                    "object-store-memory": "100000000",
                    "port": "6379"
                },
                "template": {
                    "metadata": {
                        "creationTimestamp": null,
                        "labels": {
                            "groupName": "headgroup",
                            "rayCluster": "raycluster-sample"
                        },
                        "annotations": {
                            "key": "value"
                        }
                    },
                    "spec": {
                        "containers": [
                            {
                                "name": "ray-head",
                                "image": "rayproject/ray:2.3.0",
                                "ports": [
                                    {
                                        "name": "gcs-server",
                                        "containerPort": 6379
                                    },
                                    {
                                        "name": "dashboard",
                                        "containerPort": 8265
                                    },
                                    {
                                        "name": "head",
                                        "containerPort": 10001
                                    }
                                ],
                                "env": [
                                    {
                                        "name": "MY_POD_IP",
                                        "valueFrom": {
                                            "fieldRef": {
                                                "fieldPath": "status.podIP"
                                            }
                                        }
                                    }
                                ],
                                "resources": {
                                    "limits": {
                                        "cpu": "1",
                                        "memory": "2Gi"
                                    },
                                    "requests": {
                                        "cpu": "1",
                                        "memory": "2Gi"
                                    }
                                }
                            }
                        ]
                    }
                }
            },
            "workerGroupSpecs": [
                {
                    "groupName": "small-group",
                    "replicas": 3,
                    "minReplicas": 0,
                    "maxReplicas": 10000,
                    "rayStartParams": {
                        "num-cpus": "1",
                        "port": "6379"
                    },
                    "template": {
                        "metadata": {
                            "namespace": "default",
                            "creationTimestamp": null,
                            "labels": {
                                "groupName": "small-group",
                                "rayCluster": "raycluster-sample"
                            }
                        },
                        "spec": {
                            "containers": [
                                {
                                    "name": "ray-worker",
                                    "image": "rayproject/ray:2.3.0",
                                    "command": [
                                        "echo"
                                    ],
                                    "args": [
                                        "Hello Ray"
                                    ],
                                    "env": [
                                        {
                                            "name": "MY_POD_IP",
                                            "valueFrom": {
                                                "fieldRef": {
                                                    "fieldPath": "status.podIP"
                                                }
                                            }
                                        }
                                    ],
                                    "resources": {}
                                }
                            ]
                        }
                    },
                    "scaleStrategy": {}
                }
            ],
            "rayVersion": "1.12.1"
        }
    },
    "status": {
        "jobId": "raysubmit_HY2gtJmY1zjysYbm"
    }
}`

func TestMarshallingRayJob(t *testing.T) {
	// marshal successfully
	var testRayJob RayJob
	err := json.Unmarshal([]byte(testRayJobJSON), &testRayJob)
	if err != nil {
		t.Fatal("Failed to unmarshal")
	}
	if !reflect.DeepEqual(testRayJob, expectedRayJob) {
		t.Fatal("Jobs should equal")
	}

	if testRayJob.Status.JobId != expectedRayJob.Status.JobId {
		t.Fatal()
	}
	if testRayJob.Spec.Entrypoint != expectedRayJob.Spec.Entrypoint {
		t.Fatal()
	}
}
