# Ray Operator

Ray-Operator-Ant: A simple Helm chart

Run a deployment of Ray Operator.

先执行ray operator再执行ray cluster

## Helm

Make sure helm version is v3+
```console
$ helm version
version.BuildInfo{Version:"v3.6.2", GitCommit:"ee407bdf364942bcb8e8c665f82e15aa28009b71", GitTreeState:"dirty", GoVersion:"go1.16.5"}
```



## Installing the Chart

Please use command below:
```console
$ helm install  ray-operator-ant . --values values.yaml --namespace default --create-namespace
```
## List the Chart

To list the `my-release` deployment:

```console
$ helm list -n default
```

## Add the label to namespace to make operator watch
The operator only watches the raycluster in these namespaces who has this label
```console
$ kubectl label ns ray-operator operators.sigma.alipay.com/ray-operator=true
```

## Uninstalling the Chart

To uninstall/delete the `my-release` deployment:

```console
$ helm delete ray-operator-ant
```

The command removes nearly all the Kubernetes components associated with the
chart and deletes the release.

## Configuration

The following table lists the configurable parameters of the raycluster charts, and their default values.

| Parameter                                         | Description                                                                                                                                         | Default                       |
| ------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------- |
| `replicaCount`                               | Ray Operator replicas                                                                                                                              | `1`       |
| `images.repository`                               | Ray Operator image repository                                                                                                                             | `reg.docker.alibaba-inc.com/onlinedata/ray-operator-namespaced-manager`       |
| `images.tag`                                      | Ray Operator image tag                                                                                                                          | `99082aa513a65f3ba15cde19ef5311e0c3595790`                       |
| `images.pullPolicy`                               | RayCluster image pull secret                                                                                                                  | `Always`                            |
| `imagePullSecrets`                                | RayCluster imagePullSecrets                                                                                                                   | `[]`                       |
| `nameOverride`                                    | Helm nameOverride                                                                                                                               | `ray-operator-ant`    |
| `fullnameOverride`                                | Helm fullnameOverride                                                                                                                         | `ray-operator-ant`             |
| `rbac.create`                                  |  Specifies whether Ray Operator and Cluster rbac should be created                                                                                                                | `true`                |
| `rbac.apiVersion`                                  |  Ray Operator and Cluster rbac version                                                                                                                | `v1`                |
| `serviceAccount.create`                                   |  Specifies whether a service account should be created                                                                                                                       | `true` |
| `serviceAccount.name`                                   |  Specifies whether a service account should be created                                                                                                                       | `ray-operator` |
| `service.type`                                   | Test service type                                                                                                                              | `ClusterIP` |
| `service.port`                                   |  Test service port                                                                                                                        | `8080` |
| `resources.limits.cpu`                                   |  Ray Operator resources.limits.cpu                                                                                                                   | `100m` |
| `resources.limits.memory`                                   |  Ray Operator resources.limits.memory                                                                                                                 | `128Mi` |
| `resources.requests.cpu`                                   |  Ray Operator resources.requests.cpu                                                                                                                   | `100m` |
| `resources.requests.memory`                                   |  Ray Operator resources.requests.memory                                                                                                                   | `128Mi` |
| `createCustomResource`                                   |  Specifies whether Ray Operator and Cluster CRD should be created                                                                                                                   | `true` |
| `rbacEnable`                                   |  Specifies whether role binding should be created                                                                                                                   | `true` |

