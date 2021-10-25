# Ray Cluster

Make sure ray-operator has been deployed.

[Ray](https://ray.io/) is An open source framework that provides a simple, universal API for building distributed applications. Ray is packaged with RLlib, a scalable reinforcement learning library, and Tune, a scalable hyperparameter tuning library.

## Helm

```console
$ helm version
version.BuildInfo{Version:"v3.6.2", GitCommit:"ee407bdf364942bcb8e8c665f82e15aa28009b71", GitTreeState:"dirty", GoVersion:"go1.16.5"}
```

## TL;DR;

```console
helm install --name ray-cluster-ant . --values values.yaml --namespace default
```

## Installing the Chart

To install the chart with the release name `my-release`:


```console
helm install --name sample ray-cluster-ant --values ray-cluster-ant/values.yaml --namespace ray-operator
```

> note: The chart will submit a RayCluster. 


## Uninstalling the Chart

To uninstall/delete the `my-release` deployment:

```console
helm delete ray-cluster-ant
```

The command removes nearly all the Kubernetes components associated with the
chart and deletes the release.

## Configuration

The following table lists the configurable parameters of the raycluster charts, and their default values.

| Parameter                                         | Description                                                                                                                                         | Default                       |
| ------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------- |
| `images.repository`                               | RayCluster image                                                                                                                              | `reg.docker.alibaba-inc.com/antfin_datatech_share/ray_serving_package_prod`       |
| `images.tag`                                      | RayCluster image tag                                                                                                                          | `2a6dad6738b7046d422c53a32a7aee06f1e22765`                       |
| `images.pullPolicy`                               | RayCluster image pull secret                                                                                                                  | `Always`                            |
| `images.casRepo`                                  | RayCluster **CAS** image, for scaling                                                                                                                  | `reg.docker.alibaba-inc.com/onlinedata/clusterapiserver`                |
| `images.casTag`                                   | RayCluster **CAS** image tag                                                                                                                               | `20200507`       |
| `imagePullSecrets`                                | RayCluster imagePullSecrets                                                                                                                   | `[]`                       |
| `nameOverride`                                    | Helm nameOverride                                                                                                                               | `ray`    |
| `fullnameOverride`                                | Helm fullnameOverride                                                                                                                         | ``             |
| `head.groupName`                                  | RayCluster head groupName                                                                                                                   | `headgroup`                |
| `head.replicas`                                   | RayCluster head replicas                                                                                                                             | `1` |
| `head.type`                                   | RayCluster head type                                                                                                                             | `head` |
| `head.labels`                                   | RayCluster head labels                                                                                                                             | `` |
| `head.initArgs`                                   | RayCluster head initArgs                                                                                                                             | `` |
| `head.containerEnv`                                   | RayCluster head containerEnv                                                                                                                             | `[]` |
| `head.resources`                                   | RayCluster head resources                                                                                                                             | `` |
| `head.annotations`                                   | RayCluster head annotations                                                                                                                             | `` |
| `head.nodeSelector`                                   | RayCluster head nodeSelector                                                                                                                             | `{}` |
| `head.tolerations`                                   | RayCluster head tolerations                                                                                                                             | `[]` |
| `head.affinity`                                   | RayCluster head affinity                                                                                                                             | `{}` |
| `head.volumes`                                   | RayCluster head volumes                                                                                                                             | `[]` |
| `head.volumeMounts`                                   | RayCluster head volumeMounts                                                                                                                             | `[]` |
| `worker.groupName`                                  | RayCluster worker groupName                                                                                                                   | `workergroup`                |
| `worker.replicas`                                   | RayCluster worker replicas                                                                                                                             | `1` |
| `worker.type`                                   | RayCluster worker type                                                                                                                             | `worker` |
| `worker.labels`                                   | RayCluster worker labels                                                                                                                             | `` |
| `worker.initArgs`                                   | RayCluster worker initArgs                                                                                                                             | `` |
| `worker.containerEnv`                                   | RayCluster worker containerEnv                                                                                                                             | `[]` |
| `worker.resources`                                   | RayCluster worker resources                                                                                                                             | `` |
| `worker.annotations`                                   | RayCluster worker annotations                                                                                                                             | `` |
| `worker.nodeSelector`                                   | RayCluster worker nodeSelector                                                                                                                             | `{}` |
| `worker.tolerations`                                   | RayCluster worker tolerations                                                                                                                             | `[]` |
| `worker.affinity`                                   | RayCluster worker affinity                                                                                                                             | `{}` |
| `worker.volumes`                                   | RayCluster worker volumes                                                                                                                             | `[]` |
| `worker.volumeMounts`                                   | RayCluster worker volumeMounts                                                                                                                             | `[]` |
| `headServiceSuffix`                                   | RayCluster head dns suffix                                                                                                                              | `"ray-operator.svc"` |
| `service.type`                                   | Test service type                                                                                                                              | `ClusterIP` |
| `service.port`                                   |  Test service port                                                                                                                        | `8080` |


## Check Cluster status

### Get Service 

```console
$ kubectl get svc -l ray.io/cluster=ray-cluster-ant
NAME                       TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)                       AGE
ray-cluster-ant-head-svc   ClusterIP   10.103.36.68   <none>        10001/TCP,6379/TCP,8265/TCP   9m24s
```

```console
$curl -X POST http://ray-cluster-ant-head-svc.{namespace}.svc:30021/is_ready
{"result": true, "msg": "OK", "data": {"is_ready": true}}

$curl -X POST http://ray-cluster-ant-head-svc.ray-operator.svc.cluster.local:30021/is_ready
{"result": true, "msg": "OK", "data": {"is_ready": true}}
```





## Forward to dashboard

```console
$ kubectl get pod -o wide
NAME                                       READY   STATUS    RESTARTS   AGE    IP            NODE             NOMINATED NODE   READINESS GATES
ray-cluster-ant-head-sd77l                 1/1     Running   0          8h     10.1.61.208   docker-desktop   <none>           <none>
ray-cluster-ant-worker-workergroup-czxd6   1/1     Running   0          8h     10.1.61.207   docker-desktop   <none>           <none>
ray-operator-ant-687785b964-jgfhv          1/1     Running   6          3d4h   10.1.61.196   docker-desktop   <none>           <none>

$ kubectl port-forward ray-cluster-ant-head-sd77l 8265
Forwarding from 127.0.0.1:8265 -> 8265
Forwarding from [::1]:8265 -> 8265
```
