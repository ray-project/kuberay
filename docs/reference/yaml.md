# YAML Reference Guide

This document serves as a reference guide for all YAML files in the `ray-operator/config/samples` directory.

## Table of Contents
- [RayCluster Samples](#raycluster-samples)
- [RayJob Samples](#rayjob-samples)
- [RayService Samples](#rayservice-samples)
- [Other Samples](#other-samples)

<a name="raycluster-samples"></a>
## RayCluster Samples

| YAML File | Description | Referenced By | Branch/Tag | Dependencies |
|-----------|-------------|---------------|------------|--------------|
| [ray-cluster.sample.yaml](../../ray-operator/config/samples/ray-cluster.sample.yaml) | Basic Ray cluster configuration | [Ray Documentation](https://docs.ray.io/en/master/cluster/kubernetes/getting-started/raycluster-quick-start.html) | master | - |
| [ray-cluster.complete.yaml](../../ray-operator/config/samples/ray-cluster.complete.yaml) | Comprehensive cluster configuration | [MCAD Guide](../guidance/kuberay-with-MCAD.md) | master | - |
| [ray-cluster.autoscaler.yaml](../../ray-operator/config/samples/ray-cluster.autoscaler.yaml) | Autoscaling configuration | [Autoscaler Guide](../guidance/autoscaler.md) | master | - |
| [ray-cluster.autoscaler-v2.yaml](../../ray-operator/config/samples/ray-cluster.autoscaler-v2.yaml) | Autoscaling V2 configuration | [Autoscaler Guide](../guidance/autoscaler.md) | master | - |
| [ray-cluster.auth.yaml](../../ray-operator/config/samples/ray-cluster.auth.yaml) | Authentication configuration | - | master | - |
| [ray-cluster.custom-head-service.yaml](../../ray-operator/config/samples/ray-cluster.custom-head-service.yaml) | Custom head service setup | - | master | - |
| [ray-cluster.embed-grafana.yaml](../../ray-operator/config/samples/ray-cluster.embed-grafana.yaml) | Grafana integration | [Prometheus Guide](../guidance/prometheus-grafana.md) | master | Grafana |
| [ray-cluster.external-redis.yaml](../../ray-operator/config/samples/ray-cluster.external-redis.yaml) | External Redis configuration | - | master | Redis |
| [ray-cluster.external-redis-uri.yaml](../../ray-operator/config/samples/ray-cluster.external-redis-uri.yaml) | External Redis with URI configuration | - | master | Redis |
| [ray-cluster.fluentbit.yaml](../../ray-operator/config/samples/ray-cluster.fluentbit.yaml) | Fluentbit logging setup | - | master | Fluentbit |
| [ray-cluster.tls.yaml](../../ray-operator/config/samples/ray-cluster.tls.yaml) | TLS configuration | [TLS Guide](../guidance/tls.md) | master | - |
| [ray-cluster.volcano-scheduler.yaml](../../ray-operator/config/samples/ray-cluster.volcano-scheduler.yaml) | Volcano scheduler integration | [Volcano Guide](../guidance/volcano-integration.md) | master | Volcano |
| [ray-cluster.volcano-scheduler-queue.yaml](../../ray-operator/config/samples/ray-cluster.volcano-scheduler-queue.yaml) | Volcano scheduler with queue | [Volcano Guide](../guidance/volcano-integration.md) | master | Volcano |
| [ray-cluster.yunikorn-scheduler.yaml](../../ray-operator/config/samples/ray-cluster.yunikorn-scheduler.yaml) | YuniKorn scheduler integration | - | master | YuniKorn |
| [ray-cluster.separate-ingress.yaml](../../ray-operator/config/samples/ray-cluster.separate-ingress.yaml) | Separate ingress configuration | [Ingress Guide](../guidance/ingress.md) | master | - |
| [ray-cluster.overwrite-command.yaml](../../ray-operator/config/samples/ray-cluster.overwrite-command.yaml) | Command overwrite configuration | [Pod Command Guide](../guidance/pod-command.md) | master | - |
| [ray-cluster.deprecate-gcs-ft.yaml](../../ray-operator/config/samples/ray-cluster.deprecate-gcs-ft.yaml) | Deprecated GCS fault tolerance | [GCS FT Guide](../guidance/gcs-ft.md) | master | - |
| [ray-cluster.gke-bucket.yaml](../../ray-operator/config/samples/ray-cluster.gke-bucket.yaml) | GKE bucket configuration | [GKE Guide](../guidance/gcp-gke-gpu-cluster.md) | master | GKE |
| [ray-cluster.head-command.yaml](../../ray-operator/config/samples/ray-cluster.head-command.yaml) | Head node command configuration | [Pod Command Guide](../guidance/pod-command.md) | master | - |
| [ray-cluster.persistent-redis.yaml](../../ray-operator/config/samples/ray-cluster.persistent-redis.yaml) | Persistent Redis setup | - | master | Redis |
| [ray-cluster.py-spy.yaml](../../ray-operator/config/samples/ray-cluster.py-spy.yaml) | Py-Spy profiling setup | [Profiling Guide](../guidance/profiling.md) | master | Py-Spy |

<a name="rayjob-samples"></a>
## RayJob Samples

| YAML File | Description | Referenced By | Branch/Tag | Dependencies |
|-----------|-------------|---------------|------------|--------------|
| [ray-job.sample.yaml](../../ray-operator/config/samples/ray-job.sample.yaml) | Basic job configuration | [RayJob Guide](../guidance/rayjob.md) | master | - |
| [ray-job.batch-inference.yaml](../../ray-operator/config/samples/ray-job.batch-inference.yaml) | Batch inference example | - | master | - |
| [ray-job.custom-head-svc.yaml](../../ray-operator/config/samples/ray-job.custom-head-svc.yaml) | Custom head service | - | master | - |
| [ray-job.interactive-mode.yaml](../../ray-operator/config/samples/ray-job.interactive-mode.yaml) | Interactive job setup | - | master | - |
| [ray-job.modin.yaml](../../ray-operator/config/samples/ray-job.modin.yaml) | Modin integration | - | master | Modin |
| [ray-job.resources.yaml](../../ray-operator/config/samples/ray-job.resources.yaml) | Resource configuration | - | master | - |
| [ray-job.shutdown.yaml](../../ray-operator/config/samples/ray-job.shutdown.yaml) | Job shutdown configuration | - | master | - |
| [ray-job.kueue-toy-sample.yaml](../../ray-operator/config/samples/ray-job.kueue-toy-sample.yaml) | Kueue integration example | - | master | Kueue |
| [ray-job.use-existing-raycluster.yaml](../../ray-operator/config/samples/ray-job.use-existing-raycluster.yaml) | Using existing RayCluster | - | master | - |

<a name="rayservice-samples"></a>
## RayService Samples

| YAML File | Description | Referenced By | Branch/Tag | Dependencies |
|-----------|-------------|---------------|------------|--------------|
| [ray-service.sample.yaml](../../ray-operator/config/samples/ray-service.sample.yaml) | Basic service configuration | [RayService Guide](../guidance/rayservice.md) | master | - |
| [ray-service.high-availability.yaml](../../ray-operator/config/samples/ray-service.high-availability.yaml) | HA configuration | [HA Guide](../guidance/rayservice-high-availability.md) | master | - |
| [ray-service.high-availability-locust.yaml](../../ray-operator/config/samples/ray-service.high-availability-locust.yaml) | HA with Locust testing | [HA Guide](../guidance/rayservice-high-availability.md) | master | Locust |
| [ray-service.mobilenet.yaml](../../ray-operator/config/samples/ray-service.mobilenet.yaml) | MobileNet deployment | [MobileNet Guide](../guidance/mobilenet-rayservice.md) | master | - |
| [ray-service.stable-diffusion.yaml](../../ray-operator/config/samples/ray-service.stable-diffusion.yaml) | Stable Diffusion | [Stable Diffusion Guide](../guidance/stable-diffusion-rayservice.md) | master | - |
| [ray-service.text-summarizer.yaml](../../ray-operator/config/samples/ray-service.text-summarizer.yaml) | Text summarization | [Text Summarizer Guide](../guidance/text-summarizer-rayservice.md) | master | - |
| [ray-service.custom-serve-service.yaml](../../ray-operator/config/samples/ray-service.custom-serve-service.yaml) | Custom Serve service | [RayServe Dev Guide](../guidance/rayserve-dev-doc.md) | master | - |
| [ray-service.different-port.yaml](../../ray-operator/config/samples/ray-service.different-port.yaml) | Custom port configuration | - | master | - |
| [ray-service.text-ml.yaml](../../ray-operator/config/samples/ray-service.text-ml.yaml) | Text ML service | - | master | - |

<a name="other-samples"></a>
## Other Samples

| YAML File | Description | Referenced By | Branch/Tag | Dependencies |
|-----------|-------------|---------------|------------|--------------|
| [ray-pod.tls.yaml](../../ray-operator/config/samples/ray-pod.tls.yaml) | TLS configuration for pods | [TLS Guide](../guidance/tls.md) | master | - |
| [ray-cluster-alb-ingress.yaml](../../ray-operator/config/samples/ray-cluster-alb-ingress.yaml) | AWS ALB setup | [AWS Guide](../guidance/aws-eks-gpu-cluster.md) | master | AWS ALB |
| [ingress-rayclient-tls.yaml](../../ray-operator/config/samples/ingress-rayclient-tls.yaml) | TLS for Ray client | [Ingress Guide](../guidance/rayclient-nginx-ingress.md) | master | NGINX Ingress |