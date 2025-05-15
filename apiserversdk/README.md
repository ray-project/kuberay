# KubeRay APIServerSDK

The KubeRay APIServerSDK is the HTTP proxy to the Kubernetes API server with the same
interface. User can directly use Kubernetes OpenAPI Spec and CRD for create, delete, and
update Ray resources. It contains following highlight features:

1. Enable creating ComputeTemplate, which support setting default values that can be used
   in other requests.
2. Compatible with Existing Kubernetes Clients and API Interface, where users can use
   existing Kubernetes clients to interact with the APIServerSDK.
3. Provide APIServerSDK Go library for user to build custom HTTP middleware functions.

## When to use APIServerSDK

KubeRay APIServerSDK featured in simplify Ray resources management by hiding
Kubernetes-specific details. You can considering using APIServerSDK if:

- You want to interact with Ray clusters via HTTP/REST (e.g., from a UI, SDK, or CLI).
- Your team prefers a simplified, non-Kubernetes-specific API surface to manage resources
lifecycles. <!-- TODO: Verify -->
- You want to create templates or defulat values to simplify the configuration setup.

## Installation

- helm chart
- start local APIServer
- Use the Go Library

### Helm Chart

KubeRay Helm charts are hosted on the [ray-project/kuberay-helm](https://github.com/ray-project/kuberay-helm) repository.

## Usage

The KubeRay ApiServer exposes a RESTful API that mirrors the Kubernetes API Server. You
can interact with it using Kubernetes-style endpoints and request patterns for creating,
retrieving, updating, and deleting custom resources such as `RayCluster` and `RayJob`.

### API Structure

The KubeRay API follows standard Kubernetes conventions. The structure of the endpoint
path depends on whether you are interacting with custom resources (e.g. `RayCluster`) or
core Kubernetes resources.

#### Custom Resources

For custom resources defined by CRDs (e.g. `RayCluster`, `RayJob`, etc.), the endpoint format is:

```sh
<baseURL>/apis/<group>/<version>/namespaces/<namespace>/<resourceType>/<resourceName>
```

For Ray's CRDs:

- `group` = `ray.io`
- `version` = `v1`
- `namespace` = your target Kubernetes namespace (e.g., `default`)
- `resourceType` = Custom resource type (e.g. `rayclusters`, `rayjobs`, `rayserve`)
- `resourceName` = name of the resource.

#### Core Kubernetes Resources

For built-in Kubernetes resources (e.g. `ConfigMap`), the endpoint format is:

```sh
<baseURL>/api/v1/namespaces/<namespace>/<resourceType>/<resourceName>
```

- `namespace`: The target namespace
- `resourceType`: Core resource type (e.g. `pods`, `configmaps`, `services`)
- `resourceName`: Name of the resource

### Label and Field Selectors

When listing resources (using `GET` on a collection endpoint), you can filter results using selectors:

- Label selector: label selector filters resources by their labels.

```sh
# Get all RayClusters with the label key=value
GET /apis/ray.io/v1/namespaces/default/rayclusters?labelSelector=key%3Dvalue
```

- Field selector: field selector filters resources based on the value their resource
fields (e.g. `metadata.name`, `status.phase`).

```sh
# Retrieve the RayCluster where the name is raycluster-prod
GET /apis/ray.io/v1/namespaces/default/rayclusters?fieldSelector=metadata.name%3Draycluster-prod
```

- **Combined selectors**: You can combine label and field selectors using `&`

```sh
# Get the RayCluster named raycluster-prod that also has the label env=prod.
GET /apis/ray.io/v1/namespaces/default/rayclusters?labelSelector=env%3Dprod&fieldSelector=metadata.name%3Draycluster-prod
```
