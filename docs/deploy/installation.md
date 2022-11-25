## Installation

Make sure your Kubernetes cluster and Kubectl are both at version at least 1.19.

#### Nightly version

```
export KUBERAY_VERSION=master
kubectl create -k "github.com/ray-project/kuberay/manifests/cluster-scope-resources?ref=${KUBERAY_VERSION}&timeout=90s"
kubectl apply -k "github.com/ray-project/kuberay/manifests/base?ref=${KUBERAY_VERSION}&timeout=90s"
```

#### Stable version

```
kubectl create -k "github.com/ray-project/kuberay/manifests/cluster-scope-resources?ref=v0.3.0"
kubectl apply -k "github.com/ray-project/kuberay/manifests/base?ref=v0.3.0"
```

> Observe that we must use `kubectl create` to install cluster-scoped resources.
> The corresponding `kubectl apply` command will not work. See [KubeRay issue #271](https://github.com/ray-project/kuberay/issues/271).

#### Single Namespace version

It is possible that the user can only access one single namespace while deploying KubeRay. To deploy KubeRay in a single namespace, the user
can use following commands.

```
# Nightly version
export KUBERAY_NAMESPACE=<my-awesome-namespace>
# executed by cluster admin
kustomize build "github.com/ray-project/kuberay/manifests/overlays/single-namespace-resources" | envsubst | kubectl create -f -
# executed by user
kustomize build "github.com/ray-project/kuberay/manifests/overlays/single-namespace" | envsubst | kubectl apply -f -
```
