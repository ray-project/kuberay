# KubeRay CLI

[![Build Status](https://github.com/ray-project/kuberay/workflows/Go-build-and-test/badge.svg)](https://github.com/ray-project/kuberay/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/ray-project/kuberay)](https://goreportcard.com/report/github.com/ray-project/kuberay)

# Overview
KubeRay CLI provides the ability to manage kuberay resources (ray clusters, compute templates etc) through command line interface.

# Implementation
 - Kuberay CLI uses [Cobra framework](https://github.com/spf13/cobra) for the CLI application.
 - Kuberay CLI depends on kuberay apiserver to manage these resources by sending grpc requests to the kuberay apiserver.

# Installation
TBD

# Prerequisites
 - Kuberay apiserver needs to be running and accessible.

# Build
`cd kuberay/cli`   
`go build -o kuberay -a main.go`

# Use
## Connect to kuberay apiserver
 - Default kuberay apiserver endpoint: `127.0.0.1:8887`.
 - If kuberay apiserver is not run locally, this must be set in order to manage ray clusters and ray compute templates.
### Read current kuberay apiserver endpoint
 `./kuberay config get endpoint`
### Reset kuberay apiserver endpoint to default (`127.0.0.1:8887`)
 `./kuberay config reset endpoint`
### Set kuberay apiserver endpoint
 `./kuberay config set endpoint <kuberay apiserver endpoint>`
## Manage Ray Clusters
### Create a Ray Cluster
````
Usage:
kuberay cluster create [flags]

Flags:
--environment string               environment of the cluster (valid values: DEV, TESTING, STAGING, PRODUCTION) (default "DEV")
--head-compute-tempalte string     compuate template name for ray head
--head-image string                ray head image
--head-service-type string         ray head service type (ClusterIP, NodePort, LoadBalancer) (default "ClusterIP")
--name string                      name of the cluster
--namespace string                 kubernetes namespace where the cluster will be (default "ray-system")
--user string                      SSO username of ray cluster creator
--version string                   version of the ray cluster (default "1.9.0")
--worker-compute-template string   compute template name of worker in the first worker group
--worker-group-name string         first worker group name
--worker-image string              image of worker in the first worker group
--worker-replicas uint32           pod replicas of workers in the first worker group (default 1)
````
 - Limitation: Currently only one worker compute template is supported during creation. 
### Get a Ray Cluster
 `./kuberay cluster get <cluster name>`
### List Ray Clusters
 `./kuberay cluster list`
### Delete a Ray Cluster
`./kuberay cluster delete <cluster name>`
## Manage Ray Compute Template
### Create a Compute Template
````
Usage:
  kuberay template compute create [flags]

Flags:
      --cpu uint32               ray pod CPU (default 1)
      --gpu uint32               ray head GPU
      --gpu-accelerator string   GPU Accelerator type
      --memory uint32            ray pod memory in GB (default 1)
      --name string              name of the compute template

````
### Get a Ray Compute Template
 `./kuberay template compute get <compute template name>`
### List Ray Compute Templates
 `./kuberay template compute list`
### Delete a Ray Compute Template
 `./kuberay template compute delete <compute template name>`

