# Monitoring of the API server and created RayClusters

In order to ensure a proper functioning of the API server and created RayClusters, it is
typically necessary to monitor them. This document describes how to monitor both API
server and created clusters with Prometheus and Grafana.

Current implementation of the API server provides a flag `collectMetricsFlag` that defines whether to collect and
expose Prometheus metrics, which is by default set to `true`. To disable metrics collection, this flag needs to
be set to `false` and the API server image needs to be rebuilt.

If this flag is enabled, a `http` port at `/metrics` endpoint provides API server metrics in Prometheus format.

## Monitoring of the API server

### Deploy KubeRay operator and API server

Refer to [README](README.md) for setting up KubeRay operator and API server. This will
set the flag `collectMetricsFlag` to `true` which enable the metrics collection.

> [!IMPORTANT]
> All the following guidance requires you to switch your working directory to the KubeRay project root

### Install Kubernetes Prometheus Stack via Helm Chart

Please navigate to path `install/prometheus` under to KubeRay repository and execute the
install script there to install the Prometheus Stack on kind cluster.

```sh
cd install/prometheus
bash install.sh
```

You can see multiple pods are created in the `prometheus-system`

```sh
kubectl get pods -n prometheus-system
# NAME                                                     READY   STATUS            RESTARTS   AGE
# alertmanager-prometheus-kube-prometheus-alertmanager-0   2/2     Running   0          2m7s
# prometheus-grafana-84ccb68cc-g9br2                       3/3     Running   0          2m22s
# prometheus-kube-prometheus-operator-895b579fc-f55f6      1/1     Running   0          2m22s
# prometheus-kube-state-metrics-77b6c5d54b-wd2ml           1/1     Running   0          2m22s
# prometheus-prometheus-kube-prometheus-prometheus-0       2/2     Running   0          2m7s
# prometheus-prometheus-node-exporter-fs642                1/1     Running   0          2m22s
```

The `ray-head-monitor` and `ray-workers-monitor` in the `prometheus-system` namespace
created by the script will not be used in this example, we can safely delete them by:

```shell
kubectl delete servicemonitor ray-head-monitor -n prometheus-system
kubectl delete podmonitor ray-workers-monitor -n prometheus-system
```

### Testing monitoring of the API server

Now we can install a [service monitor](deploy/prometheus/api_server_service_monitor.yaml) to scrape API Server metrics into
Prometheus using the following command:

```sh
# Assume you are already in KubeRay project root
kubectl apply -f apiserver/deploy/prometheus/api_server_service_monitor.yaml
```

Then, please open a new terminal and use port-forward to expose Prometheus with:

```shell
kubectl port-forward svc/prometheus-operated -n prometheus-system 9090
```

Now you can point your browser to `http://localhost:9090/` to get a PromQL panel. Start
typing `apiserver` in the search bar, and you will see all of the API server metrics in
Prometheus.

### Monitoring of the Ray Cluster created by the API server

Ray provides
[documentation](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/prometheus-grafana.html#kuberay-prometheus-grafana)
describing how to monitor Ray cluster created using KubeRay operator. As API server is
using KubeRay operator to create the cluster, this documentation can be used directly.

Instead of creating `service monitor` for scraping head node and `pod monitor` for
scraping worker nodes, we will utilize a simpler approach by creating a single [pod
monitor](deploy/prometheus/ray_cluster_pod_monitor.yaml) that can be installed using the
following command:

```sh
kubectl apply -f apiserver/deploy/prometheus/ray_cluster_pod_monitor.yaml
```

Now you can go back to PromQL panel at `http://localhost:9090/`. Go to Status > Targets
pane from the top bar, you should be able to see `podMonitor/prometheus-system/ray-workers-monitor/0` in the list.

Also take a look at the Ray [documentation](https://docs.ray.io/en/master/cluster/kubernetes/k8s-ecosystem/prometheus-grafana.html#kuberay-prometheus-grafana)
for additional monitoring features, including Recording rules, Alerts and Grafana integration.

### Clean up

```sh
make clean-cluster
# Remove apiserver from helm
helm uninstall kuberay-apiserver
# Remove prometheus stack from helm
helm uninstall kube-prometheus-stack -n prometheus-system
```
