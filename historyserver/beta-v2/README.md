# History Server v2 — beta-v2 (lazy mode, single binary)

beta-v2 is a refactor of the beta `historyserver + eventprocessor` pair
into a single binary. Snapshots are built **on demand** the first time a
user visits `/enter_cluster` for a dead session, rather than on a periodic
ticker. See `../beta_poc.md` for the full design (sequence diagram,
idempotency layers, risk analysis).

---

## 1. Key differences from beta

| Topic                  | beta                          | beta-v2                                               |
| ---------------------- | ----------------------------- | ----------------------------------------------------- |
| Processes              | `historyserver` + `eventprocessor` | Just `historyserver` (Pipeline lives as a goroutine) |
| Snapshot trigger       | Ticker every 10 min            | `/enter_cluster` on dead session                      |
| `/enter_cluster` latency on cold path | ~ms (sets cookies only) | Blocks until snapshot lands                          |
| Concurrent dedup       | None (one-process tick loop)   | `singleflight.Group` per session                      |
| Cache prime after PUT  | No (next handler GET pays the cost) | Yes (`SnapshotLoader.Prime`)                          |
| Replicas               | 3 HS + 1 processor             | 3 HS, no separate processor                           |

---

## 2. Repository layout

```sh
beta-v2/
├── cmd/historyserver/main.go     # single binary entrypoint
├── pkg/
│   ├── server/
│   │   ├── enter_cluster.go      # Supervisor + singleflight (new)
│   │   ├── enter_cluster_test.go
│   │   ├── cache.go              # SnapshotLoader + Prime (new)
│   │   ├── router.go             # blocking /enter_cluster handler
│   │   ├── handlers.go / server.go / …   (unchanged from beta semantically)
│   ├── processor/session.go      # Pipeline: exported ProcessSession(ctx)
│   ├── snapshot/                 # SessionSnapshot schema (unchanged)
│   └── metrics/                  # enter_cluster + singleflight metrics
├── config/                       # k8s manifests + README
├── scripts/setup-kind-dev.sh     # one-shot kind + KubeRay + MinIO + deploy
├── Dockerfile.historyserver
└── README.md                     # you are here
```

---

## 3. Local development

### 3.1 Unit tests

Quickest inner loop. Runs under `-race`; no Docker needed.

```sh
cd historyserver
make testbeta-v2
```

or equivalently from any dir:

```sh
go test -race -v ./historyserver/beta-v2/...
```

What's covered:

- `beta-v2/pkg/processor/session_test.go` — Pipeline state-machine
  branches: live/dead/already-snapped/k8s-error/ctx-canceled.
- `beta-v2/pkg/server/cache_test.go` — LRU hit/miss/eviction +
  `Prime` bypass / overwrite / nil guard.
- `beta-v2/pkg/server/enter_cluster_test.go` — Supervisor three-layer
  logic, singleflight coalescing, ctx-canceled follower release, and
  HTTP-level `/enter_cluster` against a fake Pipeline.
- `beta-v2/pkg/server/server_test.go` — route registration, cookie
  writes, proxy URL shapes (unchanged from beta).
- `beta-v2/pkg/metrics/metrics_test.go` — metric delta assertions +
  /metrics exposition smoke test.

### 3.2 Build the binary

```sh
cd historyserver
make buildbeta-v2
./output/bin/historyserver-beta-v2 --help
```

### 3.3 Build the image

```sh
cd historyserver
make localimage-beta-v2
# Tag/registry overrides:
make BETA_V2_IMAGE_REGISTRY=<your-registry> BETA_V2_IMAGE_TAG=<tag> pushbeta-v2
```

---

## 4. End-to-end on kind (one command)

```sh
cd historyserver
make deploybeta-v2-kind
```

This wraps `beta-v2/scripts/setup-kind-dev.sh`, which is idempotent —
safe to re-run. It provisions:

1. Local docker registry at `127.0.0.1:5001`.
2. kind cluster `beta-v2-dev` with the registry mirror.
3. KubeRay operator (default v1.6.0; beta-v2 is tested against v1.6.0+) —
   supplies the `RayCluster` CRD. Override with
   `KUBERAY_VERSION=<x.y.z>` when invoking the script.
4. MinIO in namespace `minio-dev` (dev creds: `minioadmin/minioadmin`).
5. `ray-historyserver` bucket in MinIO.
6. Builds the beta-v2 image and pushes it to the local registry.
7. Applies `beta-v2/config/*.yaml` with the image ref rewritten to
   `localhost:5001/...`.

---

## 5. Smoke tests on a live cluster

After `make deploybeta-v2-kind` finishes, port-forward the HS Service:

```sh
kubectl port-forward svc/historyserver-beta-v2 8080:30080
```

### 5.1 Generate a dead session

```sh
# Deploy the sample RayCluster and wait for the head pod to be Ready.
kubectl apply  -f historyserver/beta-v2/config/raycluster-sample.yaml
kubectl wait pod -l ray.io/node-type=head --for=condition=Ready --timeout=180s

# Submit a handful of tasks + an actor so collectors push real events to
# S3. A deterministic workload beats `sleep 60` — it returns as soon as
# the events are flushed, and it produces enough state (tasks, actors,
# jobs) that the /tasks and /logical/actors handlers have something to
# render in later steps.
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

# Delete the RayCluster — the session is now "dead" (CR gone, events on S3).
kubectl delete -f historyserver/beta-v2/config/raycluster-sample.yaml

# Discover the session name via the historyserver's /clusters endpoint.
# This lists BOTH live and dead cluster sessions — dead sessions carry
# the real session_* name you'll feed into /enter_cluster below.
curl -sS http://localhost:8080/clusters
```

### 5.2 Time the cold path (first call — Pipeline runs synchronously)

```sh
# Replace <session> with the value from step 5.1.
time curl -s -o /dev/null \
  http://localhost:8080/enter_cluster/default/ray-beta-sample/<session>
```

Expect this to take seconds: K8s probe + event parse + S3 PUT.

Verify the snapshot landed in MinIO:

```sh
kubectl -n minio-dev exec -it deploy/minio -- \
    mc ls local/ray-historyserver/log/ray-beta-sample_default/<session>/processed/
# Should list session.json
```

### 5.3 Time the warm path (second call — LRU hit)

```sh
time curl -s -o /dev/null \
  http://localhost:8080/enter_cluster/default/ray-beta-sample/<session>
```

Expect < 10 ms (cache hit; no S3 round-trip; no Pipeline execution).

### 5.4 Check coalescing (N parallel cold calls → one Pipeline run)

```sh
# 10 parallel cold calls for a different dead session.
for i in $(seq 1 10); do
  curl -s -o /dev/null \
    http://localhost:8080/enter_cluster/default/other-cluster/<session> &
done
wait

# Scrape the counter and confirm we deduped:
curl -s http://localhost:8080/metrics | grep singleflight_dedup_total
# Non-zero means at least one group coalesced.
```

### 5.5 Watch lazy-mode metrics

```sh
curl -s http://localhost:8080/metrics | grep -E \
  'enter_cluster|singleflight|session|cache_' \
  | grep -v '^#'
```

Useful signals:

- `server_enter_cluster_duration_seconds_*` — cold-path latency histogram.
- `server_enter_cluster_total{status="ok"|"error"}` — success/failure rate.
- `server_singleflight_dedup_total` — coalesced-caller count.
- `server_cache_hits_total` / `server_cache_misses_total` — LRU ratio.
- `processor_sessions_processed_total` — lazy Pipeline execution count.

### 5.6 Tail logs

```sh
kubectl logs -f -l app=historyserver-beta-v2 --tail=50
```

You should see one `wrote snapshot for <cluster>_<ns>/<session>` line per
first-time cold-path call and nothing else for warm-path calls.

---

## 6. Production notes

- See `config/README.md` for prerequisites, credential wiring, and the
  full apply flow.
- Rolling updates are safe: snapshots are immutable at the S3 layer
  (skip-if-exists guarantees it), so replicas racing to PUT the same
  session converge to the same bytes.
- Each replica owns an independent LRU + singleflight group — at most one
  Pipeline run per replica per session concurrently. Cross-replica dedup
  is a non-goal in this PoC (see `beta_poc.md` §9).

---

## 7. Non-goals and risks

See `../beta_poc.md` §8 risks and §9 non-goals. Summary:

- No admin endpoint to force snapshot rebuild. Snapshots are immutable.
- No cross-replica singleflight. Accepted: worst case = duplicate PUTs.
- Live-cluster partial snapshots are explicitly out of scope.
