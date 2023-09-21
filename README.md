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

## Documentation

From September 2023, most KubeRay documentation will be hosted on [Ray documentation](https://docs.ray.io/en/latest/cluster/kubernetes/index.html).

## Quick Start

* [RayCluster Quickstart](https://docs.ray.io/en/master/cluster/kubernetes/getting-started/raycluster-quick-start.html)
* [RayJob Quickstart](https://docs.ray.io/en/master/cluster/kubernetes/getting-started/rayjob-quick-start.html)
* [RayService Quickstart](https://docs.ray.io/en/master/cluster/kubernetes/getting-started/rayservice-quick-start.html)

## Examples

* [Ray Train XGBoostTrainer on Kubernetes](https://docs.ray.io/en/master/cluster/kubernetes/examples/ml-example.html#kuberay-ml-example) (CPU-only)
* [Train PyTorch ResNet model with GPUs on Kubernetes](https://docs.ray.io/en/master/cluster/kubernetes/examples/gpu-training-example.html#kuberay-gpu-training-example)
* [Serve a MobileNet image classifier on Kubernetes](https://docs.ray.io/en/master/cluster/kubernetes/examples/mobilenet-rayservice.html#kuberay-mobilenet-rayservice-example) (CPU-only)
* [Serve a StableDiffusion text-to-image model on Kubernetes](https://docs.ray.io/en/master/cluster/kubernetes/examples/stable-diffusion-rayservice.html#kuberay-stable-diffusion-rayservice-example)
* [Serve a text summarizer on Kubernetes](https://docs.ray.io/en/master/cluster/kubernetes/examples/text-summarizer-rayservice.html#kuberay-text-summarizer-rayservice-example)
* [RayJob Batch Inference Example](https://docs.ray.io/en/master/cluster/kubernetes/examples/rayjob-batch-inference-example.html#kuberay-batch-inference-example)

## Kubernetes Ecosystem

* [Ingress: AWS Application Load Balancer, GKE Ingress, Nginx](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/ingress.html#kuberay-ingress)
* [Using Prometheus and Grafana](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/prometheus-grafana.html#kuberay-prometheus-grafana)
* [Profiling with py-spy](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/pyspy.html#kuberay-pyspy-integration)
* [KubeRay integration with Volcano](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/volcano.html#kuberay-volcano)
* [Kubeflow: an interactive development solution](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/kubeflow.html#kuberay-kubeflow-integration)

## Blogs

* [A cloud-native, open-source stack for accelerating foundation model innovation](https://research.ibm.com/blog/openshift-foundation-model-stack) IBM (May 9, 2023).
* [AI/ML Models Batch Training at Scale with Open Data Hub](https://cloud.redhat.com/blog/ai/ml-models-batch-training-at-scale-with-open-data-hub) Red Hat (May 15, 2023).

## Helm Charts

KubeRay Helm charts are hosted on the [ray-project/kuberay-helm](https://github.com/ray-project/kuberay-helm) repository.
Please read [kuberay-operator](helm-chart/kuberay-operator/README.md) to deploy the operator and [ray-cluster](helm-chart/ray-cluster/README.md) to deploy a configurable Ray cluster.
To deploy the optional KubeRay API Server, see [kuberay-apiserver](helm-chart/kuberay-apiserver/README.md).


```sh
# Add the Helm repo
helm repo add kuberay https://ray-project.github.io/kuberay-helm/
helm repo update

# Confirm the repo exists
helm search repo kuberay --devel

# Install both CRDs and KubeRay operator v0.6.0.
helm install kuberay-operator kuberay/kuberay-operator --version 1.0.0-rc.0

# Check the KubeRay operator Pod in `default` namespace
kubectl get pods
# NAME                                READY   STATUS    RESTARTS   AGE
# kuberay-operator-6fcbb94f64-mbfnr   1/1     Running   0          17s
```

## Development

Please read our [CONTRIBUTING](CONTRIBUTING.md) guide before making a pull request. Refer to our [DEVELOPMENT](./ray-operator/DEVELOPMENT.md) to build and run tests locally.

## Getting Involved

Join [Ray's Slack workspace](https://docs.google.com/forms/d/e/1FAIpQLSfAcoiLCHOguOm8e7Jnn-JJdZaCxPGjgVCvFijHB5PLaQLeig/viewform), and search the following public channels:

* `#kuberay-questions` (KubeRay users): This channel aims to help KubeRay users with their questions. The messages will be closely monitored by the Ray/KubeRay maintainers.

* `#kuberay-discuss` (KubeRay contributors): This channel is for contributors to discuss what to do next with KubeRay (e.g. issue, pull request, feature request, design doc, KubeRay ecosystem integration). All KubeRay maintainers & core contributors are in the channel.

## Security

If you discover a potential security issue in this project, or think you may
have discovered a security issue, we ask that you notify KubeRay Security via our
[Slack Channel](https://ray-distributed.slack.com/archives/C02GFQ82JPM).
Please do **not** create a public GitHub issue.

## License

This project is licensed under the [Apache-2.0 License](LICENSE).
