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

> Emphasis here that the interface is the same as k9s apiserver, add some overview
> template for CRUD
