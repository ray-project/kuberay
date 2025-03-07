# Creating Autoscaling clusters using API server

One of the fundamental features of Ray is autoscaling. This [document] describes how to set up
autoscaling using Ray operator. Here we will describe how to set it up using API server.

## Deploy KubeRay operator and API server

Refer to [readme](README.md) for setting up KubRay operator and API server.

```shell
make operator-image cluster load-operator-image deploy-operator
```

Alternatively, you could build and deploy the Operator and API server from local repo for
development purpose.

```shell
make operator-image cluster load-operator-image deploy-operator docker-image load-image deploy
```

Additionally install this [ConfigMap] containing code that we will use for testing.

## Deploy Ray cluster

Once they are set up, you first need to create a Ray cluster using the following commands:

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

## Validate that Ray cluster is deployed correctly

Run:

```shell
kubectl get pods
```

You should get something like this:

```shell
test-cluster-head-pr25j             2/2     Running   0          2m49s
```

Note that only head pod is running and it has 2 containers

## Trigger RayCluster scale-up

Create a detached actor:

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

Because we have specified `num_cpu: 0` for head node, this will cause creation of a worker node. Run:

```shell
kubectl get pods
```

You should get something like this:

```shell
test-cluster-head-pr25j              2/2     Running   0          15m
test-cluster-worker-small-wg-qrjfm   1/1     Running   0          2m48s
```

You can see that a worker node have been created.

## Trigger RayCluster scale-down

Run:

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

A worker Pod will be deleted after `idleTimeoutSeconds` (default 60s, we specified 30) seconds. Run:

```shell
kubectl get pods
```

And you should see only head node (worker node is deleted)

```shell
test-cluster-head-pr25j             2/2     Running   0          27m
```

[document]: https://docs.ray.io/en/latest/cluster/kubernetes/user-guides/configuring-autoscaling.html
[ConfigMap]: test/cluster/cluster/detachedactor.yaml
