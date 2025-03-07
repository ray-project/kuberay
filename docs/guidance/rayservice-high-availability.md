<!-- markdownlint-disable MD013 -->
# RayService high availability

RayService provides high availability (HA) to ensure services continue serving requests without failure during scaling up, scaling down, and upgrading the RayService configuration (zero-downtime upgrade).

## Quickstart

### Step 1: Create a Kubernetes cluster with Kind

```sh
kind create cluster --image=kindest/node:v1.24.0
```

### Step 2: Install the KubeRay operator

Follow the instructions in [this document](/helm-chart/kuberay-operator/README.md) to install the latest stable KubeRay operator, or follow the instructions in [DEVELOPMENT.md](/ray-operator/DEVELOPMENT.md) to install the nightly KubeRay operator.

### Step 3: Create a RayService and a locust cluster

```sh
# Path: kuberay/
kubectl apply -f ./ray-operator/config/samples/ray-service.high-availability-locust.yaml
kubectl get pod
# NAME                                        READY   STATUS    RESTARTS   AGE
# kuberay-operator-64b4fc5946-zbfqd           1/1     Running   0          72s
# locust-cluster-head-6clr5                   1/1     Running   0          38s
# rayservice-ha-raycluster-pfh8b-head-58xkr   2/2     Running   0          36s
```

The [ray-service.high-availability-locust.yaml](/ray-operator/config/samples/ray-service.high-availability-locust.yaml) has several Kubernetes objects:

- A RayService with serve autoscaling and Pod autoscaling enabled.
- A RayCluster functioning as locust cluster to simulate users sending requests.
- A configmap with a locustfile sets user request levels: starts low, spikes, then drops.

### Step 4: Use Locust cluster to simulate users sending requests

```sh
# Open a new terminal and log into the locust cluster.
kubectl exec -it $(kubectl get pods -o=name | grep locust-cluster-head) -- bash

# Install locust and download locust_runner.py.
# locust_runner.py helps distribute the locust workers across the RayCluster.
pip install locust && wget https://raw.githubusercontent.com/ray-project/serve_workloads/main/microbenchmarks/locust_runner.py

# Start sending requests to the RayService.
python locust_runner.py -f /locustfile/locustfile.py --host http://rayservice-ha-serve-svc:8000
```

### Step 5: Verify high availability during scaling up and down

The locust cluster sends requests to the RayService, starting with a low number of requests, then spiking, and finally dropping. This will trigger the RayService to scale up and down. You can verify the high availability by observing the Ray Pod and the failure rate in the locust terminal.

```sh
watch -n 1 "kubectl get pod"
# Stage 1: Low request rate.
# NAME                                                 READY   STATUS     RESTARTS   AGE
# rayservice-ha-raycluster-pfh8b-head-58xkr            2/2     Running    0          78s
# rayservice-ha-raycluster-pfh8b-worker-worker-rd22n   0/1     Init:0/1   0          9s

# Stage 2: High request rate
# rayservice-ha-raycluster-pfh8b-head-58xkr            2/2     Running    0          113s
# rayservice-ha-raycluster-pfh8b-worker-worker-7thjv   0/1     Init:0/1   0          4s
# rayservice-ha-raycluster-pfh8b-worker-worker-nt98j   0/1     Init:0/1   0          4s
# rayservice-ha-raycluster-pfh8b-worker-worker-rd22n   1/1     Running    0          44s

# Stage 3: Low request rate
# NAME                                                 READY   STATUS        RESTARTS   AGE
# rayservice-ha-raycluster-pfh8b-head-58xkr            2/2     Running       0          3m38s
# rayservice-ha-raycluster-pfh8b-worker-worker-7thjv   0/1     Terminating   0          109s
# rayservice-ha-raycluster-pfh8b-worker-worker-nt98j   0/1     Terminating   0          109s
# rayservice-ha-raycluster-pfh8b-worker-worker-rd22n   1/1     Running       0          2m29s
```

Let's describe how KubeRay and Ray ensure high availability during scaling, using the example provided.

In the above example, the RayService configuration is as follows:

- Every node can have at most one serve replica.
- The initial number of serve replicas is set to zero.
- The head node will not be scheduled for any workloads to follow best practices.

With the above settings, when serve replicas scale up:

1. KubeRay creates a new worker Pod. Since no serve replicas are currently running, the readiness probe for the new Pod fails. As a result, the endpoint is not added to the serve service.
2. Ray then schedules a new serve replica to the newly created worker Pod. Once the serve replica is running, the readiness probe passes, and the endpoint is added to the serve service.

When serve replicas scale down:

1. The proxy actor in the worker Pod that is scaling down changes its stage to `draining`. The readiness probe fails immediately, and the endpoint starts to be removed from the serve service. However, this process takes some time, so incoming requests are still redirected to this worker Pod for a short period.
2. During the draining stage, the proxy actor can still redirect incoming requests. The proxy actor is only removed and changes to the `drained` stage when the following conditions are met:
    - There are no ongoing requests.
    - The minimum draining time has been reached, which can be controlled by an environmental variable: `RAY_SERVE_PROXY_MIN_DRAINING_PERIOD_S`.

    Also, removing endpoints from the serve service does not affect the existing ongoing requests. All of the above ensures high availability.
3. Once the worker Pod becomes idle, KubeRay removes it from the cluster.

  > Note, the default value of `RAY_SERVE_PROXY_MIN_DRAINING_PERIOD_S` is 30s. You may change it to fit with your k8s cluster.

### Step 6: Verify high availability during upgrade

The locust cluster will continue sending requests for 600s. Before the 600s is up, upgrade the RayService configuration by adding a new environment variable. This will trigger a rolling update. You can verify the high availability by observing the Ray Pod and the failure rate in the locust terminal.

```sh
kubectl patch rayservice rayservice-ha --type='json' -p='[
  {
    "op": "add",
    "path": "/spec/rayClusterConfig/headGroupSpec/template/spec/containers/0/env",
    "value": [
      {
        "name": "RAY_SERVE_PROXY_MIN_DRAINING_PERIOD_S",
        "value": "30"
      }
    ]
  }
]'

watch -n 1 "kubectl get pod"
# stage 1: New head pod is created.
# NAME                                                 READY   STATUS    RESTARTS   AGE
# rayservice-ha-raycluster-nhs7v-head-z6xkn            1/2     Running   0          4s
# rayservice-ha-raycluster-pfh8b-head-58xkr            2/2     Running   0          4m30s
# rayservice-ha-raycluster-pfh8b-worker-worker-rd22n   1/1     Running   0          3m21s

# stage 2: Old head pod terminates after new head pod is ready and k8s service is fully updated.
# NAME                                                 READY   STATUS        RESTARTS   AGE
# rayservice-ha-raycluster-nhs7v-head-z6xkn            2/2     Running       0          91s
# rayservice-ha-raycluster-nhs7v-worker-worker-jplrp   0/1     Init:0/1      0          3s
# rayservice-ha-raycluster-pfh8b-head-58xkr            2/2     Terminating   0          5m57s
# rayservice-ha-raycluster-pfh8b-worker-worker-rd22n   1/1     Terminating   0          4m48s
```

When a new configuration is applied, the Kuberay operator always creates a new RayCluster with the new configuration and then removes the old RayCluster.
Here are the details of the rolling update:

1. KubeRay creates a new RayCluster with the new configuration. At this time, all requests are still being served by the old RayCluster.
2. After the new RayCluster and the server app on it are ready, KubeRay updates the serve service to redirect the traffic to the new RayCluster. At this point, traffic is being served by both the old and new RayCluster as it takes time to update the k8s service.
3. After the serve service is fully updated, KubeRay removes the old RayCluster. The traffic is now fully served by the new RayCluster.

### Step 7: Examine the locust results

In your locust terminal, You will see the failed rate is 0.00%.

```sh
      # fails |
|-------------|
     0(0.00%) |
|-------------|
     0(0.00%) |
```

### Step 8: Clean up

```sh
kubectl delete -f ./ray-operator/config/samples/ray-service.high-availability-locust.yaml
kind delete cluster
```
