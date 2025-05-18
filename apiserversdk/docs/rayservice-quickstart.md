# RayCluster QuickStart

RayService consists of a RayCluster and Ray Serve deployment graphs. It offers
zero-downtime upgrades for RayCluster and high availability. The guidance on managing Ray
applications and clusters with Kubernetes using RayService can be found
[here](https://docs.ray.io/en/latest/cluster/kubernetes/getting-started/rayservice-quick-start.html).

This guide covers how to deploy, update, and maintain high-availability Ray Serve
applications and clusters using the KubeRay APIServer. We will serve two simple Ray Serve
applications using RayService as example.

## Preparation

This guide mainly focuses on the behavior of KubeRay v1.3.0 and Ray 2.41.0.

## Step 1: Create a Kubernetes cluster

This step creates a local Kubernetes cluster using [Kind](https://kind.sigs.k8s.io/). If you already have a Kubernetes
cluster, you can skip this step.

```sh
kind create cluster --image=kindest/node:v1.26.0
```

## Step 2: Install KubeRay operator and APIServer

Follow [Installation Guide](../Installation.md) to install the latest stable KubeRay operator and APIServer
 from the Helm repository.

> [!IMPORTANT]
> All the following guidance require you to switch your working directory to the `apiserversdk/`.

## Step 3: Install a RayService

The RayService can be created with follow curl command:

```sh
curl -s https://raw.githubusercontent.com/ray-project/kuberay/master/ray-operator/config/samples/ray-service.sample.yaml | \
curl -X POST http://localhost:31888/apis/ray.io/v1/namespaces/default/rayservices \
  -H "Content-Type: application/yaml" \
  --data-binary @-
```

Once the RayService CR has been created, you can view it by running:

```sh
kubectl get rayservice

# NAME                SERVICE STATUS   NUM SERVE ENDPOINTS
# rayservice-sample   Running          2
```

You can also see RayCluster being created:

```sh
kubectl get rayclusters

# NAME                                 DESIRED WORKERS   AVAILABLE WORKERS   CPUS    MEMORY   GPUS   STATUS   AGE
# rayservice-sample-raycluster-c5jm7   1                 1                   2500m   4Gi      0               99s
```

Also, Ray Operator will create head and worker pods for RayCluster. You can view them through listing all pods in the
default namespace:

```sh
kubectl get pods -l=ray.io/is-ray-node=yes

# NAME                                                          READY   STATUS    RESTARTS   AGE
# rayservice-sample-raycluster-c5jm7-head-75dmj                 1/1     Running   0          4m16s
# rayservice-sample-raycluster-c5jm7-small-group-worker-lzvzh   1/1     Running   0          4m16s
```

## Step 4: Check the status of RayService

To check if the RayService is ready, use following command and check if the `status` field
is `True`:

```sh
kubectl get rayservice rayservice-sample -o json | jq -r '.status.conditions[] | select(.type=="Ready") | to_entries[] | "\(.key): \(.value)"'

# lastTransitionTime: 2025-05-15T12:43:04Z
# message: Number of serve endpoints is greater than 0
# observedGeneration: 1
# reason: NonZeroServeEndpoints
# status: True
# type: Ready
```

When the Ray Serve applications are healthy and ready, KubeRay creates a head service and
a Ray Serve service for the RayService custom resource. You can view all services created
by:

```sh
kubectl get services -o json | jq -r '.items[].metadata.name'

# kuberay-operator
# kubernetes
# rayservice-sample-head-svc
# rayservice-sample-raycluster-c5jm7-head-svc
# rayservice-sample-serve-svc
```

Here, you can see two RayService related services:

- `rayservice-sample-head-svc`:
    This service points to the head pod of the active RayCluster and is typically used to
view the Ray Dashboard (port `8265`).
- `rayservice-sample-serve-svc`:
    This service exposes the HTTP interface of Ray Serve, typically on port `8000`. Use
this service to send HTTP requests to your deployed Serve applications (e.g., REST API, ML
inference, etc.).

## Step 5: Send requests to the Serve applications

Once the RayService is ready, we can try to send requests to our serve applications. In
our example, we created two applications, namely `calc` and `fruit`. To interact with
them, we need to run a curl pod first:

```sh
kubectl run curl --image=radial/busyboxplus:curl --command -- tail -f /dev/null
```

Then, we can use following commands to interact with the applications:

- Interact with `calc`:

```sh
kubectl exec curl -- curl -sS -X POST -H 'Content-Type: application/json' rayservice-sample-serve-svc:8000/calc/ -d '["MUL", 3]'

# Expected output:
# 15 pizzas please!
```

- Interact with `fruit`:

```sh
kubectl exec curl -- curl -sS -X POST -H 'Content-Type: application/json' rayservice-sample-serve-svc:8000/fruit/ -d '["MANGO", 2]'

# Expected output:
# 6
```

## Step 6: Zero downtime upgrade for RayCluster

RayService enables a zero downtime upgrades for RayCluster. That is, if you modify
`spec.rayClusterConfig` in the RayService config, it triggers a zero downtime upgrade for
Ray clusters. RayService temporarily creates a new RayCluster and waits for it to be
ready, then switches traffic to the new RayCluster by updating the selector of the head
service managed by RayService `rayservice-sample-head-svc` and terminates the old one.

As an example, we will modify `rayVersion` in `rayClusterConfig` to `2.100.0`. Let's have
a look at the original `rayVersion`. Execute the following command:

```sh
kubectl get rayservice rayservice-sample -o jsonpath='{.spec.rayClusterConfig.rayVersion}'

# Expected output
# 2.41.0
```

Now, use the following for modifying the `rayVersion` for RayCluster managed by RayService.

```sh
curl -X PATCH http://localhost:31888/apis/ray.io/v1/namespaces/default/rayservices/rayservice-sample \
  -H "Content-Type: application/merge-patch+json" \
  --data '{
    "spec": {
      "rayClusterConfig": {
        "rayVersion": "2.100.0"
      }
    }
  }'
```

After the execution, you can see a new RayCluster is created.

```sh
kubectl get rayclusters

# NAME                                 DESIRED WORKERS   AVAILABLE WORKERS   CPUS    MEMORY   GPUS   STATUS   AGE
# rayservice-sample-raycluster-c5jm7   1                 1                   2500m   4Gi      0      ready    46m
# rayservice-sample-raycluster-n964b   1                                     2500m   4Gi      0               5s
```

Once the new RayCluster is ready, try checking if the `rayVersion` has been modified:

```sh
kubectl get rayservice rayservice-sample -o jsonpath='{.spec.rayClusterConfig.rayVersion}'

# Expected output
# 2.100.0
```

## Step 7: Delete the RayService

To delete the RayService with KubeRay APIServer, execute the following command. The `rayservice-sample` is the name of
the RayService we created earlier.

```sh
curl -X DELETE 'localhost:31888/apis/ray.io/v1/namespaces/default/rayservices/rayservice-sample'
```

You can then see that both RayService and RayCluster are removed. Following commands will
print nothing:

```sh
# List all RayService
kubectl get rayservices

# List all RayCluster
kubectl get rayclusters
```

## Step 8: Clean up

```sh
kind delete cluster
```
