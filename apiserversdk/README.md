# KubeRay APIServer

The KubeRay APIServer provides an HTTP proxy to the Kubernetes APIserver with the same
interface. Users can directly use Kubernetes OpenAPI Spec and KubeRay CRD to create, query,
update, and delete Ray resources. It contains following highlight features:

1. Compatible with existing Kubernetes clients and API interface, where users can use
   existing Kubernetes clients to interact with the proxy provided by APIServer.
2. Provide APIServer as a Go library for users to build their proxies with custom HTTP middleware functions.

## When to use APIServer

You can consider using APIServer if:

- You want to manage Ray clusters in Kubernetes via HTTP/REST (e.g., from a UI, SDK, or CLI).
- You want to create templates or default values to simplify the configuration setup.

## Installation

Please follow the [Installation](./Installation.md) guide to install the APIServer.

## Quick Start

- [RayCluster Quickstart](./docs/raycluster-quickstart.md)
- [RayJob Quickstart](./docs/rayjob-quickstart.md)
- [RayService Quickstart](./docs/rayservice-quickstart.md)

## Usage

The KubeRay APIServer exposes a RESTful API that mirrors the Kubernetes APIServer. You
can interact with it using Kubernetes-style endpoints and request patterns for creating,
retrieving, updating, and deleting custom resources such as `RayCluster` and `RayJob`.

### API Structure

The KubeRay API follows standard Kubernetes conventions. The structure of the endpoint
path depends on whether you are interacting with custom resources (e.g., `RayCluster`) or
core Kubernetes resources.

#### Custom Resources

For custom resources defined by CRDs (e.g., `RayCluster`, `RayJob`, etc.), the endpoint format is:

```sh
<baseURL>/apis/ray.io/v1/namespaces/<namespace>/<resourceType>/<resourceName>
```

- `namespace` = your target Kubernetes namespace (e.g., `default`)
- `resourceType` = Custom resource type (e.g., `rayclusters`, `rayjobs`, `rayservices`)
- `resourceName` = name of the resource.

### Label and Field Selectors

When listing resources (using `GET` on a collection endpoint), you can filter results using selectors:

- Label selector: filters resources by their labels.

```sh
# Get all RayClusters with the label key=value
GET /apis/ray.io/v1/namespaces/default/rayclusters?labelSelector=key%3Dvalue
```

- Field selector: filters resources based on the value their resource
fields (e.g., `metadata.name`, `status.phase`).

```sh
# Retrieve the RayCluster where the name is raycluster-prod
GET /apis/ray.io/v1/namespaces/default/rayclusters?fieldSelector=metadata.name%3Draycluster-prod
```

- Combined selectors: You can combine label and field selectors using `&`

```sh
# Get the RayCluster named raycluster-prod that also has the label env=prod.
GET /apis/ray.io/v1/namespaces/default/rayclusters?labelSelector=env%3Dprod&fieldSelector=metadata.name%3Draycluster-prod
```
