## Ray GCS HA (Experimental)

> Note: This feature is still experimental, there're few limitations and stabilization will be done in future release from both Ray and KubeRay side.

### Prerequisite

* Ray 2.0 is required.
* You need to support external Redis server for Ray. (Redis HA cluster is highly recommended.)

### Enable Ray GCS HA

To enable Ray GCS HA in your newly KubeRay-managed Ray cluster, you need to enable it by add a label to the
RayCluster YAML file.

```yaml
...
kind: RayCluster
metadata:
  labels:
    controller-tools.k8s.io: "1.0"
    ray.io/ha-enabled: "true" # <- add this label enable GCS HA
...
```
An example can be found at [ray-cluster.external-redis.yaml](../../ray-operator/config/samples/ray-cluster.external-redis.yaml)

When label `ray.io/ha-enabled` is added with a `true` value, KubeRay will enable Ray GCS HA feature. This feature
contains several components:

1. Newly created Ray cluster has `Readiness Probe` and `Liveness Probe` added to all the head/worker nodes.
2. KubeRay Operator controller watches for `Event` object changes which can notify incase of readiness probe failures and mark them as `Unhealthy`.
3. KubeRay Operator controller kills and recreate any `Unhealthy` Ray head/worker node.

### Implementation Details

#### Readiness Probe vs Liveness Probe

These are the two types of probes we used in Ray GCS HA. 

The readiness probe is used to notify KubeRay in case of failures in the corresponding Ray cluster. KubeRay can try its best to
recover the Ray cluster. If KubeRay cannot recover the failed head/worker node, the liveness probe gets in, delete the old pod
and create a new pod.

By default, the liveness probe gets involved later than the readiness probe. The liveness probe is our last resort to recover the 
Ray cluster. However, in our current implementation, for the readiness probe failures, we also kill & recreate the corresponding pod that runs head/worker node.

Currently, the readiness probe and the liveness probe are using the same command to do the work. In the future, we may run
 different commands for the readiness probe and the liveness probe.

On Ray head node, we access a local Ray dashboard http endpoint and a Raylet http endpoint to make sure this head node is in
healthy state. Since Ray dashboard does not reside Ray worker node, we only check the local Raylet http endpoint to make sure
the worker node is healthy.

#### Ray GCS HA Label

Our Ray GCS HA feature checks if a label called `ray.io/ha-enabled` is set to `true` in `RayCluster` YAML file. If so, KubeRay
will also add such label to the pod whenever the head/worker node is created.

#### Use External Redis Cluster

To use external Redis cluster as the backend storage(required by Ray GCS HA),
you need to add `RAY_REDIS_ADDRESS` environment variable to the head node template.

An example can be found at [ray-cluster.external-redis.yaml](../../ray-operator/config/samples/ray-cluster.external-redis.yaml)

#### KubeRay Operator Controller

KubeRay Operator controller watches for new `Event` reconcile call. If this Event object is to notify the failed readiness probe,
controller checks if this pod has `ray.io/ha-enabled` set to `true`. If this pod has this label set to true, that means this pod
belongs to a Ray cluster that has Ray GCS HA enabled.

After this, the controller will try to recover the failed pod. If controller cannot recover it, a label named 
`ray.io/health-state` with a value `Unhealthy` is added to this pod.

In every KubeRay Operator controller reconcile loop, it monitors any pod in Ray cluster that has `Unhealthy` value in label
`ray.io/health-state`. If any pod is found, this pod is deleted and gets recreated.

#### Known issues and limitations

1. Currently, Python redis package is removed from Ray official image. We need to install Python redis package in the very begining of the container start. (This is done by KubeRay by adding commands to Ray container spec.)
2. For now, Ray head/worker node that fails the readiness probe recovers itself by restarting itself. More fine-grained control and recovery mechanisms are expected in the future.

### Test Ray GCS HA

Currently, two tests are responsible for ensuring Ray GCS HA is working correctly.

1. Detached actor test
2. RayServe test

In detached actor test, a detached actor is created at first. Then, the head node is killed. KubeRay brings back another
head node replacement pod. However, the detached actor is still expected to be available. (Note: the client that creates
the detached actor does not exist and will retry in case of Ray cluster returns failure)

In RayServe test, a simple RayServe app is deployed on the Ray cluster. In case of GCS server crash, the RayServe app
continues to be accessible after the head node recovery.
