# KubeRay APIServer Retry Configuration

The KubeRay APIServer V2 includes a retry mechanism to enhance the reliability of requests sent to the Kubernetes API server. When the APIServer forwards requests, it can automatically retry certain failures, such as those caused by temporary network issues or transient server errors. This document explains how to configure and observe the retry behavior.

## Enabling Retry (Configuration)

Retries are enabled by default. You can customize the retry behavior by setting environment variables in the KubeRay APIServer deployment. The recommended way to do this is through the Helm chart during installation.

### Configuration Parameters

The following environment variables can be used to configure the retry mechanism:

| Environment Variable             | Description                                                               | Default Value |
| -------------------------------- | ------------------------------------------------------------------------- | ------------- |
| `HTTP_CLIENT_MAX_RETRY`          | The maximum number of retry attempts for a failed request.                | `3`           |
| `HTTP_CLIENT_BACKOFF_FACTOR`     | A multiplier to increase the backoff delay between retries.               | `2.0`         |
| `HTTP_CLIENT_INIT_BACKOFF_MS`    | The initial backoff delay in milliseconds.                                | `500`         |
| `HTTP_CLIENT_MAX_BACKOFF_MS`     | The maximum backoff delay in milliseconds.                                | `10000`       |
| `HTTP_CLIENT_OVERALL_TIMEOUT_MS` | An overall timeout for the request, including all retries, in milliseconds. | `30000`       |

### Helm Chart Configuration

You can set these environment variables when installing or upgrading the `kuberay-apiserver` Helm chart. For example, you can create a `values.yaml` file:

```yaml
# values.yaml
env:
  - name: HTTP_CLIENT_MAX_RETRY
    value: "5"
  - name: HTTP_CLIENT_INIT_BACKOFF_MS
    value: "1000"
```

Then, install the chart with your custom values:

```sh
helm install kuberay-apiserver kuberay/kuberay-apiserver --version 1.4.0 --values values.yaml
```

This configuration increases the maximum number of retries to 5 and sets the initial backoff to 1000ms.

## Demonstrating Retry in Action

### When are retries triggered?

The APIServer will retry requests that fail with the following HTTP status codes, which typically indicate transient issues:

- `408 Request Timeout`
- `429 Too Many Requests`
- `500 Internal Server Error`
- `502 Bad Gateway`
- `503 Service Unavailable`
- `504 Gateway Timeout`

Requests that receive other status codes (e.g., `404 Not Found`, `403 Forbidden`) are not retried, as these generally indicate a permanent failure or an issue with the request itself.

### Observing Retries

When the APIServer retries a request, it logs the attempt. You can monitor the logs of the KubeRay APIServer pod to see the retry mechanism in action.

To view the logs, first find the name of the APIServer pod:

```sh
kubectl get pods -l app.kubernetes.io/component=kuberay-apiserver
```

Then, stream the logs for that pod:

```sh
kubectl logs -f <apiserver-pod-name>
```

When a request is retried, you will see log messages similar to the following, indicating the attempt number and the reason for the retry:

```
Retrying request to POST /apis/ray.io/v1/namespaces/default/rayclusters, attempt 1, status code: 503 Service Unavailable
```
