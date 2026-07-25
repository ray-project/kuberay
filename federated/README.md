# Federated RayCluster Local Development Environment

Local environment using kind + Cilium ClusterMesh for developing and testing
Federated RayCluster. Creates 3 interconnected Kubernetes clusters where Pods can
communicate across cluster boundaries -- the same connectivity model that Ray requires
for cross-cluster head-to-worker and worker-to-worker communication.

## Architecture

```text
Host machine (Docker network: kind)
├─ frc-primary   (podSubnet: 10.10.0.0/16, Cilium id=1)  ← Ray head, federation controller
├─ frc-member-a  (podSubnet: 10.20.0.0/16, Cilium id=2)  ← remote workers
├─ frc-member-b  (podSubnet: 10.30.0.0/16, Cilium id=3)  ← remote workers
└─ Cilium ClusterMesh: full mesh, cross-cluster Pod-to-Pod connectivity
```

## Why Cilium ClusterMesh

Ray requires **full bidirectional Pod-to-Pod IP connectivity** between all nodes.
Workers register their Pod IP with the head's GCS server, and the head connects
back to workers for task scheduling. Workers also communicate directly with each
other for object transfer (`ray.get()`).

Cilium ClusterMesh provides transparent Pod IP routing across kind clusters,
handling all of Ray's dynamic port requirements without per-port configuration.

## Prerequisites

- Docker Desktop (or Docker Engine on Linux) configured with:
  - **Memory ≥ 12 GB** (16 GB recommended). The default 8 GB is not enough — Cilium
    pods will OOM-kill in a restart loop and the API server will become unresponsive.
    Docker Desktop → Settings → Resources → Memory.
  - **"Use containerd for pulling and storing images" DISABLED** on Docker Desktop.
    When enabled, `kind load docker-image` fails with
    `ctr: content digest sha256:... not found`. Docker Desktop → Settings → General.
    See [Troubleshooting](#kind-load-docker-image-fails-with-ctr-content-digest--not-found)
    for details.
- [`kind`](https://kind.sigs.k8s.io/) (v0.20.0+)
- `kubectl`
- `helm`
- [`cilium` CLI][cilium-cli] (v0.19.2+ -- the scripts pin Cilium image versions
  to match this CLI version). On macOS: `brew install cilium-cli`.

[cilium-cli]: https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/#install-the-cilium-cli

## Quick Start

```bash
cd federated/

# Create 3 clusters with Cilium ClusterMesh.
# First run pulls ~1.2 GB of Cilium images; subsequent runs use cached images.
./hack/bootstrap-federated-ray-lab.sh

# Verify cross-cluster Pod-to-Pod connectivity (6 bidirectional checks)
./hack/smoke-test.sh
```

## Deploy a Cross-Cluster Ray Cluster

After the clusters are running, deploy a Ray head on frc-primary and one worker
on each member cluster:

```bash
# Deploy Ray head + 2 cross-cluster workers (pulls rayproject/ray:2.52.0 if not cached)
./hack/deploy-ray-cluster.sh
```

This proves that workers in remote clusters can join the head's Ray cluster via
ClusterMesh Pod IPs. The script outputs the node IPs and verifies they come from
3 different subnets (10.10.x.x, 10.20.x.x, 10.30.x.x).

## Ray Dashboard

After deploying the Ray cluster, port-forward to access the dashboard:

```bash
kubectl --context kind-frc-primary port-forward ray-head 8265:8265
```

Open <http://localhost:8265> in your browser. The **Cluster** tab shows all 3 nodes
from 3 different subnets, confirming the cross-cluster Ray cluster is working.

## Cleanup

```bash
# Remove only the Ray pods (keep the 3 clusters running)
./hack/deploy-ray-cluster.sh --cleanup

# Tear down everything (all clusters and pods)
./hack/cleanup-federated-ray-lab.sh
```

## Building and Loading Your Own Images

Build and load images directly into kind clusters:

```bash
docker build -t my-operator:dev .
for c in frc-primary frc-member-a frc-member-b; do
  kind load docker-image my-operator:dev --name=$c
done
```

When using `kind load`, set `imagePullPolicy: IfNotPresent` in your manifests.

## Contexts

After setup, use these kubectl contexts:

| Context              | Cluster       | Role                                              |
|----------------------|---------------|----------------------------------------------------|
| `kind-frc-primary`   | frc-primary   | KubeRay operator, federation controller, Ray head  |
| `kind-frc-member-a`  | frc-member-a  | KubeRay operator, remote Ray workers               |
| `kind-frc-member-b`  | frc-member-b  | KubeRay operator, remote Ray workers               |

## Version Pinning

The bootstrap script pre-pulls and loads specific image versions into kind clusters.
These versions are pinned to match what `cilium` CLI v0.19.2 deploys:

| Component | Version | Used by |
|-----------|---------|---------|
| Cilium | v1.19.1 | bootstrap |
| Cilium Envoy | v1.35.9 | bootstrap |
| Cilium Operator | v1.19.1 | bootstrap |
| ClusterMesh API Server | v1.19.1 | bootstrap |
| Certgen | v0.3.2 | bootstrap |
| Ray | 2.52.0 | deploy-ray-cluster |

If you upgrade the `cilium` CLI, update the `CILIUM_IMAGES` array in
`hack/bootstrap-federated-ray-lab.sh` to match the new default versions
(check with `cilium install --dry-run-helm-values`).

## Directory Structure

```text
federated/
├── README.md
├── docs/
│   ├── cross-cluster-ray-validation.md  # Cross-cluster Ray validation results
├── hack/
│   ├── bootstrap-federated-ray-lab.sh   # Create 3 clusters with Cilium ClusterMesh
│   ├── cleanup-federated-ray-lab.sh     # Tear down all clusters
│   ├── deploy-ray-cluster.sh            # Deploy Ray head + cross-cluster workers
│   └── smoke-test.sh                    # Cross-cluster Pod connectivity test
└── infra/
    ├── kind/
    │   ├── frc-primary.yaml             # Primary cluster config (10.10.0.0/16)
    │   ├── frc-member-a.yaml            # Member A cluster config (10.20.0.0/16)
    │   └── frc-member-b.yaml            # Member B cluster config (10.30.0.0/16)
    └── manifests/
        ├── ray-head.yaml                # Ray head pod manifest
        ├── ray-worker.yaml              # Ray worker pod manifest (template)
        ├── smoke-primary.yaml           # Echo pod for connectivity test
        ├── smoke-member-a.yaml          # Echo pod for connectivity test
        └── smoke-member-b.yaml          # Echo pod for connectivity test
```

## Troubleshooting

The three issues below are the ones we actually hit during initial setup. If the
bootstrap or smoke test fails, check these first.

### Docker Desktop is memory-starved → Cilium OOMs and API server times out

**Symptoms:**

- `cilium status --wait` hangs forever on "Cilium control plane not ready".
- `kubectl` calls eventually fail with `net/http: TLS handshake timeout`.
- `kubectl -n kube-system get pods` shows Cilium pods restarting repeatedly
  (`OOMKilled` in `kubectl describe pod`).
- `docker info | grep "Total Memory"` reports something like `7.652GiB`.

**Cause:** Running 3 kind clusters + Cilium + ClusterMesh API servers in parallel
needs more memory than Docker Desktop's default allocation (8 GB).

**Fix:**

1. Tear down any half-started clusters:

   ```bash
   ./hack/cleanup-federated-ray-lab.sh
   docker image prune -af
   ```

2. Docker Desktop → Settings → Resources:
   - Memory: **12 GB minimum**
   - Swap: 2 GB
   - Virtual disk limit: 80 GB
3. Apply & Restart Docker Desktop.
4. Verify with `docker info | grep -E "CPUs|Total Memory"` — you should see
   `Total Memory: 1[2-9]GiB` or higher.
5. Re-run `./hack/bootstrap-federated-ray-lab.sh`.

### `kind load docker-image` fails with `ctr: content digest ... not found`

**Symptoms:**

- Bootstrap script prints `WARNING: failed to load quay.io/cilium/cilium:v1.19.1 into frc-primary`.
- Running the load manually shows:
  `ctr: content digest sha256:... not found`
- `docker info | grep "Storage Driver"` reports `Storage Driver: overlayfs`.

**Cause:** Known compatibility bug between `kind` and Docker Desktop's
**containerd-backed image store**. The image exists in Docker's containerd store
but `kind load` can't read it.

**Fix:**

1. Docker Desktop → Settings → General → **Uncheck "Use containerd for pulling
   and storing images"**.
2. Apply & Restart. After restart, `docker info | grep "Storage Driver"` should
   read `Storage Driver: overlay2`.
3. Re-pull any images (they need to be re-downloaded into the legacy store):

   ```bash
   for img in quay.io/cilium/cilium:v1.19.1 \
              quay.io/cilium/operator-generic:v1.19.1 \
              quay.io/cilium/clustermesh-apiserver:v1.19.1 \
              quay.io/cilium/cilium-envoy:v1.35.9 \
              quay.io/cilium/certgen:v0.3.2 \
              rayproject/ray:2.52.0; do
     docker pull "$img"
   done
   ```

4. Re-run `./hack/bootstrap-federated-ray-lab.sh`.

### Cilium install / clustermesh timeouts because images are too large

**Symptoms:**

- `cilium status --wait` inside the bootstrap prints
  `timeout: Cilium is not ready` after ~5 minutes.
- `kubectl -n kube-system get pods` shows Cilium pods still in
  `ContainerCreating` or `Init:0/N` — the image pull hasn't finished yet.
- Your network is slow and `quay.io/cilium/cilium` (~500 MB) and
  `clustermesh-apiserver` take a long time to pull.

**Cause:** Cilium images are large and the default `--wait-duration` on
`cilium status` and `cilium clustermesh status` was too short for slow networks.

**Workarounds (in order of preference):**

1. **Pre-load images into kind** — this is what the bootstrap script already does
   for the `CILIUM_IMAGES` array. Images are pulled *once* to the host and then
   `kind load`ed into all 3 clusters. If you hit this, make sure the pre-load
   step actually succeeded (see the `ctr: content digest not found` issue above —
   silent load failures look identical to this symptom).

2. **Bump the wait duration** in `hack/bootstrap-federated-ray-lab.sh` if your
   network is genuinely slow:

   ```bash
   cilium status --context "${PRIMARY_CONTEXT}"  --wait --wait-duration 60m
   cilium status --context "${MEMBER_A_CONTEXT}" --wait --wait-duration 60m
   cilium status --context "${MEMBER_B_CONTEXT}" --wait --wait-duration 60m
   ```

   60m is a deliberately large defensive bound; it only blocks until Cilium is
   actually ready, it doesn't force you to wait the full hour.

3. **Pre-pull images on a fast network** then move to the slow one. The script
   skips `docker pull` for images already in the local cache.

### Cilium pods stuck in `Init` or `ImagePullBackOff`

The bootstrap script pre-pulls Cilium images to the host and loads them into kind
clusters to avoid slow in-cluster pulls. If your network blocks `quay.io` entirely,
pull the images from another network first, then re-run the bootstrap (it skips
already-cached images).

### ClusterMesh connect fails with "CA certificates do not match"

Each cluster generates its own Cilium CA by default. The bootstrap script already
handles this with `--allow-mismatching-ca`. If you run the commands manually,
add that flag, or share a common CA across clusters by passing
`--set tls.ca.cert=... --set tls.ca.key=...` during `cilium install`.

### Smoke test fails intermittently

Cross-cluster routes may take a few seconds to propagate after ClusterMesh connects.
The smoke test retries each check up to 3 times. If failures persist, run
`cilium clustermesh status --context kind-frc-primary` to verify all connections
are established.
