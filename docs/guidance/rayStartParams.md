
## Default Ray Start Parameters for KubeRay

This document outlines the default settings for `rayStartParams` in KubeRay.


### Options Exclusive to the Head Pod

- `--dashboard-host`: Host for the dashboard server, either `localhost` (127.0.0.1) or `0.0.0.0` (all interfaces).
The default value for both Ray and KubeRay 0.5.0 is `localhost`, however, for versions of KubeRay after 0.5.0, the default value will be `0.0.0.0`.


- `--no-monitor`: This option disables the monitor and autoscaler in the **user's container**. It will be automatically set when [autoscaling](https://github.com/ray-project/kuberay/blob/master/docs/guidance/autoscaler.md) is enabled. The autoscaling feature introduces the autoscaler as a sidecar container within the head pod, thereby obviating the need for a monitor and autoscaler in the **user's container**. See [PR #13505](https://github.com/ray-project/ray/pull/13505) for more details. Modification is not recommended.


- `--port`: Port for the GCS server. The port is set to `6379` by default. Please ensure that this value matches the `gcs-server` container port in Ray head container.

- `--redis-password`: Redis password for an external Redis, necessary when [fault tolerance](https://github.com/ray-project/kuberay/blob/master/docs/guidance/gcs-ft.md) is enabled. 
The default value is `""` after Ray 2.3.0. See [#929](https://github.com/ray-project/kuberay/pull/929) for more details. 

### Options Exclusive to the worker Pods

- `--address`: Address of the GCS server. Worker pods utilize this address to establish a connection with the Ray cluster. By default, this address takes the form `<FQDN>:<GCS_PORT>`. The `GCS_PORT` corresponds to the value set in the `--port` option. For more insights on Fully Qualified Domain Name (FQDN), refer to [PR #938](https://github.com/ray-project/kuberay/pull/938) and [PR #951](https://github.com/ray-project/kuberay/pull/951).

### Options Applicable to Both Head and Worker Pods

- `--block`: This option blocks the ray start command indefinitely. It will be automatically set by KubeRay. See [PR #675](https://github.com/ray-project/kuberay/pull/675) for more details. Modification is not recommended.

- `--memory`: Amount of memory on this Ray node. Default is determined by Ray container resource limits. Modify Ray container resource limits instead of this option. See [PR #170](https://github.com/ray-project/kuberay/pull/170).

- `--metrics-export-port`: Port for exposing Ray metrics through a Prometheus endpoint. The port is set to `8080` by default. Please ensure that this value matches the `metrics` container port if you need to customize it. See [PR #954](https://github.com/ray-project/kuberay/pull/954) and [prometheus-grafana doc](https://github.com/ray-project/kuberay/blob/master/docs/guidance/prometheus-grafana.md) for more details.

- `--num-cpus`: Number of logical CPUs on this Ray node. Default is determined by Ray container resource limits. Modify Ray container resource limits instead of this option. See [PR #170](https://github.com/ray-project/kuberay/pull/170).

- `--num-gpus`: Number of GPUs on this Ray node. Default is determined by Ray container resource limits. Modify Ray container resource limits instead of this option. See [PR #170](https://github.com/ray-project/kuberay/pull/170).


