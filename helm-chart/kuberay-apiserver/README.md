# KubeRay API Server

This document provides instructions to install the KubeRay API Server with a Helm chart.

## Helm

Make sure the version of Helm is v3+. Currently, [existing CI tests] are based on Helm v3.4.1 and
v3.9.4.

```sh
helm version
```

## Install KubeRay API Server

* Install a stable version via Helm repository (only supports KubeRay v0.4.0+)

  ```sh
  helm repo add kuberay https://ray-project.github.io/kuberay-helm/

  # Install the KubeRay API Server at Version v1.1.0.
  helm install kuberay-apiserver kuberay/kuberay-apiserver --version 1.1.0

  # Check that the KubeRay API Server is running in the "default" namespaces.
  kubectl get pods
  # NAME                        READY   STATUS    RESTARTS   AGE
  # kuberay-apiserver-xxxxxx    1/1     Running   0          17s
  ```

* Install the nightly version

  ```sh
  # Step1: Clone KubeRay repository

  # Step2: Move to `helm-chart/kuberay-apiserver`

  # Step3: Install the KubeRay apiserver
  helm install kuberay-apiserver .
  ```

## List the chart

To list the `kuberay-apiserver` release:

```sh
helm ls
# NAME                      NAMESPACE       REVISION        UPDATED                                    STATUS         CHART
# kuberay-apiserver         default         1               2022-12-02 02:13:37.514445313 +0000 UTC    deployed       kuberay-apiserver-1.1.0
```

## Uninstall the Chart

```sh
# Uninstall the `kuberay-apiserver` release
helm uninstall kuberay-apiserver

# The API Server Pod should be removed.
kubectl get pods
# No resources found in default namespace.
```

[existing CI tests]: https://github.com/ray-project/kuberay/blob/master/.github/workflows/helm-lint.yaml
