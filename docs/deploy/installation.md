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
