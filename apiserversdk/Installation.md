# KubeRay APIServer Installation

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

## Step 3: Install APIServer with Helm

### With security proxy

- Install a stable version via Helm repository

```sh
helm repo add kuberay https://ray-project.github.io/kuberay-helm/
helm repo update
# Install KubeRay APIServer
helm install kuberay-apiserver kuberay/kuberay-apiserver --version 1.4.0
```

- Install the nightly version

```sh
# Step1: Clone KubeRay repository

# Step2: Navigate to `helm-chart/kuberay-apiserver`

# Step3: Install the KubeRay apiserver
helm install kuberay-apiserver .
```

> [!IMPORTANT]
> You may receive an "Unauthorized" error when making a request if you install the
> APIServer with security proxy. Please add an authorization header to the request when
> getting the error: `-H 'Authorization: 12345'`.

### Without security proxy

- Install a stable version via Helm repository

```sh
helm repo add kuberay https://ray-project.github.io/kuberay-helm/
helm repo update
# Install KubeRay APIServer without security proxy
helm install kuberay-apiserver kuberay/kuberay-apiserver --version 1.4.0 --set security=null
```

- Install the nightly version

```sh
# Step1: Clone KubeRay repository

# Step2: Navigate to `helm-chart/kuberay-apiserver`
cd helm-chart/kuberay-apiserver

# Step3: Install the KubeRay apiserver
helm install kuberay-apiserver . --set security=null
```

## Step 4: Validate installation

Check that the KubeRay APIServer is running in the "default" namespace.

```sh
kubectl get pods
# NAME                            READY   STATUS    RESTARTS   AGE
# kuberay-apiserver-xxxxxx    1/1     Running   0          17s
```
