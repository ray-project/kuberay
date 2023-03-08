package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var (
	numReplicas   int32 = 1
	numCpus             = 0.1
	runtimeEnvStr       = "working_dir:\n - \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\""
)

var myRayService = &RayService{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "rayservice-sample",
		Namespace: "default",
	},
	Spec: RayServiceSpec{
		ServeDeploymentGraphSpec: ServeDeploymentGraphSpec{
			ImportPath: "fruit.deployment_graph",
			RuntimeEnv: runtimeEnvStr,
			ServeConfigSpecs: []ServeConfigSpec{
				{
					Name:        "MangoStand",
					NumReplicas: &numReplicas,
					UserConfig:  "price: 3",
					RayActorOptions: RayActorOptionSpec{
						NumCpus: &numCpus,
					},
				},
				{
					Name:        "OrangeStand",
					NumReplicas: &numReplicas,
					UserConfig:  "price: 2",
					RayActorOptions: RayActorOptionSpec{
						NumCpus: &numCpus,
					},
				},
				{
					Name:        "PearStand",
					NumReplicas: &numReplicas,
					UserConfig:  "price: 1",
					RayActorOptions: RayActorOptionSpec{
						NumCpus: &numCpus,
					},
				},
				{
					Name:        "FruitMarket",
					NumReplicas: &numReplicas,
					RayActorOptions: RayActorOptionSpec{
						NumCpus: &numCpus,
					},
				},
				{
					Name:        "DAGDriver",
					NumReplicas: &numReplicas,
					RoutePrefix: "/",
					RayActorOptions: RayActorOptionSpec{
						NumCpus: &numCpus,
					},
				},
			},
		},
		RayClusterSpec: RayClusterSpec{
			RayVersion: "1.12.1",
			HeadGroupSpec: HeadGroupSpec{
				ServiceType: corev1.ServiceTypeClusterIP,
				Replicas:    pointer.Int32Ptr(1),
				RayStartParams: map[string]string{
					"port":                        "6379",
					"object-store-memory":         "100000000",
					"dashboard-host":              "0.0.0.0",
					"num-cpus":                    "1",
					"node-ip-address":             "127.0.0.1",
					"block":                       "true",
					"dashboard-agent-listen-port": "52365",
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
					Replicas:    pointer.Int32Ptr(3),
					MinReplicas: pointer.Int32Ptr(0),
					MaxReplicas: pointer.Int32Ptr(10000),
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
}

var expected = `{
   "metadata":{
      "name":"rayservice-sample",
      "namespace":"default",
      "creationTimestamp":null
   },
   "spec":{
      "serveConfig":{
         "importPath":"fruit.deployment_graph",
         "runtimeEnv":"working_dir:\n - \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"",
         "deployments":[
            {
               "name":"MangoStand",
               "numReplicas":1,
               "userConfig":"price: 3",
               "rayActorOptions":{
                  "numCpus":0.1
               }
            },
            {
               "name":"OrangeStand",
               "numReplicas":1,
               "userConfig":"price: 2",
               "rayActorOptions":{
                  "numCpus":0.1
               }
            },
            {
               "name":"PearStand",
               "numReplicas":1,
               "userConfig":"price: 1",
               "rayActorOptions":{
                  "numCpus":0.1
               }
            },
            {
               "name":"FruitMarket",
               "numReplicas":1,
               "rayActorOptions":{
                  "numCpus":0.1
               }
            },
            {
               "name":"DAGDriver",
               "numReplicas":1,
               "routePrefix":"/",
               "rayActorOptions":{
                  "numCpus":0.1
               }
            }
         ]
      },
      "rayClusterConfig":{
         "headGroupSpec":{
            "serviceType":"ClusterIP",
            "replicas":1,
            "rayStartParams":{
               "block":"true",
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
                     "groupName":"headgroup",
                     "rayCluster":"raycluster-sample"
                  },
                  "annotations":{
                     "key":"value"
                  }
               },
               "spec":{
                  "containers":[
                     {
                        "name":"ray-head",
                        "image":"rayproject/ray:2.3.0",
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
                        "groupName":"small-group",
                        "rayCluster":"raycluster-sample"
                     }
                  },
                  "spec":{
                     "containers":[
                        {
                           "name":"ray-worker",
                           "image":"rayproject/ray:2.3.0",
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
         ],
         "rayVersion":"1.12.1"
      }
   },
   "status":{
      "activeServiceStatus":{
         "appStatus":{
            
         },
         "dashboardStatus":{
            
         },
         "rayClusterStatus":{
            "head":{}
         }
      },
      "pendingServiceStatus":{
         "appStatus":{
            
         },
         "dashboardStatus":{
            
         },
         "rayClusterStatus":{
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
