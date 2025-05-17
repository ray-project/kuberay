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

### IMPORTANT: Change your working directory to `apiserver/`

All the following guidance require you to switch your working directory to the KubeRay
`apiserver`:

```sh
cd apiserver/
```

### Install ConfigMap

Please install this [ConfigMap] which contains code for our example. Simply download
this file and run:

```sh
kubectl apply -f test/job/code.yaml
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
cur
# Create compute tempalte
curl -X POST 'localhost:31888/apis/v1/namespaces/default/compute_templates' \
--header 'Content-Type: application/json' \
--data  @docs/api-example/compute_template.json

curl -X POST 'localhost:31888/apis/v1/namespaces/default/clusters' \
  --header 'Content-Type: application/json' \
  --data @docs/api-example/create_serving_clusters.json
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

Note that we set the 52365 port for dashboard agent in the above curl command, which is
used internally by Ray Serve.

### Clean up

```sh
make clean-cluster
# Remove apiserver from helm
helm uninstall kuberay-apiserver
```

[ConfigMap]: test/job/code.yaml
