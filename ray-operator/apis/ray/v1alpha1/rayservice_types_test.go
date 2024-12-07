package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var myRayService = &RayService{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "rayservice-sample",
		Namespace: "default",
	},
	Spec: RayServiceSpec{
		RayClusterSpec: RayClusterSpec{
			HeadGroupSpec: HeadGroupSpec{
				RayStartParams: map[string]string{
					"port":                        "6379",
					"object-store-memory":         "100000000",
					"dashboard-host":              "0.0.0.0",
					"num-cpus":                    "1",
					"node-ip-address":             "127.0.0.1",
					"dashboard-agent-listen-port": "52365",
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"groupName": "headgroup",
						},
						Annotations: map[string]string{
							"key": "value",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Image: "rayproject/ray:2.9.0",
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
									{
										Name:          "dashboard-agent",
										ContainerPort: 52365,
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
									Image:   "rayproject/ray:2.9.0",
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
}

var expected = `{
   "metadata":{
      "name":"rayservice-sample",
      "namespace":"default",
      "creationTimestamp":null
   },
   "spec":{
      "rayClusterConfig":{
         "headGroupSpec":{
            "rayStartParams":{
               "dashboard-agent-listen-port":"52365",
               "dashboard-host":"0.0.0.0",
               "node-ip-address":"127.0.0.1",
               "num-cpus":"1",
               "object-store-memory":"100000000",
               "port":"6379"
            },
            "template":{
               "metadata":{
                  "creationTimestamp":null,
                  "labels":{
                     "groupName": "headgroup"
                  },
                  "annotations":{
                     "key":"value"
                  }
               },
               "spec":{
                  "containers":[
                     {
                        "name":"ray-head",
                        "image":"rayproject/ray:2.9.0",
                        "ports":[
                           {
                              "name":"gcs-server",
                              "containerPort":6379
                           },
                           {
                              "name":"dashboard",
                              "containerPort":8265
                           },
                           {
                              "name":"head",
                              "containerPort":10001
                           },
                           {
                              "name":"dashboard-agent",
                              "containerPort":52365
                           }
                        ],
                        "env":[
                           {
                              "name":"MY_POD_IP",
                              "valueFrom":{
                                 "fieldRef":{
                                    "fieldPath":"status.podIP"
                                 }
                              }
                           }
                        ],
                        "resources":{
                           "limits":{
                              "cpu":"1",
                              "memory":"2Gi"
                           },
                           "requests":{
                              "cpu":"1",
                              "memory":"2Gi"
                           }
                        }
                     }
                  ]
               }
            }
         },
         "workerGroupSpecs":[
            {
               "groupName":"small-group",
               "replicas":3,
               "minReplicas":0,
               "maxReplicas":10000,
               "rayStartParams":{
                  "dashboard-agent-listen-port":"52365",
                  "num-cpus":"1",
                  "port":"6379"
               },
               "template":{
                  "metadata":{
                     "namespace":"default",
                     "creationTimestamp":null,
                     "labels":{
                        "groupName":"small-group"
                     }
                  },
                  "spec":{
                     "containers":[
                        {
                           "name":"ray-worker",
                           "image":"rayproject/ray:2.9.0",
                           "command":[
                              "echo"
                           ],
                           "args":[
                              "Hello Ray"
                           ],
                           "env":[
                              {
                                 "name":"MY_POD_IP",
                                 "valueFrom":{
                                    "fieldRef":{
                                       "fieldPath":"status.podIP"
                                    }
                                 }
                              }
                           ],
                           "resources":{

                           }
                        }
                     ]
                  }
               },
               "scaleStrategy":{

               }
            }
         ]
      }
   },
   "status":{
      "activeServiceStatus":{
         "rayClusterStatus":{
            "desiredCPU": "0",
            "desiredMemory": "0",
            "desiredGPU": "0",
            "desiredTPU": "0",
            "head":{}
         }
      },
      "pendingServiceStatus":{
         "rayClusterStatus":{
            "desiredCPU": "0",
            "desiredMemory": "0",
            "desiredGPU": "0",
            "desiredTPU": "0",
            "head":{}
         }
      }
   }
}`

func TestMarshallingRayService(t *testing.T) {
	// marshal successfully
	myRayServiceJson, err := json.Marshal(&myRayService)
	if err != nil {
		t.Fatalf("Expected `%v` but got `%v`", nil, err)
	}

	require.JSONEq(t, expected, string(myRayServiceJson))
}
