# Federated RayCluster Local Development Environment

Local environment using kind + Cilium ClusterMesh for developing and testing
Federated RayCluster. Creates 3 interconnected Kubernetes clusters where Pods can
communicate across cluster boundaries -- the same connectivity model that Ray requires
for cross-cluster head-to-worker and worker-to-worker communication.

## Architecture

```
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

- Docker Desktop (or Docker Engine on Linux) with at least **16 GB memory** allocated
- [`kind`](https://kind.sigs.k8s.io/) (v0.20.0+)
- `kubectl`
- `helm`
- [`cilium` CLI](https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/#install-the-cilium-cli) (v0.19.2+ -- the scripts pin Cilium image versions to match this CLI version)

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

Open http://localhost:8265 in your browser. The **Cluster** tab shows all 3 nodes
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

```
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
