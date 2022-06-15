# KubeRay

[![Build Status](https://github.com/ray-project/kuberay/workflows/Go-build-and-test/badge.svg)](https://github.com/ray-project/kuberay/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/ray-project/kuberay)](https://goreportcard.com/report/github.com/ray-project/kuberay)

KubeRay is an open source toolkit to run Ray applications on Kubernetes.

KubeRay provides several tools to improve running and managing Ray's experience on Kubernetes.

- Ray Operator
- Backend services to create/delete cluster resources
- Kubectl plugin/CLI to operate CRD objects
- Data Scientist centric workspace for fast prototyping (incubating)
- Native Job and Serving integration with Clusters (incubating)
- Kubernetes event dumper for ray clusters/pod/services (future work)
- Operator Integration with Kubernetes node problem detector (future work)

## Documentation

You can view detailed documentation and guides at [https://ray-project.github.io/kuberay/](https://ray-project.github.io/kuberay/)

## Quick Start

### Use Yaml

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

### Use helm chart

A helm chart is a collection of files that describe a related set of Kubernetes resources. It can help users to deploy ray-operator and ray clusters conveniently.
Please read [kuberay-operator](helm-chart/kuberay-operator/README.md) to deploy an operator and [ray-cluster](helm-chart/ray-cluster/README.md) to deploy a custom cluster.

### Monitor

We have add a parameter `--metrics-expose-port=8080` to open the port and expose metrics both for the ray cluster and our control plane. We also leverage the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) to start the whole monitoring system.

You can quickly deploy one by the following on your own kubernetes cluster by using the scripts in install:
```shell
./install/prometheus/install.sh
```
It will set up the prometheus stack and deploy the related service monitor in `config/prometheus`

Then you can also use the json in `config/grafana` to generate the dashboards.

## Development

Please read our [CONTRIBUTING](CONTRIBUTING.md) guide before making a pull request. Refer to our [DEVELOPMENT](./ray-operator/DEVELOPMENT.md) to build and run tests locally.

## Security

If you discover a potential security issue in this project, or think you may
have discovered a security issue, we ask that you notify KubeRay Security via our
[Slack Channel](https://ray-distributed.slack.com/archives/C02GFQ82JPM).
Please do **not** create a public GitHub issue.

## License

This project is licensed under the [Apache-2.0 License](LICENSE).
