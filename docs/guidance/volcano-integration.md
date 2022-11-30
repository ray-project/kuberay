# KubeRay integration with Volcano

[Volcano](https://github.com/volcano-sh/volcano) is a batch scheduling system built on Kubernetes. It provides a suite of mechanisms (gang scheduling, job queues, fair scheduling policies) currently missing from Kubernetes that are commonly required by many classes of batch and elastic workloads. KubeRay's Volcano integration enables more efficient scheduling of Ray pods in multi-tenant Kubernetes environments.

## Setup

### Install Volcano

Volcano needs to be successfully installed in your Kubernetes cluster before enabling Volcano integration with KubeRay. Refer to the [Quick Start Guide](https://github.com/volcano-sh/volcano#quick-start-guide)  for Volcano installation instructions.

### Install KubeRay Operator with Batch Scheduling

Deploy the KubeRay Operator with the `--enable-batch-scheduler` flag to enable Volcano batch scheduling support.

When installing via Helm, you can set the following in your `values.yaml` file:

```
batchScheduler:
    enabled: true
```

Or on the command line:

```
# helm install kuberay-operator --set batchScheduler.enabled=true
```

## Run Ray Cluster with Volcano scheduler

Add the `ray.io/scheduler-name: volcano` label to your RayCluster CR to submit the cluster pods to Volcano for scheduling.

Example:

```
apiVersion: ray.io/v1alpha1
kind: RayCluster
metadata:
  name: test-cluster
  labels:
    ray.io/scheduler-name: volcano
    volcano.sh/queue-name: test
spec:
  rayVersion: '2.0.0'
  headGroupSpec:
    serviceType: ClusterIP
    replicas: 1
    template:
      spec:
        containers:
        - name: ray-head
          image: rayproject/ray:2.1.0
          resources:
            limits:
              cpu: "1"
              memory: "1G"
            requests:
              cpu: "1"
              memory: "2G"
  workerGroupSpecs: []
```

The following labels can also be provided in the RayCluster metadata:

- `ray.io/priority-class-name`: the cluster priority class as defined by Kubernetes [here](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/#priorityclass).
- `volcano.sh/queue-name`: the Volcano [queue](https://volcano.sh/en/docs/queue/) name the cluster will be submitted to.

If autoscaling is enabled, `minReplicas` will be used for gang scheduling, otherwise the desired `replicas` will be used.
