# History Server Quick Start Guide

## Prerequisites

- Kind
- Docker
- kubectl
- Go 1.24+

## Setup Steps

### 1. Create Kind Cluster

```bash
kind create cluster --image=kindest/node:v1.29.0
```

### 2. Build and Run Ray Operator

Build and deploy the KubeRay operator (binary or deployment). For details, please refer to the
[ray-operator development guide](https://github.com/ray-project/kuberay/blob/master/ray-operator/DEVELOPMENT.md#run-the-operator-inside-the-cluster).

### 3. Deploy & Access MinIO

```bash
kubectl apply -f historyserver/config/minio.yaml
```

Use the following command to port-forward the console and API ports. The API port is required only when running the
history server outside the kind cluster.

```bash
kubectl --namespace minio-dev port-forward svc/minio-service 9001:9001 9000:9000
```

> [!NOTE]
> Get the correct session directory from MinIO console.
> Login: `minioadmin` / `minioadmin`
> See: [MinIO Setup Guide](./set_up_collector.md#deploy-minio-for-log-and-event-storage)

### 4. Build and Load Collector & History Server Images

If you'd like to run the history server outside the Kind cluster, you don't need to build the history server image.

```bash
make -C historyserver localimage-build
kind load docker-image historyserver:v0.1.0
kind load docker-image collector:v0.1.0
```

### 5. Deploy Ray Cluster

```bash
kubectl apply -f historyserver/config/raycluster.yaml
```

### 6. Submit Ray Job

```bash
kubectl apply -f historyserver/config/rayjob.yaml
```

### 7. Delete Ray Cluster (Trigger Log Upload)

```bash
kubectl delete -f historyserver/config/raycluster.yaml
```

### 8. Create Service Account

```bash
kubectl apply -f historyserver/config/service_account.yaml
```

### 9. Run and Access History Server

#### Deploy In-Cluster History Server

```bash
kubectl apply -f historyserver/config/historyserver.yaml

# Port-forward to access the history server.
kubectl port-forward svc/historyserver 8080:30080
```

#### Run History Server Outside the Kind Cluster

You can also run the history server outside the Kind cluster to accelerate the development iteration and enable
debugging in your own IDE. For example, you can set up `.vscode/launch.json` as follows:

```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Debug (historyserver)",
            "type": "go",
            "request": "launch",
            "program": "${workspaceFolder}/historyserver/cmd/historyserver/main.go",
            "cwd": "${workspaceFolder}",
            "args": [
                "--runtime-class-name=s3",
                "--ray-root-dir=log"
            ],
            "env": {
                "S3_REGION": "test",
                "S3_ENDPOINT": "localhost:9000",
                "S3_BUCKET": "ray-historyserver",
                "AWS_ACCESS_KEY_ID": "minioadmin",
                "AWS_SECRET_ACCESS_KEY": "minioadmin",
                "AWS_SESSION_TOKEN": "",
                "S3FORCE_PATH_STYLE": "true",
                "S3DISABLE_SSL": "true"
            }
        }
    ]
}
```

For setting up the `args` and `env` fields, please refer to `spec.template.spec.containers.command` and
`spec.template.spec.containers.env` in `historyserver/config/historyserver.yaml`.

You can also build and run the history server binary directly from the command line:

```bash
# Build the history server binary.
cd historyserver
make buildhistoryserver

# Configure S3 connection via environment variables.
export S3_REGION=test
export S3_ENDPOINT=localhost:9000
export S3_BUCKET=ray-historyserver
export AWS_ACCESS_KEY_ID=minioadmin
export AWS_SECRET_ACCESS_KEY=minioadmin
export AWS_SESSION_TOKEN=
export S3FORCE_PATH_STYLE=true
export S3DISABLE_SSL=true

# Run the history server.
./output/bin/historyserver \
  --runtime-class-name=s3 \
  --ray-root-dir=log \
  --use-kubernetes-proxy=true
```

---

## Ray Token Authentication (Optional)

When a RayCluster enables token authentication (`spec.authOptions.mode: token`), its Ray Dashboard
rejects unauthenticated requests, so the history server cannot proxy live-cluster endpoints out of the
box. Starting the history server with `--use-auth-token-mode=true` makes it read the cluster's auth
token from the Kubernetes Secret the operator generates for that cluster (key `auth_token`) and attach
an `x-ray-authorization: Bearer <token>` header when proxying live-session requests. Any client-supplied
`x-ray-authorization` header is dropped first, so the server-managed token cannot be bypassed.

The flag only affects the live-cluster path: replaying a dead session reads from object storage and never
talks to a Ray Dashboard. It is also safe to leave enabled when only some of your clusters use auth — for
a cluster without `authOptions`, the history server adds no header and proxies exactly as before.

> [!IMPORTANT]
> Kubernetes-delegated token auth (`spec.authOptions.enableK8sTokenAuth: true`) is **not** supported.
> There is no static bearer token for the history server to inject, so it returns an error instead of
> silently proxying unauthenticated requests.

The steps below replace steps 5-9 of the setup above; steps 1-4 are unchanged. Step 7 (deleting the
cluster to trigger the log upload) is intentionally left out: auth token mode only matters for the live
cluster, and the collector cannot fully populate a dead session while auth is on (see the note in step
1), so keep the cluster running.

### 1. Deploy an Auth-Enabled Ray Cluster

Uncomment the `rayVersion` and `authOptions` fields under `spec` in
`historyserver/config/raycluster.yaml`:

```yaml
spec:
  rayVersion: "2.52.0"
  authOptions:
    mode: token
```

`rayVersion` is not optional here: the operator rejects a RayCluster that sets `authOptions.mode: token`
without it, and token auth requires Ray 2.52.0 or later.

```bash
kubectl apply -f historyserver/config/raycluster.yaml
```

Those are the only changes the cluster manifest needs. The KubeRay operator generates the
`raycluster-historyserver` Secret and injects `RAY_AUTH_MODE` / `RAY_AUTH_TOKEN` into the head and worker
Ray containers, so you do not set those env vars yourself.

### 2. Submit a Ray Job

```bash
kubectl apply -f historyserver/config/rayjob.yaml
```

The RayJob needs no auth-specific configuration at all. The operator injects `RAY_AUTH_MODE` /
`RAY_AUTH_TOKEN` into the submitter container from that cluster's spec.

### 3. Grant the History Server Access to the Auth Secret

`service_account.yaml` only grants read access to RayClusters. Auth token mode also needs to read the auth
Secrets, which `service_account_auth_token_mode.yaml` grants through a namespace-scoped Role:

```bash
kubectl apply -n default -f historyserver/config/service_account.yaml
kubectl apply -n default -f historyserver/config/service_account_auth_token_mode.yaml
```

The Role is deliberately namespace-scoped rather than cluster-wide, so the history server can only read
Secrets in namespaces you explicitly opt in. Neither manifest pins a namespace of its own, so they land
in whichever namespace `-n` selects — pass it explicitly rather than relying on your current context.
Use `default` here, because the RoleBinding always refers back to the `historyserver` ServiceAccount in
`default`. If your auth-enabled RayClusters run elsewhere, apply the second file once per namespace:

```bash
kubectl apply -n <namespace> -f historyserver/config/service_account_auth_token_mode.yaml
```

### 4. Deploy the History Server in Auth Token Mode

Uncomment the `--use-auth-token-mode=true` flag in the container command in
`historyserver/config/historyserver.yaml`:

```yaml
        command:
        - historyserver
        - --runtime-class-name=s3
        - --ray-root-dir=log
        - --use-auth-token-mode=true
```

```bash
kubectl apply -f historyserver/config/historyserver.yaml

# Port-forward to access the history server.
kubectl port-forward svc/historyserver 8080:30080
```

### 5. Verify

Enter the live session and hit a proxied endpoint. Without auth token mode, the same request would come
back with an authentication error from the Ray Dashboard.

```bash
curl -c ~/cookies.txt "http://localhost:8080/enter_cluster/default/raycluster-historyserver/live"
curl -b ~/cookies.txt "http://localhost:8080/api/v0/tasks"
```

The token is fetched while serving the proxied request, not while entering the session, so auth problems
surface on the second command. If it returns `500 failed to get auth token for cluster
default/raycluster-historyserver`, the history server reached the cluster but could not read its
Secret — recheck step 3.

---

## API Endpoints

### Health Check

```bash
curl "http://localhost:8080/readz"
curl "http://localhost:8080/livez"
```

### List Clusters

```bash
curl "http://localhost:8080/clusters"
```

### Enter a Session (Dead Cluster)

```bash
SESSION="session_2026-01-11_19-38-40_146706_1"  # Replace with actual session
curl -c ~/cookies.txt "http://localhost:8080/enter_cluster/default/raycluster-historyserver/$SESSION"
```

### Dead Cluster Endpoints

```bash
# All Tasks
curl -b ~/cookies.txt "http://localhost:8080/api/v0/tasks"

# Tasks by job_id
curl -b ~/cookies.txt "http://localhost:8080/api/v0/tasks?filter_keys=job_id&filter_predicates==&filter_values=AgAAAA=="

# Task by task_id
curl -b ~/cookies.txt "http://localhost:8080/api/v0/tasks?filter_keys=task_id&filter_predicates==&filter_values=Z6Loz6WgbbP///////////////8CAAAA"

# All Actors
curl -b ~/cookies.txt "http://localhost:8080/logical/actors"

# Single Actor
curl -b ~/cookies.txt "http://localhost:8080/logical/actors/<ACTOR_ID>"

# Nodes
curl -b ~/cookies.txt "http://localhost:8080/nodes?view=summary"
```

### Enter a Session (Live Cluster)

```bash
SESSION="live"
curl -c ~/cookies.txt "http://localhost:8080/enter_cluster/default/raycluster-historyserver/$SESSION"
```

If the command returns a "RayCluster not found" error, you need to deploy a new, live cluster before connecting:

```bash
kubectl apply -f historyserver/config/raycluster.yaml
```

Then submit a new RayJob:

```sh
kubectl apply -f historyserver/config/rayjob.yaml

# If rayjob already exists, please delete it first and re-apply
# kubectl delete -f historyserver/config/rayjob.yaml
```

### Live Cluster Endpoints

Switch to live session first, then:

```bash
# All Tasks
curl -b ~/cookies.txt "http://localhost:8080/api/v0/tasks"

# Tasks by job_id
curl -b ~/cookies.txt "http://localhost:8080/api/v0/tasks?filter_keys=job_id&filter_predicates==&filter_values=04000000"

# Task Summarize
curl -b ~/cookies.txt "http://localhost:8080/api/v0/tasks/summarize"

# All Actors
curl -b ~/cookies.txt "http://localhost:8080/logical/actors"

# Single Actor
curl -b ~/cookies.txt "http://localhost:8080/logical/actors/<ACTOR_ID>"

# Nodes Summary
curl -b ~/cookies.txt "http://localhost:8080/nodes?view=summary"

# Jobs
curl -b ~/cookies.txt "http://localhost:8080/api/jobs/"

# Cluster Status
curl -b ~/cookies.txt "http://localhost:8080/api/cluster_status"
```

### Live Cluster with prometheus and grafana

```bash
# Install prometheus and grafana. ref: https://docs.ray.io/en/latest/cluster/kubernetes/k8s-ecosystem/prometheus-grafana.html#step-2-install-kubernetes-prometheus-stack-via-helm-chart
./install/prometheus/install.sh --auto-load-dashboard true

# Apply RayCluster with Grafana setting
kubectl apply -f ray-operator/config/samples/ray-cluster.embed-grafana.yaml

# Get live session cookie. (Port-forward is required)
curl -c ~/cookies.txt "http://localhost:8080/enter_cluster/default/raycluster-embed-grafana/live"

# Request to prometheus health endpoint
curl -b ~/cookies.txt http://localhost:8080/api/prometheus_health

# Request to grafana health endpoint
curl -b ~/cookies.txt http://localhost:8080/api/grafana_health
```

After completing the Prometheus and Grafana testing, you can clean up the associated resources using the following commands:

```bash
# 1. Delete the RayCluster
kubectl delete -f ray-operator/config/samples/ray-cluster.embed-grafana.yaml

# 2. Uninstall the kube-prometheus-stack helm chart
helm --namespace prometheus-system uninstall prometheus

# 3. Delete the prometheus-system namespace
kubectl delete namespace prometheus-system
```
