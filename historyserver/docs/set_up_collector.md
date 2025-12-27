# KubeRay History Server - Collector

> [!NOTE]
> The KubeRay History Server is currently under active development. This document aims to provide the step-by-step
guideline to set up the Collector component for local development and testing.

## Table of Contents

TBD...

## Materials for Learning KubeRay History Server

* [REP: Ray History Server #62](https://github.com/ray-project/enhancements/pull/62)
* [Design doc](https://docs.google.com/document/d/15Y2bW4uzeUJe84FxRNUnHozoQPqYdLB2yLmgrdF2ZmI/edit?pli=1&tab=t.0#heading=h.xrvvvqarib6g)
* Related issues
  * [\[Epic\]\[Feature\] history server collector #4274](https://github.com/ray-project/kuberay/issues/4274)
  * [\[Epic\]\[Feature\] Support History Server #3966](https://github.com/ray-project/kuberay/issues/3966)
  * [\[Feature\] Ray History Server #3884](https://github.com/ray-project/kuberay/issues/3884)
* Related PRs
  * [Historyserver beta version #4187](https://github.com/ray-project/kuberay/pull/4187)
  * [add the implementation of historyserver collector #4241](https://github.com/ray-project/kuberay/pull/4241)
  * [add the implementation of historyserver #4242](https://github.com/ray-project/kuberay/pull/4242)

## Test the Collector on the Kind Cluster

### Prerequisites

Please ensure your environment matches the version requirements specified in the [ray-operator development guide](https://github.com/ray-project/kuberay/blob/2959d7d8a4174eedbf7b4a71a79219547f62cc82/ray-operator/DEVELOPMENT.md):

* Go v1.24+
* Docker: Engine for building the container image
* GNU Make
* K9s (optional)

### Spin Up a Kind Cluster and Run KubeRay Operator

The following environment setup is based on the [ray-operator development guide](https://github.com/JiangJiaWei1103/kuberay/blob/e4d8ad6e34adbe13b4c77c35313af2c9bc16da82/ray-operator/DEVELOPMENT.md#run-the-operator-inside-the-cluster).

```bash
# Clone the KubeRay repo and cd into the working dir.
git clone https://github.com/ray-project/kuberay.git
cd kuberay

# Spin up a kind cluster.
kind create cluster --image=kindest/node:v1.26.0

# Build the KubeRay operator image.
IMG=kuberay/operator:latest make -C ray-operator docker-build

# Load the custom KubeRay image into the kind cluster.
kind load docker-image kuberay/operator:latest

# Install the KubeRay operator with the custom image via local Helm chart.
helm install kuberay-operator \
  --set image.repository=kuberay/operator \
  --set image.tag=latest \
  ./helm-chart/kuberay-operator

# Check the logs via kubectl or k9s.
kubectl logs deployments/kuberay-operator
# or
k9s
```

### Build the Collector Container Image

```bash
# Build the collector image.
make -C historyserver localimage-collector

# Check the built image.
docker images | grep collector
```

You're supposed to see a `collector:v0.1.0` image. If you'd like to change the image reference, please feel free to tag
it.

### Load the Collector Image into the Kind Cluster

```bash
# Load the image into the kind cluster.
kind load docker-image collector:v0.1.0

# Check the loaded image.
docker exec -it kind-control-plane crictl images | grep collector
```

### Deploy MinIO for Log and Event Storage

Take s3 as an example, you need to deploy minio using the following commands:

```bash
# Apply the minio manifest.
kubectl apply -f historyserver/config/minio.yaml

# Port-forward the minio UI for sanity check.
kubectl -n minio-dev port-forward svc/minio-service 9001:9001 --address 0.0.0.0

# Open the minio UI.
open http://localhost:9001/browser
```

Then, you can login with:

```text
Username: minioadmin
Password: minioadmin
```

### Deploy a Ray Cluster for Checks

Finally, you can check if the collector works as expected by deploying a Ray cluster with the collector enabled and
interacting with the minio UI.

```bash
# Apply the Ray cluster manifest.
kubectl apply -f historyserver/config/raycluster.yaml
```

> [!IMPORTANT]
> After deploying the Ray cluster, you will see an automatically created bucket `ray-historyserver-log` in the minio UI:

![create_bucket](https://github.com/ray-project/kuberay/blob/69f6f0bd2a9e44a533f18a54aa014ae6a0be88ec/historyserver/docs/assets/create_bucket.png)

Then, you need to submit a Ray job to the existing cluster by specifying `clusterSelector` to verify that events can be
uploaded to the blob storage:

```bash
# Apply the Ray job manifest.
kubectl apply -f historyserver/config/rayjob.yaml
```

Since the session latest logs will be processed and events will be flushed upon the Ray cluster is deleted, you can
manully delete the Ray clsuter to trigger log file and event uploading:

```bash
# Trigger the session latest log processing and event flushing upon deletion.
kubectl delete -f historyserver/config/raycluster.yaml
```

You're supposed to see the uploaded logs and events in the minio UI as below:

![write_logs_and_events](https://github.com/ray-project/kuberay/blob/69f6f0bd2a9e44a533f18a54aa014ae6a0be88ec/historyserver/docs/assets/write_logs_and_events.png)
