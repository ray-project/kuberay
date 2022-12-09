# KubeRay

[![Build Status](https://github.com/ray-project/kuberay/workflows/Go-build-and-test/badge.svg)](https://github.com/ray-project/kuberay/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/ray-project/kuberay)](https://goreportcard.com/report/github.com/ray-project/kuberay)

KubeRay is an open source toolkit to run Ray applications on Kubernetes.
It provides several tools to simplify managing Ray clusters on Kubernetes.

- Ray Operator
- Backend services to create/delete cluster resources
- Kubectl plugin/CLI to operate CRD objects
- Native Job and Serving integration with Clusters
- Data Scientist centric workspace for fast prototyping (incubating)
- Kubernetes event dumper for ray clusters/pod/services (future work)
- Operator Integration with Kubernetes node problem detector (future work)

## Documentation

You can view detailed documentation and guides at [https://ray-project.github.io/kuberay/](https://ray-project.github.io/kuberay/).

We also recommend checking out the official Ray guides for deploying on Kubernetes at [https://docs.ray.io/en/latest/cluster/kubernetes/index.html](https://docs.ray.io/en/latest/cluster/kubernetes/index.html).

## Quick Start

### Use YAML

Please choose the version you would like to install. The examples below use the latest stable version `v0.4.0`.

| Version  |  Stable |  Suggested Kubernetes Version |
|----------|:-------:|------------------------------:|
|  master  |    N    | v1.19 - v1.25 |
|  v0.4.0  |    Y    | v1.19 - v1.25 |

Make sure your Kubernetes and Kubectl versions are both within the suggested range.
Once you have connected to a Kubernetes cluster, run the following commands to deploy the KubeRay Operator.
```
export KUBERAY_VERSION=v0.4.0
kubectl create -k "github.com/ray-project/kuberay/ray-operator/config/default?ref=${KUBERAY_VERSION}&timeout=90s"
```

To deploy both the KubeRay Operator and the optional KubeRay API Server run the following commands.
```
export KUBERAY_VERSION=v0.4.0
kubectl create -k "github.com/ray-project/kuberay/manifests/cluster-scope-resources?ref=${KUBERAY_VERSION}&timeout=90s"
kubectl apply -k "github.com/ray-project/kuberay/manifests/base?ref=${KUBERAY_VERSION}&timeout=90s"
```

> Observe that we must use `kubectl create` to install cluster-scoped resources. The corresponding `kubectl apply` command will not work. See [KubeRay issue #271](https://github.com/ray-project/kuberay/issues/271).

### Use Helm

A Helm chart is a collection of files that describe a related set of Kubernetes resources.
It can help users to deploy the KubeRay Operator and Ray clusters conveniently.
Please read [kuberay-operator](helm-chart/kuberay-operator/README.md) to deploy the operator and [ray-cluster](helm-chart/ray-cluster/README.md) to deploy a configurable Ray cluster. To deploy the optional KubeRay API Server, see [kuberay-apiserver](helm-chart/kuberay-apiserver/README.md).

## Development

Please read our [CONTRIBUTING](CONTRIBUTING.md) guide before making a pull request. Refer to our [DEVELOPMENT](./ray-operator/DEVELOPMENT.md) to build and run tests locally.

## Security

If you discover a potential security issue in this project, or think you may
have discovered a security issue, we ask that you notify KubeRay Security via our
[Slack Channel](https://ray-distributed.slack.com/archives/C02GFQ82JPM).
Please do **not** create a public GitHub issue.

## License

This project is licensed under the [Apache-2.0 License](LICENSE).
