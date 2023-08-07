# Kuberay API Server User Guide

This guide covers the purpose, requirements, and deployment of the Kuberay API Server.

## Requirements

| Software | Version  |                                                            Link |
| :------- | :------: | ------------------------------------------------------------: |
| kubectl  | v1.18.3+ | [Download](https://kubernetes.io/docs/tasks/tools/install-kubectl/) |
| Go       |  v1.17   |                            [Download](https://golang.org/dl/) |
| Docker   |  19.03+  |                  [Download](https://docs.docker.com/install/) |
| GNU Make |  3.81+   |                                                               |
| curl     |  7.88+   |                                                               |
| kind     |  v0.19.0 | [Install](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) |
| helm     | v3.12.1  | [Install](https://helm.sh/docs/intro/install/) |

## Purpose

The Kuberay API Server is designed to simplify the lifecycle management of Ray clusters for users who may not be well-versed in Kubernetes. It provides a RESTful web service to manage Ray cluster Kubernetes resources.

## Build and Deployment

The backend service can be deployed locally or within a Kubernetes cluster. The HTTP service listens on port 8888.

### Pre-requisites

Ensure that the admin Kubernetes configuration file is located at `~/.kube/config`. As convenience there are two makefile targets provided to help you manage a local kind cluster:

* `make cluster` -- creates a 3 node cluster (1 control plane 2 worker) named ray-api-server-cluster
* `make clean-cluster` -- deletes the cluster created with the `cluster` target

### Local Development

#### Build

```bash
#To build the executable
make build

#To start the executable build above
../bin/kuberay-apiserver
```

#### Test

```bash
#To run the unit tests
make test
```

#### Start Local Service

This will start the api server on your development machine. The golang race detector is turned on when starting the api server this way. It will use Kubernetes configuration file located at `~/.kube/config`. The service will not start if you do not have a connection to a Kubernetes cluster.

```bash
make run
```

#### Access

Access the service at `localhost:8888` for http, and `locahost:8887` for the RPC port.

### Kubernetes Deployment

#### Build Image

```bash
#creates an image with the tag kuberay/apiserver:latest
make docker-image 
```

#### Start Kubernetes Deployment

```bash
#To use the  helm charts
make deploy

#To use the configuration from deploy/base
make install
```

#### Stop Kubernetes Deployment

```bash
#To use the  helm charts
make undeploy

#To use the configuration 
make uninstall
```

#### Local Kind Cluster Deployment

For local development the following `make` targets are provided as a convenience.

* `make cluster` -- creates a local kind cluster, using the configuration from `hack/kind-cluster-config.yaml`. It creates a port mapping allowing for the service running in the kind cluster to be accessed on  `localhost:318888` for HTTP and `localhost:318887` for RPC.
* `make clean-cluster` -- deletes the local kind cluster created with `make cluster`
* `load-image` -- loads the docker image defined by the `IMG` make variable into the kind cluster. The default value for variable is: `kuberay/apiserver:latest`. The name of the image can be changed by using `make load-image -e IMG=<your image name and tag>`
* `operator-image` -- Build the operator image to be loaded in your kind cluster. The tag for the operator image is `kuberay/operator:latest`. This step is optional.
* `load-operator-image` -- Load the operator image to the kind cluster created with `create-kind-cluster`. The tag for the operator image is `kuberay/operator:latest`, and the tag can be overriden using `make load-operator-image -E OPERATOR_IMAGE_TAG=<operator tag>`. To use the nightly operator tag, set the tag to 'nightly`.`
* `deploy-operator` -- Deploy operator into your cluster.  The tag for the operator image is `kuberay/operator:latest`.
* `undeploy-operator` -- Undeploy operator from your cluster

#### Access API Server in the Cluster

Access the service at `localhost:318888` for http and `locahost:318887` for the RPC port.
