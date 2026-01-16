# Azure Blob Storage Backend

This package provides Azure Blob Storage support for the KubeRay History Server.

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `AZURE_STORAGE_CONNECTION_STRING` | Yes | - | Azure Storage connection string |
| `AZURE_STORAGE_CONTAINER` | No | `ray-historyserver` | Container name for storing logs and events |
| `AZURE_STORAGE_ENDPOINT` | No | - | Custom endpoint (for Azurite testing) |

### Getting Your Connection String

1. Go to the [Azure Portal](https://portal.azure.com)
2. Navigate to your Storage Account
3. Under "Security + networking", click "Access keys"
4. Copy the "Connection string" from key1 or key2

## Usage

### As a Collector Storage Backend

Configure the collector to use Azure Blob Storage:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: collector-config
data:
  storage: azureblob
```

Set the environment variables in your collector deployment:

```yaml
env:
  - name: AZURE_STORAGE_CONNECTION_STRING
    valueFrom:
      secretKeyRef:
        name: azure-storage-secret
        key: connection-string
  - name: AZURE_STORAGE_CONTAINER
    value: "ray-historyserver"
```

### As a History Server Reader

Configure the history server to read from Azure Blob Storage:

```yaml
env:
  - name: AZURE_STORAGE_CONNECTION_STRING
    valueFrom:
      secretKeyRef:
        name: azure-storage-secret
        key: connection-string
```

## Local Development with Azurite

[Azurite](https://learn.microsoft.com/en-us/azure/storage/common/storage-use-azurite) is the official Azure Storage emulator.

### Running Azurite

**Important:** Use `--skipApiVersionCheck` flag as the Azure SDK may use a newer API version than Azurite supports.

```bash
# Using Docker (recommended)
docker run -p 10000:10000 -p 10001:10001 -p 10002:10002 \
  mcr.microsoft.com/azure-storage/azurite \
  azurite-blob --blobHost 0.0.0.0 --skipApiVersionCheck

# Using npm
npm install -g azurite
azurite --blobHost 0.0.0.0 --skipApiVersionCheck
```

### Azurite Connection String

Use this well-known connection string for local testing:

```
DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;
```

### Running Tests

```bash
# Start Azurite first (with skipApiVersionCheck)
docker run -d -p 10000:10000 mcr.microsoft.com/azure-storage/azurite \
  azurite-blob --blobHost 0.0.0.0 --skipApiVersionCheck

# Run tests
cd historyserver
go test ./pkg/storage/azureblob/... -v
```

## File Structure in Azure Blob Storage

The collector organizes files as follows:

```
<container>/
├── log/
│   └── <cluster-name>_<cluster-id>/
│       └── <session-id>/
│           ├── logs/
│           │   ├── raylet.log
│           │   ├── gcs_server.log
│           │   └── ...
│           └── node_events/
│               └── <node-name>-<timestamp>.json
└── metadir/
    └── <namespace>_<cluster-name>/
        └── <session-id>/
            └── metadata.json
```

## Authentication Methods

### Connection String (Current)

The simplest method, suitable for development and testing.

### Workload Identity (Future)

For production AKS deployments, workload identity provides a more secure option by using Azure AD and Kubernetes service accounts. This will be added in a future release.

## Troubleshooting

### "Container not found" error

The container is created automatically on first use. If you see this error, check:
- Your connection string is correct
- The storage account exists
- Network connectivity to Azure

### "AuthenticationFailed" error

Verify your connection string:
- Copy it fresh from the Azure Portal
- Ensure no trailing whitespace
- Check the storage account key hasn't been rotated

### Tests skipping with "Azurite not available"

Make sure Azurite is running:

```bash
docker ps | grep azurite
```

If not running, start it:

```bash
docker run -d -p 10000:10000 mcr.microsoft.com/azure-storage/azurite azurite-blob --blobHost 0.0.0.0
```
