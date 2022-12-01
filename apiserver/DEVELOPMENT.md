# User Guide

This guide documents the purpose and deployment of kuberay-apiserver.

## Requirements

| software | version  |                                                                link |
| :------- | :------: | ------------------------------------------------------------------: |
| kubectl  | v1.18.3+ | [download](https://kubernetes.io/docs/tasks/tools/install-kubectl/) |
| go       |  v1.13+  |                                  [download](https://golang.org/dl/) |
| docker   |  19.03+  |                        [download](https://docs.docker.com/install/) |

## Purpose
Lifecycle management of ray cluster may not be friendly for kubernetes nonexperts.
Backend service is intended to provide a RESTful web service to manage ray cluster kubernetes resource.

## Build and Deployment
Backend service can be deployed locally, or in kubernetes cluster itself. The http service is listening on port 8888.

### Pre-requisites
admin kube config file is located at ~/.kube/config

### Local Deployment
#### Build
```
go build -a -o raymgr cmd/main.go
```

#### Start Service
```
./raymgr
```
#### Access
localhost:8888

### Kubernetes Deployment
#### Build
```
./docker-image-builder.sh
```
This script will build and optionally push the image to the remote docker hub (hub.byted.org, TODO: make it configurable).
#### Start Service
```
kubectl apply -f deploy/
```
#### Access
To get port 

```
NODE_PORT=$(kubectl get -o jsonpath="{.spec.ports[0].nodePort}" services backend-service -n ray-system)
```
To get node
```
NODE_IP=$(kubectl get nodes -o jsonpath='{ $.items[*].status.addresses[?(@.type=="InternalIP")].address }')
```
and pick any ip address

Use NODE_IP:NODE_PORT to access