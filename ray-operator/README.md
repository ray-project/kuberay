# Ray Kubernetes Operator

KubeRay operator makes deploying and managing Ray clusters on top of Kubernetes painless - clusters are defined as a custom RayCluster resource and managed by a fault-tolerant Ray controller.
The Ray Operator is a Kubernetes operator to automate provisioning, management, autoscaling and operations of Ray clusters deployed to Kubernetes.

![overview](media/overview.png)

Some of the main features of the operator are:
- Management of first-class RayClusters via a [custom resource](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/#custom-resources).
- Support for heterogenous worker types in a single Ray cluster.
- Built-in monitoring via Prometheus.
- Use of `PodTemplate` to create Ray pods
- Updated status based on the running pods
- Events added to the `RayCluster` instance
- Automatically populate `environment variables` in the containers
- Automatically prefix your container command with the `ray start` command
- Automatically adding the volumeMount at `/dev/shm` for shared memory
- Use of `ScaleStartegy` to remove specific nodes in specific groups

## Overview

When deployed, the ray operator will watch for K8s events (create/delete/update) for the `raycluster` resources. The ray operator can create a raycluster (head + multipe workers), delete a cluster, or update the cluster by adding or removing worker pods.

### Ray cluster creation

Once a `raycluster` resource is created, the operator will configure and create the ray-head and the ray-workers specified in the `raycluster` manifest as shown below.

![](media/create-ray-cluster.gif)

### Ray cluster Update

You can update the number of replicas in a worker goup, and specify which exact replica to remove by updated the raycluster resource manifest:

![](media/update-ray-cluster.gif)

### Ray cluster example code

An example ray code is defined in this [configmap](config/samples/config-map-ray-code.yaml) that is mounted into the ray head-pod. By examining the logs of the head pod, we can see the list of the IP addresses of the nodes that joined the ray cluster:

![](media/logs-ray-cluster.gif)


### Deploy the operator

```shell
kubectl create -k "github.com/ray-project/kuberay/ray-operator/config/default?ref=v0.3.0&timeout=90s"
```

Check that the controller is running.
```shell
$ kubectl get deployments -n ray-system
NAME           READY   UP-TO-DATE   AVAILABLE   AGE
ray-operator   1/1     1            1           40s

$ kubectl get pods -n ray-system
NAME                            READY   STATUS    RESTARTS   AGE
ray-operator-75dbbf8587-5lrvn   1/1     Running   0          31s
```

Delete the operator
```shell
kubectl delete -k "github.com/ray-project/kuberay/ray-operator/config/default"
```

### Running an example cluster

We include a few example config files to deploy RayClusters:

Sample  | Description
------------- | -------------
[ray-cluster.mini.yaml](config/samples/ray-cluster.mini.yaml)   | Small example consisting of 1 head pod.
[ray-cluster.heterogeneous.yaml](config/samples/ray-cluster.heterogeneous.yaml)  | Example with heterogenous worker types. 1 head pod and 2 worker pods, each of which has a different resource quota.
[ray-cluster.complete.yaml](config/samples/ray-cluster.complete.yaml)  | Shows all available custom resource properties.
[ray-cluster.autoscaler.yaml](config/samples/ray-cluster.autoscaler.yaml)  | Shows all available custom resource properties and demonstrates autoscaling.
[ray-cluster.complete.large.yaml](config/samples/ray-cluster.complete.large.yaml)  | Demonstrates resource configuration for production use-cases.
[ray-cluster.autoscaler.large.yaml](config/samples/ray-cluster.autoscaler.yaml)  | Demonstrates resource configuration for autoscaling Ray clusters in production.

!!! note

    For production use-cases, make sure to allocate sufficient resources for your Ray pods; it usually makes
    sense to run one large Ray pod per Kubernetes node.
    See [ray-cluster.complete.large.yaml](config/samples/ray-cluster.complete.large.yaml) and
    [ray-cluster.autoscaler.large.yaml](config/samples/ray-cluster.autoscaler.yaml) for guidance. The rest of the sample configs above are geared towards
    experimentation in local kind or minikube environments.

```shell
# Create a RayCluster and a ConfigMap with hello world Ray code.
$ kubectl create -f config/samples/ray-cluster.heterogeneous.yaml
configmap/ray-code created
raycluster.ray.io/raycluster-heterogeneous created

# List running clusters.
$ kubectl get rayclusters
NAME                AGE
raycluster-heterogeneous   2m48s

# The created cluster should include a head pod, worker pod, and a head service.
$ kubectl get pods
NAME                                                 READY   STATUS    RESTARTS   AGE
raycluster-heterogeneous-head-9t28q                  1/1     Running   0          97s
raycluster-heterogeneous-worker-medium-group-l9x9n   1/1     Running   0          97s
raycluster-heterogeneous-worker-small-group-hldxz    1/1     Running   0          97s
raycluster-heterogeneous-worker-small-group-tmgtq    1/1     Running   0          97s
raycluster-heterogeneous-worker-small-group-zc5dh    1/1     Running   0          97s
```

```shell
$ kubectl get services
NAME                                TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)                       AGE
kubernetes                          ClusterIP   10.96.0.1      <none>        443/TCP                       22h
raycluster-heterogeneous-head-svc   ClusterIP   10.96.47.129   <none>        6379/TCP,8265/TCP,10001/TCP   2m18s
```

```shell
# check the logs of the head pod
$ kubectl logs raycluster-heterogeneous-head-5r6qr
2022-09-21 13:21:57,505	INFO usage_lib.py:479 -- Usage stats collection is enabled by default without user confirmation because this terminal is detected to be non-interactive. To disable this, add `--disable-usage-stats` to the command that starts the cluster, or run the following command: `ray disable-usage-stats` before starting the cluster. See https://docs.ray.io/en/master/cluster/usage-stats.html for more details.
2022-09-21 13:21:57,505	INFO scripts.py:719 -- Local node IP: 10.244.0.144
2022-09-21 13:22:00,513	SUCC scripts.py:756 -- --------------------
2022-09-21 13:22:00,514	SUCC scripts.py:757 -- Ray runtime started.
2022-09-21 13:22:00,514	SUCC scripts.py:758 -- --------------------
2022-09-21 13:22:00,514	INFO scripts.py:760 -- Next steps
2022-09-21 13:22:00,514	INFO scripts.py:761 -- To connect to this Ray runtime from another node, run
2022-09-21 13:22:00,514	INFO scripts.py:766 --   ray start --address='10.244.0.144:6379'
2022-09-21 13:22:00,514	INFO scripts.py:780 -- Alternatively, use the following Python code:
2022-09-21 13:22:00,514	INFO scripts.py:782 -- import ray
2022-09-21 13:22:00,514	INFO scripts.py:795 -- ray.init(address='auto', _node_ip_address='10.244.0.144')
2022-09-21 13:22:00,515	INFO scripts.py:799 -- To connect to this Ray runtime from outside of the cluster, for example to
2022-09-21 13:22:00,515	INFO scripts.py:803 -- connect to a remote cluster from your laptop directly, use the following
2022-09-21 13:22:00,515	INFO scripts.py:806 -- Python code:
2022-09-21 13:22:00,515	INFO scripts.py:808 -- import ray
2022-09-21 13:22:00,515	INFO scripts.py:814 -- ray.init(address='ray://<head_node_ip_address>:10001')
2022-09-21 13:22:00,515	INFO scripts.py:820 -- If connection fails, check your firewall settings and network configuration.
2022-09-21 13:22:00,515	INFO scripts.py:826 -- To terminate the Ray runtime, run
2022-09-21 13:22:00,515	INFO scripts.py:827 --   ray stop
2022-09-21 13:22:00,515	INFO scripts.py:905 -- --block
2022-09-21 13:22:00,515	INFO scripts.py:907 -- This command will now block forever until terminated by a signal.
2022-09-21 13:22:00,515	INFO scripts.py:910 -- Running subprocesses are monitored and a message will be printed if any of them terminate unexpectedly. Subprocesses exit with SIGTERM will be treated as graceful, thus NOT reported.
```

Execute hello world Ray code
```shell
$ kubectl exec raycluster-heterogeneous-head-9t28q -- python /opt/sample_code.py
2022-09-21 13:28:41,176	INFO worker.py:1224 -- Using address 127.0.0.1:6379 set in the environment variable RAY_ADDRESS
2022-09-21 13:28:41,176	INFO worker.py:1333 -- Connecting to existing Ray cluster at address: 10.244.0.144:6379...
2022-09-21 13:28:41,183	INFO worker.py:1515 -- Connected to Ray cluster. View the dashboard at http://10.244.0.144:8265 
trying to connect to Ray!
now executing some code with Ray!
Ray Nodes:  {'10.244.0.145', '10.244.0.143', '10.244.0.146', '10.244.0.144', '10.244.0.147'}
Execution time =  4.855740308761597
```

The output of hello world Ray code show 5 nodes in the Ray cluster
```
Ray Nodes:  {'10.244.0.145', '10.244.0.143', '10.244.0.146', '10.244.0.144', '10.244.0.147'}
```

```
# Delete the cluster.
$ kubectl delete raycluster raycluster-heterogeneous
```
