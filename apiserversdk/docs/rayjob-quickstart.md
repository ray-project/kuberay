# RayJob QuickStart

RayJob automatically creates the RayCluster, submits the job when ready, and can optionally delete the RayCluster after the
job finishes. You can find a detailed introduction to RayJob and how to manage it using Kubernetes in [this
guide](https://docs.ray.io/en/latest/cluster/kubernetes/getting-started/rayjob-quick-start.html).

This document focus on explaining how to manage and interact with RayJob using the KubeRay API Server.

## Preparation

- KubeRay v0.6.0 or higher
  - KubeRay v0.6.0 or v1.0.0: Ray 1.10 or higher.
  - KubeRay v1.1.1 or newer is highly recommended: Ray 2.8.0 or higher.

### Step 0.1: Create a Kubernetes cluster

This step creates a local Kubernetes cluster using [Kind](https://kind.sigs.k8s.io/). If you already have a Kubernetes
cluster, you can skip this step.

```sh
kind create cluster --image=kindest/node:v1.26.0
```

### Step 0.2: Install KubeRay operator and API Server SDK

Follow [Installation Guide](../Installation.md) to install the latest stable KubeRay operator and API Server
SDK from the Helm repository.

### Important: Switch directory to `apiserversdk/`

All the following guidance require you to switch your working directory to the
`apiserversdk/`.

```sh
cd apiserversdk
```

## Scenario 1: Create RayJob without setting `shutdownAfterJobFinishes`

The first example will create a RayJob that does not automatically delete the RayCluster
it created after the job finished.

### Step 1.1: Install a RayJob

Once the KubeRay operator is running, we can install a RayJob through APIServer with following command:

```sh
curl -X POST 'http://localhost:31888/api/v1/namespaces/default/configmaps' \
  --header 'Content-Type: application/json' \
  --data @docs/api-example/rayjob-configmap.json

curl -X POST 'localhost:31888/apis/ray.io/v1/namespaces/default/rayjobs' \
--header 'Content-Type: application/json' \
--data  @docs/api-example/rayjob.json
```

### Step 1.2: Check RayJob Status

You can check if the RayJob is successfully created by:

```sh
kubectl get rayjob

# NAME            JOB STATUS   DEPLOYMENT STATUS   RAY CLUSTER NAME                 START TIME             END TIME   AGE
# rayjob-sample                Running             rayjob-sample-raycluster-4bds8   2025-05-14T12:40:10Z              28s
```

While RayJob will create the RayCluster automatically, we can see the RayCluster it created through:

```sh
kubectl get raycluster

# NAME                             DESIRED WORKERS   AVAILABLE WORKERS   CPUS   MEMORY   GPUS   STATUS   AGE
# rayjob-sample-raycluster-4bds8   1                 1                   400m   0        0      ready    43s
```

Also, Ray Operator will create head and worker pods for RayCluster. You can view them through listing all pods in the
default namespace:

```sh
kubectl get pods

# NAME                                                      READY   STATUS      RESTARTS   AGE
# kuberay-operator-6bc45dd644-cx2k9                         1/1     Running     0          94m
# rayjob-sample-raycluster-4bds8-head-kgszl                 1/1     Running     0          63s
# rayjob-sample-raycluster-4bds8-small-group-worker-p24sl   1/1     Running     0          63s
# rayjob-sample-pm95l                                       0/1     Completed   0          37s
```

### Step 1.3: Check the output of the RayJob

You can view the log of our job with following command. This job simply executes a counter’s increment function 5
times:

```sh
kubectl logs -l=job-name=rayjob-sample

# 2025-05-14 05:40:47,722 INFO worker.py:1654 -- Connecting to existing Ray cluster at address: 10.244.0.11:6379...
# 2025-05-14 05:40:47,735 INFO worker.py:1832 -- Connected to Ray cluster. View the dashboard at 10.244.0.11:8265
# test_counter got 1
# test_counter got 2
# test_counter got 3
# test_counter got 4
# test_counter got 5
# 2025-05-14 05:40:55,688 SUCC cli.py:63 -- -----------------------------------
# 2025-05-14 05:40:55,688 SUCC cli.py:64 -- Job 'rayjob-sample-2vlkx' succeeded
# 2025-05-14 05:40:55,688 SUCC cli.py:65 -- -----------------------------------
```

### Step 1.4: Delete the RayJob

To delete the RayJob with KubeRay APIServer, execute the following command. The `rayjob-sample` is the name of
the RayJob we created.

```sh
curl -X DELETE 'localhost:31888/apis/ray.io/v1/namespaces/default/rayclusters/rayjob-sample'
```

You can then see that both RayJob and RayCluster are removed. Following commands will
print nothing:

```sh
# List all RayJob
kubectl get rayjobs

# List all RayCluster
kubectl get rayclusters
```

## Scenario 2: Create RayJob with `shutdownAfterJobFinishes` set to true

The RayJob in this example will delete the RayCluster once the job finished.

### Step 2.1: Install a RayJob with `shutdownAfterJobFinishes` set to true

Use following commands to install the RayJob. The RayJob here has two additional settings:

- **shutdownAfterJobFinishes: true**: Tell KubeRay operator to delete the RayCluster after
job finished.
- **ttlSecondsAfterFinished: 10**: The RayCluster will be deleted 10 seconds after the job
finishes.

```sh
# Note: If you already installed the ConfigMap, you can skip this command
curl -X POST 'http://localhost:31888/api/v1/namespaces/default/configmaps' \
  --header 'Content-Type: application/json' \
  --data @docs/api-example/rayjob-configmap.json

curl -X POST 'localhost:31888/apis/ray.io/v1/namespaces/default/rayjobs' \
--header 'Content-Type: application/json' \
--data  @docs/api-example/rayjob-shutdownAfterJobFinishes.json
```

### Step 2.2: Check RayJob Status

Once the job finished, you can see `JOB STATUS` as `SUCCEED` by:

```sh
kubectl get rayjobs.ray.io rayjob-sample-shutdown -o jsonpath='{.status.jobStatus}'

# SUCCEED
```

After 10 seconds (set in `ttlSecondsAfterFinished`), you can see RayCluster is removed by
listing all RayClusters. Following command should print nothing:

```sh
kubectl get raycluster
```

## Clean up

```sh
kind delete cluster
```
