# KubeRay

[![Build Status](https://github.com/ray-project/kuberay/workflows/Go-build-and-test/badge.svg)](https://github.com/ray-project/kuberay/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/ray-project/kuberay)](https://goreportcard.com/report/github.com/ray-project/kuberay)

KubeRay is a powerful, open-source Kubernetes operator that simplifies the deployment and management of [Ray](https://github.com/ray-project/ray) applications on Kubernetes. It offers several key components:

**KubeRay core**: This is the official, fully-maintained component of KubeRay that provides three custom resource definitions, RayCluster, RayJob, and RayService. These resources are designed to help you run a wide range of workloads with ease.

* **RayCluster**: KubeRay fully manages the lifecycle of RayCluster, including cluster creation/deletion, autoscaling, and ensuring fault tolerance.

* **RayJob**: With RayJob, KubeRay automatically creates a RayCluster and submits a job when the cluster is ready. You can also configure RayJob to automatically delete the RayCluster once the job finishes.

* **RayService**: RayService is made up of two parts: a RayCluster and a Ray Serve deployment graph. RayService offers zero-downtime upgrades for RayCluster and high availability.

**Community-managed components (optional)**: Some components are maintained by the KubeRay community.

* **KubeRay APIServer**: It provides a layer of simplified configuration for KubeRay resources. The KubeRay API server is used internally
by some organizations to back user interfaces for KubeRay resource management.

* **KubeRay Python client**: This Python client library provides APIs to handle RayCluster from your Python application.

* **KubeRay CLI**: KubeRay CLI provides the ability to manage KubeRay resources through command-line interface.

## KubeRay ecosystem

* [AWS Application Load Balancer](docs/guidance/ingress.md)
* [Nginx](docs/guidance/ingress.md)
* [Prometheus and Grafana](docs/guidance/prometheus-grafana.md)
* [Volcano](docs/guidance/volcano-integration.md)
* [MCAD](docs/guidance/kuberay-with-MCAD.md)
* [Kubeflow](docs/guidance/kubeflow-integration.md)

## Blogs

* [A cloud-native, open-source stack for accelerating foundation model innovation](https://research.ibm.com/blog/openshift-foundation-model-stack) IBM (May 9, 2023).
* [AI/ML Models Batch Training at Scale with Open Data Hub](https://cloud.redhat.com/blog/ai/ml-models-batch-training-at-scale-with-open-data-hub) Red Hat (May 15, 2023).

## Documentation

You can view detailed documentation and guides at [https://ray-project.github.io/kuberay/](https://ray-project.github.io/kuberay/).

We also recommend checking out the official Ray guides for deploying on Kubernetes at [https://docs.ray.io/en/latest/cluster/kubernetes/index.html](https://docs.ray.io/en/latest/cluster/kubernetes/index.html).

## Quick Start

* Try this [end-to-end example](helm-chart/ray-cluster/README.md)!
* Please choose the version you would like to install. The examples below use the latest stable version `v0.6.0`.

| Version  |  Stable |  Suggested Kubernetes Version |
|----------|:-------:|------------------------------:|
|  master  |    N    | v1.19 - v1.25 |
|  v0.6.0  |    Y    | v1.19 - v1.25 |

### Use YAML

Make sure your Kubernetes and Kubectl versions are both within the suggested range.
Once you have connected to a Kubernetes cluster, run the following commands to deploy the KubeRay Operator.

```sh
# case 1: kubectl >= v1.22.0
export KUBERAY_VERSION=v0.6.0
kubectl create -k "github.com/ray-project/kuberay/ray-operator/config/default?ref=${KUBERAY_VERSION}&timeout=90s"

# case 2: kubectl < v1.22.0
# Clone KubeRay repository and checkout to the desired branch e.g. `release-0.6`.
kubectl create -k ray-operator/config/default
```

To deploy both the KubeRay Operator and the optional KubeRay API Server run the following commands.

```sh
# case 1: kubectl >= v1.22.0
export KUBERAY_VERSION=v0.6.0
kubectl create -k "github.com/ray-project/kuberay/manifests/cluster-scope-resources?ref=${KUBERAY_VERSION}&timeout=90s"
kubectl apply -k "github.com/ray-project/kuberay/manifests/base?ref=${KUBERAY_VERSION}&timeout=90s"

# case 2: kubectl < v1.22.0
# Clone KubeRay repository and checkout to the desired branch e.g. `release-0.4`.
kubectl create -k manifests/cluster-scope-resources
kubectl apply -k manifests/base
```

> Observe that we must use `kubectl create` to install cluster-scoped resources. The corresponding `kubectl apply` command will not work. See [KubeRay issue #271](https://github.com/ray-project/kuberay/issues/271).

### Use Helm (Helm v3+)

A Helm chart is a collection of files that describe a related set of Kubernetes resources.
It can help users to deploy the KubeRay Operator and Ray clusters conveniently.
Please read [kuberay-operator](helm-chart/kuberay-operator/README.md) to deploy the operator and [ray-cluster](helm-chart/ray-cluster/README.md) to deploy a configurable Ray cluster. To deploy the optional KubeRay API Server, see [kuberay-apiserver](helm-chart/kuberay-apiserver/README.md).

```sh
helm repo add kuberay https://ray-project.github.io/kuberay-helm/

# Install both CRDs and KubeRay operator v0.6.0.
helm install kuberay-operator kuberay/kuberay-operator --version 0.6.0

# Check the KubeRay operator Pod in `default` namespace
kubectl get pods
# NAME                                READY   STATUS    RESTARTS   AGE
# kuberay-operator-6fcbb94f64-mbfnr   1/1     Running   0          17s
```

## Development

Please read our [CONTRIBUTING](CONTRIBUTING.md) guide before making a pull request. Refer to our [DEVELOPMENT](./ray-operator/DEVELOPMENT.md) to build and run tests locally.

### Getting involved
Kuberay has an active community of developers. Here’s how to get involved with the Kuberay community:

Join our community: Join [Ray community slack](https://forms.gle/9TSdDYUgxYs8SA9e8) (search for Kuberay channel) or use our [discussion board](https://discuss.ray.io/c/ray-clusters/ray-kubernetes) to ask questions and get answers.

## Security

If you discover a potential security issue in this project, or think you may
have discovered a security issue, we ask that you notify KubeRay Security via our
[Slack Channel](https://ray-distributed.slack.com/archives/C02GFQ82JPM).
Please do **not** create a public GitHub issue.

## License

This project is licensed under the [Apache-2.0 License](LICENSE).
