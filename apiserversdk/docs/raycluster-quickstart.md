# RayCluster QuickStart

This document focuses on explaining how to manage and interact with RayCluster using the
KubeRay APIServer. For a detailed introduction and more advanced usage with Kubernetes,
please refer to [this
guide](https://docs.ray.io/en/latest/cluster/kubernetes/getting-started/raycluster-quick-start.html).

> [!IMPORTANT]
> If you encounter any problems while following this guide, please refer to the [Troubleshooting](../Troubleshooting.md)
> page.

## Preparation

- Install [Helm](https://helm.sh/docs/intro/install/) (>= v3.4),
[Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation), and
[Docker](https://docs.docker.com/engine/install/).
- KubeRay v1.4.0 or higher and Ray 2.41.0.
- Make sure your Kubernetes cluster has at least 4 CPU and 4 GB RAM.

## Step 1: Create a Kubernetes cluster

This step creates a local Kubernetes cluster using [Kind](https://kind.sigs.k8s.io/). If you already have a Kubernetes
cluster, you can skip this step.

```sh
kind create cluster --image=kindest/node:v1.26.0
```

## Step 2: Install KubeRay operator and APIServer

Follow [Installation Guide](../Installation.md) to install the latest stable KubeRay operator and APIServer
from the Helm repository.

## Step 3: Deploy a RayCluster custom resource

Once the KubeRay operator is running, you are ready to deploy a RayCluster. While we are using APIServer, we can do this
with curl. The following command will create a RayCluster CR named `raycluster-kuberay` in your current cluster:

```sh
curl -s https://raw.githubusercontent.com/ray-project/kuberay/master/ray-operator/config/samples/ray-cluster.sample.yaml | \
curl -X POST http://localhost:31888/apis/ray.io/v1/namespaces/default/rayclusters \
  -H "Content-Type: application/yaml" \
  --data-binary @-
```

Once the RayCluster CR has been created, you can view its details by executing the following command:

```sh
curl http://localhost:31888/apis/ray.io/v1/namespaces/default/rayclusters/raycluster-kuberay
```

## Step 4: Modify Created RayCluster

To modify the created RayCluster, we can use the `PATCH` method of the KubeRay APIServer.
The following command adds an `annotation` to the raycluster-kuberay resource:

```sh
curl -X PATCH 'http://localhost:31888/apis/ray.io/v1/namespaces/default/rayclusters/raycluster-kuberay' \
-H 'Content-Type: application/merge-patch+json' \
--data '{
  "metadata": {
    "annotations": {
      "example.com/purpose": "model-training"
    }
  }
}'
```

You can verify if the `annotation` is added with the following command. You should see the
annotations you added in the output:

```sh
curl -s http://localhost:31888/apis/ray.io/v1/namespaces/default/rayclusters/raycluster-kuberay \
  | jq '.metadata.annotations'
```

## Step 5: Delete the RayCluster

To delete the RayCluster with KubeRay APIServer, execute the following command. The `raycluster-kuberay` is the name of
the RayCluster that we created earlier:

```sh
curl -X DELETE 'localhost:31888/apis/ray.io/v1/namespaces/default/rayclusters/raycluster-kuberay'
```

You can then verify if the RayCluster is removed. The following command should return 404:

```sh
curl -s http://localhost:31888/apis/ray.io/v1/namespaces/default/rayclusters/raycluster-kuberay
```

## Clean up

```sh
kind delete cluster
```
