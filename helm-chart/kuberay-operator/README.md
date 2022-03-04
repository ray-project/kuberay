# Ray Operator

Kuberay-operator: A simple Helm chart

Run a deployment of Ray Operator.

Deploy ray operator first, then deploy ray cluster.

## Helm

Make sure helm version is v3+
```console
$ helm version
version.BuildInfo{Version:"v3.6.2", GitCommit:"ee407bdf364942bcb8e8c665f82e15aa28009b71", GitTreeState:"dirty", GoVersion:"go1.16.5"}
```

## Installing the Chart

To avoid duplicate CRD definitions in this repo, we reuse CRD config in `ray-operator`:
```console
$ kubectl apply -k "../../ray-operator/config/crd"
```

Please use command below:
```console
$ helm install kuberay-operator . --values values.yaml --namespace kuberay-operator --create-namespace
```
## List the Chart

To list the `my-release` deployment:

```console
$ helm list -n kuberay-operator
```

## Uninstalling the Chart

To uninstall/delete the `my-release` deployment:

```console
$ helm delete kuberay-operator -n kuberay-operator
```

The command removes nearly all the Kubernetes components associated with the
chart and deletes the release.
