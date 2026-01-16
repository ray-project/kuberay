# Azure Blob Storage Backend

This package provides Azure Blob Storage support for the KubeRay History Server.

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `AZURE_STORAGE_CONNECTION_STRING` | Conditional* | - | Azure Storage connection string |
| `AZURE_STORAGE_ACCOUNT_URL` | Conditional* | - | Storage account URL (e.g., `https://<account>.blob.core.windows.net`) |
| `AZURE_STORAGE_CONTAINER` | No | `ray-historyserver` | Container name for storing logs and events |
| `AZURE_STORAGE_AUTH_MODE` | No | auto-detect | Authentication mode: `connection_string`, `workload_identity`, or `default` |
| `AZURE_STORAGE_ENDPOINT` | No | - | Custom endpoint (for Azurite testing) |

*Either `AZURE_STORAGE_CONNECTION_STRING` or `AZURE_STORAGE_ACCOUNT_URL` must be set.

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

### Connection String

The simplest method, suitable for development and testing. Set `AZURE_STORAGE_CONNECTION_STRING` with your storage account connection string.

```yaml
env:
  - name: AZURE_STORAGE_CONNECTION_STRING
    valueFrom:
      secretKeyRef:
        name: azure-storage-secret
        key: connection-string
```

### Workload Identity (Recommended for AKS)

For production AKS deployments, [Microsoft Entra Workload Identity](https://learn.microsoft.com/en-us/azure/aks/workload-identity-overview) provides passwordless authentication using managed identities and Kubernetes service accounts.

**Prerequisites:**
1. AKS cluster with OIDC issuer and workload identity enabled
2. User-assigned managed identity with "Storage Blob Data Contributor" role
3. Federated identity credential linking the managed identity to the Kubernetes service account

**Pod Configuration:**

```yaml
apiVersion: v1
kind: Pod
metadata:
  labels:
    azure.workload.identity/use: "true"  # Required label
spec:
  serviceAccountName: ray-collector      # Must have workload identity annotation
  containers:
  - name: collector
    env:
    - name: AZURE_STORAGE_ACCOUNT_URL
      value: "https://<storage-account>.blob.core.windows.net"
    - name: AZURE_STORAGE_CONTAINER
      value: "ray-historyserver"
    # No connection string needed - authentication is automatic
```

**ServiceAccount Configuration:**

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ray-collector
  annotations:
    azure.workload.identity/client-id: "<managed-identity-client-id>"
```

The workload identity webhook automatically injects these environment variables:
- `AZURE_CLIENT_ID` - Managed identity client ID
- `AZURE_TENANT_ID` - Azure AD tenant ID
- `AZURE_FEDERATED_TOKEN_FILE` - Path to projected service account token

The Azure SDK's `DefaultAzureCredential` automatically uses these for authentication.

### DefaultAzureCredential

Uses the Azure SDK's credential chain which tries multiple authentication methods in order:
1. Workload Identity (if running on AKS with workload identity)
2. Managed Identity (if running on Azure VMs/containers)
3. Azure CLI (for local development)
4. Other methods...

Set `AZURE_STORAGE_ACCOUNT_URL` and let the SDK auto-detect credentials:

```yaml
env:
  - name: AZURE_STORAGE_ACCOUNT_URL
    value: "https://<storage-account>.blob.core.windows.net"
```

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

### Workload Identity "401 Unauthorized" error

Check the following:
1. Pod has the required label: `azure.workload.identity/use: "true"`
2. ServiceAccount has the annotation: `azure.workload.identity/client-id`
3. Federated identity credential issuer matches your AKS OIDC issuer URL
4. Federated identity credential subject matches: `system:serviceaccount:<namespace>:<sa-name>`
5. Managed identity has "Storage Blob Data Contributor" role on the storage account

Verify environment variables are injected:

```bash
kubectl exec <pod-name> -- env | grep AZURE
```

Expected output should include `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, and `AZURE_FEDERATED_TOKEN_FILE`.

### "failed to create default credential" error

This occurs when `DefaultAzureCredential` cannot find any valid credentials. Ensure:
- On AKS: Workload identity is properly configured
- On Azure VMs: Managed identity is enabled
- Locally: Azure CLI is logged in (`az login`)
