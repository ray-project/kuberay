# APIServer Retry Behavior & Configuration

This guide walks you through observing the default retry behavior of the KubeRay APIServer and then customizing its configuration for your needs.
By default, the APIServer automatically retries failed requests to the Kubernetes API when transient errors occur
(like 429, 502, 503, etc.).
This mechanism improves reliability, and this guide shows you how to see it in action and change it.

## Prerequisite

Follow [installation](installation.md) to install the cluster and apiserver.

## Default Retry Behavior

By default, the APIServer automatically retries for these HTTP status codes:

- 408 (Request Timeout)  
- 429 (Too Many Requests)  
- 500 (Internal Server Error)  
- 502 (Bad Gateway)  
- 503 (Service Unavailable)  
- 504 (Gateway Timeout)

With the following default configuration:  
  
- **MaxRetry**: 3 attempts (total 4 tries including initial attempt)  
- **InitBackoff**: 500ms (initial wait time)  
- **BackoffFactor**: 2.0 (exponential multiplier)  
- **MaxBackoff**: 10s (maximum wait time between retries)  
- **OverallTimeout**: 30s (total timeout for all attempts)

## Customize the Retry Configuration

Currently, retry configuration is hardcoded. If you would like a customized retry behaviour, please follow the steps below.

### Step 1: Modify the config in `apiserversdk/util/config.go`

For example,

```go
const (  
    HTTPClientDefaultMaxRetry = 5  // Increase retries  
    HTTPClientDefaultBackoffFactor = float64(2)  
    HTTPClientDefaultInitBackoff = 2 * time.Second  // Longer backoff makes timing visible  
    HTTPClientDefaultMaxBackoff = 20 * time.Second  
    HTTPClientDefaultOverallTimeout = 120 * time.Second  // Longer timeout to allow more retries  
)
```

### Step 2: Rebuild and load the new APIServer image into your Kind cluster.

```bash
cd apiserver
export IMG_REPO=kuberay-apiserver
export IMG_TAG=dev
export KIND_CLUSTER_NAME=$(kubectl config current-context | sed 's/^kind-//')

make docker-image IMG_REPO=kuberay-apiserver IMG_TAG=dev
make load-image IMG_REPO=$IMG_REPO IMG_TAG=$IMG_TAG KIND_CLUSTER_NAME=$KIND_CLUSTER_NAME
```

### Step 3: Redeploy the APIServer using Helm, overriding the image to use the new one you just built.

```bash
helm upgrade --install kuberay-apiserver ../helm-chart/kuberay-apiserver --wait \
  --set image.repository=$IMG_REPO,image.tag=$IMG_TAG,image.pullPolicy=IfNotPresent \
  --set security=null
```

To make sure it works. first find the name of the APIServer pod:

```bash
kubectl get pods -l app.kubernetes.io/component=kuberay-apiserver
```

Describe the pod and check the Image field:

```bash
kubectl describe pod <your-apiserver-pod-name> | grep Image:
# The output should show Image: kuberay-apiserver:dev.
```

### Demonstrating Retries

Make sure you have the apiserver port forwarded as mentioned in the [installation](installation.md).

```bash
kubectl port-forward service/kuberay-apiserver-service 31888:8888
```
  
After port-forwarding, test the retry mechanism:  
  
```bash  
# This request will automatically retry on transient failures  [header-7](#header-7)
curl -X GET http://localhost:31888/apis/ray.io/v1/namespaces/default/rayclusters -v  
  
# Watch for timing in the verbose output:  [header-8](#header-8)
# - Initial attempt  [header-9](#header-9)
# - If it fails with 503, wait 500ms  [header-10](#header-10)
# - Second attempt after 500ms  [header-11](#header-11)
# - If it fails again, wait 1s  [header-12](#header-12)
# - Third attempt after 1s  [header-13](#header-13)
# - If it fails again, wait 2s  [header-14](#header-14)
# - Fourth attempt after 2s  [header-15](#header-15)

To see retry in action, you can check the APIServer logs:

```bash
kubectl logs -f deployment/kuberay-apiserver 
```

## Clean Up

Once you are finished, you can delete the Helm release and the Kind cluster.

```bash
# Delete the Helm release
helm delete kuberay-apiserver

# Delete the Kind cluster
kind delete cluster
```
