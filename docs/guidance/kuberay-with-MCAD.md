<!-- markdownlint-disable MD013 -->
# KubeRay integration with MCAD (Multi-Cluster-App-Dispatcher)

The multi-cluster-app-dispatcher is a Kubernetes controller providing mechanisms for applications to manage batch jobs in a single or multi-cluster environment. For more details please refer [here](https://github.com/project-codeflare/multi-cluster-app-dispatcher).

## Use case

MCAD allows you to deploy Ray cluster with a guarantee that sufficient resources are available in the cluster prior to actual pod creation in the Kubernetes cluster. It supports features such as:

- Integrates with upstream Kubernetes scheduling stack for features such co-scheduling, Packing on GPU dimension etc.
- Ability to wrap any Kubernetes objects.
- Increases control plane stability by JIT (Just-in Time) object creation.
- Queuing with policies.
- Quota management that goes across namespaces.
- Support for multiple Kubernetes clusters; dispatching jobs to any one of a number of Kubernetes clusters.

In order to queue Ray cluster(s) and `gang dispatch` them when aggregated resources are available please create a KinD cluster using the [instruction](#create-kind-cluster) below and then refer to the setup [KubeRay-MCAD integration](https://github.com/project-codeflare/multi-cluster-app-dispatcher/blob/main/doc/usage/examples/kuberay/kuberay-mcad.md) on a Kubernetes Cluster or an OpenShift Cluster.

On OpenShift, MCAD and KubeRay are already part of the Open Data Hub Distributed Workload Stack. The stack provides a simple, user-friendly abstraction for scaling, queuing and resource management of distributed AI/ML and Python workloads. Please follow the Quick Start in the [Distributed Workloads](https://github.com/opendatahub-io/distributed-workloads) for installation.

## Create KinD cluster

 We need a KinD cluster with the specified cluster resources to consistently observe the expected behavior described in the [demo](#submitting-kuberay-cluster-to-mcad) below. This can be done with running KinD with [Podman](https://podman.io/docs/installation).

> Note: Without Podman, a KinD worker node is allowed to see the cpu/memory resources on the host. In addition, this environment is created to run the tutorial on a resource-constrained local Kubernetes environment. It is not recommended for real workloads or production.

```bash
podman machine init --cpus 8 --memory 8196
podman machine start
podman machine list
```

Expect the Podman Machine running with the follow CPU and MEMORY resources

```text
NAME                     VM TYPE     CREATED        LAST UP            CPUS        MEMORY      DISK SIZE
podman-machine-default*  qemu        2 minutes ago  Currently running  8           8.594GB     107.4GB
```

Create KinD cluster on the Podman Machine:

```bash
KIND_EXPERIMENTAL_PROVIDER=podman kind create cluster
```

Creating a KinD cluster should take less than 1 minute. Expect the output similar to:

```console
using podman due to KIND_EXPERIMENTAL_PROVIDER
enabling experimental podman provider
Creating cluster "kind" ...
 ✓ Ensuring node image (kindest/node:v1.26.3) 🖼
 ✓ Preparing nodes 📦
 ✓ Writing configuration 📜
 ✓ Starting control-plane 🕹️
 ✓ Installing CNI 🔌
 ✓ Installing StorageClass 💾
Set kubectl context to "kind-kind"
You can now use your cluster with:

kubectl cluster-info --context kind-kind

Have a nice day! 👋
```

Describe the single node cluster:

```console
kubectl describe node kind-control-plane
```

Expect the `cpu` and `memory` in the `Allocatable` section to be similar to:

```text
Allocatable:
  cpu:            8
  hugepages-1Gi:  0
  hugepages-2Mi:  0
  memory:         8118372Ki
  pods:           110
```

## Submitting KubeRay cluster to MCAD

After the KinD cluster is created using the instruction above, make sure to install the [KubeRay-MCAD integration Prerequisites](https://github.com/project-codeflare/multi-cluster-app-dispatcher/blob/main/doc/usage/examples/kuberay/kuberay-mcad.md#prerequisites) for KinD cluster.

Let's create two RayClusters using the AppWrapper custom resource(CR) on the same Kubernetes cluster. The AppWrapper is the custom resource definition provided by MCAD to dispatch resources and manage batch jobs on Kubernetes clusters.

- We submit the first RayCluster with the AppWrapper CR [aw-raycluster.yaml](https://github.com/project-codeflare/multi-cluster-app-dispatcher/blob/main/doc/usage/examples/kuberay/config/aw-raycluster.yaml):

  ```bash
  kubectl create -f https://raw.githubusercontent.com/project-codeflare/multi-cluster-app-dispatcher/main/doc/usage/examples/kuberay/config/aw-raycluster.yaml
  ```

  In the above AppWrapper CR, we wrapped an example of [RayCluster CR](https://github.com/ray-project/kuberay/blob/master/ray-operator/config/samples/ray-cluster.complete.yaml) in the `generictemplate`. We also specified matching resources for each of the RayCluster Head node and worker node in the `custompodresources`. The MCAD uses the `custompodresources` to reserve the required resources to run the RayCluster without creating pending Pods.

  > Note: Within the same AppWrapper, you may also wrap any individual k8s resources (i.e. configMap, secret, etc) associated with this job as a generictemplate to be dispatched together with the RayCluster.

  Check AppWrapper status by describing the job.

  ```console
  kubectl describe appwrapper raycluster-complete -n default
  ```

  The `Status:` stanza would show the `State` of `Running` if the wrapped RayCluster has been deployed. The 2 Pods associated with the RayCluster were also created.

  ```text
  Status:
    Canrun:  true
    Conditions:
      Last Transition Micro Time:  2023-08-29T02:50:18.829462Z
      Last Update Micro Time:      2023-08-29T02:50:18.829462Z
      Status:                      True
      Type:                        Init
      Last Transition Micro Time:  2023-08-29T02:50:18.829496Z
      Last Update Micro Time:      2023-08-29T02:50:18.829496Z
      Reason:                      AwaitingHeadOfLine
      Status:                      True
      Type:                        Queueing
      Last Transition Micro Time:  2023-08-29T02:50:18.842010Z
      Last Update Micro Time:      2023-08-29T02:50:18.842010Z
      Reason:                      FrontOfQueue.
      Status:                      True
      Type:                        HeadOfLine
      Last Transition Micro Time:  2023-08-29T02:50:18.902379Z
      Last Update Micro Time:      2023-08-29T02:50:18.902379Z
      Reason:                      AppWrapperRunnable
      Status:                      True
      Type:                        Dispatched
    Controllerfirsttimestamp:      2023-08-29T02:50:18.829462Z
    Filterignore:                  true
    Queuejobstate:                 Dispatched
    Sender:                        before manageQueueJob - afterEtcdDispatching
    State:                         Running
  Events:                          <none>
  (base) asmalvan@mcad-dev:~/mcad-kuberay$ kubectl get pod -n default
  NAME                                           READY   STATUS    RESTARTS   AGE
  raycluster-complete-head-9s4x5                 1/1     Running   0          47s
  raycluster-complete-worker-small-group-4s6jv   1/1     Running   0          47s
  ```

- Let's submit another RayCluster with the AppWrapper CR and see it queued without creating pending Pods using the command:

  ```bash
  kubectl create -f https://raw.githubusercontent.com/project-codeflare/multi-cluster-app-dispatcher/main/doc/usage/examples/kuberay/config/aw-raycluster-1.yaml
  ```

  Check the raycluster-complete-1 AppWrapper

  ```console
  kubectl describe appwrapper raycluster-complete-1 -n default
  ```

  The `Status:` stanza should show the `State` of `Pending` if the wrapped object (RayCluster) has been queued. No pods from the second `AppWrapper` were created due to `Insufficient resources to dispatch AppWrapper`.

  ```text
  Status:
    Conditions:
      Last Transition Micro Time:  2023-08-29T17:39:08.406401Z
      Last Update Micro Time:      2023-08-29T17:39:08.406401Z
      Status:                      True
      Type:                        Init
      Last Transition Micro Time:  2023-08-29T17:39:08.406452Z
      Last Update Micro Time:      2023-08-29T17:39:08.406451Z
      Reason:                      AwaitingHeadOfLine
      Status:                      True
      Type:                        Queueing
      Last Transition Micro Time:  2023-08-29T17:39:08.423208Z
      Last Update Micro Time:      2023-08-29T17:39:08.423208Z
      Reason:                      FrontOfQueue.
      Status:                      True
      Type:                        HeadOfLine
      Last Transition Micro Time:  2023-08-29T17:39:08.439753Z
      Last Update Micro Time:      2023-08-29T17:39:08.439753Z
      Message:                     Insufficient resources to dispatch AppWrapper.
      Reason:                      AppWrapperNotRunnable.
      Status:                      True
      Type:                        Backoff
    Controllerfirsttimestamp:      2023-08-29T17:39:08.406399Z
    Filterignore:                  true
    Queuejobstate:                 Backoff
    Sender:                        before ScheduleNext - setHOL
    State:                         Pending
  Events:                          <none>
  ```

We may manually check the allocated resources:

```bash
kubectl describe node kind-control-plane
```

The `Allocated resources` section showed cpu Requests as 6050m(75%) therefore the remaining cpu resource did not satisfy the second AppWrapper.

```text
Allocated resources:
  (Total limits may be over 100 percent, i.e., overcommitted.)
  Resource           Requests         Limits
  --------           --------         ------
  cpu                6050m (75%)      5200m (65%)
  memory             6824650Ki (84%)  6927050Ki (85%)
  ephemeral-storage  0 (0%)           0 (0%)
  hugepages-1Gi      0 (0%)           0 (0%)
  hugepages-2Mi      0 (0%)           0 (0%)
```

Dispatching policy out of the box is FIFO which can be augmented as per user needs. The second RayCluster will be dispatched when additional aggregated resources are available in the cluster or the first AppWrapper is deleted.

For example, observe the other RayCluster been created after deleting the first AppWrapper using:

```bash
kubectl delete appwrapper raycluster-complete -n default
```

> Note: This would also simultaneously remove any K8s resources you may have wrapped as generictemplates within this AppWrapper.
