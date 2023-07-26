## Installation

Make sure your Kubernetes cluster and Kubectl are both at version at least 1.19.

### Nightly version

```sh
export KUBERAY_VERSION=master

# Install CRDs
kubectl create -k "github.com/ray-project/kuberay/manifests/cluster-scope-resources?ref=${KUBERAY_VERSION}&timeout=90s"

# Install KubeRay operator
kubectl apply -k "github.com/ray-project/kuberay/manifests/base?ref=${KUBERAY_VERSION}&timeout=90s"
```

### Stable version
#### Method 1: Install charts from Helm repository
```sh
helm repo add kuberay https://ray-project.github.io/kuberay-helm/

# Install both CRDs and KubeRay operator
helm install kuberay-operator kuberay/kuberay-operator
```

#### Method 2: Kustomize
```sh
# Install CRDs
kubectl create -k "github.com/ray-project/kuberay/manifests/cluster-scope-resources?ref=v0.6.0&timeout=90s"

# Install KubeRay operator
kubectl apply -k "github.com/ray-project/kuberay/manifests/base?ref=v0.6.0"
```

> Observe that we must use `kubectl create` to install cluster-scoped resources.
> The corresponding `kubectl apply` command will not work. See [KubeRay issue #271](https://github.com/ray-project/kuberay/issues/271).

### Single Namespace version

Users can use the following commands to deploy KubeRay operator in a specific namespace.

```sh
export KUBERAY_NAMESPACE=<my-awesome-namespace>

# Install CRDs (Executed by cluster admin)
kustomize build "github.com/ray-project/kuberay/manifests/overlays/single-namespace-resources?ref=v0.6.0" | envsubst | kubectl create -f -

# Install KubeRay operator (Executed by user)
kustomize build "github.com/ray-project/kuberay/manifests/overlays/single-namespace?ref=v0.6.0" | envsubst | kubectl apply -f -
```
