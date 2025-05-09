# Creating Autoscaling clusters using API server

One of Ray's key features is autoscaling. This [document] explains setting up autoscaling
with the Ray operator. Here, we demonstrate how to configure it using the API server and
run an example.

## Setup

Refer to [README](README.md) for setting up KubRay operator and API server.

Alternatively, you could build and deploy the Operator and API server from local repo for
development purpose.

```shell
make start-local-apiserver deploy
```

## Example

This example walks through how to trigger scale-up and scale-down for RayCluster.

Before going through the example, remove any running RayClusters to ensure a successful
run through of the example below.

```sh
kubectl delete raycluster --all
```

### Install ConfigMap

Please install this [ConfigMap] which contains code for our example. Simply download
this file and run:

```sh
kubectl apply -f detachedactor.yaml
```

Check if the config map is successfully created, you should see `ray-example` in the list:

```sh
kubectl get configmaps
# NAME               DATA   AGE
# ray-example        2      8s
```

### Deploy RayCluster

Before running the example, you need to first deploy a RayCluster with following command.

```shell
curl -X POST 'localhost:31888/apis/v1/namespaces/default/compute_templates' \
--header 'Content-Type: application/json' \
--data '{
  "name": "default-template",
  "namespace": "default",
  "cpu": 2,
  "memory": 4
}'
curl -X POST 'localhost:31888/apis/v1/namespaces/default/clusters' \
--header 'Content-Type: application/json' \
--data '{
  "name": "test-cluster",
  "namespace": "default",
  "user": "boris",
  "clusterSpec": {
    "enableInTreeAutoscaling": true,
    "autoscalerOptions": {
        "upscalingMode": "Default",
        "idleTimeoutSeconds": 30,
        "cpu": "500m",
        "memory": "512Mi"
    },
    "headGroupSpec": {
      "computeTemplate": "default-template",
      "image": "rayproject/ray:2.9.0-py310",
      "serviceType": "NodePort",
      "rayStartParams": {
         "dashboard-host": "0.0.0.0",
         "metrics-export-port": "8080",
         "num-cpus": "0"
       },
       "volumes": [
         {
           "name": "code-sample",
           "mountPath": "/home/ray/samples",
           "volumeType": "CONFIGMAP",
           "source": "ray-example",
           "items": {
            "detached_actor.py": "detached_actor.py",
            "terminate_detached_actor.py": "terminate_detached_actor.py"
           }
         }
       ]
    },
    "workerGroupSpec": [
      {
        "groupName": "small-wg",
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.9.0-py310",
        "replicas": 0,
        "minReplicas": 0,
        "maxReplicas": 5,
        "rayStartParams": {
           "node-ip-address": "$MY_POD_IP"
         },
        "volumes": [
          {
            "name": "code-sample",
            "mountPath": "/home/ray/samples",
            "volumeType": "CONFIGMAP",
            "source": "ray-example",
            "items": {
                "detached_actor.py": "detached_actor.py",
                "terminate_detached_actor.py": "terminate_detached_actor.py"
            }
          }
        ]
      }
    ]
  }
}'
```

This command performs two main operations:

1. Creates a compute template `default-template` that specifies resources to use when
   scale-up (2 CPUs and 4 GiB memory).

2. Deploys a RayCluster (test-cluster) with:
    - A head pod that manages the cluster
    - A worker group configured to scale between 0 and 5 replicas

The worker group uses the following autoscalerOptions to control scaling behavior:

- **`upscalingMode: "Default"`**: Default scaling behavior. Ray will scale up only as
needed.
- **`idleTimeoutSeconds: 30`** If a worker pod remains idle (i.e., not running any tasks)
for 30 seconds, it will be automatically removed.
- **`cpu: "500m"`, `memory: "512Mi"`**: Defines the **minimum resource unit** Ray uses to
assess scaling needs.  If no worker pod has at least this much free capacity, Ray will
trigger a scale-up and launch a new worker pod.

> **Note:** These values **do not determine the actual size** of the worker pod. The
> pod size comes from the `computeTemplate` (in this case, 2 CPUs and 4 GiB memory).

### Validate that RayCluster is deployed correctly

Run following command to get list of pods running. You should see something like below:

```sh
kubectl get pods
# NAME                                READY   STATUS    RESTARTS   AGE
# kuberay-operator-545586d46c-f9grr   1/1     Running   0          49m
# test-cluster-head                   2/2     Running   0          3m1s
```

Note that there is no worker for `test-cluster` as we set its initial replicas to 0. You
will only see head pod with 2 containers for `test-cluster`.

### Trigger RayCluster scale-up

Create a detached actor to trigger scale-up with following command:

```sh
curl -X POST 'localhost:31888/apis/v1/namespaces/default/jobs' \
--header 'Content-Type: application/json' \
--data '{
  "name": "create-actor",
  "namespace": "default",
  "user": "boris",
  "entrypoint": "python /home/ray/samples/detached_actor.py actor1",
  "clusterSelector": {
    "ray.io/cluster": "test-cluster"
  }
}'
```

The `detached_actor.py` file is defined in the [ConfigMap] we installed earlier, which
requires `num_cpus=1`. Recall that initially there is no worker pod exists, RayCluster
needs to scale up a worker for running this actor.

Check if a worker is created. You can see a worker `test-cluster-small-wg-worker` spins
up.

```sh
kubectl get pods

# NAME                                 READY   STATUS      RESTARTS   AGE
# create-actor-tsvfc                   0/1     Completed   0          99s
# kuberay-operator-545586d46c-f9grr    1/1     Running     0          55m
# test-cluster-head                    2/2     Running     0          9m37s
# test-cluster-small-wg-worker-j54xf   1/1     Running     0          88s
```

### Trigger RayCluster scale-down

Run following to delete the actor we created earlier.

```sh
curl -X POST 'localhost:31888/apis/v1/namespaces/default/jobs' \
--header 'Content-Type: application/json' \
--data '{
  "name": "delete-actor",
  "namespace": "default",
  "user": "boris",
  "entrypoint": "python /home/ray/samples/terminate_detached_actor.py actor1",
  "clusterSelector": {
    "ray.io/cluster": "test-cluster"
  }
}'
```

While actor is deleted, we do not need the worker anymore. The worker pod will be deleted
after `idleTimeoutSeconds` (default 60, we specified 30) seconds.

List all pods to verify if the worker pod is deleted:

```sh
kubectl get pods

# NAME                                READY   STATUS      RESTARTS   AGE
# create-actor-tsvfc                  0/1     Completed   0          6m37s
# delete-actor-89z8c                  0/1     Completed   0          83s
# kuberay-operator-545586d46c-f9grr   1/1     Running     0          60m
# test-cluster-head                   2/2     Running     0          14m

```

### Clean up

Run following command to clean up RayCluster:

```sh
kubectl delete raycluster test-cluster
```

[document]: https://docs.ray.io/en/latest/cluster/kubernetes/user-guides/configuring-autoscaling.html
[ConfigMap]: test/cluster/cluster/detachedactor.yaml
