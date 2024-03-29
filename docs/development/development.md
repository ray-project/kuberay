# KubeRay Development Guide

This guide provides an overview of the different components in the KubeRay project and instructions for developing and testing each component.
Most developers will be concerned with the KubeRay Operator; the other components are optional.

## KubeRay Operator

The KubeRay Operator is responsible for managing Ray clusters on Kubernetes.
To learn more about developing and testing the KubeRay Operator, please refer to the [Operator Development Guide](https://github.com/ray-project/kuberay/blob/master/ray-operator/DEVELOPMENT.md).

## KubeRay APIServer

The KubeRay APIServer is a central component that exposes the KubeRay API for managing Ray clusters.
For more information about developing and testing the KubeRay APIServer, please refer to the [APIServer Development Guide](https://github.com/ray-project/kuberay/blob/master/apiserver/DEVELOPMENT.md).

## KubeRay CLI

The KubeRay CLI is a command-line interface for interacting with Ray clusters managed by KubeRay.
For more information about developing and testing the KubeRay CLI, please refer to the [CLI Development Guide](https://github.com/ray-project/kuberay/blob/master/cli/README.md).

## Proto and OpenAPI

KubeRay uses Protocol Buffers (protobuf) and OpenAPI specifications to define the API and data structures.
For more information about developing and testing proto files and OpenAPI specifications, please refer to the [Proto and OpenAPI Development Guide](https://github.com/ray-project/kuberay/blob/master/proto/README.md).

## Deploying Documentation Locally

To preview the KubeRay documentation locally, follow these steps:

- Make sure you have Docker installed on your machine.
- Open a terminal and navigate to the root directory of your KubeRay repository.
- Run the following command:

```sh
docker run --rm -it -p 8000:8000 -v ${PWD}:/docs squidfunk/mkdocs-material
```

- Open your web browser and navigate to <http://127.0.0.1:8000/kuberay/> to view the documentation.

If you make any changes to the documentation files, the local preview will automatically update to reflect those changes.
