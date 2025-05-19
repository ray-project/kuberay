<!-- markdownlint-disable MD013 MD033 -->
# Welcome

<p align="center">
    <a href="https://github.com/ray-project/kuberay/issues">
        <img src="https://img.shields.io/badge/contributions-welcome-brightgreen.svg?style=flat" alt="contributions welcome"/>
    </a>
    <a href="https://github.com/ray-project/kuberay/issues">
        <img src="https://img.shields.io/github/issues-raw/ray-project/kuberay?style=flat" alt="github issues"/>
    </a>
    <img src="https://img.shields.io/badge/status-alpha-brightgreen?style=flat" alt="status is alpha"/>
    <img src="https://img.shields.io/github/license/ray-project/kuberay?style=flat" alt="apache license"/>
</p>
<p align="center">
    <a href="https://goreportcard.com/report/github.com/ray-project/kuberay">
        <img src="https://goreportcard.com/badge/github.com/ray-project/kuberay" alt="go report card"/>
    </a>
    <img src="https://img.shields.io/github/watchers/ray-project/kuberay?style=social" alt="github watchers"/>
    <img src="https://img.shields.io/github/stars/ray-project/kuberay?style=social" alt="github stars"/>
    <img src="https://img.shields.io/github/forks/ray-project/kuberay?style=social" alt="github forks"/>
    <a href="https://hub.docker.com/r/kuberay/operator/">
        <img src="https://img.shields.io/docker/pulls/kuberay/operator" alt="docker pulls"/>
    </a>
</p>

## KubeRay

KubeRay is a powerful, open-source Kubernetes operator that simplifies the deployment and management of [Ray](https://github.com/ray-project/ray) applications on Kubernetes. It offers several key components:

**KubeRay core**: This is the official, fully-maintained component of KubeRay that provides three custom resource definitions, RayCluster, RayJob, and RayService. These resources are designed to help you run a wide range of workloads with ease.

* **RayCluster**: KubeRay fully manages the lifecycle of RayCluster, including cluster creation/deletion, autoscaling, and ensuring fault tolerance.

* **RayJob**: With RayJob, KubeRay automatically creates a RayCluster and submits a job when the cluster is ready. You can also configure RayJob to automatically delete the RayCluster once the job finishes.

* **RayService**: RayService is made up of two parts: a RayCluster and a Ray Serve deployment graph. RayService offers zero-downtime upgrades for RayCluster and high availability.

**Community-managed components (optional)**: Some components are maintained by the KubeRay community.

* **KubeRay APIServer**: It provides a layer of simplified configuration for KubeRay resources. The KubeRay API server is used internally
by some organizations to back user interfaces for KubeRay resource management.

* **KubeRay Python client**: This Python client library provides APIs to handle RayCluster from your Python application.

## KubeRay ecosystem

* [AWS Application Load Balancer](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/ingress.html#aws-application-load-balancer-alb-ingress-support-on-aws-eks)
* [Nginx](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/ingress.html#manually-setting-up-nginx-ingress-on-kind)
* [Prometheus and Grafana](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/prometheus-grafana.html#kuberay-prometheus-grafana)
* [Volcano](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/volcano.html)
* [MCAD](guidance/kuberay-with-MCAD.md)

## Security

**Security and isolation must be enforced outside of the Ray Cluster.** Restrict network access with Kubernetes or other external controls. Refer to [**Ray security documentation**](https://docs.ray.io/en/master/ray-security/index.html) for more guidance on what controls to implement.

Please report security issues to <security@anyscale.com>.

## The Ray docs

You can find even more information on deployments of Ray on Kubernetes at the [official Ray docs](https://docs.ray.io/en/latest/cluster/kubernetes/index.html).
