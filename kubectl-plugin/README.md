# Kuberay Kubectl Plugin

Kubectl plugin/extension for Kuberay CLI that provides the ability to manage ray resources.

## Installation

<!-- 1. Check [release page](https://github.com/ray-project/kuberay/releases) and download the necessary binaries. -->
1. Run `go build cmd/kubectl-ray.go`
2. Move the binary, which will be named `kubectl-ray` to your `PATH`

## Prerequisites

1. Make sure there is a Kubernetes cluster running with KubeRay installed.
2. Make sure `kubectl` has the right context.

## Usage

### Retrieve Ray Clusters

    Usage:
    ray cluster get [flags]

    Aliases:
    get, list

    Flags:
    -A, --all-namespaces                 If present, list the requested clusters across all namespaces. Namespace in current context is ignored even if specified with --namespace.
        --as string                      Username to impersonate for the operation. User could be a regular user or a service account in a namespace.
        --as-group stringArray           Group to impersonate for the operation, this flag can be repeated to specify multiple groups.
        --as-uid string                  UID to impersonate for the operation.
        --cache-dir string               Default cache directory
        --certificate-authority string   Path to a cert file for the certificate authority
        --client-certificate string      Path to a client certificate file for TLS
        --client-key string              Path to a client key file for TLS
        --cluster string                 The name of the kubeconfig cluster to use
        --context string                 The name of the kubeconfig context to use
        --disable-compression            If true, opt-out of response compression for all requests to the server
    -h, --help                           help for get
        --insecure-skip-tls-verify       If true, the server's certificate will not be checked for validity. This will make your HTTPS connections insecure
        --kubeconfig string              Path to the kubeconfig file to use for CLI requests.
    -n, --namespace string               If present, the namespace scope for this CLI request
        --request-timeout string         The length of time to wait before giving up on a single server request. Non-zero values should contain a corresponding time unit (e.g. 1s, 2m, 3h). A value of zero means don't timeout requests. (default "0")
    -s, --server string                  The address and port of the Kubernetes API server
        --tls-server-name string         Server name to use for server certificate validation. If it is not provided, the hostname used to contact the server is used
        --token string                   Bearer token for authentication to the API server
        --user string                    The name of the kubeconfig user to use
