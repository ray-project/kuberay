# Creating HA cluster with API Server

One of the issue for long-running Ray applications, for example, Ray Serve is that Ray Head node is a single
point of failure, which means that if the Head node dies, complete cluster has to be restarted. Fortunately,
KubeRay cluster provides an option to create
[fault tolerance Ray cluster](https://docs.ray.io/en/master/cluster/kubernetes/user-guides/kuberay-gcs-ft.html).
The similar type of highly available Ray cluster can also be created using API server. The foundation of this
approach is ensuring high availability Global Control Service (GCS) data. GCS manages cluster-level
metadata. By default, the GCS lacks fault tolerance as it stores all data in-memory, and a failure can cause the
entire Ray cluster to fail. To make the GCS fault tolerant, you must have a high-availability Redis. This way,
in the event of a GCS restart, it retrieves all the data from the Redis instance and resumes its regular
functioning.

## Creating external Redis cluster

A comprehensive documentation on creating Redis cluster on Kubernetes can be found
[here]( https://www.dragonflydb.io/guides/redis-kubernetes). For this example we will use a rather simple
[yaml file](test/cluster/redis/redis.yaml). To create Redis run:

```sh
kubectl create ns redis
kubectl apply -f <your location>/kuberay/apiserver/test/cluster/redis/redis.yaml -n redis
```

Note that here we are deploying redis to the `redis` namespace, that we are creating here.

Alternatively, if you run on the cloud you can use managed version of HA Redis, which will not require
you to stand up, run, manage and monitor your own version of redis.

## Creating Redis password secret

Before creating your cluster, you need to create [secret](test/cluster/redis/redis_passwrd.yaml) in the
namespace where you are planning to create your Ray cluster (remember, that secret is visible only within a given
namespace). To create a secret for using external redis, run:

```sh
kubectl apply -f <your location>/kuberay/apiserver/test/cluster/redis/redis_passwrd.yaml
```

## Ray Code for testing

For both Ray Jobs and Ray Serve we recommend packaging user code in the image. For a simple testing here
we will create a [config map](test/cluster/code_configmap.yaml), containing simple code, that we will use for
testing. To deploy it run the following:

```sh
kubectl apply -f <your location>/kuberay/apiserver/test/cluster/code_configmap.yaml
```

## API server request

To create a Ray cluster we can use the following curl command:

```sh
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

Note that computeTemplate here has to be created using this [command](test/cluster//template/simple)

Lets discuss the important pieces here:
You need to specify annotation, that tells Ray that this is cluster with GCS fault tolerance

```sh
ray.io/ft-enabled: "true"
```

For the `headGroupSpec` you need the following. In the `rayStartParams` you need to add information about Redis
password.

```sh
"redis-password:: "$REDIS_PASSWORD"
"num-cpu": "0"
```

Where the value of `REDIS_PASSWORD` comes from environment variable (below). Additionally `num-cpus:
0` ensures that no application code runs on a head node.

The following environment variable have to be added here:

```sh
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

For the `workerGroupSpecs` you might want to increase `gcs_rpc_server_reconnect_timeout` by specifying the following
environment variable:

```sh
        "environment": {
           "values": {
             "RAY_gcs_rpc_server_reconnect_timeout_s": "300"
           }
        },
```

This environment variable allows to increase GCS heartbeat timeout, which is 60 sec by default. The reason for
increasing it is because restart of the head node can take some time, and we want to make sure that the worker node
will not be killed during this time.

## Testing resulting cluster

Once the cluster is created, we can validate that it is working correctly. To do this first create a detached actor.
To do this, note the name of the head node and create a detached actor using the following command:

```sh
kubectl exec -it <head node pod name> -- python3 /home/ray/samples/detached_actor.py
```

Once this is done, open Ray dashboard (using port-forward). In the cluster tab you should see 2 nodes and in the
Actor's pane you should see created actor.

Now you can delete head node pod:

```sh
kubectl delete pods <head node pod name>
```

The operator will recreate it. Make sure that only head node is recreated (note that it now has a different name),
while worker node stays as is. Now you can go to the dashboard and make sure that in the Cluster tab you still see
2 nodes and in the Actor's pane you still see created actor.

For additional test run the following command:

```sh
kubectl exec -it <head node pod name> -- python3 /home/ray/samples/increment_counter.py
```

and make sure that it executes correctly. Note that the name of the head node here is different
