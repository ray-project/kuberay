# YAML Reference Guide

This document serves as a reference guide for all YAML files used in the KubeRay project.

## Table of Contents
- [Monitoring](#monitoring)
- [Security](#security)
- [High Availability](#ha)
- [Custom Resource Definitions (CRDs)](#crds)
- [Development & Testing](#development)

<a name="monitoring"></a>
## Monitoring

| YAML File | Path | Description | Referenced By | Branch/Tag | Dependencies |
|-----------|------|-------------|---------------|------------|--------------|
| [API Server Service Monitor](../../apiserver/deploy/prometheus/api_server_service_monitor.yaml) | apiserver/deploy/prometheus/api_server_service_monitor.yaml | Collects API server metrics into Prometheus. Key metrics: request count, response time, etc. | [Monitoring Guide](../../apiserver/Monitoring.md) - "Now we can install a service monitor to scrape Api Server metrics" | master | Prometheus Operator CRD |
| [Ray Cluster Pod Monitor](../../apiserver/deploy/prometheus/ray_cluster_pod_monitor.yaml) | apiserver/deploy/prometheus/ray_cluster_pod_monitor.yaml | Monitors Ray cluster node metrics. Collects CPU, memory, GPU usage, etc. | [Monitoring Guide](../../apiserver/Monitoring.md) - "we suggest we create a single pod monitor that can be installed" | master | Prometheus Operator CRD |

<a name="security"></a>
## Security

| YAML File | Path | Description | Referenced By | Branch/Tag | Dependencies |
|-----------|------|-------------|---------------|------------|--------------|
| [API Server Values](../../helm-chart/kuberay-apiserver/values.yaml) | helm-chart/kuberay-apiserver/values.yaml | API server Helm chart configuration. Includes authentication, authorization, TLS settings | [Securing Implementation](../../apiserver/SecuringImplementation.md) - "please modify values.yaml to set the parameter" | master | - |
| [Secure API Server](../../apiserver/deploy/base/secure/apiserver.yaml) | apiserver/deploy/base/secure/apiserver.yaml | API server deployment manifest with security settings | Used in kustomization.yaml but not directly referenced in docs | master | API Server Values |
| [Insecure API Server](../../apiserver/deploy/base/insecure/apiserver.yaml) | apiserver/deploy/base/insecure/apiserver.yaml | Basic API server deployment manifest without security settings | Used in kustomization.yaml but not directly referenced in docs | master | API Server Values |

<a name="ha"></a>
## High Availability

| YAML File | Path | Description | Referenced By | Branch/Tag | Dependencies |
|-----------|------|-------------|---------------|------------|--------------|
| [Redis Configuration](../../apiserver/test/cluster/redis/redis.yaml) | apiserver/test/cluster/redis/redis.yaml | Redis configuration for HA cluster. Includes replication and persistence settings | [HA Cluster Guide](../../apiserver/HACluster.md) - "For this example we will use a rather simple yaml file" | master | Redis Password Secret |
| [Redis Password Secret](../../apiserver/test/cluster/redis/redis_passwrd.yaml) | apiserver/test/cluster/redis/redis_passwrd.yaml | Redis authentication password configuration | [HA Cluster Guide](../../apiserver/HACluster.md) - "you need to create secret in the namespace" | master | - |
| [Test Code ConfigMap](../../apiserver/test/cluster/code_configmap.yaml) | apiserver/test/cluster/code_configmap.yaml | Test code configuration for HA cluster | [HA Cluster Guide](../../apiserver/HACluster.md) - "we will create a config map, containing simple code" | master | Redis Configuration |

<a name="crds"></a>
## Custom Resource Definitions (CRDs)

| YAML File | Path | Description | Referenced By | Branch/Tag | Dependencies |
|-----------|------|-------------|---------------|------------|--------------|
| [Ray Clusters CRD](../../ray-operator/config/crd/bases/ray.io_rayclusters.yaml) | ray-operator/config/crd/bases/ray.io_rayclusters.yaml | Ray cluster CRD. Includes cluster configuration and scaling settings | [Helm Installation Guide](../deploy/helm.md) - "kubectl create -k github.com/ray-project/kuberay/ray-operator/config/crd?ref=v1.1.0" | master | - |
| [Ray Jobs CRD](../../ray-operator/config/crd/bases/ray.io_rayjobs.yaml) | ray-operator/config/crd/bases/ray.io_rayjobs.yaml | Ray job CRD. Includes job scheduling and resource allocation settings | [Helm Installation Guide](../deploy/helm.md) - "kubectl create -k github.com/ray-project/kuberay/ray-operator/config/crd?ref=v1.1.0" | master | Ray Clusters CRD |
| [Ray Services CRD](../../ray-operator/config/crd/bases/ray.io_rayservices.yaml) | ray-operator/config/crd/bases/ray.io_rayservices.yaml | Ray service CRD. Includes service deployment and routing settings | [Helm Installation Guide](../deploy/helm.md) - "kubectl create -k github.com/ray-project/kuberay/ray-operator/config/crd?ref=v1.1.0" | master | Ray Clusters CRD |

<a name="development"></a>
## Development & Testing

| YAML File | Path | Description | Referenced By | Branch/Tag | Dependencies |
|-----------|------|-------------|---------------|------------|--------------|
| [Kind Cluster Config](../../apiserver/hack/kind-cluster-config.yaml) | apiserver/hack/kind-cluster-config.yaml | Local Kind cluster configuration. Port mappings: HTTP(31888), RPC(31887) | [Development Guide](../../apiserver/DEVELOPMENT.md) - "creates a local kind cluster, using the configuration from hack/kind-cluster-config.yaml" | master | - |