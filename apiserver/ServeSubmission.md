# Managing RayServe with the API server

Ray Serve is a scalable model serving library for building online inference APIs. This document describes creation and management of Ray Serve enabled Ray clusters

## Deploy KubeRay operator and API server

Reffer to [readme](README.md) for setting up KubRay operator and API server.

```shell
make operator-image docker-image cluster load-operator-image load-image  deploy-operator deploy
```

Once they are set up, you first need to create a Ray cluster

## Creating a cluster with RayServe support

Before creating a cluster you need to create a template using the following command:

```shell
curl -X POST 'localhost:31888/apis/v1/namespaces/default/compute_templates' \
--header 'Content-Type: application/json' \
--data '{
  "name": "default-template",
  "namespace": "default",
  "cpu": 2,
  "memory": 4
}'
```

Up until recently the only way to create a Ray cluster supporting RayServe was by using `Create ray service` APIs. Although it does work, quite often you want to create cluster supporting Ray serve so that you can experiment with serve APIs directly. Now it is possible by adding the following annotation to the cluster:

```json
"annotations" : {
    "ray.io/enable-serve-service": "true"
  },
```

the complete curl command to creation such cluster is as follows:

```shell
curl -X POST 'localhost:31888/apis/v1/namespaces/default/clusters' \
--header 'Content-Type: application/json' \
--data '{
  "name": "test-cluster",
  "namespace": "default",
  "user": "boris",
  "annotations" : {
    "ray.io/enable-serve-service": "true"
  },
  "clusterSpec": {
    "headGroupSpec": {
      "computeTemplate": "default-template",
      "image": "rayproject/ray:2.8.0-py310",
      "serviceType": "ClusterIP",
      "rayStartParams": {
         "dashboard-host": "0.0.0.0",
         "metrics-export-port": "8080",
         "dashboard-agent-listen-port": "52365"
       }
    },
    "workerGroupSpec": [
      {
        "groupName": "small-wg",
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.8.0-py310",
        "replicas": 1,
        "minReplicas": 0,
        "maxReplicas": 5,
        "rayStartParams": {
           "node-ip-address": "$MY_POD_IP"
         }
      }
    ]
  }
}'
```

To confirm that the cluster is created correctly, check created services using that following command:

```shell
kubectl get service
```

that should return the following:

```shell
test-cluster-head-svc    ClusterIP   10.96.19.185    <none>        8265/TCP,52365/TCP,10001/TCP,8080/TCP,6379/TCP,8000/TCP 
test-cluster-serve-svc   ClusterIP   10.96.144.162   <none>        8000/TCP
```

As you can see, in this case two services are created - one for the head node to be able to see the dashboard and configure the cluster and one for submission of the serve requests.

For the head node service, note that the additional port - 52365 is created for serve configuration.

## Using Serve submission APIs

Current implementation is based on this Ray [documentation](https://docs.ray.io/en/latest/serve/api/index.html#serve-rest-api) and provides three methods:

* SubmitServeApplications - declaratively deploys a list of Serve applications. If Serve is already running on the Ray cluster, removes all applications not listed in the new config. If Serve is not running on the Ray cluster, starts Serve. List of applications is defined by yaml file (passed as string) and defined [here](https://docs.ray.io/en/latest/serve/production-guide/config.html).
* GetServeApplications - gets cluster-level info and comprehensive details on all Serve applications deployed on the Ray cluster. Definition of the return data is [here](../proto/serve_submission.proto)
* DeleteRayServeApplications - shuts down Serve and all applications running on the Ray cluster. Has no effect if Serve is not running on the Ray cluster.

Currently API server does not provide support for serving ML models. Using an API server to support this functionality will negatively impact scalability and performance of model serving. As a result we decided not to support this functionality currently.

### Create Serve applications

Once the cluster is up and running, you can submit an application to the cluster using the following command:

```shell
curl -X POST 'localhost:31888/apis/v1/namespaces/default/serveapplication/test-cluster' \
--header 'Content-Type: application/json' \
--data '{
  "configyaml": "applications:\n  - name: fruit_app\n    import_path: fruit.deployment_graph\n    route_prefix: /fruit\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: MangoStand\n        num_replicas: 1\n        user_config:\n          price: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: OrangeStand\n        num_replicas: 1\n        user_config:\n          price: 2\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: PearStand\n        num_replicas: 1\n        user_config:\n          price: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: FruitMarket\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: DAGDriver\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n  - name: math_app\n    import_path: conditional_dag.serve_dag\n    route_prefix: /calc\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: Adder\n        num_replicas: 1\n        user_config:\n          increment: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Multiplier\n        num_replicas: 1\n        user_config:\n          factor: 5\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Router\n        num_replicas: 1\n      - name: create_order\n        num_replicas: 1\n      - name: DAGDriver\n        num_replicas: 1\n"
}'
```

This command does not have any return value, it should just finish successfully. Once it is done, you should be able to see application's deployment in the `serve` pane of the Ray dashboard.
  
**Note** The easiest way to get to the Ray dashboard is by using `port-forward` command.

```shell
kubectl port-forward svc/test-cluster-head-svc 8265
```

and then connecting to `localhost:8265`

### Get Serve applications

Once the serve applcation is submitted, the following command can be used to get applications details.

```shell
curl -X GET 'localhost:31888/apis/v1/namespaces/default/serveapplication/test-cluster' \
--header 'Content-Type: application/json' 
```

This should return JSON similar to the one below:

```json
{
   "deployMode":"MULTI_APP",
   "proxyLocation":"EveryNode",
   "controllerInfo":{
      "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
      "nodeIp":"10.244.1.2",
      "actorId":"ceac9504c1ce994f152afcbe01000000",
      "actorName":"SERVE_CONTROLLER_ACTOR",
      "logFilePath":"/serve/controller_451.log"
   },
   "httpOptions":{
      "host":"0.0.0.0",
      "port":8000,
      "keepAliveTimeoutS":5
   },
   "grpcOptions":{
      "port":9000
   },
   "applications":{
      "fruit_app":{
         "name":"fruit_app",
         "status":"RUNNING",
         "routePrefix":"/fruit",
         "lastDeployedTimeS":1702216400,
         "deployments":{
            "DAGDriver":{
               "name":"DAGDriver",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"DAGDriver",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
                     "nodeIp":"10.244.2.3",
                     "actorId":"b9ea9613d134735b1a42f5bb01000000",
                     "actorName":"SERVE_REPLICA::fruit_app#DAGDriver#HhnVXp",
                     "workerId":"bc1aa743dc38799fe5314192c4b82efcbf6b3dd2473b062ac15cc376",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_DAGDriver_fruit_app#DAGDriver#HhnVXp.log",
                     "replicaId":"fruit_app#DAGDriver#HhnVXp",
                     "pid":249,
                     "startTimeS":1702216400
                  }
               ]
            },
            "FruitMarket":{
               "name":"FruitMarket",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"FruitMarket",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
                     "nodeIp":"10.244.1.2",
                     "actorId":"95e00bd164c069eaced206c801000000",
                     "actorName":"SERVE_REPLICA::fruit_app#FruitMarket#vMilEM",
                     "workerId":"6eb5f8ead62db87f088a7e3c13211c442c5344e431905a556052ebc7",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_FruitMarket_fruit_app#FruitMarket#vMilEM.log",
                     "replicaId":"fruit_app#FruitMarket#vMilEM",
                     "pid":613,
                     "startTimeS":1702216400
                  }
               ]
            },
            "MangoStand":{
               "name":"MangoStand",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"MangoStand",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "userConfig":{
                     "price":"3"
                  },
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
                     "nodeIp":"10.244.2.3",
                     "actorId":"d80b934549a6155716d706d701000000",
                     "actorName":"SERVE_REPLICA::fruit_app#MangoStand#MbsqaU",
                     "workerId":"af8dbb9355cf00e52212dfc8309f240dd0147119319829ea2801eeb5",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_MangoStand_fruit_app#MangoStand#MbsqaU.log",
                     "replicaId":"fruit_app#MangoStand#MbsqaU",
                     "pid":199,
                     "startTimeS":1702216400
                  }
               ]
            },
            "OrangeStand":{
               "name":"OrangeStand",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"OrangeStand",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "userConfig":{
                     "price":"2"
                  },
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
                     "nodeIp":"10.244.1.2",
                     "actorId":"b3fe6a18933f73bab4028f8801000000",
                     "actorName":"SERVE_REPLICA::fruit_app#OrangeStand#duLXfz",
                     "workerId":"ca5859275605da35ee5b4aea595a05b3311e75a71abb99923bb8831b",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_OrangeStand_fruit_app#OrangeStand#duLXfz.log",
                     "replicaId":"fruit_app#OrangeStand#duLXfz",
                     "pid":612,
                     "startTimeS":1702216400
                  }
               ]
            },
            "PearStand":{
               "name":"PearStand",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"PearStand",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "userConfig":{
                     "price":"1"
                  },
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
                     "nodeIp":"10.244.2.3",
                     "actorId":"f36536b0dca1fe7cc6ecc6de01000000",
                     "actorName":"SERVE_REPLICA::fruit_app#PearStand#MdjHOm",
                     "workerId":"fd0519e76dc5728c8a9b6f940fe59eaba2dc634d17b700de2166110b",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_PearStand_fruit_app#PearStand#MdjHOm.log",
                     "replicaId":"fruit_app#PearStand#MdjHOm",
                     "pid":200,
                     "startTimeS":1702216400
                  }
               ]
            }
         },
         "deployedAppConfig":{
            "name":"fruit_app",
            "routePrefix":"/fruit",
            "importPath":"fruit.deployment_graph",
            "runtimeEnv":{
               "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
            },
            "deployments":[
               {
                  "name":"MangoStand",
                  "numReplicas":1,
                  "userConfig":{
                     "price":"3"
                  },
                  "rayActorOptions":{
                     "numCpus":0.1
                  }
               },
               {
                  "name":"OrangeStand",
                  "numReplicas":1,
                  "userConfig":{
                     "price":"2"
                  },
                  "rayActorOptions":{
                     "numCpus":0.1
                  }
               },
               {
                  "name":"PearStand",
                  "numReplicas":1,
                  "userConfig":{
                     "price":"1"
                  },
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
                  "rayActorOptions":{
                     "numCpus":0.1
                  }
               }
            ]
         }
      },
      "math_app":{
         "name":"math_app",
         "status":"RUNNING",
         "routePrefix":"/calc",
         "lastDeployedTimeS":1702216400,
         "deployments":{
            "Adder":{
               "name":"Adder",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"Adder",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "userConfig":{
                     "increment":"3"
                  },
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
                     "nodeIp":"10.244.2.3",
                     "actorId":"6838547ea22fbcf566d2057201000000",
                     "actorName":"SERVE_REPLICA::math_app#Adder#ZtvAeP",
                     "workerId":"e1b2d156136dec836ae78935043e8e9aa0d175b2ea5ddd2ac4839a2c",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_Adder_math_app#Adder#ZtvAeP.log",
                     "replicaId":"math_app#Adder#ZtvAeP",
                     "pid":250,
                     "startTimeS":1702216400
                  }
               ]
            },
            "DAGDriver":{
               "name":"DAGDriver",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"DAGDriver",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
                     "nodeIp":"10.244.1.2",
                     "actorId":"7149d5935b9088cb030fb3ba01000000",
                     "actorName":"SERVE_REPLICA::math_app#DAGDriver#LBkgea",
                     "workerId":"ddc782bd94d10608b59bc31f12240a01df29c0b5814ca6499010de7c",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_DAGDriver_math_app#DAGDriver#LBkgea.log",
                     "replicaId":"math_app#DAGDriver#LBkgea",
                     "pid":718,
                     "startTimeS":1702216400
                  }
               ]
            },
            "Multiplier":{
               "name":"Multiplier",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"Multiplier",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "userConfig":{
                     "factor":"5"
                  },
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
                     "nodeIp":"10.244.1.2",
                     "actorId":"303757bcd36f8dae1c6bbc8701000000",
                     "actorName":"SERVE_REPLICA::math_app#Multiplier#MCQNmH",
                     "workerId":"184f5a9d39e19e001a27818f7b4e31645c094066dfbb2aa0d4d34171",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_Multiplier_math_app#Multiplier#MCQNmH.log",
                     "replicaId":"math_app#Multiplier#MCQNmH",
                     "pid":638,
                     "startTimeS":1702216400
                  }
               ]
            },
            "Router":{
               "name":"Router",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"Router",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
                     "nodeIp":"10.244.1.2",
                     "actorId":"54724715f2e3ee487b42852401000000",
                     "actorName":"SERVE_REPLICA::math_app#Router#uezNSp",
                     "workerId":"9bc286583533cd9f1bc429067a3a130ee810bedbb913ac62d0b1a102",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_Router_math_app#Router#uezNSp.log",
                     "replicaId":"math_app#Router#uezNSp",
                     "pid":663,
                     "startTimeS":1702216400
                  }
               ]
            },
            "create_order":{
               "name":"create_order",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"create_order",
                  "numReplicas":1,
                  "maxConcurrentQueries":100,
                  "gracefulShutdownWaitLoopS":2,
                  "gracefulShutdownTimeoutS":20,
                  "healthCheckPeriodS":10,
                  "healthCheckTimeoutS":30,
                  "rayActorOptions":{
                     "runtimeEnv":{
                        "env_vars":"map[]",
                        "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
                     "nodeIp":"10.244.2.3",
                     "actorId":"0a29af7ba1777655ed63d6b701000000",
                     "actorName":"SERVE_REPLICA::math_app#create_order#IadAbU",
                     "workerId":"9c452e868e05f16ad10d71b92d76f1285efaccb6bf902238c7e4fc9f",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_create_order_math_app#create_order#IadAbU.log",
                     "replicaId":"math_app#create_order#IadAbU",
                     "pid":328,
                     "startTimeS":1702216400
                  }
               ]
            }
         },
         "deployedAppConfig":{
            "name":"math_app",
            "routePrefix":"/calc",
            "importPath":"conditional_dag.serve_dag",
            "runtimeEnv":{
               "working_dir":"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
            },
            "deployments":[
               {
                  "name":"Adder",
                  "numReplicas":1,
                  "userConfig":{
                     "increment":"3"
                  },
                  "rayActorOptions":{
                     "numCpus":0.1
                  }
               },
               {
                  "name":"Multiplier",
                  "numReplicas":1,
                  "userConfig":{
                     "factor":"5"
                  },
                  "rayActorOptions":{
                     "numCpus":0.1
                  }
               },
               {
                  "name":"Router",
                  "numReplicas":1,
                  "rayActorOptions":{
                     
                  }
               },
               {
                  "name":"create_order",
                  "numReplicas":1,
                  "rayActorOptions":{
                     
                  }
               },
               {
                  "name":"DAGDriver",
                  "numReplicas":1,
                  "rayActorOptions":{
                     
                  }
               }
            ]
         }
      }
   },
   "proxies":{
      "4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba":{
         "nodeId":"4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
         "nodeIp":"10.244.2.3",
         "actorId":"985d4e1b1115bfd8aadfcd5201000000",
         "actorName":"SERVE_CONTROLLER_ACTOR:SERVE_PROXY_ACTOR-4def7ebc8f2307aeb166f3a9585c7d7ed0d974506cc6d764d812b2ba",
         "workerId":"3ca22e41adcba21db3f155c344945306c600a0db994f3e8d9349c523",
         "status":"HEALTHY",
         "logFilePath":"/serve/proxy_10.244.2.3.log"
      },
      "e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26":{
         "nodeId":"e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
         "nodeIp":"10.244.1.2",
         "actorId":"a6a37816bca13d25d88a900201000000",
         "actorName":"SERVE_CONTROLLER_ACTOR:SERVE_PROXY_ACTOR-e986907a0656721d0870c9797ac752edf97ea401599e76ababa68e26",
         "workerId":"d8ea829af0423c1dcf640d4531c284ab5b2f74b6ec764a6e75d64f98",
         "status":"HEALTHY",
         "logFilePath":"/serve/proxy_10.244.1.2.log"
      }
   }
}
```

### Delete Serve applications

Finally, you can delete serve applications using the following command:

```shell
curl -X DELETE 'localhost:31888/apis/v1/namespaces/default/serveapplication/test-cluster' \
--header 'Content-Type: application/json' 
```


You can validate job deletion by looking at the Ray dashboard (serve pane) and ensuring that it was removed

## Managing RayServe with the API server vs RayService CRD

[RayService CRD](https://docs.ray.io/en/latest/cluster/kubernetes/getting-started/rayservice-quick-start.html#kuberay-rayservice-quickstart) provides many important features, including:

* In-place updating for Ray Serve applications: See RayService for more details.
* Zero downtime upgrading for Ray clusters: See RayService for more details.
* High-availability services: See [RayCluter high availability](HACluster.md) for more details.

So why this implementation? Several reasons:

* It is more convenient in development. You can create a cluster and then deploy/undeploy applications until you are happy with results.
* You can create Ray cluster for serve with the set of features that you want, including [high availability](HACluster.md), [autoscaling support](Autoscaling.md), etc. You can choose cluster configuration differently for testing vs production. Moreover, all of this can be done using [Python](../clients/python-apiserver-client/python_apiserver_client)
* When it comes to upgrading Ray cluster or model in production, using in place update is dangerous. The preferred way of doing it is usage of [traffic splitting](https://gateway-api.sigs.k8s.io/guides/traffic-splitting/), more specifically [canary deployments](https://codefresh.io/learn/software-deployment/what-are-canary-deployments/). This allows to validate new deployments on a small percentage of data, easily rolling back in the case of issues. Managing RayServe with the API server gives one all the basic tools for such implementation and combined with, for example [gateway APIs](https://gateway-api.sigs.k8s.io/) can provide a complete solution for updates management.
