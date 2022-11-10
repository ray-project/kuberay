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
If you use the nodeport as service to expose the raycluster endpoint, like dashboard or redis, there are `endpoints` field in the status to record the service endpoints.

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

In this section we will describe how to monitor Ray Clusters in Kubernetes using Prometheus & Grafana.

We also leverage the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) to start the whole monitoring system.

Requirements:
- Prometheus deployed in Kubernetes
  - Required CRD: `servicemonitors.monitoring.coreos.com`
  - Requered CRD: `podmonitors.monitoring.coreos.com`
- Grafana up and running

### Enable Ray Cluster Metrics

Before we define any Prometheus objects, let us first enable and export metrics to a specific port.

To enable ray metrics on Head node or a worker node, we need to pass the following option `--metrics-expose-port=9001`. We can set the specific option by adding `metrics-export-port: "9001"` to the head node & worker nodes in the rayclusters.ray.io manifest.

We also need to export port `9001` in the head node & worker nodes

```yaml
apiVersion: ray.io/v1alpha1
kind: RayCluster
metadata:
  ...
  name: ray-cluster
spec:
  enableInTreeAutoscaling: true
  headGroupSpec:
    rayStartParams:
      ...
      metrics-export-port: "9001"      <--- Enable for the head node
    ...
    template:
      metadata:
        ...
      spec:
        ...
        containers:
        - ports:
          - containerPort: 10001
            name: client
            protocol: TCP
          - containerPort: 8265
            name: dashboard
            protocol: TCP
          - containerPort: 8000
            name: ray-serve
            protocol: TCP
          - containerPort: 6379
            name: redis
            protocol: TCP
          - containerPort: 9001
            name: metrics
            protocol: TCP
  workerGroupSpecs:
  - groupName: workergroup
    ...
    rayStartParams:
      ...
      metrics-export-port: "9001"      <--- Enable for worker nodes
    ...
    template:
      metadata:
        ...
      spec:
        ...
        containers:
        - ports:
          - containerPort: 9001
            name: metrics
            protocol: TCP
        ...
...
```

If you use `$kuberay/helm-chart/ray-cluster`, then you can add it in the `values.yaml` 

```yaml
head:
  groupName: headgroup
  ...
  initArgs:
    metrics-export-port: "9001"      <--- Enable for the head node
    ...
  ports:
  - containerPort: 10001
    protocol: TCP
    name: "client"
  - containerPort: 8265
    protocol: TCP
    name: "dashboard"
  - containerPort: 8000
    protocol: TCP
    name: "ray-serve"
  - containerPort: 6379
    protocol: TCP
    name: "redis"
  - containerPort: 9001              <--- Enable this port
    protocol: TCP
    name: "metrics"
  ...
worker:
  groupName: workergroup
  ...
  initArgs:
    ...
    metrics-export-port: "9001"      <--- Enable for the head node
  ports:
  - containerPort: 9001              <--- Enable this port
    protocol: TCP
    name: "metrics"
  ...
...
```

Deploying the cluster with the above options should export metrics on port `9001`. To check, we can port-forward port `9001` to our localhost and query via curl.

```bash
k port-forward <ray-head-node-id> 9001:9001
```

From a second terminal issue

```bash
$> curl localhost:9001
# TYPE ray_pull_manager_object_request_time_ms histogram
...
ray_pull_manager_object_request_time_ms_sum{Component="raylet",...
...
```

Before we move on, first ensure that the required metrics port is also defined in the Ray's cluster Kubernetes service. This is done automatically via the Ray Operator if you define the metrics port `containerPort: 9001` along with the name and protocol. 

```bash
$> kubectl get svc <ray-cluster-name>-head-svc -o yaml
NAME  TYPE       ...   PORT(S)                                         ...
...   ClusterIP  ...   6379/TCP,9001/TCP,10001/TCP,8265/TCP,8000/TCP   ...
```

We are now ready to create the required Prometheus CRDs to collect metrics

### Collect Head Node metrics with ServiceMonitors

Prometheus provides a CRD that targets Kubernetes services to collect metrics. The idea is that we will define a CRD that will have selectors that match the Ray Cluster Kubernetes service labels and ports, the metrics port.

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: <ray-cluster-name>-head-monitor              <-- Replace <ray-cluster-name> with the actual Ray Cluster name
  namespace: <ray-cluster-namespace>                 <-- Add the namespace of your ray cluster
spec:
  endpoints:
  - interval: 1m
    path: /metrics
    scrapeTimeout: 10s
    port: metrics
  jobLabel: <ray-cluster-name>-ray-head              <-- Replace <ray-cluster-name> with the actual Ray Cluster name
  namespaceSelector:
    matchNames:
    - <ray-cluster-namespace>                        <-- Add the namespace of your ray cluster
  selector:
    matchLabels:
      ray.io/cluster: <ray-cluster-name>             <-- Replace <ray-cluster-name> with the actual Ray Cluster name
      ray.io/identifier: <ray-cluster-name>-head     <-- Replace <ray-cluster-name> with the actual Ray Cluster name
      ray.io/node-type: head
  targetLabels:
  - ray.io/cluster
```

A notes for the `targetLabels`. We added `spec.targetLabels[0].ray.io/cluster` because we want to include the name of the ray cluster in the metrics that will be generated by this service monitor. The `ray.io/cluster` label is part of the Ray head node service and it will be transformed to a `ray_io_cluster` metric label. That is, any metric that will be imported, will also container the following label `ray_io_cluster=<ray-cluster-name>`. This may seem like optional but it becomes mandatory if you deploy multiple ray clusters.

Create the above service monitor by issuing

```bash
k apply -f serviceMonitor.yaml
```

After a while, Prometheus should start scraping metrics from the head node. You can confirm that by visiting the Prometheus web ui and start typing `ray_`. Prometheus should create a dropdown list with suggested Ray metrics.

```bash
curl 'https://<prometheus-endpoint>/api/v1/query?query=ray_object_store_available_memory' -H 'Accept: */*'
```

### Collect Worker Node metrics with PodMonitors

Ray operator does not create a Kubernetes service for the ray workers, therefore we can not use a Prometheus ServiceMonitors to scrape the metrics from our workers. 

**Note**: We could create a Kubernetes service with selectors a common label subset from our worker pods, however this is not ideal because our workers are independent from each other, that is, they are not a collection of replicas spawned by replicaset controller. Due to that, we should avoid using a Kubernetes service for grouping them together.

To collect worker metrics, we can use `Prometheus PodMonitros CRD`.

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  labels:
    ray.io/cluster: <ray-cluster-name>               <-- Replace <ray-cluster-name> with the actual Ray Cluster name
  name: <ray-cluster-name>-workers-monitor           <-- Replace <ray-cluster-name> with the actual Ray Cluster name
  namespace: <ray-cluster-namespace>                 <-- Add the namespace of your ray cluster
spec:
  jobLabel: <ray-cluster-name>-ray-workers           <-- Replace <ray-cluster-name> with the actual Ray Cluster name
  namespaceSelector:
    matchNames:
    - <ray-cluster-namespace>                        <-- Add the namespace of your ray cluster
  podMetricsEndpoints:
  - interval: 30s
    port: metrics
    scrapeTimeout: 10s
  podTargetLabels:
  - ray.io/cluster
  selector:
    matchLabels:
      ray.io/is-ray-node: "yes"
      ray.io/node-type: worker
```

Since we are not selecting a Kubernetes service but pods, our `matchLabels` now define a set of labels that is common on all Ray workers.

We also define `metadata.labels` by manually adding `ray.io/cluster: <ray-cluster-name>` and then instructing the PodMonitors resource to add that label in the scraped metrics via `spec.podTargetLabels[0].ray.io/cluster`.

Apply the above PodMonitor manifest

```bash
k apply -f podMonitor.yaml
```

Last, wait a bit and then ensure that you can see Ray worker metrics in Prometheus

```bash
curl 'https://<prometheus-endpoint>/api/v1/query?query=ray_object_store_available_memory' -H 'Accept: */*'
```

The above http query should yield metrics from the head node and your worker nodes

We have everything we need now and we can use Grafana to create some panels and visualize the scrapped metrics

### Grafana: Visualize ingested Ray metrics

You can use the json in `config/grafana` to import in Grafana the Ray dashboards.

### Custom Metrics & Alerting 

We can also define custom metrics, and create alerts by using `prometheusrules.monitoring.coreos.com` CRD. Because custom metrics, and alerting is different for each team and setup, we have included an example under `$kuberay/config/prometheus/rules` that you can use to build custom metrics and alerts


