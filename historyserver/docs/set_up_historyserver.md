# History Server Quick Start Guide

## Prerequisites

- Kind
- Docker
- kubectl
- Go 1.24+

## Setup Steps

### 1. Create Kind Cluster

```bash
kind create cluster --image=kindest/node:v1.27.0
```

### 2. Build and Run Ray Operator

Build and deploy the KubeRay operator (binary or deployment). For details, please refer to the
[ray-operator development guide](https://github.com/ray-project/kuberay/blob/master/ray-operator/DEVELOPMENT.md#run-the-operator-inside-the-cluster).

### 3. Deploy MinIO

```bash
kubectl apply -f historyserver/config/minio.yaml
```

### 4. Build and Load Collector & History Server Images

If you'd like to run the history server outside the Kind cluster, you don't need to build the history server image.

```bash
cd historyserver

# (Optional) Build the hisotry server image if you want to deploy it in the Kind cluster.
make localimage-historyserver
kind load docker-image historyserver:v0.1.0

make localimage-collector
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

### 8. Run and Access History Server

#### Deploy In-Cluster History Server

```bash
kubectl apply -f config/historyserver.yaml

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
                // Use localhost rather than the Kubernetes service name.
                "S3_ENDPOINT": "localhost:9000",
                "S3_BUCKET": "ray-historyserver",
                "AWS_S3ID": "minioadmin",
                "AWS_S3SECRET": "minioadmin",
                "AWS_S3TOKEN": "",
                "S3FORCE_PATH_STYLE": "true",
                "S3DISABLE_SSL": "true"
            }
        }
    ]
}
```

For setting up the `args` and `env` fields, please refer to `spec.template.spec.containers.command` and
`spec.template.spec.containers.env` in `historyserver/config/historyserver.yaml`.

### 9. Access MinIO

Use the following command to port-forward the console and API ports. The API port is required only when running the
history server outside the kind cluster.

```bash
kubectl --namespace minio-dev port-forward svc/minio-service 9001:9001 9000:9000
```

> **Note**: Get the correct session directory from MinIO console.
> Login: `minioadmin` / `minioadmin`
> See: [MinIO Setup Guide](./set_up_collector.md#deploy-minio-for-log-and-event-storage)

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
