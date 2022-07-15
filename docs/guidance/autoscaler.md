## Autoscaler (Experimental)

> Note: This feature is still experimental, there're few limitations and stabilization will be done in future release from both Ray and KubeRay side.

### Prerequisite

You have to use nightly operator images because [autoscaler support](https://github.com/ray-project/kuberay/pull/163) is merged recently.
To deploy the nightly RayCluster CRD and KubeRay, run

```
git clone https://github.com/ray-project/kuberay.git
cd kuberay
kubectl create -k manifests/cluster-scope-resources
kubectl apply -k manifests/overlays/autoscaling
```

### Deploy a cluster with autoscaling enabled

Next, to deploy a sample autoscaling Ray cluster, run
```
kubectl apply -f ray-operator/config/samples/ray-cluster.autoscaler.yaml
```

See the above config file for details on autoscaling configuration.

To verify autoscaler is working, **Do not** trust READY container status because autoscaler container has in-built retry and even there's error, it won't crash now.

```
$ kubectl get pods
NAME                                             READY   STATUS    RESTARTS   AGE
raycluster-autoscaler-head-mgwwk                 2/2     Running   0          4m41s
```

The recommended way is to check containers logs. Here's an example of logs from a healthy autoscaler.
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

1. The operator will recognize the following setting and automatically inject a preconfigured autoscaler container to the head pod.
   The service account, role, and role binding needed by the autoscaler will be created by the operator out-of-box.
   The operator will also configure an empty-dir logging volume for the Ray head pod. The volume will be mounted into the Ray and
   autoscaler containers; this is necessary to support the event logging introduced in [Ray PR #13434](https://github.com/ray-project/ray/pull/13434).

    ```
    spec:
      rayVersion: 'nightly'
      enableInTreeAutoscaling: true
    ```

2. The autoscaler image is `rayproject/ray:a304d1` which reflects the latest changes from the Ray master branch.

3. Autoscaling functionality is supported only with Ray versions at least as new as 1.11.0. The autoscaler image used
is compatible with all Ray versions >= 1.11.0.

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
