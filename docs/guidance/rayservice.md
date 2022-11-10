## Ray Services (alpha)

> Note: This is the alpha version of Ray Services. There will be ongoing improvements for Ray Services in the future releases.

### Prerequisite

* Ray 2.0 is required.

### What is a RayService?

RayService is a new custom resource (CR) supported by KubeRay in v0.3.0.

A RayService manages 2 things:

* **Ray Cluster**: Manages resources in a Kubernetes cluster.
* **Ray Serve Deployment Graph**: Manages users' deployment graphs.

### What does the RayService provide?

* **Kubernetes-native support for Ray clusters and Ray Serve deployment graphs.** After using a Kubernetes config to define a Ray cluster and its Ray Serve deployment graphs, you can use `kubectl` to create the cluster and its graphs.
* **In-place update for Ray Serve deployment graph.** Users can update the Ray Serve deployment graph config in the RayService CR config and use `kubectl apply` to update the deployment graph.
* **Zero downtime upgrade for Ray clusters.** Users can update the Ray cluster config in the RayService CR config and use `kubectl apply` to update the cluster. RayService will temporarily create a pending cluster and wait for it to be ready, then switch traffic to the new cluster and terminate the old one.
* **Services HA.** RayService will monitor the Ray cluster and Serve deployments' health statuses. If RayService detects an unhealthy status for a period of time, RayService will try to create a new Ray cluster and switch traffic to the new cluster when it is ready.

### Deploy the Operator

```
$ kubectl create -k "github.com/ray-project/kuberay/ray-operator/config/default?ref=v0.3.0&timeout=90s"
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

### Run an Example Cluster

An example config file to deploy RayService is included here:
[ray_v1alpha1_rayservice.yaml](https://github.com/ray-project/kuberay/blob/master/ray-operator/config/samples/ray_v1alpha1_rayservice.yaml)

```shell
# Create a ray service and deploy fruit deployment graph.
$ kubectl apply -f config/samples/ray_v1alpha1_rayservice.yaml
```

```shell
# List running RayServices.
$ kubectl get rayservice
NAME                AGE
rayservice-sample   7s
```

```shell
# The created RayService should include a head pod, a worker pod, and four services.
$ kubectl get pods
NAME                                                      READY   STATUS    RESTARTS   AGE
ervice-sample-raycluster-qd2vl-worker-small-group-bxpp6   1/1     Running   0          24m
rayservice-sample-raycluster-qd2vl-head-45hj4             1/1     Running   0          24m

$ kubectl get services
NAME                                               TYPE        CLUSTER-IP       EXTERNAL-IP   PORT(S)                                          AGE
kubernetes                                         ClusterIP   10.100.0.1       <none>        443/TCP                                          62d
# A head node service maintained by the RayService.
rayservice-sample-head-svc                         ClusterIP   10.100.34.24     <none>        6379/TCP,8265/TCP,10001/TCP,8000/TCP,52365/TCP   24m
# A dashboard agent service maintained by the RayCluster.
rayservice-sample-raycluster-qd2vl-dashboard-svc   ClusterIP   10.100.109.177   <none>        52365/TCP                                        24m
# A head node service maintained by the RayCluster.
rayservice-sample-raycluster-qd2vl-head-svc        ClusterIP   10.100.180.221   <none>        6379/TCP,8265/TCP,10001/TCP,8000/TCP,52365/TCP   24m
# A serve service maintained by the RayService.
rayservice-sample-serve-svc                        ClusterIP   10.100.39.92     <none>        8000/TCP                                         24m
```

> Note: Default ports and their definitions. 

| Port  | Definition          |
|-------|---------------------|
| 6379  | Ray GCS             |
| 8265  | Ray Dashboard       |
| 10001 | Ray Client          |
| 8000  | Ray Serve           |
| 52365 | Ray Dashboard Agent |

Get information about the RayService using its name.
```shell
$ kubectl describe rayservices rayservice-sample
```

### Access User Services

The users' traffic can go through the `serve` service (e.g. `rayservice-sample-serve-svc`).

#### Run a Curl Pod

```shell
$ kubectl run curl --image=radial/busyboxplus:curl -i --tty
```

Or if you already have a curl pod running, you can login using `kubectl exec -it curl sh`.

For the fruit example deployment, you can try the following request:
```shell
[ root@curl:/ ]$ curl -X POST -H 'Content-Type: application/json' rayservice-sample-serve-svc.default.svc.cluster.local:8000 -d '["MANGO", 2]'
> 6
```
You should get the response `6`.

#### Use Port Forwarding
Set up Kubernetes port forwarding.
```shell
$ kubectl port-forward service/rayservice-sample-serve-svc 8000
```
For the fruit example deployment, you can try the following request:
```shell
[ root@curl:/ ]$ curl -X POST -H 'Content-Type: application/json' localhost:8000 -d '["MANGO", 2]'
> 6
```
> Note:
> `serve-svc` is HA in general. It will do traffic routing among all the workers which have serve deployments and will always try to point to the healthy cluster, even during upgrading or failing cases. 
> You can set `serviceUnhealthySecondThreshold` to define the threshold of seconds that the serve deployments fail. You can also set `deploymentUnhealthySecondThreshold` to define the threshold of seconds that Ray fails to deploy any serve deployments.

### Access Ray Dashboard
Set up Kubernetes port forwarding for the dashboard.
```shell
$ kubectl port-forward service/rayservice-sample-head-svc 8265
```
Access the dashboard using a web browser at `localhost:8265`.

### Update Ray Serve Deployment Graph

You can update the `serveConfig` in your RayService config file.
For example, update the price of mangos to `4` in [ray_v1alpha1_rayservice.yaml](https://github.com/ray-project/kuberay/blob/master/ray-operator/config/samples/ray_v1alpha1_rayservice.yaml):
```shell
- name: MangoStand
  numReplicas: 1
  userConfig: |
  price: 4
```

Use `kubectl apply` to update your RayService and `kubectl describe  rayservices rayservice-sample` to take a look at the RayService's information. It should look similar to:
```shell
serveDeploymentStatuses:
- healthLastUpdateTime: "2022-07-18T21:51:37Z"
  lastUpdateTime: "2022-07-18T21:51:41Z"
  name: MangoStand
  status: UPDATING
```

After it finishes deployment, let's send a request again. In the curl pod from earlier, run:
```shell
[ root@curl:/ ]$ curl -X POST -H 'Content-Type: application/json' rayservice-sample-serve-svc.default.svc.cluster.local:8000 -d '["MANGO", 2]'
> 8
```
Or if using port forwarding:
```shell
curl -X POST -H 'Content-Type: application/json' localhost:8000 -d '["MANGO", 2]'
> 8
```
You should now get `8` as a result.

### Upgrade RayService RayCluster Config
You can update the `rayClusterConfig` in your RayService config file.
For example, you can increase the number of workers to 2:
```shell
workerGroupSpecs:
  # the pod replicas in this group typed worker
  - replicas: 2
```

Use `kubectl apply` to update your RayService and `kubectl describe  rayservices rayservice-sample` to take a look at the RayService's information. It should look similar to:
```shell
  pendingServiceStatus:
    appStatus: {}
    dashboardStatus:
      healthLastUpdateTime: "2022-07-18T21:54:53Z"
      lastUpdateTime: "2022-07-18T21:54:54Z"
    rayClusterName: rayservice-sample-raycluster-bshfr
    rayClusterStatus: {}
```
You can see the RayService is preparing a pending cluster. Once the pending cluster is healthy, the RayService will make it the active cluster and terminate the previous one.

### RayService Observability
You can use `kubectl logs` to check the operator logs or the head/worker nodes logs.
You can also use `kubectl describe rayservices rayservice-sample` to check the states and event logs of your RayService instance.

For Ray Serve monitoring, you can refer to the [Ray observability documentation](https://docs.ray.io/en/master/ray-observability/state/state-api.html).
To run Ray state APIs, log in to the head pod by running `kubectl exec -it <head-node-pod> bash` and use the Ray CLI or you can run commands locally using `kubectl exec -it <head-node-pod> -- <ray state api>`.

For example, `kubectl exec -it <head-node-pod> -- ray summary tasks` outputs the following:
```shell
======== Tasks Summary: 2022-07-28 15:10:24.801670 ========
Stats:
------------------------------------
total_actor_scheduled: 17
total_actor_tasks: 5
total_tasks: 0


Table (group by func_name):
------------------------------------
    FUNC_OR_CLASS_NAME                 STATE_COUNTS    TYPE
0   ServeController.listen_for_change  RUNNING: 5      ACTOR_TASK
1   ServeReplica:MangoStand.__init__   FINISHED: 3     ACTOR_CREATION_TASK
2   HTTPProxyActor.__init__            FINISHED: 2     ACTOR_CREATION_TASK
3   ServeReplica:PearStand.__init__    FINISHED: 3     ACTOR_CREATION_TASK
4   ServeReplica:OrangeStand.__init__  FINISHED: 3     ACTOR_CREATION_TASK
5   ServeReplica:FruitMarket.__init__  FINISHED: 3     ACTOR_CREATION_TASK
6   ServeReplica:DAGDriver.__init__    FINISHED: 2     ACTOR_CREATION_TASK
7   ServeController.__init__           FINISHED: 1     ACTOR_CREATION_TASK
```

### Delete the RayService Instance
```
$ kubectl delete -f config/samples/ray_v1alpha1_rayservice.yaml
```

### Delete the Operator

```
$ kubectl delete -k "github.com/ray-project/kuberay/ray-operator/config/default"
```
