# Kuberay API Server User Guide

This guide covers the purpose, requirements, and deployment of the Kuberay API Server.

## Requirements

| Software | Version  |                                                            Link |
| :------- | :------: | ------------------------------------------------------------: |
| kubectl  | v1.18.3+ | [Download](https://kubernetes.io/docs/tasks/tools/install-kubectl/) |
| Go       |  v1.13+  |                            [Download](https://golang.org/dl/) |
| Docker   |  19.03+  |                  [Download](https://docs.docker.com/install/) |

## Purpose

The Kuberay API Server is designed to simplify the lifecycle management of Ray clusters for users who may not be well-versed in Kubernetes. It provides a RESTful web service to manage Ray cluster Kubernetes resources.

## Build and Deployment

The backend service can be deployed locally or within a Kubernetes cluster. The HTTP service listens on port 8888.

### Pre-requisites

Ensure that the admin Kubernetes configuration file is located at `~/.kube/config`.

### Local Deployment

#### Build

```bash
go build -a -o raymgr cmd/main.go
```

#### Start Service

```bash
./raymgr
```

#### Access

Access the service at `localhost:8888`.

### Kubernetes Deployment

#### Build

```bash
./docker-image-builder.sh
```

This script will build and optionally push the image to the remote Docker Hub (hub.byted.org).

#### Start Service

```bash
kubectl apply -f deploy/
```

#### Access

To obtain the port, run the following command:

```bash
NODE_PORT=$(kubectl get -o jsonpath="{.spec.ports[0].nodePort}" services backend-service -n ray-system)
```

To obtain the node, run the following command:

```bash
NODE_IP=$(kubectl get nodes -o jsonpath='{ $.items[*].status.addresses[?(@.type=="InternalIP")].address }')
```

Select any IP address from the output, and use `NODE_IP:NODE_PORT` to access the service.
