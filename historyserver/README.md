# KubeRay History Server

This project is under active development.
See [#ray-history-server](https://app.slack.com/client/TN4768NRM/C09QLLU8HTL) channel to provide feedback.

Ray History Server is a service for collecting, storing, and viewing historical logs and metadata from Ray clusters.
It provides a web interface to explore the history of Ray jobs, tasks, actors, and other cluster activities.

## Components

The History Server consists of two main components:

1. **Collector**: Runs as a sidecar container in Ray clusters to collect logs and metadata
2. **History Server**: Central service that aggregates data from collectors and provides a web UI

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
- `--ray-cluster-id`: ID of the Ray cluster
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
- `backend/`: Core logic for storage backends and collection
- `backend/collector/`: Collector-specific code
- `backend/historyserver/`: History server implementation
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

## Deployment

History Server can be deployed in Kubernetes using the manifests in the `config/samples/` directory.
Examples are provided for different storage backends including MinIO and Aliyun OSS.
