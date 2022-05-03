# KubeRay APIServer

KubeRay APIServer provides the gRPC and HTTP API to manage kuberay resources.

## Usage

### Compute Template

#### Create compute templates
```
POST {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/compute_templates
```

```
{
  "name": "default-template",
  "namespace": "<namespace>",
  "cpu": 2,
  "memory": 4,
  "gpu": 1,
  "gpuAccelerator": "Tesla-V100"
}
```

#### List all compute templates

```
GET {{baseUrl}}/apis/v1alpha2/namespaces/<namespace>/compute_templates
```

```
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
```
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
    "workerGroupSepc": [
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

```
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
                "worker_group_sepc": [
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
