# Ray Kubernetes Operator (experimental)

This is a variation implementation of [Design 1B](https://docs.google.com/document/d/1DPS-e34DkqQ4AeJpoBnSrUM8SnHnQVkiLlcmI4zWEWg/edit?ts=5f906e13#heading=h.825wx4vpnxmb) discussed with the Ray community

![overview](media/overview.png)

NOTE: The documentation is still in progress and incomplete. The operator is still under active development. Please see [the documentation](https://docs.ray.io/en/latest/deploy-on-kubernetes.html#deploying-on-kubernetes) for current best practices.

This directory contains the source code for a Ray operator for Kubernetes.

The operator makes deploying and managing Ray clusters on top of Kubernetes painless - clusters are defined as a custom RayCluster resource and managed by a fault-tolerant Ray controller.
The Ray Operator is a Kubernetes operator to automate provisioning, management, autoscaling and operations of Ray clusters deployed to Kubernetes.

Some of the main features of the operator are:
- Management of first-class RayClusters via a [custom resource](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/#custom-resources).
- Support for hetergenous worker types in a single Ray cluster.
- Built-in monitoring via Prometheus.
- Use of `PodTemplate` to create Ray pods
- Updated status based on the running pods
- Events added to the `RayCluster` instance
- Automatically populate `environment variables` in the containers
- Automatically prefix your container command with the `ray start` command
- Automatically adding the volumeMount at `/dev/shm` for shared memory
- Use of `ScaleStartegy` to remove specific nodes in specific groups


## Usage

This section walks through how to build and deploy the operator in a running Kubernetes cluster.

### Requirements
software  | version | link
:-------------  | :---------------:| -------------:
kubectl |  v1.18.3+    | [download](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
go  | v1.13+|[download](https://golang.org/dl/)
docker   | 19.03+|[download](https://docs.docker.com/install/)

The instructions assume you have access to a running Kubernetes cluster via ``kubectl``. If you want to test locally, consider using [Minikube](https://kubernetes.io/docs/tasks/tools/install-minikube/).

### Running the unit tests


```
go build
go test ./... -cover
```

You can also build the operator using Bazel:

```generate BUILD.bazel
bazel run //:gazelle
```

```build script
bazel build //:ray-operator
```

#### Testing using Ginkgo
```
sudo apt install golang-ginkgo-dev
ginkgo ./controllers/
```

example results:
```
Running Suite: Controller Suite
===============================
Random Seed: 1605120291
Will run 6 of 6 specs

••••••

Ran 6 of 6 Specs in 5.068 seconds
SUCCESS! -- 6 Passed | 0 Failed | 0 Pending | 0 Skipped
PASS

Ginkgo ran 1 suite in 6.968063881s
Test Suite Passed
```

### Building the controller

The first step to deploying the Ray operator is building the container image that contains the operator controller and pushing it to Docker Hub so it can be pulled down and run in the Kubernetes cluster.

```shell script
# From the ray/deploy/ray-operator directory.
# Replace DOCKER_ACCOUNT with your docker account or push to your preferred Docker image repository.
docker build -t $DOCKER_ACCOUNT:controller .
docker push $DOCKER_ACCOUNT:controller
```

In the future (once the operator is stabilized), an official controller image will be uploaded and available to users on Docker Hub.

### Installing the custom resource definition

The next step is to install the RayCluster custom resource definition into the cluster. Build and apply the CRD:

```shell script
kubectl kustomize config/crd | kubectl apply -f -
```

Refer to [raycluster_types.go](api/v1alpha1/raycluster_types.go) and [ray.io_rayclusters.yaml](config/crd/bases/ray.io_rayclusters.yaml) for the details of the CRD.

### Deploying the controller

First, modify the controller config to use the image you build. Replace "controller" in config/manager/kustomization.yaml with the name of your image. Then, build the controller config and apply it to the cluster:

```shell script
kubectl kustomize config/manager | kubectl apply -f -
```

```shell script
# Check that the controller is running.
$ kubectl get pods  -n system
NAME                                               READY   STATUS    RESTARTS   AGE
ray-manager-66b9b97bcf-8l6lt   2/2     Running   0          30s

$ kubectl get deployments -n system
NAME                              READY   UP-TO-DATE   AVAILABLE   AGE
ray-manager-manager   1/1     1            1           44m

# Delete the controller if need be.
$ kubectl delete deployment ray-operator-controller-manager -n system
```

### Running an example cluster

There are three example config files to deploy RayClusters included here:

Sample  | Description
------------- | -------------
[ray-cluster.mini.yaml](config/samples/ray-cluster.mini.yaml)   | Small example consisting of 1 head pod.
[ray-cluster.heterogeneous.yaml](config/samples/ray-cluster.heterogeneous.yaml)  | Example with heterogenous worker types. 1 head pod and 2 worker pods, each of which has a different resource quota.
[ray-cluster.complete.yaml](config/samples/ray-cluster.complete.yaml)  | Shows all available custom resouce properties.


```shell script
# Create a configmap with a hello world Ray code.
kubectl create -f config/samples/config-map-ray-code.yaml
configmap/ray-code created
```


```shell script
# Create a cluster.
$ kubectl create -f config/samples/ray-cluster.heterogeneous.yaml
raycluster.ray.io/raycluster-heterogeneous created

# List running clusters.
$ kubectl get rayclusters
NAME                AGE
raycluster-heterogeneous   2m48s

# The created cluster should include a head pod, worker pod, and a head service.
$ kubectl get pods
NAME                                                 READY   STATUS    RESTARTS   AGE
raycluster-heterogeneous-head-5r6qr                  1/1     Running   0          14m
raycluster-heterogeneous-worker-medium-group-ljzzt   1/1     Running   0          14m
raycluster-heterogeneous-worker-small-group-76qxb    1/1     Running   0          14m
raycluster-heterogeneous-worker-small-group-dcl4d    1/1     Running   0          14m
```
```shell
$ kubectl get services
NAME                     TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)    AGE
kubernetes                        ClusterIP   10.152.183.1   <none>        443/TCP   35d
raycluster-heterogeneous-my-svc   ClusterIP   None           <none>        80/TCP    15m
```

The logs of the head pod should show 4 nodes in the Ray cluster
```
Ray Nodes:  {'10.1.73.139', '10.1.73.138', '10.1.73.140', '10.1.73.141'}
```

```shell
# check the logs of the head pod
$ kubectl logs raycluster-heterogeneous-head-5r6qr
2020-11-18 09:23:32,069 INFO services.py:1092 -- View the Ray dashboard at http://10.1.73.141:8265
2020-11-18 09:23:31,668 INFO scripts.py:467 -- Local node IP: 10.1.73.141
2020-11-18 09:23:32,093 SUCC scripts.py:497 -- --------------------
2020-11-18 09:23:32,093 SUCC scripts.py:498 -- Ray runtime started.
2020-11-18 09:23:32,093 SUCC scripts.py:499 -- --------------------
2020-11-18 09:23:32,093 INFO scripts.py:501 -- Next steps
2020-11-18 09:23:32,093 INFO scripts.py:503 -- To connect to this Ray runtime from another node, run
2020-11-18 09:23:32,093 INFO scripts.py:507 --   ray start --address='10.1.73.141:6379' --redis-password='LetMeInRay'
2020-11-18 09:23:32,093 INFO scripts.py:509 -- Alternatively, use the following Python code:
2020-11-18 09:23:32,093 INFO scripts.py:512 -- import ray
2020-11-18 09:23:32,093 INFO scripts.py:519 -- ray.init(address='auto', _redis_password='LetMeInRay')
2020-11-18 09:23:32,093 INFO scripts.py:522 -- If connection fails, check your firewall settings and network configuration.
2020-11-18 09:23:32,093 INFO scripts.py:526 -- To terminate the Ray runtime, run
2020-11-18 09:23:32,094 INFO scripts.py:527 --   ray stop
2020-11-18 09:23:32,656 INFO worker.py:651 -- Connecting to existing Ray cluster at address: 10.1.73.141:6379
2020-11-18 09:23:32,669 WARNING services.py:202 -- Some processes that the driver needs to connect to have not registered with Redis, so retrying. Have you run 'ray start' on this node?
trying to connect to Ray!
now executing some code with Ray!
Ray Nodes:  {'10.1.73.139', '10.1.73.138', '10.1.73.140', '10.1.73.141'}
Execution time =  6.961702346801758
```
```
# Delete the cluster.
$ kubectl delete raycluster raycluster-heterogeneous
```
