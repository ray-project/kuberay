# Ray Services (alpha)

> Note: This is the alpha version of Ray Services. There will be ongoing improvements for Ray Services in the future releases.

# Prerequisites

This guide focuses solely on the Ray Serve multi-application API, which is available starting from Ray version 2.4.0.

* Ray 2.4.0 or newer.
* KubeRay 0.6.0 or newer.

# What is a RayService?

A RayService manages 2 things:

* **RayCluster**: Manages resources in a Kubernetes cluster.
* **Ray Serve Applications**: Manages users' applications.

# What does the RayService provide?

* **Kubernetes-native support for Ray clusters and Ray Serve applications:** After using a Kubernetes config to define a Ray cluster and its Ray Serve applications, you can use `kubectl` to create the cluster and its applications.
* **In-place update for Ray Serve applications:** Users can update the Ray Serve config in the RayService CR config and use `kubectl apply` to update the applications.
* **Zero downtime upgrade for Ray clusters:** Users can update the Ray cluster config in the RayService CR config and use `kubectl apply` to update the cluster. RayService will temporarily create a pending cluster and wait for it to be ready, then switch traffic to the new cluster and terminate the old one.
* **Services HA:** RayService will monitor the Ray cluster and Serve deployments' health statuses. If RayService detects an unhealthy status for a period of time, RayService will try to create a new Ray cluster and switch traffic to the new cluster when it is ready.

# Example: Serve two simple Ray Serve applications using RayService

## Step 1: Create a Kubernetes cluster with Kind.

```sh
kind create cluster --image=kindest/node:v1.23.0
```

## Step 2: Install KubeRay operator

Follow [this document](https://github.com/ray-project/kuberay/blob/master/helm-chart/kuberay-operator/README.md) to install the nightly KubeRay operator via Helm.
Note that sample RayService in this guide uses `serveConfigV2` to specify a multi-application Serve config.
This will be first supported in Kuberay 0.6.0, and is currently supported only on the nightly KubeRay operator.

## Step 3: Install a RayService

```sh
# path: ray-operator/config/samples/
kubectl apply -f ray_v1alpha1_rayservice.yaml
```

* Let's first take a look at the Ray Serve config (i.e. `serveConfigV2`) embedded in the RayService yaml. At a high level, there are two applications: a fruit stand app and a calculator app. Some details about the fruit stand application:
  * The fruit stand application is contained in the `deployment_graph` variable in `fruit.py` in the [test_dag](https://github.com/ray-project/test_dag/tree/41d09119cbdf8450599f993f51318e9e27c59098) repo, so `import_path` in the config points to this variable to tell Serve from where to import the application.
  * It is hosted at the route prefix `/fruit`, meaning HTTP requests with routes that start with the prefix `/fruit` will be sent to the fruit stand application.
  * The working directory points to the [test_dag](https://github.com/ray-project/test_dag/tree/41d09119cbdf8450599f993f51318e9e27c59098) repo, which will be downloaded at runtime, and your application will be started in this directory. See the [Runtime Environment Documentation](https://docs.ray.io/en/latest/ray-core/handling-dependencies.html#runtime-environments) for more details.
  * For more details on configuring Ray Serve deployments, see the [Ray Serve Documentation](https://docs.ray.io/en/master/serve/configure-serve-deployment.html).
  * Similarly, the calculator app is imported from the `conditional_dag.py` file in the same repo, and it's hosted at the route prefix `/calc`.
  ```yaml
  serveConfigV2: |
    applications:
      - name: fruit_app
        import_path: fruit.deployment_graph
        route_prefix: /fruit
        runtime_env:
          working_dir: "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
        deployments: ...
      - name: math_app
        import_path: conditional_dag.serve_dag
        route_prefix: /calc
        runtime_env:
          working_dir: "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
        deployments: ...
  ```

## Step 4: Verify the Kubernetes cluster status 

```sh
# Step 4.1: List all RayService custom resources in the `default` namespace.
kubectl get rayservice

# [Example output]
# NAME                AGE
# rayservice-sample   2m42s

# Step 4.2: List all RayCluster custom resources in the `default` namespace.
kubectl get raycluster

# [Example output]
# NAME                                 DESIRED WORKERS   AVAILABLE WORKERS   STATUS   AGE
# rayservice-sample-raycluster-6mj28   1                 1                   ready    2m27s

# Step 4.3: List all Ray Pods in the `default` namespace.
kubectl get pods -l=ray.io/is-ray-node=yes

# [Example output]
# ervice-sample-raycluster-6mj28-worker-small-group-kg4v5   1/1     Running   0          3m52s
# rayservice-sample-raycluster-6mj28-head-x77h4             1/1     Running   0          3m52s

# Step 4.4: List services in the `default` namespace.
kubectl get services

# NAME                                          TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)                                                   AGE
# ...
# rayservice-sample-head-svc                    ClusterIP   10.96.34.90     <none>        10001/TCP,8265/TCP,52365/TCP,6379/TCP,8080/TCP,8000/TCP   4m58s
# rayservice-sample-raycluster-6mj28-head-svc   ClusterIP   10.96.171.184   <none>        10001/TCP,8265/TCP,52365/TCP,6379/TCP,8080/TCP,8000/TCP   6m21s
# rayservice-sample-serve-svc                   ClusterIP   10.96.161.84    <none>        8000/TCP                                                  4m58s
```

KubeRay will create a RayCluster based on `spec.rayClusterConfig` defined in the RayService YAML for a RayService custom resource.
Next, after the head Pod is running and ready, KubeRay will submit a request to the head's dashboard agent port (default: 52365) to create the Ray Serve applications defined in `spec.serveConfigV2`.

After the Ray Serve applications are healthy and ready, KubeRay will create a head service and a serve service for the RayService custom resource (e.g., `rayservice-sample-head-svc` and `rayservice-sample-serve-svc` in Step 4.4).
Users can access the head Pod through both the head service managed by RayService (i.e. `rayservice-sample-head-svc`) and the head service managed by RayCluster (i.e. `rayservice-sample-raycluster-6mj28-head-svc`).
However, during a zero downtime upgrade, a new RayCluster will be created, and a new head service will be created for the new RayCluster.
If `rayservice-sample-head-svc` is not used, users will need to update their ingress configuration to point to the new head service.
However, if `rayservice-sample-head-svc` is used, KubeRay will automatically update the selector to point to the new head Pod, eliminating the need for users to update their ingress configuration.


> Note: Default ports and their definitions. 

| Port  | Definition          |
|-------|---------------------|
| 6379  | Ray GCS             |
| 8265  | Ray Dashboard       |
| 10001 | Ray Client          |
| 8000  | Ray Serve           |
| 52365 | Ray Dashboard Agent |

## Step 5: Verify the status of the Serve applications

```sh
# Step 5.1: Check the status of the RayService.
kubectl describe rayservices rayservice-sample

# Active Service Status:
#   Application Statuses:
#     fruit_app:
#       Health Last Update Time:  2023-07-11T22:21:24Z
#       Last Update Time:         2023-07-11T22:21:24Z
#       Serve Deployment Statuses:
#         fruit_app_DAGDriver:
#           Health Last Update Time:  2023-07-11T22:21:24Z
#           Last Update Time:         2023-07-11T22:21:24Z
#           Status:                   HEALTHY
#         fruit_app_FruitMarket:
#           ...
#       Status:                       RUNNING
#     math_app:
#       Health Last Update Time:  2023-07-11T22:21:24Z
#       Last Update Time:         2023-07-11T22:21:24Z
#       Serve Deployment Statuses:
#         math_app_Adder:
#           Health Last Update Time:  2023-07-11T22:21:24Z
#           Last Update Time:         2023-07-11T22:21:24Z
#           Status:                   HEALTHY
#         math_app_DAGDriver:
#           ...
#       Status:                       RUNNING

# Step 5.2: Check the Serve applications in the Ray dashboard.
# (1) Forward the dashboard port to localhost.
# (2) Check the Serve page in the Ray dashboard at http://localhost:8265/#/serve.
kubectl port-forward svc/rayservice-sample-head-svc --address 0.0.0.0 8265:8265
```

* Refer to [rayservice-troubleshooting.md](https://github.com/ray-project/kuberay/blob/master/docs/guidance/rayservice-troubleshooting.md#observability) for more details on RayService observability.
Below is a screenshot example of the Serve page in the Ray dashboard.
  ![Ray Serve Dashboard](../images/dashboard_serve.png)

## Step 6: Send requests to the Serve applications via the Kubernetes serve service

```sh
# Step 6.1: Run a curl Pod.
kubectl run curl --image=radial/busyboxplus:curl -i --tty

# Step 6.2: Send a request to the fruit stand app.
curl -X POST -H 'Content-Type: application/json' rayservice-sample-serve-svc:8000/fruit/ -d '["MANGO", 2]'
# [Expected output]: 6

# Step 6.3: Send a request to the calculator app.
curl -X POST -H 'Content-Type: application/json' rayservice-sample-serve-svc:8000/calc/ -d '["MUL", 3]'
# [Expected output]: "15 pizzas please!"
```

* `rayservice-sample-serve-svc` is HA in general. It will do traffic routing among all the workers which have serve deployments and will always try to point to the healthy cluster, even during upgrading or failing cases. 


> You can set `serviceUnhealthySecondThreshold` to define the threshold of seconds that the serve deployments fail. You can also set `deploymentUnhealthySecondThreshold` to define the threshold of seconds that Ray fails to deploy any serve deployments.

### Upgrade RayService Using In-Place Update

You can update the configurations for the applications by modifying `serveConfigV2` in the RayService config file. Re-applying the modified config with `kubectl apply` will re-apply the new configurations to the existing Serve cluster.

Let's try it out. Update the price of mangos from `3` to `4` for the fruit stand app in [ray_v1alpha1_rayservice.yaml](https://github.com/ray-project/kuberay/blob/master/ray-operator/config/samples/ray_v1alpha1_rayservice.yaml). This will reconfigure the existing MangoStand deployment, and future requests will use the updated Mango price.
```shell
- name: MangoStand
  num_replicas: 1
  user_config:
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
[ root@curl:/ ]$ curl -X POST -H 'Content-Type: application/json' rayservice-sample-serve-svc.default.svc.cluster.local:8000/fruit/ -d '["MANGO", 2]'
> 8
```
Or if using port forwarding:
```shell
curl -X POST -H 'Content-Type: application/json' localhost:8000/fruit/ -d '["MANGO", 2]'
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
