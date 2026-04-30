# History Server Beta

History Server Beta is a single-binary HTTP daemon that serves Ray Dashboard-shaped API calls and drives the
per-session snapshot pipeline on demand.

## 1. Repository Layout

```sh
beta-v2/
├── cmd/historyserver/        # single-binary entrypoint
├── pkg/
│   ├── processor/            # Pipeline: dead-session events → SessionSnapshot
│   ├── snapshot/             # SessionSnapshot schema
│   ├── server/               # HTTP handlers, Supervisor, SnapshotLoader
│   └── metrics/              # Prometheus declarations
├── config/                   # k8s manifests (Deployment, RBAC, MinIO, sample RayCluster)
├── scripts/
│   └── setup-kind-dev.sh     # one-shot kind + KubeRay + MinIO + image push + deploy
├── docs/
│   └── design.md             # (wip) architecture, design decisions, v1 ports, Ray refs
├── Dockerfile.historyserver
└── README.md
```

## 2. Local Development

### 2.1 Unit Tests

Run unit tests under `-race` (no Docker needed):

```sh
cd historyserver
make testbeta-v2
```

or equivalently from any directory:

```sh
go test -race -v ./historyserver/beta-v2/...
```

Coverage by package:

```text
pkg/snapshot          100.0%
pkg/metrics           100.0%
pkg/processor          71.9%
pkg/server             43.0%
cmd/historyserver       0.0%   (main entry; integration-only)
```

What each test file focuses on:

| File | Focus |
|---|---|
| `pkg/processor/session_test.go` | Pipeline state machine: Live / Processed / K8sProbeErr / Canceled. |
| `pkg/server/cache_test.go` | LRU hit/miss, `Prime` semantics + nil guard, eviction. |
| `pkg/server/enter_cluster_test.go` | Supervisor singleflight dedup, follower ctx-cancel release, Pipeline status routing. |
| `pkg/server/handlers_test.go` | Tasks / actor / timeline / nodelogs / cluster_metadata / cluster_status happy + error paths. |
| `pkg/server/server_test.go` | Route registration, cookie writes, proxy URL shapes, `redirectRequest` 501 short-circuit. |
| `pkg/snapshot/snapshot_test.go` | Schema round-trip. |
| `pkg/metrics/metrics_test.go` | Counter increments + `/metrics` exposition smoke. |

### 2.2 Build the Binary

```sh
cd historyserver
make buildbeta-v2
./output/bin/historyserver-beta-v2 --help
```

### 2.3 Build the Image

```sh
cd historyserver
make localimage-beta-v2

# Override tag and registry.
make BETA_V2_IMAGE_REGISTRY=<your-registry> \
     BETA_V2_IMAGE_TAG=<tag> \
     pushbeta-v2
```

## 3. End-to-End on Kind

```sh
cd historyserver
make deploybeta-v2-kind
```

This wraps `beta-v2/scripts/setup-kind-dev.sh`, which is idempotent (safe to re-run). It provisions:

1. Local Docker registry at `127.0.0.1:5001`
2. kind cluster `beta-v2-dev` with the registry mirror
3. KubeRay operator (default `v1.6.0`; override with `KUBERAY_VERSION=<x.y.z>`), supplying the `RayCluster` CRD
4. MinIO in namespace `minio-dev` (dev creds: `minioadmin/minioadmin`)
5. Build the beta-v2 image and push it to the local registry
6. Apply `beta-v2/config/*.yaml` with the image ref rewritten to `localhost:5001/...`

## 4. Smoke Tests

Port-forward the History Server Service:

```sh
kubectl port-forward svc/historyserver-beta-v2 8080:30080
```

### 4.1 Generate a Dead Cluster

```sh
# Deploy the sample RayCluster and wait for the head pod to be Ready.
kubectl apply -f historyserver/beta-v2/config/raycluster-sample.yaml
kubectl wait pod -l ray.io/node-type=head --for=condition=Ready --timeout=180s

# Run a deterministic Ray workload so collectors push real tasks/actors/jobs.
kubectl exec $(kubectl get pod -l ray.io/node-type=head -o name) \
  -c ray-head -- python -c "
import ray
ray.init(address='auto')

@ray.remote
def add(x, y): return x + y

@ray.remote
class Counter:
    def __init__(self): self.n = 0
    def inc(self): self.n += 1; return self.n

print('tasks:', ray.get([add.remote(i, i) for i in range(5)]))
c = Counter.remote()
print('actor counts:', ray.get([c.inc.remote() for _ in range(3)]))
print('done')
"

# Delete the RayCluster (a dead session).
kubectl delete -f historyserver/beta-v2/config/raycluster-sample.yaml

# Discover the session name. /clusters lists both live and dead sessions;
# dead sessions carry the `session_*` name you'll feed into /enter_cluster.
curl -sS http://localhost:8080/clusters
```

### 4.2 Cold Path

Enter a cluster for the first time:

```sh
# Replace <session> with the value from §4.1.
time curl -s -o /dev/null \
  http://localhost:8080/enter_cluster/default/ray-beta-sample/<session>
```

> [!NOTE] Pipeline runs synchronously. Expect this to take seconds: K8s probe + event parse. The built snapshot is held
in the per-replica LRU only; it is **not** persisted to object storage.

### 4.3 Warm Path

Enter the same cluster again on the same replica:

```sh
# Replace <session> with the value from §4.1.
time curl -s -o /dev/null \
  http://localhost:8080/enter_cluster/default/ray-beta-sample/<session>
```

> [!NOTE] Expect `< 10 ms` (LRU cache hit; no Pipeline execution). If the LRU has evicted this session, or another
replica handles the request, the cold path will run again.

### 4.4 Singleflight Coalescing

Execute N parallel cold calls:

```sh
# 10 parallel cold calls for another dead session.
for i in $(seq 1 10); do
  curl -s -o /dev/null \
    http://localhost:8080/enter_cluster/default/other-cluster/<session> &
done
wait

# Confirm dedup happened (only one Pipeline runs):
curl -s http://localhost:8080/metrics | grep singleflight_dedup_total
# Non-zero means at least one group coalesced.
```

### 4.5 Lazy-Mode Metrics

```sh
curl -s http://localhost:8080/metrics \
  | grep -E 'enter_cluster|singleflight|session|cache_' \
  | grep -v '^#'
```

Useful signals:

- `server_enter_cluster_duration_seconds_*` — cold-path latency histogram
- `server_enter_cluster_total{status="ok"|"error"}` — success / failure rate
- `server_singleflight_dedup_total` — coalesced-caller count
- `server_cache_hits_total` / `server_cache_misses_total` — LRU ratio
- `processor_sessions_processed_total` — lazy Pipeline execution count

### 4.6 Tail Logs

```sh
kubectl logs -f -l app=historyserver-beta-v2 --tail=50
```

Cold-path calls log the K8s probe and event parse; warm-path calls
(LRU hit) produce no Pipeline log lines.

## 5. Integration with Ray Dashboard
