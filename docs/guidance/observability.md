# Observability

## RayCluster Status

### State

In the RayCluster resource definition, we use `State` to represent the current status of the Ray cluster.

For now, there are three types of the status exposed by the RayCluster's status.state: `ready`, `unhealthy` and `failed`.

| State     | Description                                                                                     |
| --------- | ----------------------------------------------------------------------------------------------- |
| ready     | The Ray cluster is ready for use.                                                               |
| unhealthy | The `rayStartParams` are misconfigured and the Ray cluster may not function properly.           |
| failed    | A severe issue has prevented the head node or worker nodes from starting.                       |

If you use the apiserver to retrieve the resource, you may find the state in the `clusterState` field.

```json
curl --request GET '<baseUrl>/apis/v1alpha2/namespaces/<namespace>/clusters/<raycluster-name>'
{
    "name": "<raycluster-name>",
    "namespace": "<namespace>",
    //...
    "createdAt": "2022-08-10T10:31:25Z",
    "clusterState": "ready",
    //...
}
```

### Endpoint

If you use the nodeport as service to expose the raycluster endpoint, like dashboard or redis, there
are `endpoints` field in the status to record the service endpoints.

you can directly use the ports in the `endpoints` to connect to the related service.

Also, if you use apiserver to retrieve the resource, you can find the endpoints in the `serviceEndpoint` field.

```json
curl --request GET '<baseUrl>/apis/v1alpha2/namespaces/<namespace>/clusters/<raycluster-name>'
{
    "name": "<raycluster-name>",
    "namespace": "<namespace>",
    //...
    "serviceEndpoint": {
        "dashboard": "30001",
        "head": "30002",
        "metrics": "30003",
        "redis": "30004"
    },
    //...
}
```

## Ray Cluster: Monitoring with Prometheus & Grafana

See [prometheus-grafana.md](./prometheus-grafana.md) for more details.

## Profiling with KubeRay

See [profiling.md](./profiling.md) for more details.
