# RayJob QuickStart

RayJob automatically creates the RayCluster, submits the job when ready, and can
optionally delete the RayCluster after the job finishes. This document focus on explaining
how to manage and interact with RayJob using the KubeRay APIServer. For detailed
introduction and more advanced usage with Kubernetes, please refer to [this
guide](https://docs.ray.io/en/latest/cluster/kubernetes/getting-started/rayjob-quick-start.html).

> [!IMPORTANT]
> If you encounter any problems while following this guide, please refer to the [Troubleshooting](../Troubleshooting.md)
> page.

## Preparation

- Install [Helm](https://helm.sh/docs/intro/install/) (>= v3.4),
[Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation), and
[Docker](https://docs.docker.com/engine/install/).
- KubeRay v1.4.0 or higher and Ray 2.41.0.

## Step 1: Create a Kubernetes cluster

This step creates a local Kubernetes cluster using [Kind](https://kind.sigs.k8s.io/). If you already have a Kubernetes
cluster, you can skip this step.

```sh
kind create cluster --image=kindest/node:v1.26.0
```

## Step 2: Install KubeRay operator and APIServer

Follow [Installation Guide](../Installation.md) to install the latest stable KubeRay operator and APIServer
 from the Helm repository.

## Step 3: Install a RayJob

Once the KubeRay operator is running, we can install a RayJob through APIServer with
following command. This will create a RayJob named `rayjob-interactive-mode`:

```sh
curl -s https://raw.githubusercontent.com/ray-project/kuberay/master/ray-operator/config/samples/ray-job.interactive-mode.yaml \
  | curl -X POST http://localhost:31888/apis/ray.io/v1/namespaces/default/rayjobs \
    -H "Content-Type: application/yaml" \
    --data-binary @-
```

## Step 4: Check RayJob Status

You can check the detail of the submitted RayJob by executing following command:

```sh
curl -s http://localhost:31888/apis/ray.io/v1/namespaces/default/rayjobs/rayjob-interactive-mode
```

## Step 5: Delete the RayJob

To delete the RayJob with KubeRay APIServer, execute the following command. The `rayjob-interactive-mode` is the name of
the RayJob we created.

```sh
curl -X DELETE 'localhost:31888/apis/ray.io/v1/namespaces/default/rayjobs/rayjob-interactive-mode'
```

You can then verify if the RayJob is removed. The following command should return 404:

```sh
curl -s http://localhost:31888/apis/ray.io/v1/namespaces/default/rayjobs/rayjob-interactive-mode
```

## Clean up

```sh
kind delete cluster
```
