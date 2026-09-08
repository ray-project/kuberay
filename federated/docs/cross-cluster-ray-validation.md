# Cross-Cluster Ray Cluster Validation

This document describes how to deploy a Ray cluster spanning 3 Kubernetes clusters
and validates that the networking setup works for Federated RayCluster development.

## What This Validates

1. A Ray head in the **primary** cluster can accept workers from **member** clusters
2. Workers register with their Pod IP and the head can connect back to them
3. All 3 nodes form a single, unified Ray cluster via Cilium ClusterMesh Pod-to-Pod routing

## Prerequisites

- 3 kind clusters running with Cilium ClusterMesh (run `./hack/bootstrap-federated-ray-lab.sh`)
- `rayproject/ray:2.52.0` image available

## Quick Start

```bash
cd federated/

# Deploy head on primary, workers on member-a and member-b
./hack/deploy-ray-cluster.sh

# Clean up Ray pods (keeps clusters running)
./hack/deploy-ray-cluster.sh --cleanup
```

## What the Script Does

### Step 1: Deploy Ray Head on frc-primary

```yaml
# ray-head.yaml
command: ray start --head --port=6379 --dashboard-host=0.0.0.0 --node-ip-address=$POD_IP --block
ports: [6379 (GCS), 8265 (dashboard), 10001 (client)]
```

The head starts GCS on port 6379 and advertises its Pod IP (`10.10.x.x`) via `--node-ip-address`.

### Step 2: Get Head Pod IP

```bash
HEAD_IP=$(kubectl --context kind-frc-primary get pod ray-head -o jsonpath='{.status.podIP}')
# Example: 10.10.2.231
```

### Step 3: Deploy Workers on Member Clusters

Workers are deployed with `RAY_HEAD_ADDRESS` set to the head's Pod IP:

```yaml
# ray-worker.yaml
command: ray start --address=$RAY_HEAD_ADDRESS --node-ip-address=$POD_IP --block
env:
  RAY_HEAD_ADDRESS: "10.10.2.231:6379"   # Injected by deploy script
  POD_IP: <from downward API>             # Worker's own Pod IP
```

The `--node-ip-address=$POD_IP` is critical: it tells Ray to advertise the worker's
Pod IP (e.g., `10.20.x.x`) so the head can connect back to it via ClusterMesh.

### Step 4: Verify

```bash
kubectl --context kind-frc-primary exec ray-head -- ray status
```

Expected output:
```
Node status
---------------------------------------------------------------
Active:
 1 node_<hash1>    ← head  (10.10.x.x)
 1 node_<hash2>    ← worker (10.20.x.x)
 1 node_<hash3>    ← worker (10.30.x.x)
```

3 active nodes, 3 unique subnets = workers from 2 different Kubernetes clusters
successfully joined the head's Ray cluster.

## Validated Results

| Component | Cluster | Pod IP Subnet | Status |
|-----------|---------|---------------|--------|
| Ray Head  | frc-primary  | 10.10.x.x | Active |
| Worker A  | frc-member-a | 10.20.x.x | Active, joined head |
| Worker B  | frc-member-b | 10.30.x.x | Active, joined head |

Total resources visible to Ray: 6 CPU, ~3 GiB memory (2 CPU per node).

## What This Proves for Federated RayCluster

1. **GCS bootstrap works cross-cluster**: Workers in member clusters connect to the
   head's GCS port via ClusterMesh Pod IP routing.

2. **Bidirectional connectivity works**: The head can reach workers back at their Pod IPs
   for task scheduling (NodeManager RPCs). If this didn't work, workers would register
   but `ray status` would show them as dead.

3. **Pod IP advertisement works**: `--node-ip-address=$POD_IP` correctly advertises
   each node's ClusterMesh-routable Pod IP. The head uses these IPs for reverse
   connections.

4. **No special networking configuration needed**: Beyond Cilium ClusterMesh setup,
   no additional ports, services, or routing rules were needed. Ray's dynamic port
   allocation works transparently over ClusterMesh.

## What's NOT Validated Yet

- **Object transfer across clusters**: Running `ray.get()` on a remote object would
  exercise the ObjectManager path (Pod-to-Pod on random ports). This requires
  submitting a Ray job, which needs a driver process with sufficient resources.

- **KubeRay operator integration**: This test uses plain pods. The FederatedRayCluster
  controller would use KubeRay to manage these pods.

- **Autoscaling**: Ray autoscaler is not configured in this manual test.

- **Head restart / reconnection**: What happens when the head pod restarts and gets
  a new IP.

## Useful Commands

```bash
# Check Ray cluster status
kubectl --context kind-frc-primary exec ray-head -- ray status

# Access Ray dashboard
kubectl --context kind-frc-primary port-forward ray-head 8265:8265
# Then open http://localhost:8265

# Check worker logs
kubectl --context kind-frc-member-a logs ray-worker
kubectl --context kind-frc-member-b logs ray-worker

# Clean up Ray pods only (keep clusters running)
./hack/deploy-ray-cluster.sh --cleanup

# Tear down everything
./hack/cleanup-federated-ray-lab.sh
```
