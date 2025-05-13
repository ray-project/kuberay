# RayCluster QuickStart

The guidance on managing the Ray clusters with Kubernetes can be found
[here](https://docs.ray.io/en/latest/cluster/kubernetes/getting-started/raycluster-quick-start.html). This guide goes
through how to manage and interact with Ray clusters with KubeRay APIServer.

## Preperation

- Install [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) (>= 1.23),
[Helm](https://helm.sh/docs/intro/install/) (>= v3.4) if needed,
[Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation), and
[Docker](https://docs.docker.com/engine/install/).
- Make sure your Kubernetes cluster has at least 4 CPU and 4 GB RAM.

## Step 1: Create a Kubernetes cluster

This step creates a local Kubernetes cluster using [Kind](https://kind.sigs.k8s.io/). If you already have a Kubernetes
cluster, you can skip this step.

```sh
kind create cluster --image=kindest/node:v1.26.0
```

## Step 2: Deploy a KubeRay operator

Follow [this
document](https://docs.ray.io/en/latest/cluster/kubernetes/getting-started/kuberay-operator-installation.html#kuberay-operator-deploy)
to install the latest stable KubeRay operator from the Helm repository.

## Step 3: Deploy a RayCluster custom resource

Once the KubeRay operator is running, you are ready to deploy a RayCluster. While we are using APIServer, we can do this
with curl. The following command will create a RayCluster CR in your current cluster:

```sh
curl -X POST 'https://127.0.0.1:37435/apis/ray.io/v1/namespaces/default/rayclusters' \
--cert /tmp/client.crt \
--key /tmp/client.key \
--cacert /tmp/ca.crt \
--header 'Content-Type: application/json' \
--data  @api-example/raycluster.json
```

Once the RayCluster CR has been created, you can view it by running:

- With kubectl:

```sh
kubectl get rayclusters
# NAME                 DESIRED WORKERS   AVAILABLE WORKERS   CPUS   MEMORY   GPUS   STATUS   AGE
# raycluster-kuberay   1                                     2      3G       0               89s
```

- With KubeRay APIServer:

```sh
curl -s 'https://127.0.0.1:37435/apis/ray.io/v1/namespaces/default/rayclusters' \
  --cert /tmp/client.crt \
  --key /tmp/client.key \
  --cacert /tmp/ca.crt \
  | jq -r '.items[].metadata.name'
```

The KubeRay operator detects the RayCluster object and starts your Ray cluster by creating head and worker pods. To view
Ray cluster’s pods, run the following command:

- With kubectl

```sh
# View the pods in the RayCluster named "raycluster-kuberay"
kubectl get pods --selector=ray.io/cluster=raycluster-kuberay

# NAME                                          READY   STATUS    RESTARTS   AGE
# raycluster-kuberay-head-k7rlq                 1/1     Running   0          56s
# raycluster-kuberay-workergroup-worker-65zl8   1/1     Running   0          56s
```

- With KubeRay APIServer

```sh
curl -s https://127.0.0.1:37435/api/v1/namespaces/default/pods\?labelSelector=ray.io/cluster=raycluster-kuberay \
    --cert /tmp/client.crt \
    --key /tmp/client.key \
    --cacert /tmp/ca.crt  \
    | jq -r '.items[].metadata.name'

# raycluster-kuberay-head-wbjzx
# raycluster-kuberay-workergroup-worker-ss5zl
```

## Step 4: Delete the RayCluster

To delete the RayCluster with KubeRay APIServer, execute the following command. The `raycluster-kuberay` is the name of
the RayCluster we created earlier. You should see the "Success" status after the execution:

```sh
curl -X DELETE 'https://127.0.0.1:37435/apis/ray.io/v1/namespaces/default/rayclusters/raycluster-kuberay' \
  --cert /tmp/client.crt \
  --key /tmp/client.key \
  --cacert /tmp/ca.crt

# {
#   "kind": "Status",
#   "apiVersion": "v1",
#   "metadata": {},
#   "status": "Success",
#   "details": {
#     "name": "raycluster-kuberay",
#     "group": "ray.io",
#     "kind": "rayclusters",
#     "uid": "5528a3bc-b02c-4b8a-ac1b-48d911a42f1b"
#   }
# }
```

You can then verify if the RayCluster is removed. The following command should print nothing:

```sh
kubectl get rayclusters
```

## Step 5: Clean up

```sh
kind delete cluster
```
