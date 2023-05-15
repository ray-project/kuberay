
## Default Ray Start Parameters for Kuberay

This document outlines the default settings for `rayStartParams` in Kuberay.

### Options Exclusive to the Head Pod

- `--redis-password`: Redis password for an external Redis, necessary when [fault tolerance](https://github.com/ray-project/kuberay/blob/master/docs/guidance/gcs-ft.md) is enabled. No default value. See this [example](https://github.com/kevin85421/kuberay/blob/master/ray-operator/config/samples/ray-cluster.external-redis.yaml).

- `--port`: Port for the head Ray process (GCS server). Default is `6379`. Please ensure this value matches the `gcs-server` container port.

- `--dashboard-host`: Host for the dashboard server, either `localhost` (127.0.0.1) or `0.0.0.0` (all interfaces). No default value.

- `--no-monitor`: This option disables the monitor and autoscaler in the **user's container**. It will be automatically set when [autoscaling](https://github.com/ray-project/kuberay/blob/master/docs/guidance/autoscaler.md) is enabled(autoscaling introduces the autoscaler as a sidecar container within the head pod). See [PR #13505](https://github.com/ray-project/ray/pull/13505) for more details. Modification is not recommended.

### Options Exclusive to the worker Pods

- `--address`: Address of the GCS server. Worker pods utilize this address to establish a connection with the Ray cluster. By default, this address takes the form `<FQDN>:<GCS_PORT>`. The `GCS_PORT` corresponds to the value set in the `--port` option. For more insights on Fully Qualified Domain Name (FQDN), refer to [PR #938](https://github.com/ray-project/kuberay/pull/938) and [PR #951](https://github.com/ray-project/kuberay/pull/951).

### Options Applicable to Both Head and Worker Pods

- `--metrics-export-port`: Port for exposing Ray metrics through a Prometheus endpoint. Default is `8080`.

- `--num-cpus`: Number of logical CPUs on the pod. Default is determined by Ray container resource limits. Modify Ray container resource limits instead of this option. See [PR #170](https://github.com/ray-project/kuberay/pull/170).

- `--memory`: Amount of memory on the pod. Default is determined by Ray container resource limits. Modify Ray container resource limits instead of this option. See [PR #170](https://github.com/ray-project/kuberay/pull/170).

- `--num-gpus`: Number of GPUs on the pod. Default is determined by Ray container resource limits. Modify Ray container resource limits instead of this option. See [PR #170](https://github.com/ray-project/kuberay/pull/170).

- `--block`: This option blocks the ray start command indefinitely. It will be automatically set for performance reasons. See [PR #675](https://github.com/ray-project/kuberay/pull/675) for more details. Modification is not recommended.

