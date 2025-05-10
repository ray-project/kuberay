# Creating HA cluster with API Server

One of the issue for long-running Ray application (e.g. RayServe) is that if Ray head node
dies, the whole cluster has to be restarted. Fortunately, KubeRay cluster solved it by
introducing [Fault Tolerance Ray Cluster](https://docs.ray.io/en/master/cluster/kubernetes/user-guides/kuberay-gcs-ft.html).

The RayCluster with high availability can also be created in API server, which aims to
ensure a high availability Global Control Service (GCS) data. The GCS manages
cluster-level metadata by storing all data in memory, which is lack of fault tolerance. A
single failure can cause the entire RayCluster to fail. To enable GCS's fault tolerance,
we should have a highly available Redis so that when GCS restart, it can resume its
status by retrieve previous data from the Redis instance.

We will provide a detailed example on how to create this highly available API Server.

## Setup

### Setup Ray Operator and API Server

Refer to [README](README.md) for setting up KubRay operator and API server.

## Example

Before going through the example, remove any running RayClusters to ensure a successful
run through of the example below.

```sh
kubectl delete raycluster --all
```

### Create external Redis cluster

A comprehensive documentation on creating Redis cluster on Kubernetes can be found
[here]( https://www.dragonflydb.io/guides/redis-kubernetes). For this example we will use a rather simple
[RedisYAML]. Simply download this YAML file and run:

```sh
kubectl create ns redis
kubectl apply -f redis.yaml -n redis
```

Note that we created a new `redis` namespace and deploy redis in it.

Alternatively, if you run on the cloud you can use managed version of HA Redis, which will not require
you to stand up, run, manage and monitor your own version of redis.

Check if the redis is successfully set up with following command. You should see
`redis-config` in the list:

```sh
kubectl get configmaps -n redis

# NAME               DATA   AGE
# redis-config       1      19s
```

### Create Redis password secret

Before creating your cluster, you need to create secret in the
namespace where you are planning to create your Ray cluster (remember, that secret is visible only within a given
namespace). To create a secret for using external redis, please download the [Secret] and
run following command:

```sh
kubectl apply -f redis_passwrd.yaml
```

### Install ConfigMap

We will use this [ConfigMap] which contains code for our example. For the real world
cases, it is recommended to pack user code in an image.

Please download the config map and deploy it with following command:

```sh
kubectl apply -f code_configmap.yaml
```

### Create RayCluster

Use following command to create a compute template and a RayCluster:

```sh
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
  "name": "ha-cluster",
  "namespace": "default",
  "user": "boris",
  "version": "2.9.0",
  "environment": "DEV",
  "annotations" : {
    "ray.io/ft-enabled": "true"
  },
  "clusterSpec": {
    "headGroupSpec": {
      "computeTemplate": "default-template",
      "image": "rayproject/ray:2.9.0-py310",
      "serviceType": "NodePort",
      "rayStartParams": {
         "dashboard-host": "0.0.0.0",
         "metrics-export-port": "8080",
         "num-cpus": "0",
         "redis-password": "$REDIS_PASSWORD"
       },
       "environment": {
         "values": {
            "RAY_REDIS_ADDRESS": "redis.redis.svc.cluster.local:6379"
         },
         "valuesFrom": {
            "REDIS_PASSWORD": {
                "source": 1,
                "name": "redis-password-secret",
                "key": "password"
            }
         }
       },
       "volumes": [
         {
           "name": "code-sample",
           "mountPath": "/home/ray/samples",
           "volumeType": "CONFIGMAP",
           "source": "ray-example",
           "items": {
              "detached_actor.py" : "detached_actor.py",
              "increment_counter.py" : "increment_counter.py"
            }
         }
       ]
    },
    "workerGroupSpec": [
      {
        "groupName": "small-wg",
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.9.0-py310",
        "replicas": 1,
        "minReplicas": 0,
        "maxReplicas": 5,
        "rayStartParams": {
           "node-ip-address": "$MY_POD_IP",
           "metrics-export-port": "8080"
        },
        "environment": {
           "values": {
             "RAY_gcs_rpc_server_reconnect_timeout_s": "300"
           }
        },
        "volumes": [
          {
            "name": "code-sample",
            "mountPath": "/home/ray/samples",
            "volumeType": "CONFIGMAP",
            "source": "ray-example",
            "items": {
              "detached_actor.py" : "detached_actor.py",
              "increment_counter.py" : "increment_counter.py"
            }
          }
        ]
      }
    ]
  }
}'
```

To enable the RayCluster's GCS fault tolerance feature, we added the annotation:

```json
"annotations" : {
    "ray.io/ft-enabled": "true"
}
```

For connecting to Redis, we set the following content in `rayStartParams` of
`headGroupSpec`: We also added following content in `rayStartParams` of `headGroupSpec`,
which set the Redis password and the number of cpu. Setting `num-cpu` to 0 ensures that no
application code runs on a head node.

```json
"redis-password:: "$REDIS_PASSWORD"
"num-cpu": "0"
```

Here, the `$REDIS_PASSWORD` is defined in `headGroupSpec`'s environment variable below:

```json
"environment": {
    "values": {
        "RAY_REDIS_ADDRESS": "redis.redis.svc.cluster.local:6379"
    },
    "valuesFrom": {
        "REDIS_PASSWORD": {
            "source": 1,
            "name": "redis-password-secret",
            "key": "password"
        }
    }
},
```

For the `workerGroupSpecs`, we set the `gcs_rpc_server_reconnect_timeout` environment
variable, which controls the GCS heartbeat timeout (default 60 seconds). This controls how
long after the head node dies do we kill the worker node. While it takes time to restart
the head node, we want this values to be large enough to prevent the worker node being
killed during the restarting period.

```json
"environment": {
    "values": {
        "RAY_gcs_rpc_server_reconnect_timeout_s": "300"
    }
},
```

### Validate that RayCluster is deployed correctly

Run following command to get list of pods running. You should see one head and worker node
like below:

```sh
kubectl get pods
# NAME                                READY   STATUS    RESTARTS   AGE
# ha-cluster-head                     1/1     Running   0          2m36s
# ha-cluster-small-wg-worker-22lbx    1/1     Running   0          2m36s
```

### Create an Actor

Before we try to trigger the restoration, we need to find a way to validate our GCS restore
is working correctly. We will validate this by creating a detached actor. If it still
exists and functions after the head node deletion and restoration, we can confirm that the
GCS data is restored correctly.

Run following command for creating a detached actor. Please change `ha-cluster-head` to
your head node's name:

```sh
kubectl exec -it ha-cluster-head -- python3 /home/ray/samples/detached_actor.py
```

Then, open a new termianl and use port-forward to enable accessing to the Ray dashboard.
The dashboard can be accessed through `http://localhost:8265`:

```sh
kubectl port-forward pod/ha-cluster-head 8265:8265
```

In the dashboard, You can see 2 nodes in the Cluster pane, which is head and worker:

![hacluster-dashboard-cluster](img/hacluster-dashboard-cluster.png)

If you go to the Actor pane, you can see the actor we created earlier:

![hacluster-dashboard-actor](img/hacluster-dashboard-actor.png)

### Trigger the GCS restore

To trigger the restoration, we can simply delete the head node with:

```sh
kubectl delete pods ha-cluster-head
```

If you list the pods now, you can see a new head node is recreated

```sh
kubectl get pods
# NAME                                READY   STATUS    RESTARTS   AGE
# ha-cluster-head                     0/1     Running   0          5s
# ha-cluster-small-wg-worker-tpgqs    1/1     Running   0          9m19s
```

Note that only head node will be recreated, while the worker node stays as is.

Port-forward again and access the dashboard through `http://localhost:8265`:

```sh
kubectl port-forward pod/ha-cluster-head 8265:8265
```

You can see one pod marked as "DEAD" in the Cluster pane and the actor in Actors pane
still running.

### Clean up

```sh
make clean-cluster
# Remove apiserver from helm
helm uninstall kuberay-apiserver
```

[RedisYAML]: test/cluster/redis/redis.yaml
[Secret]: test/cluster/redis/redis_passwrd.yaml
[ConfigMap]: test/cluster/code_configmap.yaml
