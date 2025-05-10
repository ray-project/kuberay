# Creating a cluster with RayServe support

Currently, there are two ways for creating a RayCluster with RayService support:

1. Create a RayCluster first, then use [Create RayService](./HttpRequestSpec.md#Create-ray-service-in-a-given-namespace)
   APIs to create a service.
2. Directly create cluster with RayServe support (described in this document)

For creating a cluster with RayService support, simply add the following annotation when
creating the cluster:

```json
"annotations" : {
    "ray.io/enable-serve-service": "true"
  },
```

## Setup

Refer to [README](README.md) for setting up KubRay operator and API server.

## Example

This example walks through how to create a cluster with RayService support.

Before going through the example, remove any running RayClusters to ensure a successful
run through of the example below.

```sh
kubectl delete raycluster --all
```

### Install ConfigMap

Please install this [ConfigMap] which contains code for our example. Simply download
this file and run:

```sh
kubectl apply -f code.yaml
```

Check if the config map is successfully created, you should see `ray-job-code-sample` in
the list:

```sh
kubectl get configmaps
# NAME                  DATA   AGE
# ray-job-code-sample   1      13s
```

### Create RayCluster

Use following command to create a compute template and a RayCluster with RayService support:

```sh
curl -X POST 'localhost:31888/apis/v1/namespaces/default/compute_templates' \
  --header 'Content-Type: application/json' \
  --data '{
    "name": "default-template",
    "namespace": "default",
    "cpu": 2,
    "memory": 4
  }'
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
        "image": "rayproject/ray:2.9.0-py310",
        "serviceType": "ClusterIP",
        "rayStartParams": {
           "dashboard-host": "0.0.0.0",
           "metrics-export-port": "8080",
           "dashboard-agent-listen-port": "52365"
         },
         "volumes": [
           {
             "name": "code-sample",
             "mountPath": "/home/ray/samples",
             "volumeType": "CONFIGMAP",
             "source": "ray-job-code-sample",
             "items": {"sample_code.py" : "sample_code.py"}
           }
         ]
      },
      "workerGroupSpec": [
        {
          "groupName": "small-wg",
          "computeTemplate": "default-template",
          "image": "rayproject/ray:2.9.0-py310",
          "replicas": 1,
          "minReplicas": 0,
          "maxReplicas": 5,
          "rayStartParams": {
             "node-ip-address": "$MY_POD_IP"
           },
          "volumes": [
            {
              "name": "code-sample",
              "mountPath": "/home/ray/samples",
              "volumeType": "CONFIGMAP",
              "source": "ray-job-code-sample",
              "items": {"sample_code.py" : "sample_code.py"}
            }
          ]
        }
      ]
    }
  }'
```

To confirm that the cluster is created correctly, check created services using that following command:

```sh
kubectl get service
# NAME                     TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)                                                   AGE
# test-cluster-head-svc    ClusterIP   None            <none>        8265/TCP,52365/TCP,10001/TCP,8080/TCP,6379/TCP,8000/TCP   7s
# test-cluster-serve-svc   ClusterIP   10.96.161.26    <none>        8000/TCP                                                  7s
```

You should see two services created, one for head node to access dashboard and manage the
cluster, the other for submit the serve request.

Note that the 52365 port for head node service is for serve configuration.

### Clean up

```sh
make clean-cluster
# Remove apiserver from helm
helm uninstall kuberay-apiserver
```

[ConfigMap]: test/job/code.yaml
