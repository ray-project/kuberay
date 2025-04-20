<!-- markdownlint-disable MD013 -->
# KubeRay APIServer

The KubeRay APIServer provides gRPC and HTTP APIs to manage KubeRay resources.

## Introduction

The KubeRay APIServer is an optional component. It provides a layer of simplified configuration for KubeRay resources. The KubeRay API server is used internally by some organizations to back user interfaces for KubeRay resource management.

The KubeRay APIServer is community-managed and is not officially endorsed by the Ray maintainers. At this time, the only officially supported methods for
managing KubeRay resources are:

- Direct management of KubeRay custom resources via kubectl, kustomize, and Kubernetes language clients.
- Helm charts.

KubeRay APIServer maintainer contacts (GitHub handles):
@Jeffwan @scarlet25151

## Installation

### Start a local apiserver

You could build and start apiserver from scratch on your local environment in one simple command. It will deploy all the necessities to a local kind cluster.

```sh
make start-local-apiserver
```

apiserver supports HTTP request, so you could easily check whether it's started successfully by by issuing two simple curl requests.

```sh
# Create complete template.
curl --silent -X 'POST' \
    'http://localhost:31888/apis/v1/namespaces/ray-system/compute_templates' \
    -H 'accept: application/json' \
    -H 'Content-Type: application/json' \
    -d '{
        "name": "default-template",
        "namespace": "ray-system",
        "cpu": 2,
        "memory": 4
      }'

# Check whether compute template is created successfully.
curl --silent -X 'GET' \
    'http://localhost:31888/apis/v1/namespaces/ray-system/compute_templates' \
    -H 'accept: application/json'
```

### Helm

Make sure the version of Helm is v3+. Currently, [existing CI tests](https://github.com/ray-project/kuberay/blob/master/.github/workflows/helm-lint.yaml) are based on Helm v3.4.1 and v3.9.4.

```sh
helm version
```

### Install KubeRay Operator

- Install a stable version via Helm repository (only supports KubeRay v0.4.0+)

  ```sh
    # Install the KubeRay helm repo
    helm repo add kuberay https://ray-project.github.io/kuberay-helm/

    # Install KubeRay Operator v1.1.0.
    helm install kuberay-operator kuberay/kuberay-operator --version v1.1.0

    # Check the KubeRay Operator Pod in `default` namespace
    kubectl get pods
    # NAME                                             READY   STATUS    RESTARTS   AGE
    # kuberay-operator-7456c6b69b-t6pt7                1/1     Running   0          172m
  ```

### Install KubeRay APIServer

```text
Please note that examples show here will only work with the nightly builds of the api-server. `v1.0.0` does not yet contain critical fixes
to the api server that would allow Kuberay Serve endpoints to work properly
```

- Install a stable version via Helm repository (only supports KubeRay v0.4.0+)

  ```sh
  # Install the KubeRay helm repo
  helm repo add kuberay https://ray-project.github.io/kuberay-helm/

  # Install KubeRay APIServer.
  helm install kuberay-apiserver kuberay/kuberay-apiserver

  # Check the KubeRay APIServer Pod in `default` namespace
  kubectl get pods
  # NAME                                             READY   STATUS    RESTARTS   AGE
  # kuberay-apiserver-857869f665-b94px               1/1     Running   0          86m
  # kuberay-operator-7456c6b69b-t6pt7                1/1     Running   0          172m
  ```

- Install the nightly version

  ```sh
  # Step1: Clone KubeRay repository

  # Step2: Move to `helm-chart/kuberay-apiserver`
  cd helm-chart/kuberay-apiserver

  # Step3: Install KubeRay APIServer
  helm install kuberay-apiserver .
  ```

- Install the current (working branch) version

  ```sh
  # Step1: Clone KubeRay repository

  # Step2: Change directory to the api server
  cd apiserver

  # Step3: Build docker image, create a local kind cluster and deploy api server (using helm)
  make docker-image cluster load-image deploy
  ```

### List the chart

To list the deployments:

```sh
helm ls
# NAME                    NAMESPACE       REVISION        UPDATED                                 STATUS          CHART                           APP VERSION
# kuberay-apiserver       default         1               2023-09-25 10:42:34.267328 +0300 EEST   deployed        kuberay-apiserver-1.0.0
# kuberay-operator        default         1               2023-09-25 10:41:48.355831 +0300 EEST   deployed        kuberay-operator-1.0.0
```

### Uninstall the Chart

```sh
# Uninstall the `kuberay-apiserver` release
helm uninstall kuberay-apiserver

# The KubeRay APIServer Pod should be removed.
kubectl get pods
# No resources found in default namespace.
```

## Usage

After the deployment we may use the `{{baseUrl}}` to access the service. See [swagger support section](https://ray-project.github.io/kuberay/components/apiserver/#swagger-support) to get the complete definitions of APIs.

- (default) for nodeport access, use port `31888` for connection

- for ingress access, you will need to create your own ingress

The requests parameters detail can be seen in [KubeRay swagger](https://github.com/ray-project/kuberay/tree/master/proto/swagger), this document only presents basic examples.

### Setup a smoke test

The following steps allow you to validate that the KubeRay API Server components and KubeRay Operator integrate in your environment.

1. (Optional) You may use your local kind cluster or minikube

    ```bash
    cat <<EOF | kind create cluster --name ray-test --config -
    kind: Cluster
    apiVersion: kind.x-k8s.io/v1alpha4
    nodes:
    - role: control-plane
      image: kindest/node:v1.23.17@sha256:59c989ff8a517a93127d4a536e7014d28e235fb3529d9fba91b3951d461edfdb
      kubeadmConfigPatches:
        - |
          kind: InitConfiguration
          nodeRegistration:
            kubeletExtraArgs:
              node-labels: "ingress-ready=true"
      extraPortMappings:
      - containerPort: 30265
        hostPort: 8265
        listenAddress: "0.0.0.0"
        protocol: tcp
      - containerPort: 30001
        hostPort: 10001
        listenAddress: "0.0.0.0"
        protocol: tcp
      - containerPort: 8000
        hostPort: 8000
        listenAddress: "0.0.0.0"
      - containerPort: 31888
        hostPort: 31888
        listenAddress: "0.0.0.0"
      - containerPort: 31887
        hostPort: 31887
        listenAddress: "0.0.0.0"
    - role: worker
      image: kindest/node:v1.23.17@sha256:59c989ff8a517a93127d4a536e7014d28e235fb3529d9fba91b3951d461edfdb
    - role: worker
      image: kindest/node:v1.23.17@sha256:59c989ff8a517a93127d4a536e7014d28e235fb3529d9fba91b3951d461edfdb
    EOF
    ```

2. Deploy the KubeRay APIServer within the same cluster of KubeRay operator

    ```sh
    helm repo add kuberay https://ray-project.github.io/kuberay-helm/
    helm -n ray-system install kuberay-apiserver kuberay/kuberay-apiserver -n ray-system --create-namespace
    ```

3. The APIServer expose service using `NodePort` by default. You can test access by your host and port, the default port is set to `31888`. The examples below assume a kind (localhost) deployment. If Kuberay API server is deployed on another type of cluster, you'll need to adjust the hostname to match your environment.

    ```sh
    curl localhost:31888
    ...
    {
      "code": 5,
      "message": "Not Found"
    }
    ```

4. You can create `RayCluster`, `RayJobs` or `RayService` by dialing the endpoints.

## Swagger Support

Kuberay API server has support for Swagger UI. The swagger page can be reached at:

- [localhost:31888/swagger-ui](localhost:31888/swagger-ui) for local kind deployments
- [localhost:8888/swagger-ui](localhost:8888/swagger-ui) for instances started with `make run` (development machine builds)
- `<host name>:31888/swagger-ui` for nodeport deployments

## HTTP definition endpoints

apiserver supports HTTP requests, for detailed specification checkout [full spec doc](HttpRequestSpec.md).
