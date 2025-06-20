# kuberay-operator

![Version: 1.4.0](https://img.shields.io/badge/Version-1.4.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square)

A Helm chart for deploying the Kuberay operator on Kubernetes.

**Homepage:** <https://github.com/ray-project/kuberay>

## Introduction

This document provides instructions to install both CRDs (RayCluster, RayJob, RayService) and
KubeRay operator with a Helm chart.

## Prerequisites

- [Kubernetes](https://kubernetes.io)
- [Helm](https://helm.sh) >= 3

Make sure the version of Helm is v3+. Currently, [existing CI tests] are based on Helm v3.4.1 and v3.9.4.

```bash
helm version
```

## Install CRDs and KubeRay operator

- Install a stable version via Helm repository (only supports KubeRay v0.4.0+)

  ```sh
  helm repo add kuberay https://ray-project.github.io/kuberay-helm/

  # Install both CRDs and KubeRay operator v1.4.0.
  helm install kuberay-operator kuberay/kuberay-operator --version 1.4.0

  # Check the KubeRay operator Pod in `default` namespace
  kubectl get pods
  # NAME                                READY   STATUS    RESTARTS   AGE
  # kuberay-operator-6fcbb94f64-mbfnr   1/1     Running   0          17s
  ```

- Install the nightly version

  ```sh
  # Step1: Clone KubeRay repository

  # Step2: Move to `helm-chart/kuberay-operator`

  # Step3: Install KubeRay operator
  helm install kuberay-operator .
  ```

- Install KubeRay operator without installing CRDs
  - In some cases, the installation of the CRDs and the installation of the operator may require
    different levels of admin permissions, so these two installations could be handled as different
    steps by different roles.
  - Use Helm's built-in `--skip-crds` flag to install the operator only.
    See [this document] for more details.

  ```sh
  # Step 1: Install CRDs only (for cluster admin)
  kubectl create -k "github.com/ray-project/kuberay/ray-operator/config/crd?ref=v1.4.0&timeout=90s"

  # Step 2: Install KubeRay operator only. (for developer)
  helm install kuberay-operator kuberay/kuberay-operator --version 1.4.0 --skip-crds
  ```

## List the chart

To list the `my-release` deployment:

```sh
helm ls
# NAME                    NAMESPACE       REVISION        UPDATED                                 STATUS          CHART                           APP VERSION
# kuberay-operator        default         1               2023-09-22 02:57:17.306616331 +0000 UTC deployed        kuberay-operator-1.4.0
```

## Uninstall the Chart

```sh
# Uninstall the `kuberay-operator` release
helm uninstall kuberay-operator

# The operator Pod should be removed.
kubectl get pods
# No resources found in default namespace.
```

## Working with Argo CD

If you are using [Argo CD] to manage the operator, you will encounter the issue which complains the
CRDs too long. Same with [this issue]. The recommended solution is to split the operator into two
Argo apps, such as:

- The first app just for installing the CRDs with `Replace=true` directly, snippet:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ray-operator-crds
spec:
  project: default
  source:
    repoURL: https://github.com/ray-project/kuberay
    targetRevision: v1.4.0
    path: helm-chart/kuberay-operator/crds
  destination:
    server: https://kubernetes.default.svc
  syncPolicy:
    syncOptions:
    - Replace=true
...
```

- The second app that installs the Helm chart with `skipCrds=true` (new feature in Argo CD 2.3.0), snippet:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: ray-operator
spec:
  source:
    repoURL: https://github.com/ray-project/kuberay
    targetRevision: v1.4.0
    path: helm-chart/kuberay-operator
    helm:
      skipCrds: true
  destination:
    server: https://kubernetes.default.svc
    namespace: ray-operator
  syncPolicy:
    syncOptions:
    - CreateNamespace=true
...
```

[existing CI tests]: https://github.com/ray-project/kuberay/blob/master/.github/workflows/helm-lint.yaml
[Argo CD]: https://argoproj.github.io
[this issue]: https://github.com/prometheus-operator/prometheus-operator/issues/4439
[this document]: https://helm.sh/docs/chart_best_practices/custom_resource_definitions/

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| nameOverride | string | `"kuberay-operator"` | String to partially override release name. |
| fullnameOverride | string | `"kuberay-operator"` | String to fully override release name. |
| componentOverride | string | `"kuberay-operator"` | String to override component name. |
| image.repository | string | `"quay.io/kuberay/operator"` | Image repository. |
| image.tag | string | `"v1.4.0"` | Image tag. |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy. |
| labels | object | `{}` | Extra labels. |
| annotations | object | `{}` | Extra annotations. |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created. |
| serviceAccount.name | string | `"kuberay-operator"` | The name of the service account to use. If not set and create is true, a name is generated using the fullname template. |
| logging.stdoutEncoder | string | `"json"` | Log encoder to use for stdout (one of `json` or `console`). |
| logging.fileEncoder | string | `"json"` | Log encoder to use for file logging (one of `json` or `console`). |
| logging.baseDir | string | `""` | Directory for kuberay-operator log file. |
| logging.fileName | string | `""` | File name for kuberay-operator log file. |
| logging.sizeLimit | string | `""` | EmptyDir volume size limit for kuberay-operator log file. |
| batchScheduler.enabled | bool | `false` |  |
| batchScheduler.name | string | `""` |  |
| featureGates[0].name | string | `"RayClusterStatusConditions"` |  |
| featureGates[0].enabled | bool | `true` |  |
| featureGates[1].name | string | `"RayJobDeletionPolicy"` |  |
| featureGates[1].enabled | bool | `false` |  |
| metrics.enabled | bool | `true` | Whether KubeRay operator should emit control plane metrics. |
| metrics.serviceMonitor.enabled | bool | `false` | Enable a prometheus ServiceMonitor |
| metrics.serviceMonitor.interval | string | `"30s"` | Prometheus ServiceMonitor interval |
| metrics.serviceMonitor.honorLabels | bool | `true` | When true, honorLabels preserves the metric’s labels when they collide with the target’s labels. |
| metrics.serviceMonitor.selector | object | `{}` | Prometheus ServiceMonitor selector |
| metrics.serviceMonitor.namespace | string | `""` | Prometheus ServiceMonitor namespace |
| operatorCommand | string | `"/manager"` | Path to the operator binary |
| leaderElectionEnabled | bool | `true` | If leaderElectionEnabled is set to true, the KubeRay operator will use leader election for high availability. |
| rbacEnable | bool | `true` | If rbacEnable is set to false, no RBAC resources will be created, including the Role for leader election, the Role for Pods and Services, and so on. |
| crNamespacedRbacEnable | bool | `true` | When crNamespacedRbacEnable is set to true, the KubeRay operator will create a Role for RayCluster preparation (e.g., Pods, Services) and a corresponding RoleBinding for each namespace listed in the "watchNamespace" parameter. Please note that even if crNamespacedRbacEnable is set to false, the Role and RoleBinding for leader election will still be created.  Note: (1) This variable is only effective when rbacEnable and singleNamespaceInstall are both set to true. (2) In most cases, it should be set to true, unless you are using a Kubernetes cluster managed by GitOps tools such as ArgoCD. |
| singleNamespaceInstall | bool | `false` | When singleNamespaceInstall is true: - Install namespaced RBAC resources such as Role and RoleBinding instead of cluster-scoped ones like ClusterRole and ClusterRoleBinding so that   the chart can be installed by users with permissions restricted to a single namespace.   (Please note that this excludes the CRDs, which can only be installed at the cluster scope.) - If "watchNamespace" is not set, the KubeRay operator will, by default, only listen   to resource events within its own namespace. |
| env | string | `nil` | Environment variables. |
| resources | object | `{"limits":{"cpu":"100m","memory":"512Mi"}}` | Resource requests and limits for containers. |
| livenessProbe.initialDelaySeconds | int | `10` |  |
| livenessProbe.periodSeconds | int | `5` |  |
| livenessProbe.failureThreshold | int | `5` |  |
| readinessProbe.initialDelaySeconds | int | `10` |  |
| readinessProbe.periodSeconds | int | `5` |  |
| readinessProbe.failureThreshold | int | `5` |  |
| podSecurityContext | object | `{}` | Set up `securityContext` to improve Pod security. |
| service.type | string | `"ClusterIP"` | Service type. |
| service.port | int | `8080` | Service port. |
