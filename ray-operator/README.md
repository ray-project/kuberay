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

An example ray code is defined in this [configmap](msft-operator/ray-operator/config/samples/config-map-ray-code.yaml) that is mounted into the ray head-pod. By examining the logs of the head pod, we can see the list of the IP addresses of the nodes that joined the ray cluster:

![](media/logs-ray-cluster.gif)


### Deploy the operator

```shell
kubectl apply -k "github.com/ray-project/kuberay/ray-operator/config/default"
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

There are three example config files to deploy RayClusters included here:

Sample  | Description
------------- | -------------
[ray-cluster.mini.yaml](config/samples/ray-cluster.mini.yaml)   | Small example consisting of 1 head pod.
[ray-cluster.heterogeneous.yaml](config/samples/ray-cluster.heterogeneous.yaml)  | Example with heterogenous worker types. 1 head pod and 2 worker pods, each of which has a different resource quota.
[ray-cluster.complete.yaml](config/samples/ray-cluster.complete.yaml)  | Shows all available custom resouce properties.

```shell
# Create a configmap with a hello world Ray code.
kubectl create -f config/samples/config-map-ray-code.yaml
configmap/ray-code created
```


```shell
# Create a cluster.
$ kubectl create -f config/samples/ray-cluster.heterogeneous.yaml
ray.ray.io/ray-heterogeneous created

# List running clusters.
$ kubectl get rayclusters
NAME                AGE
ray-heterogeneous   2m48s

# The created cluster should include a head pod, worker pod, and a head service.
$ kubectl get pods
NAME                                                 READY   STATUS    RESTARTS   AGE
ray-heterogeneous-head-5r6qr                  1/1     Running   0          14m
ray-heterogeneous-worker-medium-group-ljzzt   1/1     Running   0          14m
ray-heterogeneous-worker-small-group-76qxb    1/1     Running   0          14m
ray-heterogeneous-worker-small-group-dcl4d    1/1     Running   0          14m
```

```shell
$ kubectl get services
NAME                     TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)    AGE
kubernetes                        ClusterIP   10.152.183.1   <none>        443/TCP   35d
ray-heterogeneous-my-svc   ClusterIP   None           <none>        80/TCP    15m
```

The logs of the head pod should show 4 nodes in the Ray cluster
```
Ray Nodes:  {'10.1.73.139', '10.1.73.138', '10.1.73.140', '10.1.73.141'}
```

```shell
# check the logs of the head pod
$ kubectl logs ray-heterogeneous-head-5r6qr
2020-11-18 09:23:32,069 INFO services.py:1092 -- View the Ray dashboard at http://10.1.73.141:8265
2020-11-18 09:23:31,668 INFO scripts.py:467 -- Local node IP: 10.1.73.141
2020-11-18 09:23:32,093 SUCC scripts.py:497 -- --------------------
2020-11-18 09:23:32,093 SUCC scripts.py:498 -- Ray runtime started.
2020-11-18 09:23:32,093 SUCC scripts.py:499 -- --------------------
2020-11-18 09:23:32,093 INFO scripts.py:501 -- Next steps
2020-11-18 09:23:32,093 INFO scripts.py:503 -- To connect to this Ray runtime from another node, run
2020-11-18 09:23:32,093 INFO scripts.py:507 --   ray start --address='10.1.73.141:6379' --redis-password='LetMeInRay'
2020-11-18 09:23:32,093 INFO scripts.py:509 -- Alternatively, use the following Python code:
2020-11-18 09:23:32,093 INFO scripts.py:512 -- import ray
2020-11-18 09:23:32,093 INFO scripts.py:519 -- ray.init(address='auto', _redis_password='LetMeInRay')
2020-11-18 09:23:32,093 INFO scripts.py:522 -- If connection fails, check your firewall settings and network configuration.
2020-11-18 09:23:32,093 INFO scripts.py:526 -- To terminate the Ray runtime, run
2020-11-18 09:23:32,094 INFO scripts.py:527 --   ray stop
2020-11-18 09:23:32,656 INFO worker.py:651 -- Connecting to existing Ray cluster at address: 10.1.73.141:6379
2020-11-18 09:23:32,669 WARNING services.py:202 -- Some processes that the driver needs to connect to have not registered with Redis, so retrying. Have you run 'ray start' on this node?
trying to connect to Ray!
now executing some code with Ray!
Ray Nodes:  {'10.1.73.139', '10.1.73.138', '10.1.73.140', '10.1.73.141'}
Execution time =  6.961702346801758
```

```
# Delete the cluster.
$ kubectl delete raycluster raycluster-heterogeneous
```
