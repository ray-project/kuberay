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

Build and deploy the KubeRay operator (binary or deployment).

### 3. Deploy MinIO

```bash
kubectl apply -f historyserver/config/minio.yaml
```

### 4. Build and Load Collector & History Server Images

```bash
cd historyserver
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

### 8. Create Service Account

```bash
kubectl apply -f config/service_account.yaml
```

### 9. Deploy History Server

```bash
kubectl apply -f config/historyserver.yaml
```

### 10. Access History Server

```bash
kubectl port-forward svc/historyserver 8080:30080
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
