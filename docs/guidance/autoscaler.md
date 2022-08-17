## Autoscaler (beta)


### Prerequisite

Ray Autoscaler integration is beta with KubeRay 0.3.0 and Ray 2.0.0.
The details of autoscaler behavior and configuration may change in future releases.

Start by deploying the latest stable version of the KubeRay operator:
```
kubectl create -k "github.com/ray-project/kuberay/manifests/cluster-scope-resources?ref=v0.3.0-rc.1&timeout=90s"
kubectl apply -k "github.com/ray-project/kuberay/manifests/base?ref=v0.3.0-rc.1&timeout=90s"
```

### Deploy a cluster with autoscaling enabled

Next, to deploy a sample autoscaling Ray cluster, run
```
kubectl apply -f https://raw.githubusercontent.com/ray-project/kuberay/release-0.3/ray-operator/config/samples/ray-cluster.autoscaler.yaml
```

See the above config file for details on autoscaling configuration.

The output of `kubectl get pods` should indicate the presence of two containers,
the Ray container and the autoscaler container.


```
$ kubectl get pods
NAME                                             READY   STATUS    RESTARTS   AGE
raycluster-autoscaler-head-mgwwk                 2/2     Running   0          4m41s
```

Check the autoscaler container's logs to confirm that the autoscaler is healthy.
Here's an example of logs from a healthy autoscaler.
```
kubectl logs -f raycluster-autoscaler-head-mgwwk autoscaler

2022-03-10 07:51:22,616	INFO monitor.py:226 -- Starting autoscaler metrics server on port 44217
2022-03-10 07:51:22,621	INFO monitor.py:243 -- Monitor: Started
2022-03-10 07:51:22,824	INFO node_provider.py:143 -- Creating KuberayNodeProvider.
2022-03-10 07:51:22,825	INFO autoscaler.py:282 -- StandardAutoscaler: {'provider': {'type': 'kuberay', 'namespace': 'default', 'disable_node_updaters': True, 'disable_launch_config_check': True}, 'cluster_name': 'raycluster-autoscaler', 'head_node_type': 'head-group', 'available_node_types': {'head-group': {'min_workers': 0, 'max_workers': 0, 'node_config': {}, 'resources': {'CPU': 1}}, 'small-group': {'min_workers': 1, 'max_workers': 300, 'node_config': {}, 'resources': {'CPU': 1}}}, 'max_workers': 300, 'idle_timeout_minutes': 5, 'upscaling_speed': 1, 'file_mounts': {}, 'cluster_synced_files': [], 'file_mounts_sync_continuously': False, 'initialization_commands': [], 'setup_commands': [], 'head_setup_commands': [], 'worker_setup_commands': [], 'head_start_ray_commands': [], 'worker_start_ray_commands': [], 'auth': {}, 'head_node': {}, 'worker_nodes': {}}
2022-03-10 07:51:23,027	INFO autoscaler.py:327 --
======== Autoscaler status: 2022-03-10 07:51:23.027271 ========
Node status
---------------------------------------------------------------
Healthy:
 1 head-group
Pending:
 (no pending nodes)
Recent failures:
 (no failures)

Resources
---------------------------------------------------------------
Usage:
 0.0/1.0 CPU
 0.00/0.931 GiB memory
 0.00/0.200 GiB object_store_memory

Demands:
 (no resource demands)
```

#### Notes

1. To enable autoscaling, set your RayCluster CR's `spec.enableInTreeAutoscaling` field to true.
   The operator will then automatically inject a preconfigured autoscaler container to the head pod.
   The service account, role, and role binding needed by the autoscaler will be created by the operator out-of-box.
   The operator will also configure an empty-dir logging volume for the Ray head pod. The volume will be mounted into the Ray and
   autoscaler containers; this is necessary to support the event logging introduced in [Ray PR #13434](https://github.com/ray-project/ray/pull/13434).

    ```
    spec:
      enableInTreeAutoscaling: true
    ```

2. If your RayCluster CR's `spec.rayVersion` field is at least `2.0.0`, the autoscaler container will use the same image as the Ray container.
   For Ray versions older than 2.0.0, the image `rayproject/ray:2.0.0` will be used to run the autoscaler.

3. Autoscaling functionality is supported only with Ray versions at least as new as 1.11.0. Autoscaler support
   is beta as of Ray 2.0.0 and KubeRay 0.3.0. The details of autoscaler behavior and configuration may change in future releases.

### Test autoscaling

Let's now try out the autoscaler. We can run the following command to get a Python interpreter in the head pod:

```
kubectl exec `kubectl get pods -o custom-columns=POD:metadata.name | grep raycluster-autoscaler-head` -it -c ray-head -- python
```

In the Python interpreter, run the following snippet to scale up the cluster:

```
import ray.autoscaler.sdk
ray.init("auto")
ray.autoscaler.sdk.request_resources(num_cpus=4)
```

You should then see two extra Ray nodes (pods) scale up to satisfy the 4 CPU demand.
