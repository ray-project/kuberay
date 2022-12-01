# KubeRay integration with Volcano

[Volcano](https://github.com/volcano-sh/volcano) is a batch scheduling system built on Kubernetes. It provides a suite of mechanisms (gang scheduling, job queues, fair scheduling policies) currently missing from Kubernetes that are commonly required by many classes of batch and elastic workloads. KubeRay's Volcano integration enables more efficient scheduling of Ray pods in multi-tenant Kubernetes environments.

Note that this is a new feature. Feedback and contributions welcome.

## Setup

### Install Volcano

Volcano needs to be successfully installed in your Kubernetes cluster before enabling Volcano integration with KubeRay. Refer to the [Quick Start Guide](https://github.com/volcano-sh/volcano#quick-start-guide) for Volcano installation instructions.

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
  rayVersion: '2.1.0'
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
              memory: "2G"
            requests:
              cpu: "1"
              memory: "2G"
  workerGroupSpecs: []
```

The following labels can also be provided in the RayCluster metadata:

- `ray.io/priority-class-name`: the cluster priority class as defined by Kubernetes [here](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/#priorityclass).
- `volcano.sh/queue-name`: the Volcano [queue](https://volcano.sh/en/docs/queue/) name the cluster will be submitted to.

If autoscaling is enabled, `minReplicas` will be used for gang scheduling, otherwise the desired `replicas` will be used.

### Example: Gang scheduling

In this example, we'll walk through how gang scheduling works with Volcano and KubeRay.

First, let's create a queue with a capacity of 4 CPUs and 4Gi or RAM:

```
kubectl create -f - <<EOF
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: ray-test
spec:
  weight: 1
  capability:
    cpu: 2
    memory: 4Gi
EOF
```

Next we'll create a RayCluster with a head node and two workers, each requesting 1 CPU and 1Gi of RAM each, for a total of 3 CPU and 3Gi or RAM:

```
kubectl create -f - <<EOF
apiVersion: ray.io/v1alpha1
kind: RayCluster
metadata:
  name: test-cluster
  labels:
    ray.io/scheduler-name: volcano
    volcano.sh/queue-name: ray-test
spec:
  rayVersion: '2.1.0'
  headGroupSpec:
    serviceType: ClusterIP
    rayStartParams:
      block: 'true'
    replicas: 1
    template:
      spec:
        containers:
        - name: ray-head
          image: rayproject/ray:2.1.0
          resources:
            limits:
              cpu: "1"
              memory: "1Gi"
            requests:
              cpu: "1"
              memory: "1Gi"
  workerGroupSpecs:
    - groupName: worker
      rayStartParams:
        block: 'true'
      replicas: 2
      minReplicas: 2
      maxReplicas: 2
      template:
        spec:
          containers:
          - name: ray-head
            image: rayproject/ray:2.1.0
            resources:
              limits:
                cpu: "1"
                memory: "1Gi"
              requests:
                cpu: "1"
                memory: "1Gi"
EOF
```

Because our queue has a capacity of 4 CPU and 4Gi of RAM, this resource should schedule successfully without any issues. We can verify this by checking the status of our cluster's Volcano PodGroup:

```
```

And checking the status of our queue:

```
```

Next we'll add an additional RayCluster with the same configuration of head / worker nodes, but a different name:

```
```

Because our new cluster requires more CPU and RAM than our queue will allow, even though we could fit one of the pods with the remaining 1 CPU and 1Gi of RAM, none of the cluster's pods will be placed until there is enough room for all the pods. Without using Volcano for gang scheduling in this way, one of the pods would ordinarily be placed, leading to the cluster being partially allocated, and some jobs (like [Horovod](https://github.com/horovod/horovod) training) getting stuck waiting for resources to become available.

Let's go ahead and delete the first RayCluster:

```
```

Now when we check the status of the second cluster, we see that its pods have been placed successfully, as the queue now has enough available capacity for the second cluster.

Finally, we'll cleanup the remaining cluster and queue:

```
```

## Questions

Reach out to @tgaddair for questions regarding usage of this integration.
