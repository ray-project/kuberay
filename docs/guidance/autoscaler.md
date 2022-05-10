## Autoscaler (Experimental)

> Note: This feature is still experimental, there're few limitations and stabilization will be done in future release from both Ray and KubeRay side.

### Prerequisite

You have to use nightly operator images because [autoscaler support](https://github.com/ray-project/kuberay/pull/163) is merged recently.
You can follow below steps for a quick deployment.

```
git clone https://github.com/ray-project/kuberay.git
cd kuberay
kubectl apply -k manifests/cluster-scope
```

### Deploy a cluster with autoscaling enabled

```
kubectl apply -f ray-operator/config/samples/ray-cluster.autoscaler.yaml
```

To verify autoscaler is working, **Do not** trust READY container status because autoscaler container has in-built retry and even there's error, it won't crash now.

```
$ kubectl get pods
NAME                                             READY   STATUS    RESTARTS   AGE
raycluster-autoscaler-head-mgwwk                 2/2     Running   0          4m41s
```

The recommended way is to check containers logs. Here's an example of well-behaved autoscaler.
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

#### Known issues and limitations

1. operator will recognize following setting and automatically inject preconfigured autoscaler container to head pod.
   The service account, role, role binding needed by autoscaler will be created by operator out-of-box.

    ```
    spec:
      rayVersion: 'nightly'
      enableInTreeAutoscaling: true
    ```

2. head and work images are `rayproject/ray:413fe0`. This image was built based on [commit](https://github.com/ray-project/ray/commit/413fe08f8744d50b439717564709bc0af2f778f1) from master branch. 
The reason we need to use a nightly version is because autoscaler needs to connect to Ray cluster. Due to ray [version requirements](https://docs.ray.io/en/latest/cluster/ray-client.html#versioning-requirements).
We determine to use nightly version to make sure integration is working.

3. Autoscaler image is `kuberay/autoscaler:nightly` which is built from [commit](https://github.com/ray-project/ray/pull/22689/files).

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

You can see new ray nodes (pods) are joinning the cluster if the default node group doesn't have enough resources.
