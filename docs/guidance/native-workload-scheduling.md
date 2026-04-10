<!-- markdownlint-disable MD013 -->
# Native Workload Scheduling (Gang Scheduling)

This guide explains how to use KubeRay's native Kubernetes gang scheduling integration, which ensures that all pods in a RayCluster worker group are either entirely schedulable or not scheduled at all.

## Overview

Distributed AI/ML workloads on Kubernetes can suffer from partial scheduling: some pods in a group get scheduled and hold expensive GPU nodes idle while waiting for the remaining pods,
or partially scheduled groups block other workloads indefinitely (livelock). Gang scheduling solves this by treating a group of pods as an atomic unit.

KubeRay integrates with the **native** Kubernetes gang scheduling APIs (`scheduling.k8s.io/v1alpha2`) introduced in [KEP-4671](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/4671-gang-scheduling)
and [KEP-5832](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/5832-decouple-podgroup-api).
"Native" means this gang scheduling is built into the default Kubernetes scheduler (kube-scheduler) starting in Kubernetes 1.36, unlike other external solutions (Volcano, YuniKorn, etc.) which replace or wrap the default scheduler.

When enabled, the KubeRay operator creates `Workload` and `PodGroup` resources for each RayCluster and sets `spec.schedulingGroup` on every pod, linking it to the appropriate PodGroup.
The Kubernetes scheduler then evaluates each worker group as a gang — all pods in the group must be schedulable before any of them are scheduled.

## Prerequisites

- **Kubernetes 1.36+** with the following feature gates enabled:
  - `GenericWorkload=true` on the **kube-apiserver** — enables the Workload/PodGroup APIs
  - `scheduling.k8s.io/v1alpha2=true` in the apiserver's **runtime-config** — serves the alpha API (alpha APIs are off by default)
  - `GenericWorkload=true` on the **kube-controller-manager**
  - `GangScheduling=true` on the **kube-scheduler** — enables the gang scheduling plugin that processes `schedulingGroup` on pods
- **KubeRay operator** with the `NativeWorkloadScheduling` feature gate enabled

## Enabling the Feature

Start with a Kubernetes 1.36+ cluster with the required feature gates (`GenericWorkload`, `GangScheduling`) and alpha API enabled as described in [Prerequisites](#prerequisites).

### 1. Enable the KubeRay operator feature gate

Pass the `NativeWorkloadScheduling` feature gate when starting the operator:

```bash
--feature-gates=NativeWorkloadScheduling=true
```

With Helm:

```yaml
featureGates:
  - name: NativeWorkloadScheduling
    enabled: true
```

Or via `--set`:

```bash
helm install kuberay-operator kuberay/kuberay-operator \
  --set featureGates[0].name=NativeWorkloadScheduling \
  --set featureGates[0].enabled=true
```

### 2. Opt-in per RayCluster

Add the `ray.io/native-workload-scheduling: "true"` annotation to each RayCluster that should use gang scheduling:

```yaml
apiVersion: ray.io/v1
kind: RayCluster
metadata:
  name: my-cluster
  annotations:
    ray.io/native-workload-scheduling: "true"
spec:
  headGroupSpec:
    rayStartParams:
      dashboard-host: "0.0.0.0"
    template:
      spec:
        containers:
          - name: ray-head
            image: rayproject/ray:2.52.0
            resources:
              limits:
                cpu: "1"
                memory: "2Gi"
  workerGroupSpecs:
    - groupName: gpu-workers
      replicas: 4
      minReplicas: 4
      maxReplicas: 4
      rayStartParams: {}
      template:
        spec:
          containers:
            - name: ray-worker
              image: rayproject/ray:2.52.0
              resources:
                limits:
                  cpu: "1"
                  memory: "2Gi"
                  nvidia.com/gpu: "1"
```

## What Happens Under the Hood

When both the feature gate and annotation are set, the operator creates the following resources before creating pods:

1. **A `Workload` object** (one per RayCluster) with a `podGroupTemplate` for each group:
   - One template named `head` using `BasicSchedulingPolicy` (single pod, no gang constraint)
   - One template per worker group named `worker-<groupName>` using `GangSchedulingPolicy` with `minCount` set to the desired replica count

2. **PodGroup objects** (one per template) named `<clusterName>-head`, `<clusterName>-worker-<groupName>`, etc.

3. **`spec.schedulingGroup`** is set on every pod, linking it to the appropriate PodGroup

The Workload and PodGroups are owned by the RayCluster (via `ownerReferences`), so they are automatically garbage collected when the RayCluster is deleted.

### Spec drift detection

If you change the RayCluster spec (add/remove worker groups, change replica counts), the operator detects the mismatch, deletes the stale Workload and PodGroups, and recreates them from the updated spec.

### Suspend and resume

When a RayCluster is suspended, the operator deletes the Workload and PodGroups alongside the pods. On resume, fresh scheduling resources are created with the current spec.

### Status condition

When the `RayClusterStatusConditions` feature gate is also enabled, the operator sets a `WorkloadScheduled` condition on the RayCluster:

| Condition | Status | Reason | Meaning |
|-----------|--------|--------|---------|
| `WorkloadScheduled` | `True` | `WorkloadReady` | Workload and PodGroups have been created |
| `WorkloadScheduled` | `False` | `WorkloadPending` | Workload has not been created yet |

The condition is removed when the cluster is suspended or when native scheduling is not enabled.

## Limitations

> **Note**: This feature is in early alpha. Both the Kubernetes `scheduling.k8s.io/v1alpha2` API and the KubeRay integration are under active development. Notably, autoscaling is not supported — only fixed-size worker groups are compatible.

- **No autoscaling support**: RayClusters with autoscaling enabled (`enableInTreeAutoscaling: true`) will skip native scheduling with a warning event. Fixed-size worker groups only.
- **Max 7 worker groups**: The `scheduling.k8s.io/v1alpha2` API allows at most 8 PodGroupTemplates per Workload (1 reserved for the head group).
- **Per-worker-group atomicity only**: Each worker group is scheduled as an independent gang. There is no cross-worker-group atomicity (e.g., "schedule all GPU workers AND all CPU workers or none").
- **Mutually exclusive with batch schedulers**: Cannot be used together with `batchScheduler` configuration (Volcano, YuniKorn, etc.). The operator will refuse to start if both are enabled.
- **Immutable `schedulingGroup` on pods**: The `spec.schedulingGroup` field on pods is immutable. If you enable native scheduling on an already-running cluster, existing pods will not get `schedulingGroup` set. New pods (from scale-up, recreation, or suspend/resume) will be correctly configured.
- **Requires Kubernetes 1.36+**: The `scheduling.k8s.io/v1alpha2` API is not available in earlier versions.

## Troubleshooting

### Verify resources were created

```bash
# Check Workload
kubectl get workloads -n <namespace>

# Check PodGroups
kubectl get podgroups.scheduling.k8s.io -n <namespace>

# Check events on the RayCluster
kubectl describe raycluster <name> -n <namespace> | grep -A 20 Events
```

You should see `CreatedWorkload` and `CreatedPodGroup` events on the RayCluster.

### Pods stuck in PreEnqueue

If the kube-apiserver's `GenericWorkload` feature gate is enabled but the kube-scheduler's `GangScheduling` feature gate is **not**, the operator will successfully create Workload and PodGroup resources, but pods will remain stuck in the `PreEnqueue` scheduling gate.

The operator's startup check cannot detect this — it only verifies the API is registered on the apiserver, not that the scheduler plugin is active. To diagnose:

```bash
# Check pod events for scheduling gate messages
kubectl describe pod <pod-name> -n <namespace>

# Verify the scheduler has GangScheduling enabled
kubectl get pod kube-scheduler-<node> -n kube-system -o yaml | grep feature-gates
```

Ensure **both** `GenericWorkload` (apiserver) and `GangScheduling` (scheduler) feature gates are enabled.

### Operator fails to start

If you see an error like:

```text
NativeWorkloadScheduling feature gate and batchScheduler configuration are mutually exclusive
```

The operator has both native scheduling and a batch scheduler (Volcano, YuniKorn, etc.) configured. Disable one of them.

If you see:

```text
scheduling.k8s.io/v1alpha2 API is not available
```

Your Kubernetes cluster is either older than 1.36 or the `GenericWorkload` feature gate / `scheduling.k8s.io/v1alpha2` runtime-config is not enabled on the apiserver.

### Warning events

| Event | Meaning |
|-------|---------|
| `WorkloadSchedulingSkipped` | Native scheduling was skipped because autoscaling is enabled or a batch scheduler is configured |
| `WorkloadSchedulingInvalidSpec` | Too many worker groups (>7) |
| `FailedToCreateWorkload` / `FailedToCreatePodGroup` | API error creating scheduling resources |

## Setting Up a Local Test Cluster

To test native workload scheduling locally with [kind](https://kind.sigs.k8s.io/), you need a Kubernetes 1.36+ node image with the required feature gates:

```bash
# Build the kind node image from a K8s 1.36 release
kind build node-image v1.36.0-beta.0

# Create the cluster with the required feature gates
kind create cluster --name native-sched \
  --config ci/kind-config-native-workload-scheduling.yml
```

The [`ci/kind-config-native-workload-scheduling.yml`](../../ci/kind-config-native-workload-scheduling.yml) config enables `GenericWorkload` on the apiserver/controller-manager, `GangScheduling` on the scheduler, and serves the `scheduling.k8s.io/v1alpha2` alpha API.

Then deploy the operator with the feature gate enabled:

```bash
cd ray-operator
make docker-image IMG=kuberay/operator:latest
kind load docker-image kuberay/operator:latest --name native-sched
make deploy IMG=kuberay/operator:latest
# Edit the deployment to add --feature-gates=NativeWorkloadScheduling=true
```
