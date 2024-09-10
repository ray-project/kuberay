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

    Description:
    Retrieves the ray cluster information.

    ```
    $ kubectl ray cluster get
    NAME                       NAMESPACE   DESIRED WORKERS   AVAILABLE WORKERS   CPUS   GPUS   TPUS   MEMORY   AGE
    random-kuberay-cluster   default     1                 1                   5      1      0      24Gi     13d
    ```

### Retrieve Ray Cluster Head Logs

    Usage:
    log (RAY_CLUSTER_NAME) [--out-dir directory] [--node-type all|head|worker]
    **Currently only `head` is supported**

    Aliases:
    log, logs

    Description:
    Retrieves ray cluster head pod logs and all the logs under `/tmp/ray/session_latest/logs/`

    ```
    $ kubectl ray cluster logs --out-dir temp-dir
    ...
    $ ls temp-dir/
    stable-diffusion-cluster-head-hqcxt
    $ ls temp-dir/stable-diffusion-cluster-head-hqcxt
    agent-424238335.err dashboard_agent.log gcs_server.err      monitor.err     old                     raylet.out              stdout.log
    agent-424238335.out debug_state.txt     gcs_server.out      monitor.log     ray_client_server.err   runtime_env_agent.err
    dashboard.err       debug_state_gcs.txt log_monitor.err     monitor.out     ray_client_server.out   runtime_env_agent.log
    dashboard.log       events              log_monitor.log     nsight          raylet.err              runtime_env_agent.out
    ```
