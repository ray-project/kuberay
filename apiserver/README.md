# KubeRay APIServer

KubeRay APIServer provides the gRPC and HTTP API to manage kuberay resources.

## Usage

### Compute Template

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
