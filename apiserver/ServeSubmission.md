# Managing RayServe with the API server

Ray Serve is a scalable model serving library for building online inference APIs. This document describes creation and management of Ray Serve enabled Ray clusters

## Deploy KubeRay operator and API server

Refer to [readme](README.md) for setting up KubRay operator and API server.

```shell
make operator-image cluster load-operator-image deploy-operator install
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
  "configyaml": "applications:\n  - name: fruit_app\n    import_path: fruit.deployment_graph\n    route_prefix: /fruit\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip\"\n    deployments:\n      - name: MangoStand\n        num_replicas: 2\n        max_replicas_per_node: 1\n        user_config:\n          price: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: OrangeStand\n        num_replicas: 1\n        user_config:\n          price: 2\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: PearStand\n        num_replicas: 1\n        user_config:\n          price: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: FruitMarket\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n  - name: math_app\n    import_path: conditional_dag.serve_dag\n    route_prefix: /calc\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip\"\n    deployments:\n      - name: Adder\n        num_replicas: 1\n        user_config:\n          increment: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Multiplier\n        num_replicas: 1\n        user_config:\n          factor: 5\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Router\n        num_replicas: 1\n"
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
      "nodeId":"836e497ae6b3a1bf1e775689634807b2176ef57d6817b8a82b3511d6",
      "nodeIp":"10.244.3.2",
      "actorId":"1a7dcb5f48bb6dff6dcd0fb601000000",
      "actorName":"SERVE_CONTROLLER_ACTOR",
      "logFilePath":"/serve/controller_1111.log"
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
         "lastDeployedTimeS":1705573500,
         "deployments":{
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
                        "working_dir":"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"836e497ae6b3a1bf1e775689634807b2176ef57d6817b8a82b3511d6",
                     "nodeIp":"10.244.3.2",
                     "actorId":"ddff7b1174e60ce343fceba901000000",
                     "actorName":"SERVE_REPLICA::fruit_app#FruitMarket#HUTrZX",
                     "workerId":"2b2c95ba76e4d4d2ee4fd48e6e4172cc341f83c77d164899318cbd7f",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_FruitMarket_fruit_app#FruitMarket#HUTrZX.log",
                     "replicaId":"fruit_app#FruitMarket#HUTrZX",
                     "pid":1302,
                     "startTimeS":1705573500
                  }
               ]
            },
            "MangoStand":{
               "name":"MangoStand",
               "status":"HEALTHY",
               "deploymentConfig":{
                  "name":"MangoStand",
                  "numReplicas":2,
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
                        "working_dir":"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"836e497ae6b3a1bf1e775689634807b2176ef57d6817b8a82b3511d6",
                     "nodeIp":"10.244.3.2",
                     "actorId":"82db549c9d78ef2814ea3ac101000000",
                     "actorName":"SERVE_REPLICA::fruit_app#MangoStand#YDMOaG",
                     "workerId":"e756b692b56008da7e2499c7a15806b88db90da16ebd37bf6dba791a",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_MangoStand_fruit_app#MangoStand#YDMOaG.log",
                     "replicaId":"fruit_app#MangoStand#YDMOaG",
                     "pid":1256,
                     "startTimeS":1705573500
                  },
                  {
                     "nodeId":"12d09d1146b4c2d03a01de826c5601487c33224b50e80d1f2c25906d",
                     "nodeIp":"10.244.1.3",
                     "actorId":"d0261419969c3e7abbe3767d01000000",
                     "actorName":"SERVE_REPLICA::fruit_app#MangoStand#kpIBiU",
                     "workerId":"a717086f84ea4cb0ca05ea2a9442ca32a06ca6009a42c21af0e0ea22",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_MangoStand_fruit_app#MangoStand#kpIBiU.log",
                     "replicaId":"fruit_app#MangoStand#kpIBiU",
                     "pid":713,
                     "startTimeS":1705573500
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
                        "working_dir":"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"836e497ae6b3a1bf1e775689634807b2176ef57d6817b8a82b3511d6",
                     "nodeIp":"10.244.3.2",
                     "actorId":"bba7e0c617469b59ec2cd4a801000000",
                     "actorName":"SERVE_REPLICA::fruit_app#OrangeStand#CFkCCJ",
                     "workerId":"18a08186aaec24bde29f88f307548ec37491d47eacf1592cc0235039",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_OrangeStand_fruit_app#OrangeStand#CFkCCJ.log",
                     "replicaId":"fruit_app#OrangeStand#CFkCCJ",
                     "pid":1257,
                     "startTimeS":1705573500
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
                        "working_dir":"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"12d09d1146b4c2d03a01de826c5601487c33224b50e80d1f2c25906d",
                     "nodeIp":"10.244.1.3",
                     "actorId":"218ddd8886a11964535e34ab01000000",
                     "actorName":"SERVE_REPLICA::fruit_app#PearStand#mFuMsA",
                     "workerId":"49fad5d40ef7e1b0aeb1ac319f263b18b991eff20bdefc6c234328cc",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_PearStand_fruit_app#PearStand#mFuMsA.log",
                     "replicaId":"fruit_app#PearStand#mFuMsA",
                     "pid":714,
                     "startTimeS":1705573500
                  }
               ]
            }
         },
         "deployedAppConfig":{
            "name":"fruit_app",
            "routePrefix":"/fruit",
            "importPath":"fruit.deployment_graph",
            "runtimeEnv":{
               "working_dir":"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
            },
            "deployments":[
               {
                  "name":"MangoStand",
                  "numReplicas":2,
                  "maxReplicasPerNode":1,
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
               }
            ]
         }
      },
      "math_app":{
         "name":"math_app",
         "status":"RUNNING",
         "routePrefix":"/calc",
         "lastDeployedTimeS":1705573500,
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
                        "working_dir":"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"836e497ae6b3a1bf1e775689634807b2176ef57d6817b8a82b3511d6",
                     "nodeIp":"10.244.3.2",
                     "actorId":"e298385c0866a18893aabfba01000000",
                     "actorName":"SERVE_REPLICA::math_app#Adder#rQsBou",
                     "workerId":"de18047878c29bf5ac74a6d5d57a4985012e8b5a5348f95075430da7",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_Adder_math_app#Adder#rQsBou.log",
                     "replicaId":"math_app#Adder#rQsBou",
                     "pid":1327,
                     "startTimeS":1705573500
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
                        "working_dir":"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"12d09d1146b4c2d03a01de826c5601487c33224b50e80d1f2c25906d",
                     "nodeIp":"10.244.1.3",
                     "actorId":"ceefb38fcfcaec9aed02ff8701000000",
                     "actorName":"SERVE_REPLICA::math_app#Multiplier#YAwdhS",
                     "workerId":"390f78c6a0293b5c988fb961c660f591bdc0d962131a8becc56c0a27",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_Multiplier_math_app#Multiplier#YAwdhS.log",
                     "replicaId":"math_app#Multiplier#YAwdhS",
                     "pid":742,
                     "startTimeS":1705573500
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
                        "working_dir":"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
                     },
                     "numCpus":0.1
                  }
               },
               "replicas":[
                  {
                     "nodeId":"12d09d1146b4c2d03a01de826c5601487c33224b50e80d1f2c25906d",
                     "nodeIp":"10.244.1.3",
                     "actorId":"fb2209a2b08c874f26bb047101000000",
                     "actorName":"SERVE_REPLICA::math_app#Router#ajQmlX",
                     "workerId":"2df7677d14bcb8de4b1e9e2846c212862a2d9270f2a8729aafeb2dcb",
                     "state":"RUNNING",
                     "logFilePath":"/serve/deployment_Router_math_app#Router#ajQmlX.log",
                     "replicaId":"math_app#Router#ajQmlX",
                     "pid":765,
                     "startTimeS":1705573500
                  }
               ]
            }
         },
         "deployedAppConfig":{
            "name":"math_app",
            "routePrefix":"/calc",
            "importPath":"conditional_dag.serve_dag",
            "runtimeEnv":{
               "working_dir":"https://github.com/ray-project/test_dag/archive/78b4a5da38796123d9f9ffff59bab2792a043e95.zip"
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
               }
            ]
         }
      }
   },
   "proxies":{
      "12d09d1146b4c2d03a01de826c5601487c33224b50e80d1f2c25906d":{
         "nodeId":"12d09d1146b4c2d03a01de826c5601487c33224b50e80d1f2c25906d",
         "nodeIp":"10.244.1.3",
         "actorId":"0af2772b7fea74a14dd7304b01000000",
         "actorName":"SERVE_CONTROLLER_ACTOR:SERVE_PROXY_ACTOR-12d09d1146b4c2d03a01de826c5601487c33224b50e80d1f2c25906d",
         "workerId":"678f17e6f183c80228b4e645f1075cf436ad53cb1e7e5bf06ca5b151",
         "status":"HEALTHY",
         "logFilePath":"/serve/proxy_10.244.1.3.log"
      },
      "836e497ae6b3a1bf1e775689634807b2176ef57d6817b8a82b3511d6":{
         "nodeId":"836e497ae6b3a1bf1e775689634807b2176ef57d6817b8a82b3511d6",
         "nodeIp":"10.244.3.2",
         "actorId":"69d4ba541cba316c67a1e0cc01000000",
         "actorName":"SERVE_CONTROLLER_ACTOR:SERVE_PROXY_ACTOR-836e497ae6b3a1bf1e775689634807b2176ef57d6817b8a82b3511d6",
         "workerId":"716dd9498012f37791ee0418b33b4ecce682642ec7e131d6bacfbc60",
         "status":"HEALTHY",
         "logFilePath":"/serve/proxy_10.244.3.2.log"
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
