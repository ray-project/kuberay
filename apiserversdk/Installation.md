# KubeRay APIServer SDK Installation

## Step 1: Create a Kubernetes cluster

This step creates a local Kubernetes cluster using [Kind](https://kind.sigs.k8s.io/). If you already have a Kubernetes
cluster, you can skip this step.

```sh
kind create cluster --image=kindest/node:v1.26.0
```

## Step 2: Deploy a KubeRay operator

Follow [this
document](https://docs.ray.io/en/latest/cluster/kubernetes/getting-started/kuberay-operator-installation.html#kuberay-operator-deploy)
to install the latest stable KubeRay operator from the Helm repository.

## Step 3: Install APIServer SDK with Helm

```sh
helm repo add kuberay https://ray-project.github.io/kuberay-helm/
helm repo update
# Install both CRDs and KubeRay APIServer SDK
helm install kuberay-apiserver-sdk kuberay/kuberay-apiserver-sdk --version 1.0.0
```

## Step 4: Validate installation

Check that the KubeRay API Server is running in the "default" namespaces.

```sh
kubectl get pods
# NAME                        READY   STATUS    RESTARTS   AGE
# kuberay-apiserver-xxxxxx    1/1     Running   0          17s
```
