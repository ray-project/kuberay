# History Server v2 beta — Deployment

## 1. Overview

The v2 beta splits the v1 monolith into two cooperating binaries that share
only the object-store snapshots as their contract:

- **eventprocessor** — 1 pod, `strategy: Recreate`. Scans object storage every
  10 minutes, probes the K8s API for each session's RayCluster CR, and writes
  an immutable per-session snapshot once the cluster is gone.
- **historyserver** — 3 stateless pods, default `RollingUpdate`. Serves the
  Ray Dashboard-shaped HTTP API from snapshots, proxies live clusters by
  looking up the head pod via the K8s API.

Full design lives in
`../../../examples/historyserver/beta_poc/implementation_plan.md`.

## 2. Quick start (kind dev)

One command spins up kind + KubeRay + MinIO, builds and pushes the beta
images to a local registry, and applies all manifests:

```sh
make deploybeta-kind
```

That wraps `beta/scripts/setup-kind-dev.sh`. Re-running is safe — every
step is idempotent.

Verify:

```sh
kubectl port-forward svc/historyserver-beta 8080:30080
curl http://localhost:8080/api/v2/clusters
```

Processor metrics:

```sh
kubectl port-forward deploy/eventprocessor-beta 9090:9090
curl http://localhost:9090/metrics
```

MinIO console (creds `minioadmin:minioadmin`):

```sh
kubectl -n minio-dev port-forward svc/minio-service 9001:9001
open http://localhost:9001
```

## 3. Production deployment

### 3.1 Prerequisites

1. A Kubernetes cluster with the [KubeRay operator][kuberay] installed
   (the RayCluster CRD must exist).
2. An S3-compatible object store (S3 / GCS / Azure Blob / Aliyun OSS /
   MinIO). For non-S3 backends, swap `configmap-s3.yaml` for the
   backend-specific version and update `--runtime-class-name` on both
   Deployments.
3. A container registry your cluster can pull from.

### 3.2 Build and push images

From the repo root (`historyserver/`):

```sh
make BETA_IMAGE_REGISTRY=<your-registry> \
     BETA_IMAGE_TAG=<your-tag> \
     pushbeta
```

### 3.3 Configure credentials

```sh
kubectl create secret generic historyserver-beta-s3 \
  --from-literal=access_key=<ACCESS_KEY> \
  --from-literal=secret_key=<SECRET_KEY>
```

The Secret is marked `optional: true` in the Deployments, so pods fall
back to the node's IAM role / instance profile if no Secret is created.

### 3.4 Point the ConfigMap at your backend

Edit `configmap-s3.yaml` — change `bucket`, `endpoint`, `region`,
`forcePathStyle`, `disableSSL` to match your environment.

### 3.5 Update image fields

Edit `eventprocessor.yaml` and `historyserver.yaml` — replace the
placeholder `historyserver-beta-*:v0.1.0` with
`<your-registry>/historyserver-beta-*:<your-tag>`.

### 3.6 Apply

```sh
kubectl apply -f rbac.yaml \
              -f configmap-s3.yaml \
              -f eventprocessor.yaml \
              -f historyserver.yaml
```

## 4. Architecture

```text
         +----------------+                +---------------+
 K8s API | RayCluster CRs |<--- get ------>| eventprocessor|
         +----------------+      list      |  (1 pod,      |
                                           |   Recreate)   |
                                           +-------+-------+
                                                   |
                                         write per-session
                                         immutable snapshots
                                                   v
                                        +----------+----------+
                                        |     S3 / MinIO      |
                                        | ray-historyserver/  |
                                        +----------+----------+
                                                   ^
                                         read snapshots (LRU)
                                                   |
                                           +-------+-------+
                                           | historyserver |
                                           | (3 pods,      |
                                           |  RollingUpdate)|
                                           +-------+-------+
                                                   |
                                            HTTP :30080
                                                   v
                                                users
```

## 5. Observability

Metrics paths:

- historyserver: `:30080/metrics`
- eventprocessor: `:9090/metrics`

Key metrics + starter PromQL:

| Metric                         | Query                                                     |
| ------------------------------ | --------------------------------------------------------- |
| Snapshot write rate            | `rate(processor_snapshots_written_total[5m])`             |
| Processor loop duration        | `histogram_quantile(0.95, rate(processor_loop_seconds_bucket[5m]))` |
| Orphan sessions (no snapshot)  | `processor_orphan_sessions`                               |
| History server request rate    | `rate(http_requests_total{job="historyserver-beta"}[5m])` |
| History server 5xx             | `rate(http_requests_total{job="historyserver-beta",code=~"5.."}[5m])` |

Example alert — sessions stuck without a snapshot for 30 minutes:

```yaml
- alert: HistoryServerOrphanSessions
  expr: processor_orphan_sessions > 0
  for: 30m
  labels:
    severity: warning
  annotations:
    summary: "eventprocessor has orphan sessions it cannot snapshot"
```

## 6. Troubleshooting

| Symptom                                                | Likely cause                                                                                  |
| ------------------------------------------------------ | --------------------------------------------------------------------------------------------- |
| `503` from historyserver for a recently-dead session   | Snapshot not yet written; check `kubectl logs deploy/eventprocessor-beta` for loop progress.  |
| Processor logs `k8s probe ... Forbidden`               | RBAC issue. Re-apply `rbac.yaml`; confirm ClusterRoleBinding targets the right ServiceAccount.|
| Empty `/clusters` response                             | Either the K8s client has no RayCluster CRD installed, or the kubeconfig context is wrong. Check operator install + SA token mount. |
| `pull access denied` on image                          | Registry/tag mismatch. Verify `BETA_IMAGE_REGISTRY`/`BETA_IMAGE_TAG` and that `pushbeta` completed. |
| `AccessDenied` writing to S3                           | Secret missing or wrong creds; for IRSA, check the ServiceAccount annotations.                |

## 7. Risks to keep in mind

See `implementation_plan.md` §7 for the full list.

- **Risk #3**: `replicas=1` + `Recreate` on eventprocessor is deliberate.
  A rolling update would briefly run two writers against the same S3 key.
  Leader election unlocks `replicas>1` in a future phase.
- **Risk #6**: Live / long-running clusters (RayServe, ClusterSelector)
  are never snapshotted; they go through the live proxy path.
- **Risk #8**: No alert yet for orphan sessions — see §5 for a starter rule.

[kuberay]: https://github.com/ray-project/kuberay
