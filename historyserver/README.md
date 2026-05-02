# KubeRay History Server

This project is under active development.
See [#ray-history-server](https://app.slack.com/client/TN4768NRM/C09QLLU8HTL) channel to provide feedback.

Ray History Server is a service for collecting, storing, and viewing historical logs and metadata from Ray clusters.
It provides a web interface to explore the history of Ray jobs, tasks, actors, and other cluster activities.

## Components

The History Server consists of two main components:

1. **Collector**: Runs as a sidecar container in Ray clusters to collect logs and metadata
2. **History Server**: Serves a Ray Dashboard-shaped HTTP API and ingests cluster sessions' events on demand

## Building

### Prerequisites

- Go 1.19 or higher
- Docker (for building container images)
- Make

### Building Binaries

To build the binaries locally:

```bash
make build
```

This will generate two binaries in the `output/bin/` directory:

- `collector`: The collector service that runs alongside Ray nodes
- `historyserver`: The main history server service

You can also build individual components:

```bash
make buildcollector      # Build only the collector
make buildhistoryserver  # Build only the history server
```

### Building Docker Images

To build a Docker image:

```bash
make localimage
```

This creates a Docker image named `historyserver:laster` with both binaries and necessary assets.

For multi-platform builds, you can use:

```bash
docker buildx build -t <image-name>:<tag> --platform linux/amd64,linux/arm64 . --push
```

## Configuration

### History Server Configuration

The history server can be configured using command-line flags:

- `--runtime-class-name`: Storage backend type (e.g., "s3", "aliyunoss", "localtest")
- `--ray-root-dir`: Root directory for Ray logs
- `--kubeconfigs`: Path to kubeconfig file(s) for accessing Kubernetes clusters
- `--dashboard-dir`: Directory containing dashboard assets (default: "/dashboard")
- `--runtime-class-config-path`: Path to runtime class configuration file

### Collector Configuration

The collector can be configured using command-line flags:

- `--role`: Node role ("Head" or "Worker")
- `--runtime-class-name`: Storage backend type (e.g., "s3", "aliyunoss")
- `--ray-cluster-name`: Name of the Ray cluster
- `--ray-cluster-namespace`: Namespace of the Ray cluster
- `--ray-root-dir`: Root directory for Ray logs
- `--log-batching`: Number of log entries to batch before writing
- `--events-port`: Port for the events server
- `--push-interval`: Interval between pushes to storage
- `--runtime-class-config-path`: Path to runtime class configuration file

## Supported Storage Backends

History Server supports multiple storage backends:

1. **S3/MinIO**: For AWS S3 or MinIO compatible storage
2. **Aliyun OSS**: For Alibaba Cloud Object Storage Service
3. **Local Test**: For local testing and development

Each backend requires specific configuration parameters passed through environment variables or configuration files.

## Running

### Running the History Server

```bash
./output/bin/historyserver \
  --runtime-class-name=s3 \
  --ray-root-dir=/path/to/logs
```

### Running the Collector

```bash
./output/bin/collector \
  --role=Head \
  --runtime-class-name=s3 \
  --ray-cluster-name=my-cluster \
  --ray-root-dir=/path/to/logs
```

## Development

### Code Structure

- `cmd/`: Main applications (collector and historyserver)
- `pkg/`: Core logic for storage backends and collection
- `pkg/collector/`: Collector-specific code
- `pkg/storage/`: Storage backend implementations
- `dashboard/`: Web UI files

### Testing

To run tests:

```bash
make test
```

### Linting

To run lint checks:

```bash
make alllint
```

## Smoke Tests

### 1. Deploy History Server

Apply MinIO and the History Server manifests:

```bash
kubectl apply -f historyserver/config/minio.yaml
kubectl apply -f historyserver/config/service_account.yaml
kubectl apply -f historyserver/config/historyserver.yaml
```

Port-forward the HS service:

```bash
kubectl port-forward svc/historyserver 8080:30080
```

### 2. Generate a Dead Session

Deploy the sample RayCluster, run a deterministic workload, then delete the CR:

```bash
kubectl apply -f historyserver/config/raycluster.yaml
kubectl wait pod -l ray.io/node-type=head --for=condition=Ready --timeout=180s

# Run a workload so events are written to MinIO
kubectl exec $(kubectl get pod -l ray.io/node-type=head -o name) \
  -c ray-head -- python -c "
import ray
ray.init(address='auto')

@ray.remote
def add(x, y): return x + y

print('tasks:', ray.get([add.remote(i, i) for i in range(5)]))
"

# Delete the cluster — produces a 'dead' session
kubectl delete -f historyserver/config/raycluster.yaml

# Discover the session name. /clusters lists both live and dead sessions;
# dead sessions carry the `session_*` name you'll feed into /enter_cluster.
curl -sS http://localhost:8080/clusters
```

### 3. Cold Path (first visit)

Trigger the lazy load synchronously. Replace `<session>` with the session name from §2:

```bash
time curl -s -o /dev/null \
  http://localhost:8080/enter_cluster/default/raycluster-historyserver/<session>
```

> [!NOTE]
> Cold path runs synchronously: K8s probe + event parse. Expect this to take seconds. Subsequent endpoint calls
> (`/api/v0/jobs`, `/api/v0/tasks/...`) read from in-memory state populated by this call.

### 4. Warm Path (subsequent visits on same replica)

Re-enter the same cluster:

```bash
time curl -s -o /dev/null \
  http://localhost:8080/enter_cluster/default/raycluster-historyserver/<session>
```

> [!NOTE]
> Warm path returns immediately — Supervisor's loaded-session set fast-paths the request without re-parsing. If the HS
> process restarts, the next visit returns to cold-path latency.

### 5. Tail Logs

```bash
kubectl logs -f -l app=historyserver --tail=50
```

You should see one `current eventFileList for cluster ...` line (and the per-file `Reading event file: ...` entries)
per first-time cold-path call. Warm-path calls produce no parse log lines.

## Deployment

History Server can be deployed in Kubernetes using the manifests in the `config/samples/` directory.
Examples are provided for different storage backends including MinIO and Aliyun OSS.
