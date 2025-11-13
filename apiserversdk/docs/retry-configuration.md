# APIServer Retry Behavior

By default, the KubeRay APIServer automatically retries failed requests to the Kubernetes API when transient errors occur.
This built-in resilience improves reliability without requiring manual intervention.
This guide explains the retry behavior and how to customize it.

## Prerequisite

Follow [installation](installation.md) to install the cluster and apiserver.

## Default Retry Behavior

The APIServer automatically retries for these HTTP status codes:

- 408 (Request Timeout)
- 429 (Too Many Requests)
- 500 (Internal Server Error)
- 502 (Bad Gateway)
- 503 (Service Unavailable)
- 504 (Gateway Timeout)

Note that non-retryable errors (4xx except 408/429) fail immediately without retries.

The following default configuration explains how retry works:

- **MaxRetry**: 3 retries (4 total attempts including the initial one)
- **InitBackoff**: 500ms (initial wait time)
- **BackoffFactor**: 2.0 (exponential multiplier)
- **MaxBackoff**: 10s (maximum wait time between retries)
- **OverallTimeout**: 30s (total timeout for all attempts)

which means $$Backoff_i = \min(InitBackoff \times BackoffFactor^i, MaxBackoff)$$
where $i$ is the attempt number (starting from 0).
The retries will stop if the total time exceeds the `OverallTimeout`.

## Customize the Retry Configuration

Currently, retry configuration is hardcoded. If you need custom retry behavior,
you'll need to modify the source code and rebuild the image.

### Step 1: Modify the config in `apiserversdk/util/config.go`

For example,

```go
const (
    HTTPClientDefaultMaxRetry = 5  // Increase retries from 3 to 5
    HTTPClientDefaultBackoffFactor = float64(2)
    HTTPClientDefaultInitBackoff = 2 * time.Second  // Longer backoff makes timing visible
    HTTPClientDefaultMaxBackoff = 20 * time.Second
    HTTPClientDefaultOverallTimeout = 120 * time.Second  // Longer timeout to allow more retries
)
```

### Step 2: Rebuild and load the new APIServer image into your Kind cluster

```bash
cd apiserver
export IMG_REPO=kuberay-apiserver
export IMG_TAG=dev
export KIND_CLUSTER_NAME=$(kubectl config current-context | sed 's/^kind-//')

make docker-image IMG_REPO=kuberay-apiserver IMG_TAG=dev
make load-image IMG_REPO=$IMG_REPO IMG_TAG=$IMG_TAG KIND_CLUSTER_NAME=$KIND_CLUSTER_NAME
```

### Step 3: Redeploy the APIServer using Helm, overriding the image to use the new one you just built

```bash
helm upgrade --install kuberay-apiserver ../helm-chart/kuberay-apiserver --wait \
  --set image.repository=$IMG_REPO,image.tag=$IMG_TAG,image.pullPolicy=IfNotPresent \
  --set security=null
```

## Observing Retry Behavior

### In Production

When retry occurs in production, you won't see explicit logs by default because
the retry mechanism operates silently. However, you can observe its effects:

1. **Monitor request latency**: Retried requests will take longer due to backoff delays
2. **Check Kubernetes API Server logs**: Look for repeated requests from the same client

### In Development

To verify retry behavior during development, you can:

1. Run the unit tests to ensure retry logic works correctly:

```bash
make test
```
