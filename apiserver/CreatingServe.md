# Creating a cluster with RayServe support

Up until rescently the only way to create a Ray cluster supporting RayServe was by using `Create ray
service` APIs. Although it does work, quite often you want to create cluster supporting Ray serve so
that you can experiment with serve APIs directly. Now it is possible by adding the following
annotation to the cluster:

```json
"annotations" : {
    "ray.io/enable-serve-service": "true"
  },
```

the complete curl command to creation such cluster is as follows:

```shell
curl -X POST 'localhost:31888/apis/v1/namespaces/default/clusters' \
  --header 'Content-Type: application/json' \
  --data '{
    "name": "test-cluster",
    "namespace": "default",
    "user": "boris",
    "annotations" : {
      "ray.io/enable-serve-service": "true"
    },
    "clusterSpec": {
      "headGroupSpec": {
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.9.0-py310",
        "serviceType": "ClusterIP",
        "rayStartParams": {
           "dashboard-host": "0.0.0.0",
           "metrics-export-port": "8080",
           "dashboard-agent-listen-port": "52365"
         },
         "volumes": [
           {
             "name": "code-sample",
             "mountPath": "/home/ray/samples",
             "volumeType": "CONFIGMAP",
             "source": "ray-job-code-sample",
             "items": {"sample_code.py" : "sample_code.py"}
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
             "node-ip-address": "$MY_POD_IP"
           },
          "volumes": [
            {
              "name": "code-sample",
              "mountPath": "/home/ray/samples",
              "volumeType": "CONFIGMAP",
              "source": "ray-job-code-sample",
              "items": {"sample_code.py" : "sample_code.py"}
            }
          ]
        }
      ]
    }
  }'
```

Note, that before creating a cluster you need to install this [configmap] and
create default template using the following command:

```shell
curl -X POST 'localhost:31888/apis/v1/namespaces/default/compute_templates' \
  --header 'Content-Type: application/json' \
  --data '{
    "name": "default-template",
    "namespace": "default",
    "cpu": 2,
    "memory": 4
  }'
```

To confirm that the cluster is created correctly, check created services using that following command:

```shell
kubectl get service
```

that should return the following:

```shell
test-cluster-head-svc    ClusterIP   10.96.19.185    <none>        8265/TCP,52365/TCP,10001/TCP,8080/TCP,6379/TCP,8000/TCP
test-cluster-serve-svc   ClusterIP   10.96.144.162   <none>        8000/TCP
```

As you can see, in this case two services are created - one for the head node to be able to see the
dashboard and configure the cluster and one for submission of the serve requests.

For the head node service note that the additional port - 52365 is created for serve configuration.

[configmap]: test/job/code.yaml
