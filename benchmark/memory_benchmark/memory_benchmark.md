## Step 1: Create a new Kubernetes cluster

This benchmark will deploy 100 RayCluster custom resources to the Kubernetes cluster.
Each custom resource requires 1 CPU and 1 GB of memory.
We will create a GKE cluster with autoscaling enabled.
The following command creates a Kubernetes cluster named `kuberay-benchmark-cluster` on Google GKE.
The cluster can scale up to 8 nodes, and each node of type `e2-highcpu-16` has 16 CPUs and 16 GB of memory.

```sh
gcloud container clusters create kuberay-benchmark-cluster \
    --num-nodes=1 --min-nodes 0 --max-nodes 8 --enable-autoscaling \
    --zone=us-west1-b --machine-type e2-highcpu-16
```

## Step 2: Install Prometheus and Grafana

```sh
# Path: kuberay/
./install/prometheus/install.sh
```

Follow "Step 2: Install Kubernetes Prometheus Stack via Helm chart" in [prometheus-grafana.md](https://github.com/ray-project/kuberay/blob/master/docs/guidance/prometheus-grafana.md#step-2-install-kubernetes-prometheus-stack-via-helm-chart) to install the [kube-prometheus-stack v48.2.1](https://github.com/prometheus-community/helm-charts/tree/kube-prometheus-stack-48.2.1/charts/kube-prometheus-stack) chart and related custom resources.

## Step 3: Install a KubeRay operator

Follow [this document](https://github.com/ray-project/kuberay/blob/master/helm-chart/kuberay-operator/README.md) to install the latest stable KubeRay operator via Helm repository.

## Step 4: Create RayCluster custom resources periodically.


