# Explanation and Best Practice for workers-head Reconnection

## Problem

For a `RayCluster` with a head and several workers, if a worker is crashed, it will be relaunched immediately and re-join the same cluster quickly; however, when the head is crashed, it will run into the issue [#104](https://github.com/ray-project/kuberay/issues/104) that all worker nodes are lost from the head for a long period of time. 

## Explanation

When the head pod was deleted, it will be recreated with a new IP by KubeRay controller，and the GCS server address is changed accordingly. The Raylets of all workers will try to get GCS address from Redis in `ReconnectGcsServer`, but the redis_clients always use the previous head IP, so they will always fail to get new GCS address. The Raylets will not exit until max retries are reached. There are two configurations determining this long delay:

```
/// The interval at which the gcs rpc client will check if gcs rpc server is ready.
RAY_CONFIG(int64_t, ping_gcs_rpc_server_interval_milliseconds, 1000)

/// Maximum number of times to retry ping gcs rpc server when gcs server restarts.
RAY_CONFIG(int32_t, ping_gcs_rpc_server_max_retries, 600)

https://github.com/ray-project/ray/blob/98be9fb5e08befbd6cac3ffbcaa477c5117b0eef/src/ray/gcs/gcs_client/gcs_client.cc#L294-L295
```

It retries 600 times and each interval is 1s, resulting in total 600s timeout, i.e. 10 min. So immediately after 10-min wait for retries, each client exits and gets restarted while connecting to the new head IP. This issue exists in all stable ray versions (including 1.9.1). This has been reduced to 60s in recent commit in master. 

## Best Practice

GCS FT feature is now alpha release, for further understand we can rely on the FT feature. To enable the GCS, please refer to [Ray GCS Fault Tolerance](https://github.com/ray-project/kuberay/blob/master/docs/guidance/gcs-ft.md)

Also, to solve the workers-head connection lost, there are two others options:

- Make head more stable: when creating the cluster, allocate sufficient amount of resources on head pod such that it tends to be stable and not easy to crash. You can also set {"num-cpus": "0"} in "rayStartParams" of "headGroupSpec" such that Ray scheduler will skip the head node when scheduling workloads. This also helps to maintain the stability of the head. 

- Make reconnection shorter: for version <= 1.9.1, you can set this head param --system-config='{"ping_gcs_rpc_server_max_retries": 20}' to reduce the delay from 600s down to 20s before workers reconnect to the new head. 
