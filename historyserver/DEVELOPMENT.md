# History Server - Local Development with Ray Dashboard

This guide walks through how to develop the History Server locally with MinIO as the S3 backend, and browse the UI
through the Ray Dashboard's middleware (from [ray-project/ray#61295](https://github.com/ray-project/ray/pull/61295)).

## Table of Contents

- [Prerequisites](#prerequisites)
- [Step 1: Set Up Kind and KubeRay Operator](#step-1-set-up-kind-and-kuberay-operator)
- [Step 2: Build and Load Images](#step-2-build-and-load-images)
- [Step 3: Deploy MinIO](#step-3-deploy-minio)
- [Step 4: Generate a Dead Session](#step-4-generate-a-dead-session)
- [Step 5: Deploy History Server](#step-5-deploy-history-server)
- [Step 6: Access the Local Ray Dashboard](#step-6-access-the-local-ray-dashboard)
- [Step 7: Validate the Dead Path](#step-7-validate-the-dead-path)
- [Step 8: Validate the Live Path](#step-8-validate-the-live-path)
- [Cleanup](#cleanup)
- [Troubleshooting](#troubleshooting)

## Prerequisites

- Kind, kubectl
- Go v1.24+
- Docker: Engine for building the container image
- GNU Make
- K9s (optional)

## Step 1: Set Up Kind and KubeRay Operator

Make sure you are under the `kuberay` directory.

```bash
# Spin up the kind cluster.
kind create cluster --image=kindest/node:v1.29.0

# Build and load the operator.
IMG=kuberay/operator:latest make -C ray-operator docker-build
kind load docker-image kuberay/operator:latest

# Install via Helm.
helm install kuberay-operator \
  --set image.repository=kuberay/operator \
  --set image.tag=latest \
  ./helm-chart/kuberay-operator
```

## Step 2: Build and Load Images

Build the collector and the history server images, then load both of them into the kind cluster.

```bash
# Build images.
make -C historyserver localimage-build

# Load into kind.
kind load docker-image collector:v0.1.0
kind load docker-image historyserver:v0.1.0

# Verify.
docker exec -it kind-control-plane crictl images | grep -E 'collector|historyserver'
```

## Step 3: Deploy MinIO

```bash
kubectl apply -f historyserver/config/minio.yaml

# Port-forward the console and API ports.
kubectl -n minio-dev port-forward svc/minio-service 9001:9001 9000:9000
```

> [!NOTE]
> Open `http://localhost:9001/browser`, log in with `minioadmin` / `minioadmin`, and confirm the `ray-historyserver`
> bucket exists.

## Step 4: Generate a Dead Session

> [!IMPORTANT]
> We deploy the data-generating RayCluster first and the history server [Step 5](#step-5-deploy-history-server)
> afterwards. By deleting the RayCluster before starting the history server, we guarantee the startup
> `processAllEvents()` picks up every event file. Reversing the order means the pages stay empty until the hourly tick
> fires (or you do a `kubectl rollout restart`, please see [Troubleshooting](#troubleshooting)).

```bash
# 1. Apply the data-generating RayCluster (has the collector sidecar).
kubectl apply -f historyserver/config/raycluster.yaml

# 2. Submit the RayJob.
kubectl apply -f historyserver/config/rayjob.yaml

# 3. Watch the RayJob until status is SUCCEEDED.
kubectl get rayjob rayjob -w

# 4. Delete to trigger a final event/log flush.
kubectl delete -f historyserver/config/rayjob.yaml
kubectl delete -f historyserver/config/raycluster.yaml
```

## Step 5: Deploy History Server

Run the History Server **in-cluster** so that the dashboard middleware can reach it via `http://historyserver:30080`.

```bash
kubectl apply -f historyserver/config/service_account.yaml
kubectl apply -f historyserver/config/historyserver.yaml
```

## Step 6: Access the Local Ray Dashboard

To access the local Ray Dashboard, you have to port forward the History Server service:

```bash
kubectl port-forward svc/historyserver 8080:30080
```

Install Ray locally. Make sure to use at least Ray `v2.55`.

```bash
pip uninstall -y ray
pip install -U "ray[default]==2.55.0"
```

Run the `ray start` command:

```bash
ray start --head --num-cpus=1 --proxy-server-url=http://localhost:8080
```

> [!NOTE]
> Use `--proxy-server-url` parameter to route requests to the port-forwarded History Server.

## Step 7: Validate the Dead Path

Open a browser and request the `/clusters` endpoint to view all clusters (including live and dead):

```text
http://localhost:8265/clusters
```

The result should look something like the following:

![clusters_endpoint](https://github.com/ray-project/kuberay/blob/40bf59590022c459086629e56b96444297c507d1/historyserver/docs/assets/clusters_endpoint.png)

Substitute your real `<SELECTED_SESSION_ID>`:

```text
http://localhost:8265/enter_cluster/default/raycluster-historyserver/<SELECTED_SESSION_ID>
```

A successful request produces output like the following:

![enter_cluster_dead](https://github.com/ray-project/kuberay/blob/40bf59590022c459086629e56b96444297c507d1/historyserver/docs/assets/enter_cluster_dead.png)

Once set up, you can hit any endpoint via the Ray Dashboard. Take `http://localhost:8265/#/cluster` as an example:

![dead_raycluster](https://github.com/ray-project/kuberay/blob/93e81e60ddc486f60ae6432a3d178edbf952eff3/historyserver/docs/assets/dead_raycluster.png)

## Step 8: Validate the Live Path

Re-apply the data-generating cluster and submit a RayJob.

```bash
kubectl apply -f historyserver/config/raycluster.yaml
kubectl apply -f historyserver/config/rayjob.yaml
```

In the browser, switch to the live session by visiting:

```text
http://localhost:8265/enter_cluster/default/raycluster-historyserver/live
```

The result should look like the following:

![enter_cluster_live](https://github.com/ray-project/kuberay/blob/40bf59590022c459086629e56b96444297c507d1/historyserver/docs/assets/enter_cluster_live.png)

Navigate the same pages as in Step 7. This time the History Server reverse-proxies each API call to the live
RayCluster's head dashboard service, so you see real-time state instead of replay:

![live_raycluster](https://github.com/ray-project/kuberay/blob/93e81e60ddc486f60ae6432a3d178edbf952eff3/historyserver/docs/assets/live_raycluster.png)

## Cleanup

```bash
kubectl delete -f historyserver/config/rayjob.yaml
kubectl delete -f historyserver/config/raycluster.yaml
kubectl delete -f historyserver/config/historyserver.yaml
kubectl delete -f historyserver/config/service_account.yaml
kubectl delete -f historyserver/config/minio.yaml

# Or delete the whole cluster directly.
# kind delete cluster
```

## Troubleshooting

- **Pages are empty but `/clusters` and `/enter_cluster` work, history server logs
  `Task map not found for cluster session: ...`:** The EventHandler's in-memory maps were not populated for this
  session.

  To fix this problem, force a re-scan by restarting the history server:

  ```bash
  kubectl rollout restart deploy/historyserver-demo
  kubectl rollout status deploy/historyserver-demo
  ```

  Then, hard-refresh the browser, the UI will now render.
