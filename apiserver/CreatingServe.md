# Creating a cluster with RayServe support

Currently, there are two ways to create a RayCluster with RayService support:

1. Create a RayCluster first, then use [Create RayService](./HttpRequestSpec.md#Create-ray-service-in-a-given-namespace)
   APIs to create a service.
2. Directly create a cluster with RayServe support (described in this document)

To create a cluster with RayService support, simply add the following annotation when
creating the cluster:

```json
"annotations" : {
    "ray.io/enable-serve-service": "true"
  },
```

## Setup

Refer to the [Install with Helm](README.md#install-with-helm) section in the README for
setting up the KubeRay operator and APIServer, and port-forward the HTTP endpoint to local
port 31888.

## Example

This example walks through how to create a cluster with RayService support.

Before proceeding with the example, remove any running RayClusters to ensure a successful
execution of the steps below.

```sh
kubectl delete raycluster --all
```

> [!IMPORTANT]
> All the following guidance requires you to switch your working directory to the KubeRay `apiserver`

### Install ConfigMap

Install this [ConfigMap], which contains the code for our example. Simply download
the file and run:

```sh
kubectl apply -f test/job/code.yaml
```

Check if the ConfigMap is successfully created. You should see `ray-job-code-sample` in
the list:

```sh
kubectl get configmaps
# NAME                  DATA   AGE
# ray-job-code-sample   1      13s
```

### Create RayCluster

Use the following commands to create a compute template and a RayCluster with RayService support:

```sh
# Create compute tempalte
curl -X POST 'localhost:31888/apis/v1/namespaces/default/compute_templates' \
--header 'Content-Type: application/json' \
--data  @docs/api-example/compute_template.json

curl -X POST 'localhost:31888/apis/v1/namespaces/default/clusters' \
  --header 'Content-Type: application/json' \
  --data @docs/api-example/create_serving_clusters.json
```

To confirm that the cluster is created correctly, check the created services using the following command:

```sh
kubectl get service
# NAME                     TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)                                                   AGE
# test-cluster-head-svc    ClusterIP   None            <none>        8265/TCP,52365/TCP,10001/TCP,8080/TCP,6379/TCP,8000/TCP   7s
# test-cluster-serve-svc   ClusterIP   10.96.161.26    <none>        8000/TCP                                                  7s
```

You should see two services created: one for the head node to access the dashboard and manage the
cluster, and the other to submit the serve request.

Note that we set the 52365 port for the dashboard agent in the above curl command. This port is
used internally by Ray Serve.

### Clean up

```sh
make clean-cluster
# Remove APIServer from helm
helm uninstall kuberay-apiserver
```

[ConfigMap]: test/job/code.yaml
