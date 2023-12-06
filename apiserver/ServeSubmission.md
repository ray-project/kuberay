# Managing RayServe with the API server

Ray Serve is a scalable model serving library for building online inference APIs. This document describes creation and management of Ray Serve enabled Ray clusters

## Creating a cluster with RayServe support

Up until rescently the only way to create a Ray cluster supporting RayServe was by using `Create ray service` APIs. Although it does work, quite often you want to create cluster supporting Ray serve so that you can experiment with serve APIs directly. Now it is possible by adding the following annotation to the cluster:

```json
"annotations" : {
    "ray.io/enable-serve-service": "true"
  },
```

the complete curl command to creation such cluster is as follows:

```shell
curl -X POST 'localhost:8888/apis/v1/namespaces/default/clusters' \
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
      "image": "rayproject/ray:2.8.0-py310",
      "serviceType": "ClusterIP",
      "rayStartParams": {
         "dashboard-host": "0.0.0.0",
         "metrics-export-port": "8080",
         "dashboard-agent-listen-port": "52365"
       }
    },
    "workerGroupSpec": [
      {
        "groupName": "small-wg",
        "computeTemplate": "default-template",
        "image": "rayproject/ray:2.8.0-py310",
        "replicas": 1,
        "minReplicas": 0,
        "maxReplicas": 5,
        "rayStartParams": {
           "node-ip-address": "$MY_POD_IP"
         }
      }
    ]
  }
}'
```

Note, that before creating a cluster you need to create default template using the following command:

```shell
curl -X POST 'localhost:8888/apis/v1/namespaces/default/compute_templates' \
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

As you can see, in this case two services are created - one for the head node to be able to see the dashboard and configure the cluster and one for submission of the serve requests.

For the head node service note that the additional port - 52365 is created for serve configuration.

## Using Serve submission APIs

Current implementation is based on this Ray [documentation](https://docs.ray.io/en/latest/serve/api/index.html#serve-rest-api) and provides three methods:

* SubmitServeApplications - declaratively deploys a list of Serve applications. If Serve is already running on the Ray cluster, removes all applications not listed in the new config. If Serve is not running on the Ray cluster, starts Serve. List of applications is defined by yaml file (passed as string) and defined [here](https://docs.ray.io/en/latest/serve/production-guide/config.html).
* GetServeApplications - gets cluster-level info and comprehensive details on all Serve applications deployed on the Ray cluster. Definition of the return data is [here](../proto/serve_submission.proto)
* DeleteRayServeApplications - shuts down Serve and all applications running on the Ray cluster. Has no effect if Serve is not running on the Ray cluster.

Currently API server does not provide support for serving ML models. Using an API server to support this functionality will negatively impact scalability and performance of model serving. As a result we decided not to support this functionality currently.

### Create Serve applications

Once the cluster is up and running, you can submit a job to the cluster using the following command:

```shell
curl -X POST 'localhost:8888/apis/v1/namespaces/default/serveapplication/test-cluster' \
--header 'Content-Type: application/json' \
--data '{
  "configyaml": "applications:\n  - name: fruit_app\n    import_path: fruit.deployment_graph\n    route_prefix: /fruit\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: MangoStand\n        num_replicas: 1\n        user_config:\n          price: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: OrangeStand\n        num_replicas: 1\n        user_config:\n          price: 2\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: PearStand\n        num_replicas: 1\n        user_config:\n          price: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: FruitMarket\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: DAGDriver\n        num_replicas: 1\n        ray_actor_options:\n          num_cpus: 0.1\n  - name: math_app\n    import_path: conditional_dag.serve_dag\n    route_prefix: /calc\n    runtime_env:\n      working_dir: \"https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip\"\n    deployments:\n      - name: Adder\n        num_replicas: 1\n        user_config:\n          increment: 3\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Multiplier\n        num_replicas: 1\n        user_config:\n          factor: 5\n        ray_actor_options:\n          num_cpus: 0.1\n      - name: Router\n        num_replicas: 1\n      - name: create_order\n        num_replicas: 1\n      - name: DAGDriver\n        num_replicas: 1\n"
}'
```

### Get Serve applications

Once the serve applcation is submitted, the following command can be used to get applications details.

```shell
curl -X GET 'localhost:8888/apis/v1/namespaces/default/serveapplication/test-cluster' \
--header 'Content-Type: application/json' 
```

This should return JSON similar to the one below

### Delete Serve applications

Finally, you can delete serve applications using the following command:

```shell
curl -X DELETE 'localhost:8888/apis/v1/namespaces/default/serveapplication/test-cluster' \
--header 'Content-Type: application/json' 
```

You can validate job deletion by looking at the Ray dashboard (jobs pane) and ensuring that it was removed

## Managing RayServe with the API server vs RayService CRD

[RayService CRD](https://docs.ray.io/en/latest/cluster/kubernetes/getting-started/rayservice-quick-start.html#kuberay-rayservice-quickstart) provides many important features, including:

* In-place updating for Ray Serve applications: See RayService for more details.
* Zero downtime upgrading for Ray clusters: See RayService for more details.
* High-availabilable services: See [RayCluter high availability](HACluster.md) for more details.

So why implement this support? Several reasons:

* It is more convinient in development. You can create a cluster and then deploy/undeploy applications until you are happy with results.
* You can create Ray cluster with the set of features that you want, including [high availabilty](HACluster.md), [autoscaling support](), etc. You can choose cluster configuration differently for testing vs production. Moreover, all of this can be done using [Python](../clients/python-apiserver-client/python_apiserver_client)
* When it comes to upgrading Ray cluster or model in production, using in place update is dangerous. Preffered way of doing it is usage of [traffic splitting](https://gateway-api.sigs.k8s.io/guides/traffic-splitting/), more specifically [canary deployments](https://codefresh.io/learn/software-deployment/what-are-canary-deployments/). This allows to validate new deployments on a small percentage of data, easily rolling back in the case of issues. Managing RayServe with the API server gives one all the basic tools for such implementation and combined with, for example [gateway APIs](https://gateway-api.sigs.k8s.io/) can provide a complete solution for updates management, which is more powerful that the current CRD.
