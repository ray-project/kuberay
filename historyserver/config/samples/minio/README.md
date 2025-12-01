# Ray History Server with MinIO Backend Storage

This document describes how to deploy Ray History Server using MinIO as the backend storage. Ray History Server is used to collect, store, and view historical logs and metadata from Ray clusters.

## Components Overview

This deployment includes the following components:

1. **MinIO**: Object storage backend for storing Ray cluster logs and metadata
2. **RayCluster**: Ray cluster configured with collector sidecar containers for collecting logs and uploading them to MinIO
3. **HistoryServer**: Service that reads logs and metadata from MinIO and provides a web interface for viewing historical information

## Deployment Steps

### 1. Deploy MinIO

First, deploy MinIO service as the storage backend:

```bash
kubectl apply -f minio.yaml
```

This will create:
- A namespace named `minio-dev`
- A Secret containing MinIO credentials (default username/password: minioadmin/minioadmin)
- MinIO Deployment
- MinIO Service (port 9000 for API, 9001 for console)

### 2. Create ServiceAccount and RBAC Permissions

```bash
kubectl apply -f sa.yaml
```

This will create:
- ServiceAccount for HistoryServer
- ClusterRole allowing read access to RayCluster resources
- Corresponding ClusterRoleBinding

### 3. Deploy RayCluster

```bash
kubectl apply -f raycluster.yaml
```

The RayCluster configuration includes:
- Collector sidecar containers responsible for collecting logs and uploading them to MinIO
- Environment variables specifying connection information for MinIO
- PostStart lifecycle hooks to obtain node IDs

Note: You need to replace `image: xxx` with actual image addresses in the configuration files.

### 4. Deploy HistoryServer

```bash
kubectl apply -f historyserver.yaml
```

HistoryServer configuration includes:
- Environment variables for connecting to MinIO
- Specifies `s3` as the runtime class name

Note: You need to replace `image: xxx` with actual image addresses in the configuration files.

## Configuration Details

### MinIO Configuration

MinIO-related configurations are passed through environment variables in various components:

- `S3DISABLE_SSL`: Set to "true" to disable SSL
- `AWS_S3ID`: MinIO access key ID (default: minioadmin)
- `AWS_S3SECRET`: MinIO secret access key (default: minioadmin)
- `AWS_S3TOKEN`: Session token (usually empty)
- `S3_BUCKET`: Bucket name (default: ray-historyserver-log)
- `S3_ENDPOINT`: MinIO service address (minio-service.minio-dev:9000)
- `S3_REGION`: Region (test)
- `S3FORCE_PATH_STYPE`: Set to "true" to use path-style URLs (important for MinIO)

### Accessing HistoryServer

After deployment, you can access HistoryServer through port forwarding:

```bash
kubectl port-forward svc/historyserver 8080:30080
```

Then visit http://localhost:8080 in your browser to view historical logs and metadata.

## Important Notes

1. In production environments, please change the default MinIO username and password
2. Ensure all components use correct image addresses
3. Adjust resource requests and limits according to actual needs
4. Storage bucket names and other configuration parameters can be modified as needed