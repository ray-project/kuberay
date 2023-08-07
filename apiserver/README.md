# KubeRay APIServer

The KubeRay APIServer provides gRPC and HTTP APIs to manage KubeRay resources.

**Note**

    The KubeRay APIServer is an optional component. It provides a layer of simplified
    configuration for KubeRay resources. The KubeRay API server is used internally
    by some organizations to back user interfaces for KubeRay resource management.

    The KubeRay APIServer is community-managed and is not officially endorsed by the
    Ray maintainers. At this time, the only officially supported methods for
    managing KubeRay resources are

    - Direct management of KubeRay custom resources via kubectl, kustomize, and Kubernetes language clients.
    - Helm charts.

    KubeRay APIServer maintainer contacts (GitHub handles):
    @Jeffwan @scarlet25151

## Installation

### Helm

Make sure the version of Helm is v3+. Currently, [existing CI tests](https://github.com/ray-project/kuberay/blob/master/.github/workflows/helm-lint.yaml) are based on Helm v3.4.1 and v3.9.4.

```sh
helm version
```

### Install KubeRay APIServer

* Install a stable version via Helm repository (only supports KubeRay v0.4.0+)
  ```sh
  helm repo add kuberay https://ray-project.github.io/kuberay-helm/

  # Install KubeRay APIServer v0.6.0.
  helm install kuberay-apiserver kuberay/kuberay-apiserver --version 0.6.0

  # Check the KubeRay APIServer Pod in `default` namespace
  kubectl get pods
  # NAME                                 READY   STATUS    RESTARTS   AGE
  # kuberay-apiserver-67b46b88bf-m7dzg   1/1     Running   0          6s
  ```

* Install the nightly version
  ```sh
  # Step1: Clone KubeRay repository

  # Step2: Move to `helm-chart/kuberay-apiserver`

  # Step3: Install KubeRay APIServer
  helm install kuberay-apiserver .
  ```

### List the chart

To list the `my-release` deployment:

```sh
helm ls
# NAME                    NAMESPACE       REVISION        UPDATED                                 STATUS   CHART                    APP VERSION
# kuberay-apiserver       default         1               2023-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx deployed kuberay-apiserver-0.6.0
```

### Uninstall the Chart

```sh
# Uninstall the `kuberay-apiserver` release
helm uninstall kuberay-apiserver

# The KubeRay APIServer Pod should be removed.
kubectl get pods
# No resources found in default namespace.
```

## Usage

After the deployment we may use the `{{baseUrl}}` to access the

- (default) for nodeport access, we provide the default http port `31888` for connection and you can connect it using.

- for ingress access, you will need to create your own ingress

The requests parameters detail can be seen in [KubeRay swagger](https://github.com/ray-project/kuberay/tree/master/proto/swagger), here we only present some basic example:

### Setup end-to-end test

0. (Optional) You may use your local kind cluster or minikube

```bash
cat <<EOF | kind create cluster --name ray-test --config -
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: InitConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            node-labels: "ingress-ready=true"
    extraPortMappings:
      - containerPort: 30379
        hostPort: 6379
        listenAddress: "0.0.0.0"
        protocol: tcp
      - containerPort: 30265
        hostPort: 8265
        listenAddress: "0.0.0.0"
        protocol: tcp
      - containerPort: 30001
        hostPort: 10001
        listenAddress: "0.0.0.0"
        protocol: tcp
      - containerPort: 8000
        hostPort: 8000
        listenAddress: "0.0.0.0"
      - containerPort: 31888
        hostPort: 31888
        listenAddress: "0.0.0.0"
  - role: worker
  - role: worker
EOF
```

1. Deploy the KubeRay APIServer within the same cluster of KubeRay operator

```bash
helm repo add kuberay https://ray-project.github.io/kuberay-helm/
helm -n ray-system install kuberay-apiserver kuberay/kuberay-apiserver
```

2. The APIServer expose service using `NodePort` by default. You can test access by your host and port, the default port is set to `31888`.

```
curl localhost:31888
{"code":5, "message":"Not Found"}
```

3. You can create `RayCluster`, `RayJobs` or `RayService` by dialing the endpoints. The following is a simple example for creating the `RayService` object, follow [swagger support](https://ray-project.github.io/kuberay/components/apiserver/#swagger-support) to get the complete definitions of APIs.

```shell
curl -X POST 'localhost:31888/apis/v1alpha2/namespaces/ray-system/compute_templates' \
--header 'Content-Type: application/json' \
--data '{
  "name": "default-template",
  "namespace": "ray-system",
  "cpu": 2,
  "memory": 4
}'

curl -X POST 'localhost:31888/apis/v1alpha2/namespaces/ray-system/services' \
--header 'Content-Type: application/json' \
--data '{
  "name": "user-test-1",
  "namespace": "ray-system",
  "user": "user",
  "serveDeploymentGraphSpec": {
      "importPath": "fruit.deployment_graph",
      "runtimeEnv": "working_dir: \"https://github.com/ray-project/test_dag/archive/c620251044717ace0a4c19d766d43c5099af8a77.zip\"\n",
      "serveConfigs": [
      {
        "deploymentName": "OrangeStand",
        "replicas": 1,
        "userConfig": "price: 2",
        "actorOptions": {
          "cpusPerActor": 0.1
        }
      },
      {
        "deploymentName": "PearStand",
        "replicas": 1,
        "userConfig": "price: 1",
        "actorOptions": {
          "cpusPerActor": 0.1
        }
      },
      {
        "deploymentName": "FruitMarket",
        "replicas": 1,
        "actorOptions": {
          "cpusPerActor": 0.1
        }
      },{
        "deploymentName": "DAGDriver",
        "replicas": 1,
        "routePrefix": "/",
        "actorOptions": {
          "cpusPerActor": 0.1
        }
      }]
  },
  "clusterSpec": {
    "headGroupSpec": {
      "computeTemplate": "default-template",
      "image": "rayproject/ray:2.5.0",
      "serviceType": "NodePort",
      "rayStartParams": {
            "dashboard-host": "0.0.0.0",
            "metrics-export-port": "8080"
        },
       "volumes": []
    },
    "workerGroupSpec": [
      {
        "groupName": "small-wg",
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.5.0",
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
The Ray resource will then be created in your Kubernetes cluster.

## Full definition of payload

### Compute Template

For the purpose to simplify the setting of resource, we abstract the resource
of the pods template resource to the `compute template` for usage, you can
define the resource in the `compute template` and then choose the appropriate
template for your `head` and `workergroup` when you are creating the real objects of `RayCluster`, `RayJobs` or `RayService`.

#### Create compute templates in a given namespace

```
POST {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/compute_templates
```

```json
{
  "name": "default-template",
  "namespace": "<namespace>",
  "cpu": 2,
  "memory": 4,
  "gpu": 1,
  "gpuAccelerator": "Tesla-V100"
}
```

#### List all compute templates in a given namespace

```
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/compute_templates
```

```json
{
    "compute_templates": [
        {
            "name": "default-template",
            "namespace": "<namespace>",
            "cpu": 2,
            "memory": 4,
            "gpu": 1,
            "gpu_accelerator": "Tesla-V100"
        }
    ]
}
```

#### List all compute templates in all namespaces

```
GET {{baseUrl}}/apis/v1alpha2/compute_templates
```

#### Get compute template by name

```
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/compute_templates/<compute_template_name>
```

#### Delete compute template by name

```
DELETE {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/compute_templates/<compute_template_name>
```

### Clusters

#### Create cluster in a given namespace

```
POST {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/clusters
```

payload
```json
{
  "name": "test-cluster",
  "namespace": "<namespace>",
  "user": "jiaxin.shan",
  "version": "1.9.2",
  "environment": "DEV",
  "clusterSpec": {
    "headGroupSpec": {
      "computeTemplate": "head-template",
      "image": "ray.io/ray:1.9.2",
      "serviceType": "NodePort",
      "rayStartParams": {}
    },
    "workerGroupSpec": [
      {
        "groupName": "small-wg",
        "computeTemplate": "worker-template",
        "image": "ray.io/ray:1.9.2",
        "replicas": 2,
        "minReplicas": 0,
        "maxReplicas": 5,
        "rayStartParams": {}
      }
    ]
  }
}
```

#### List all clusters in a given namespace

```
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/clusters
```

```json
{
    "clusters": [
        {
            "name": "test-cluster",
            "namespace": "<namespace>",
            "user": "jiaxin.shan",
            "version": "1.9.2",
            "environment": "DEV",
            "cluster_spec": {
                "head_group_spec": {
                    "compute_template": "head-template",
                    "image": "rayproject/ray:1.9.2",
                    "service_type": "NodePort",
                    "ray_start_params": {
                        "dashboard-host": "0.0.0.0",
                        "node-ip-address": "$MY_POD_IP",
                        "port": "6379"
                    }
                },
                "worker_group_spec": [
                    {
                        "group_name": "small-wg",
                        "compute_template": "worker-template",
                        "image": "rayproject/ray:1.9.2",
                        "replicas": 2,
                        "min_replicas": 0,
                        "max_replicas": 5,
                        "ray_start_params": {
                            "node-ip-address": "$MY_POD_IP",
                        }
                    }
                ]
            },
            "created_at": "2022-03-13T15:13:09Z",
            "deleted_at": null
        },
    ]
}
```

#### List all clusters in all namespaces

```
GET {{baseUrl}}/apis/v1alpha2/clusters
```

#### Get cluster by its name and namespace

```
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/clusters/<cluster_name>
```


#### Delete cluster by its name and namespace

```
DELETE {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/clusters/<cluster_name>
```

### RayJob

#### Create ray job in a given namespace

```
POST {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/jobs
```


payload
```json
{
  "name": "string",
  "namespace": "string",
  "user": "string",
  "entrypoint": "string",
  "metadata": {
    "additionalProp1": "string",
    "additionalProp2": "string",
    "additionalProp3": "string"
  },
  "runtimeEnv": "string",
  "jobId": "string",
  "shutdownAfterJobFinishes": true,
  "clusterSelector": {
    "additionalProp1": "string",
    "additionalProp2": "string",
    "additionalProp3": "string"
  },
  "clusterSpec": {
    "headGroupSpec": {
      "computeTemplate": "string",
      "image": "string",
      "serviceType": "string",
      "rayStartParams": {
        "additionalProp1": "string",
        "additionalProp2": "string",
        "additionalProp3": "string"
      },
      "volumes": [
        {
          "mountPath": "string",
          "volumeType": "PERSISTENT_VOLUME_CLAIM",
          "name": "string",
          "source": "string",
          "readOnly": true,
          "hostPathType": "DIRECTORY",
          "mountPropagationMode": "NONE"
        }
      ]
    },
    "workerGroupSpec": [
      {
        "groupName": "string",
        "computeTemplate": "string",
        "image": "string",
        "replicas": 0,
        "minReplicas": 0,
        "maxReplicas": 0,
        "rayStartParams": {
          "additionalProp1": "string",
          "additionalProp2": "string",
          "additionalProp3": "string"
        },
        "volumes": [
          {
            "mountPath": "string",
            "volumeType": "PERSISTENT_VOLUME_CLAIM",
            "name": "string",
            "source": "string",
            "readOnly": true,
            "hostPathType": "DIRECTORY",
            "mountPropagationMode": "NONE"
          }
        ]
      }
    ]
  },
  "ttlSecondsAfterFinished": 0,
  "createdAt": "2022-08-19T21:20:30.494Z",
  "deleteAt": "2022-08-19T21:20:30.494Z",
  "jobStatus": "string",
  "jobDeploymentStatus": "string",
  "message": "string"
}
```

#### List all jobs in a given namespace

```
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/jobs
```

Response
```json
{
  "jobs": [
    {
      "name": "string",
      "namespace": "string",
      "user": "string",
      "entrypoint": "string",
      "metadata": {
        "additionalProp1": "string",
        "additionalProp2": "string",
        "additionalProp3": "string"
      },
      "runtimeEnv": "string",
      "jobId": "string",
      "shutdownAfterJobFinishes": true,
      "clusterSelector": {
        "additionalProp1": "string",
        "additionalProp2": "string",
        "additionalProp3": "string"
      },
      "clusterSpec": {
        "headGroupSpec": {
          "computeTemplate": "string",
          "image": "string",
          "serviceType": "string",
          "rayStartParams": {
            "additionalProp1": "string",
            "additionalProp2": "string",
            "additionalProp3": "string"
          },
          "volumes": [
            {
              "mountPath": "string",
              "volumeType": "PERSISTENT_VOLUME_CLAIM",
              "name": "string",
              "source": "string",
              "readOnly": true,
              "hostPathType": "DIRECTORY",
              "mountPropagationMode": "NONE"
            }
          ]
        },
        "workerGroupSpec": [
          {
            "groupName": "string",
            "computeTemplate": "string",
            "image": "string",
            "replicas": 0,
            "minReplicas": 0,
            "maxReplicas": 0,
            "rayStartParams": {
              "additionalProp1": "string",
              "additionalProp2": "string",
              "additionalProp3": "string"
            },
            "volumes": [
              {
                "mountPath": "string",
                "volumeType": "PERSISTENT_VOLUME_CLAIM",
                "name": "string",
                "source": "string",
                "readOnly": true,
                "hostPathType": "DIRECTORY",
                "mountPropagationMode": "NONE"
              }
            ]
          }
        ]
      },
      "ttlSecondsAfterFinished": 0,
      "createdAt": "2022-08-19T21:31:24.352Z",
      "deleteAt": "2022-08-19T21:31:24.352Z",
      "jobStatus": "string",
      "jobDeploymentStatus": "string",
      "message": "string"
    }
  ]
}
```

#### List all jobs in all namespaces

```
GET {{baseUrl}}/apis/v1alpha2/jobs
```

#### Get job by its name and namespace

```
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/jobs/<job_name>
```


#### Delete job by its name and namespace

```
DELETE {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/jobs/<job_name>
```


### RayService

#### Create ray service in a given namespace

```
POST {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/services
```

payload

```json
{
  "name": "string",
  "namespace": "string",
  "user": "string",
  "serveDeploymentGraphSpec": {
    "importPath": "string",
    "runtimeEnv": "string",
    "serveConfigs": [
      {
        "deploymentName": "string",
        "replicas": 0,
        "routePrefix": "string",
        "maxConcurrentQueries": 0,
        "userConfig": "string",
        "autoscalingConfig": "string",
        "actorOptions": {
          "runtimeEnv": "string",
          "cpus": 0,
          "gpu": 0,
          "memory": 0,
          "objectStoreMemory": 0,
          "resource": "string",
          "accceleratorType": "string"
        }
      }
    ]
  },
  "clusterSpec": {
    "headGroupSpec": {
      "computeTemplate": "string",
      "image": "string",
      "serviceType": "string",
      "rayStartParams": {
        "additionalProp1": "string",
        "additionalProp2": "string",
        "additionalProp3": "string"
      },
      "volumes": [
        {
          "mountPath": "string",
          "volumeType": "PERSISTENT_VOLUME_CLAIM",
          "name": "string",
          "source": "string",
          "readOnly": true,
          "hostPathType": "DIRECTORY",
          "mountPropagationMode": "NONE"
        }
      ]
    },
    "workerGroupSpec": [
      {
        "groupName": "string",
        "computeTemplate": "string",
        "image": "string",
        "replicas": 0,
        "minReplicas": 0,
        "maxReplicas": 0,
        "rayStartParams": {
          "additionalProp1": "string",
          "additionalProp2": "string",
          "additionalProp3": "string"
        },
        "volumes": [
          {
            "mountPath": "string",
            "volumeType": "PERSISTENT_VOLUME_CLAIM",
            "name": "string",
            "source": "string",
            "readOnly": true,
            "hostPathType": "DIRECTORY",
            "mountPropagationMode": "NONE"
          }
        ]
      }
    ]
  },
  "rayServiceStatus": {
    "applicationStatus": "string",
    "applicationMessage": "string",
    "serveDeploymentStatus": [
      {
        "deploymentName": "string",
        "status": "string",
        "message": "string"
      }
    ],
    "rayServiceEvent": [
      {
        "id": "string",
        "name": "string",
        "createdAt": "2022-08-19T21:30:01.097Z",
        "firstTimestamp": "2022-08-19T21:30:01.097Z",
        "lastTimestamp": "2022-08-19T21:30:01.097Z",
        "reason": "string",
        "message": "string",
        "type": "string",
        "count": 0
      }
    ],
    "rayClusterName": "string",
    "rayClusterState": "string",
    "serviceEndpoint": {
      "additionalProp1": "string",
      "additionalProp2": "string",
      "additionalProp3": "string"
    }
  },
  "createdAt": "2022-08-19T21:30:01.097Z",
  "deleteAt": "2022-08-19T21:30:01.097Z"
}
```

#### List all services in a given namespace

```
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/services
```

Response
```json

  "name": "string",
  "namespace": "string",
  "user": "string",
  "serveDeploymentGraphSpec": {
    "importPath": "string",
    "runtimeEnv": "string",
    "serveConfigs": [
      {
        "deploymentName": "string",
        "replicas": 0,
        "routePrefix": "string",
        "maxConcurrentQueries": 0,
        "userConfig": "string",
        "autoscalingConfig": "string",
        "actorOptions": {
          "runtimeEnv": "string",
          "cpus": 0,
          "gpu": 0,
          "memory": 0,
          "objectStoreMemory": 0,
          "resource": "string",
          "accceleratorType": "string"
        }
      }
    ]
  },
  "clusterSpec": {
    "headGroupSpec": {
      "computeTemplate": "string",
      "image": "string",
      "serviceType": "string",
      "rayStartParams": {
        "additionalProp1": "string",
        "additionalProp2": "string",
        "additionalProp3": "string"
      },
      "volumes": [
        {
          "mountPath": "string",
          "volumeType": "PERSISTENT_VOLUME_CLAIM",
          "name": "string",
          "source": "string",
          "readOnly": true,
          "hostPathType": "DIRECTORY",
          "mountPropagationMode": "NONE"
        }
      ]
    },
    "workerGroupSpec": [
      {
        "groupName": "string",
        "computeTemplate": "string",
        "image": "string",
        "replicas": 0,
        "minReplicas": 0,
        "maxReplicas": 0,
        "rayStartParams": {
          "additionalProp1": "string",
          "additionalProp2": "string",
          "additionalProp3": "string"
        },
        "volumes": [
          {
            "mountPath": "string",
            "volumeType": "PERSISTENT_VOLUME_CLAIM",
            "name": "string",
            "source": "string",
            "readOnly": true,
            "hostPathType": "DIRECTORY",
            "mountPropagationMode": "NONE"
          }
        ]
      }
    ]
  },
  "rayServiceStatus": {
    "applicationStatus": "string",
    "applicationMessage": "string",
    "serveDeploymentStatus": [
      {
        "deploymentName": "string",
        "status": "string",
        "message": "string"
      }
    ],
    "rayServiceEvent": [
      {
        "id": "string",
        "name": "string",
        "createdAt": "2022-08-19T21:33:15.485Z",
        "firstTimestamp": "2022-08-19T21:33:15.485Z",
        "lastTimestamp": "2022-08-19T21:33:15.485Z",
        "reason": "string",
        "message": "string",
        "type": "string",
        "count": 0
      }
    ],
    "rayClusterName": "string",
    "rayClusterState": "string",
    "serviceEndpoint": {
      "additionalProp1": "string",
      "additionalProp2": "string",
      "additionalProp3": "string"
    }
  },
  "createdAt": "2022-08-19T21:33:15.485Z",
  "deleteAt": "2022-08-19T21:33:15.485Z"
}
```

#### List all services in all namespaces

```
GET {{baseUrl}}/apis/v1alpha2/services
```

#### Get service by its name and namespace

```
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/services/<service_name>
```


#### Delete service by its name and namespace

```
DELETE {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/services/<service_name>
```

## Swagger Support

1. Download Swagger UI from [Swagger-UI](https://swagger.io/tools/swagger-ui/download/). In this case, we use `swagger-ui-3.51.2.tar.gz`
2. Unzip package and copy `dist` folder to `third_party` folder
3. Use `go-bindata` to generate go code from static files.

```
mkdir third_party
tar -zvxf ~/Downloads/swagger-ui-3.51.2.tar.gz /tmp
mv /tmp/swagger-ui-3.51.2/dist  third_party/swagger-ui

cd apiserver/
go-bindata --nocompress --pkg swagger -o pkg/swagger/datafile.go ./third_party/swagger-ui/...
```
