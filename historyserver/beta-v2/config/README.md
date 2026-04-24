# History Server v2 beta-v2 — Deployment

## 1. Overview

beta-v2 is a **single-binary** historyserver: the eventprocessor has been
folded back in as a request-driven `Supervisor` inside the HS process. The
first `/enter_cluster` hit for a dead session builds the snapshot inline,
puts it to S3 (skip-if-exists), and primes the LRU; subsequent requests
are served from cache without any object-store read.

- **historyserver-beta-v2** — 3 stateless pods, default `RollingUpdate`.
  Each pod owns an independent LRU + `singleflight.Group`, so replicas
  race on snapshot writes but converge to the same bytes (snapshots are
  immutable — see `beta_poc.md` §1 for the three-layer idempotency proof).

No separate eventprocessor Deployment exists. Full design lives in
`../../beta_poc.md`.

## 2. Quick start (kind dev)

One command spins up kind + KubeRay + MinIO, builds + pushes the beta-v2
image, and applies all manifests:

```sh
cd ../
./scripts/setup-kind-dev.sh
```

Verify:

```sh
kubectl port-forward svc/historyserver-beta-v2 8080:30080
curl http://localhost:8080/api/v2/clusters
```

The /enter_cluster cold path now blocks; time it to see the lazy build:

```sh
# First call for a dead session — blocks until the snapshot is written.
time curl -s http://localhost:8080/enter_cluster/default/ray-beta-sample/session_<ts>
# Second call — fast path (LRU hit).
time curl -s http://localhost:8080/enter_cluster/default/ray-beta-sample/session_<ts>
```

Metrics (combined with the API on :30080):

```sh
curl http://localhost:8080/metrics | grep -E 'enter_cluster|singleflight|sessions_'
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
   backend-specific version and update `--runtime-class-name` on the
   Deployment.
3. A container registry your cluster can pull from.

### 3.2 Build and push the image

From the `historyserver/` directory:

```sh
make BETA_V2_IMAGE_REGISTRY=<your-registry> \
     BETA_V2_IMAGE_TAG=<your-tag> \
     pushbeta-v2
```

### 3.3 Configure credentials

```sh
kubectl create secret generic historyserver-beta-v2-s3 \
  --from-literal=access_key=<ACCESS_KEY> \
  --from-literal=secret_key=<SECRET_KEY>
```

The Secret is marked `optional: true` in the Deployment, so pods fall
back to the node's IAM role / instance profile if no Secret is created.

### 3.4 Point the ConfigMap at your backend

Edit `configmap-s3.yaml` — change `s3Bucket`, `s3Endpoint`, `s3Region`,
`s3ForcePathStyle`, `s3DisableSSL` to match your environment.

### 3.5 Update the image field

Edit `historyserver.yaml` — replace the placeholder
`historyserver-beta-v2-historyserver:v0.1.0` with
`<your-registry>/historyserver-beta-v2-historyserver:<your-tag>`.

### 3.6 Apply

```sh
kubectl apply -f rbac.yaml \
              -f configmap-s3.yaml \
              -f historyserver.yaml
```

## 4. Architecture

```text
         +----------------+      get/list
 K8s API | RayCluster CRs |<-------+
         +----------------+        |
                                   |
                               +---+-----------+
                               | historyserver |
                               | beta-v2 (3)   |
                               |               |
                               |  HTTP router  |
                               |       |       |
                               |       v       |
                               |   Supervisor  |
                               |  (singleflight|
                               |   + LRU Prime)|
                               |       |       |
                               |       v       |
                               |   Pipeline    |
                               |  (isDead +    |
                               |   parse +     |
                               |   PUT)        |
                               +---+-------+---+
                          write    |       |    read snapshots
                    snapshots PUT  |       |    (LRU + S3 fallback)
                        (skip-if-  |       |
                         exists)   v       v
                            +------+-------+------+
                            |     S3 / MinIO      |
                            | ray-historyserver/  |
                            +---------------------+
                                      ^
                                      |
                                  users via :30080
```

## 5. Observability

Metrics path (combined with API): `:30080/metrics`

Key beta-v2 metrics + starter PromQL:

| Metric                                     | Query                                                                           |
| ------------------------------------------ | ------------------------------------------------------------------------------- |
| /enter_cluster OK rate                     | `rate(server_enter_cluster_total{status="ok"}[5m])`                             |
| /enter_cluster error rate                  | `rate(server_enter_cluster_total{status="error"}[5m])`                          |
| /enter_cluster p95 latency                 | `histogram_quantile(0.95, rate(server_enter_cluster_duration_seconds_bucket[5m]))` |
| Coalesced participants (dedup savings)     | `rate(server_singleflight_dedup_total[5m])`                                     |
| Snapshot cache hits vs misses              | `rate(server_cache_hits_total[5m]) / rate(server_cache_misses_total[5m])`       |
| Lazy Pipeline write rate                   | `rate(processor_sessions_processed_total[5m])`                                  |
| Pipeline error rate by stage               | `rate(processor_session_errors_total[5m])`                                      |

Example alert — a steady error rate on /enter_cluster:

```yaml
- alert: HistoryServerEnterClusterErrors
  expr: rate(server_enter_cluster_total{status="error"}[5m]) > 0.05
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "beta-v2 /enter_cluster returning 500s"
```

## 6. Troubleshooting

| Symptom                                                       | Likely cause                                                                                         |
| ------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `503` from a dead-snapshot endpoint (`/api/v0/tasks`, …)      | Handler was hit directly (stale cookies) without going through /enter_cluster — ask user to re-enter. |
| `/enter_cluster` returns 500                                  | Check pod logs. Common: K8s probe error (RBAC or API outage), S3 PUT error (creds or network).       |
| `/enter_cluster` slow (> 10s)                                 | Cold-path Pipeline with a large session. Check `processor_session_duration_seconds` p99.              |
| Empty `/clusters` response                                    | K8s RBAC missing; re-apply `rbac.yaml` and confirm the ClusterRoleBinding targets `historyserver-beta-v2`. |
| `pull access denied` on image                                 | Registry/tag mismatch. Verify `BETA_V2_IMAGE_REGISTRY`/`BETA_V2_IMAGE_TAG` and that `pushbeta-v2` completed. |
| `AccessDenied` writing to S3                                  | Secret missing or wrong creds; for IRSA, check the ServiceAccount annotations.                       |

## 7. Risks to keep in mind

See `../../beta_poc.md` §8 for the full list.

- **Risk #1**: Pipeline tail latency translates directly into user-visible
  HTTP latency. Monitor `server_enter_cluster_duration_seconds` p95/p99.
- **Risk #2**: Singleflight follower cancellation works via `DoChan`, but
  a follower is still tied to the winner's ctx result. Accepted per
  beta_poc.md §8.
- **Risk #3**: Snapshot PUT success but pod crashes before `Prime` — next
  call re-hydrates via S3 GET (Layer 2). No data loss.
- **Risk #4**: No ticker means stale snapshots are never rebuilt. This is
  intentional (snapshots immutable by design). Admin rebuild endpoint is
  a non-goal; see beta_poc.md §9.

[kuberay]: https://github.com/ray-project/kuberay
