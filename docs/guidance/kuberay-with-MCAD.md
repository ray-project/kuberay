# KubeRay integration with MCAD (Multi-Cluster-App-Dispatcher)

The multi-cluster-app-dispatcher is a Kubernetes controller providing mechanisms for applications to manage batch jobs in a single or multi-cluster environment. For more details please refer [here](https://github.com/IBM/multi-cluster-app-dispatcher).

## Use case

MCAD allows you to deploy Ray cluster with a guarantee that sufficient resources are available in the cluster prior to actual pod creation in the Kubernetes cluster. It supports features such as:

- Integrates with upstream Kubernetes scheduling stack for features such co-scheduling, Packing on GPU dimension etc.
- Ability to wrap any Kubernetes objects.
- Increases control plane stability by JIT (Just-in Time) object creation.
- Queuing with policies.
- Quota management that goes across namespaces.
- Support for multiple Kubernetes clusters; dispatching jobs to any one of a number of Kubernetes clusters.


In order to queue Ray cluster(s) and `gang dispatch` them when aggregated resources are available please refer to the setup [KubeRay-MCAD integration](https://github.com/project-codeflare/multi-cluster-app-dispatcher/blob/main/doc/usage/examples/kuberay/kuberay-mcad.md) on a Kubernetes Cluster or an OpenShift Cluster.

On OpenShift, MCAD and KubeRay are already part of the Open Data Hub Distributed Workload Stack. The stack provides a simple, user-friendly abstraction for scaling, queuing and resource management of distributed AI/ML and Python workloads. Please follow the Quick Start in the [Distributed Workloads](https://github.com/opendatahub-io/distributed-workloads) for installation.

## Create KinD cluster
In addition to the pre-requisites mentioned in the [KubeRay-MCAD integration](https://github.com/project-codeflare/multi-cluster-app-dispatcher/blob/main/doc/usage/examples/kuberay/kuberay-mcad.md), we need a KinD cluster with the specific resources. This can be done with running KinD with Podmam.
>Note: This environment is created to run the tutorial on a resource-constrained local Kubernetes environment. It is not recommended for real workloads or production.
```bash
podman machine init --cpus 4 --memory 8196
podman machine start
podman machine list
```
Expect the Podman Machine running with the follow CPU and MEMORY resources
```
NAME                     VM TYPE     CREATED        LAST UP            CPUS        MEMORY      DISK SIZE
podman-machine-default*  qemu        3 minutes ago  Currently running  4           8.389GB     107.4GB
```
Create KinD cluster on the Podman Machine:
```bash
KIND_EXPERIMENTAL_PROVIDER=podman kind create cluster
```
Creating a KinD cluster should take less than 1 minute. Expect the output similar to:
```
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
```
kubectl describe node kind-control-plane
```

Expect the `cpu` and `memroy` in the `Allocatable` section to be similar to:
```
Allocatable:
  cpu:            4
  hugepages-1Gi:  0
  hugepages-2Mi:  0
  memory:         7922976Ki
  pods:           110
```
## Submitting KubeRay cluster to MCAD

In additional to KinD cluster requirement above, make sure to install the other pre-requisites mentioned in the [KubeRay-MCAD integration](https://github.com/project-codeflare/multi-cluster-app-dispatcher/blob/main/doc/usage/examples/kuberay/kuberay-mcad.md). Let's create two RayCluster custom resources with the AppWrapper custom resource(CR) on the same Kubernetes cluster.

- We submit the first RayCluster with the AppWrapper CR [aw-raycluster.yaml](https://github.com/project-codeflare/multi-cluster-app-dispatcher/blob/main/doc/usage/examples/kuberay/config/aw-raycluster.yaml):

  ```bash
  kubectl create -f https://raw.githubusercontent.com/project-codeflare/multi-cluster-app-dispatcher/quota-management/doc/usage/examples/kuberay/config/aw-raycluster.yaml
  ```
  In the above AppWrapper CR, we wrapped an example of [RayCluster CR](https://github.com/ray-project/kuberay/blob/master/ray-operator/config/samples/ray-cluster.complete.yaml) in the `generictemplate`. We also specified matching resources for each of the RayCluster Head node and worker node in the `custompodresources`. The MCAD uses the `custompodresources` to reserve the required resources to run the RayCluster without creating pending Pods.

  Check AppWrapper status by describing the job.
  ```
  kubectl describe appwrapper raycluster-complete -n default
  ```

  The `Status:` stanza would show the `State` of `Running` if the wrapped RayCluster has been deployed. The 2 Pods associated with the RayCluster were also created.
  ```
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

- Let's submit another RayCluster with AppWrapper CR and see it queued without creating pending Pods using the command:
  ```bash
  kubectl create -f https://raw.githubusercontent.com/project-codeflare/multi-cluster-app-dispatcher/quota-management/doc/usage/examples/kuberay/config/aw-raycluster-1.yaml
  ```
  Check the raycluster-complete-1 AppWrapper
  ```
  kubectl describe appwrapper raycluster-complete-1 -n default
  ```
  The `Status:` stanza should show the `State` of `Pending` if the wrapped object (RayCluster) has been queued. No pods from the second `AppWrapper` were created due to `Insufficient resources to dispatch AppWrapper`.
  ```
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
The `Allocated resources` section showed cpu Requests as 3050m(76%) therefore the remaining cpu resource did not satisfy the second AppWrapper.
```
Allocated resources:
  (Total limits may be over 100 percent, i.e., overcommitted.)
  Resource           Requests          Limits
  --------           --------          ------
  cpu                3050m (76%)       2200m (55%)
  memory             4939865600 (60%)  5044723200 (62%)
  ephemeral-storage  0 (0%)            0 (0%)
  hugepages-1Gi      0 (0%)            0 (0%)
  hugepages-2Mi      0 (0%)            0 (0%)
```
Dispatching policy out of the box is FIFO which can be augmented as per user needs. The second cluster will be dispatched when additional aggregated resources are available in the cluster or the first AppWrapper Ray cluster is deleted.
