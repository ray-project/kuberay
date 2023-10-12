# Monitoring of the API server and created Ray clusters

In order to ensure a proper functioning of the API server and created Ray clusters, it is typically necessary to
monitor the API server itself and created Ray clusters. This document describes how to instrument both API server
and created clusters with Prometheus and Grafana

## Monitoring of the API server

Current implementation of the API server provides a flag `collectMetricsFlag` that defines whether to collect and
expose Prometheus metrics, which is by default set to `true`. To disable metrics collection, this flag needs to
be set to `false` and the API server image needs to be rebuild.

If this flag is enabled, an `http` port at `/metrics` endpoint provides API server metrics in Prometheus format.

### Testing monitoring of the API server

On the kind server Install Kubernetes Prometheus Stack via Helm chart using the following
[command](../install/prometheus/install.sh). The script additionally creates `ray-head-monitor` and
`ray-workers-monitor` in the `prometheus-system` namespace, that we do not need. We can delete them using:

```shell
kubectl delete servicemonitor ray-head-monitor -n prometheus-system
kubectl delete podmonitor ray-workers-monitor -n prometheus-system
```

To install API server, please follow the instructions from the [README.md](README.md) on installing
API server using Helm

Now we can install a [service monitor](deploy/prometheus/api_server_service_monitor.yaml) to scrape Api Server metrics into
Prometheus using the following command:

```shell
kubectl apply -f deploy/prometheus/api_server_service_monitor.yaml
```

With this in place, you can use port-forward to expose Prometheus:

```shell
kubectl port-forward svc/prometheus-operated -n prometheus-system 9090
```

Now you can point your browser to `http://localhost:9090/` to get a PromQL panel. Start typing `apiserver` and
you will see all of the api server metrics in Prometheus.

## Monitoring of the Ray Cluster created by the API server

Ray provides [documentation](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/prometheus-grafana.html#kuberay-prometheus-grafana)
describing how to monitor Ray cluster created using KubeRay operator. Because API server is using KubeRay operator
to create the cluster. this documentation can be used directly. Here we will show a slightly simpler approach to
monitor cluster. Instead of creating `service monitor` for scraping head node and `pod monitor` for scraping worker
nodes we suggest we create a single [pod monitor](deploy/prometheus/ray_cluster_pod_monitor.yaml) that can be installed using
the following command:

```shell
kubectl apply -f deploy/prometheus/ray_cluster_pod_monitor.yaml
```

With this in place, you can again use port-forward to expose Prometheus:

```shell
kubectl port-forward svc/prometheus-operated -n prometheus-system 9090
```

Now you can point your browser to `http://localhost:9090/` to get a PromQL panel. Start typing `ray` and
you will see all of the Ray cluster metrics in Prometheus.

Also take a look at the Ray [documentation](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/prometheus-grafana.html#kuberay-prometheus-grafana)
for additional monitoring features, including Recording rules, Alerts and Grafana integration.
