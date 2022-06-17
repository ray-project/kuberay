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

var numReplicas int32 = 1
var numCpus = 0.1

var myRayService = &RayService{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "rayservice-sample",
		Namespace: "default",
	},
	Spec: RayServiceSpec{
		ServeConfigSpecs: []ServeConfigSpec{
			{
				Name:        "shallow",
				ImportPath:  "test_env.shallow_import.ShallowClass",
				NumReplicas: &numReplicas,
				RoutePrefix: "/shallow",
				RayActorOptions: RayActorOptionSpec{
					NumCpus: &numCpus,
					RuntimeEnv: map[string][]string{
						"py_modules": {
							"https://github.com/ray-project/test_deploy_group/archive/67971777e225600720f91f618cdfe71fc47f60ee.zip",
							"https://github.com/ray-project/test_module/archive/aa6f366f7daa78c98408c27d917a983caa9f888b.zip",
						},
					},
				},
			},
			{
				Name:        "deep",
				ImportPath:  "test_env.subdir1.subdir2.deep_import.DeepClass",
				NumReplicas: &numReplicas,
				RoutePrefix: "/deep",
				RayActorOptions: RayActorOptionSpec{
					NumCpus: &numCpus,
					RuntimeEnv: map[string][]string{
						"py_modules": {
							"https://github.com/ray-project/test_deploy_group/archive/67971777e225600720f91f618cdfe71fc47f60ee.zip",
							"https://github.com/ray-project/test_module/archive/aa6f366f7daa78c98408c27d917a983caa9f888b.zip",
						},
					},
				},
			},
			{
				Name:        "one",
				ImportPath:  "test_module.test.one",
				NumReplicas: &numReplicas,
				RayActorOptions: RayActorOptionSpec{
					NumCpus: &numCpus,
					RuntimeEnv: map[string][]string{
						"py_modules": {
							"https://github.com/ray-project/test_deploy_group/archive/67971777e225600720f91f618cdfe71fc47f60ee.zip",
							"https://github.com/ray-project/test_module/archive/aa6f366f7daa78c98408c27d917a983caa9f888b.zip",
						},
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
							"rayCluster":  "raycluster-sample",
							"rayNodeType": "head",
							"groupName":   "headgroup",
						},
						Annotations: map[string]string{
							"key": "value",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Image: "rayproject/ray:1.12.1",
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
									Image:   "rayproject/ray:1.12.1",
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
      "serveConfigs":[
         {
            "name":"shallow",
            "importPath":"test_env.shallow_import.ShallowClass",
            "numReplicas":1,
            "routePrefix":"/shallow",
            "rayActorOptions":{
               "runtimeEnv":{
                  "py_modules":[
                     "https://github.com/ray-project/test_deploy_group/archive/67971777e225600720f91f618cdfe71fc47f60ee.zip",
                     "https://github.com/ray-project/test_module/archive/aa6f366f7daa78c98408c27d917a983caa9f888b.zip"
                  ]
               },
               "numCpus":0.1
            }
         },
         {
            "name":"deep",
            "importPath":"test_env.subdir1.subdir2.deep_import.DeepClass",
            "numReplicas":1,
            "routePrefix":"/deep",
            "rayActorOptions":{
               "runtimeEnv":{
                  "py_modules":[
                     "https://github.com/ray-project/test_deploy_group/archive/67971777e225600720f91f618cdfe71fc47f60ee.zip",
                     "https://github.com/ray-project/test_module/archive/aa6f366f7daa78c98408c27d917a983caa9f888b.zip"
                  ]
               },
               "numCpus":0.1
            }
         },
         {
            "name":"one",
            "importPath":"test_module.test.one",
            "numReplicas":1,
            "rayActorOptions":{
               "runtimeEnv":{
                  "py_modules":[
                     "https://github.com/ray-project/test_deploy_group/archive/67971777e225600720f91f618cdfe71fc47f60ee.zip",
                     "https://github.com/ray-project/test_module/archive/aa6f366f7daa78c98408c27d917a983caa9f888b.zip"
                  ]
               },
               "numCpus":0.1
            }
         }
      ],
      "rayClusterConfig":{
         "headGroupSpec":{
            "serviceType":"ClusterIP",
            "replicas":1,
            "rayStartParams":{
               "block":"true",
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
                     "rayCluster":"raycluster-sample",
                     "rayNodeType":"head"
                  },
                  "annotations":{
                     "key":"value"
                  }
               },
               "spec":{
                  "containers":[
                     {
                        "name":"ray-head",
                        "image":"rayproject/ray:1.12.1",
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
                           "image":"rayproject/ray:1.12.1",
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
         "dashboardStatus":{
            
         },
         "rayClusterStatus":{
            
         }
      },
      "pendingServiceStatus":{
         "dashboardStatus":{
            
         },
         "rayClusterStatus":{
            
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
