## Installation

#### Nightly version

```
kubectl create -k "github.com/ray-project/kuberay/manifests/cluster-scope-resources"
kubectl apply -k "github.com/ray-project/kuberay/manifests/base"
```

#### Stable version

```
kubectl create -k "github.com/ray-project/kuberay/manifests/cluster-scope-resources?ref=v0.2.0"
kubectl apply -k "github.com/ray-project/kuberay/manifests/base?ref=v0.2.0"
```

> Observe that we must use `kubectl create` to install cluster-scoped resources.
> The corresponding `kubectl apply` command will not work. This is tracked in [KubeRay issue #271](https://github.com/ray-project/kuberay/issues/271).
