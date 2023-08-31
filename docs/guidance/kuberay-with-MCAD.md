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


## Submitting KubeRay cluster to MCAD

Let's create two RayCluster custom resources with the AppWrapper custom resource on the same Kubernetes cluster.

- Assuming you have installed all the pre-requisites mentioned in the [KubeRay-MCAD integration](https://github.com/project-codeflare/multi-cluster-app-dispatcher/blob/main/doc/usage/examples/kuberay/kuberay-mcad.md), we submit the first RayCluster with the AppWrapper CR [aw-raycluster.yaml](https://github.com/project-codeflare/multi-cluster-app-dispatcher/blob/main/doc/usage/examples/kuberay/config/aw-raycluster.yaml):

  ```bash
  kubectl create -f https://raw.githubusercontent.com/project-codeflare/multi-cluster-app-dispatcher/quota-management/doc/usage/examples/kuberay/config/aw-raycluster.yaml
  ```
  Check Appwrapper status by describing the job.
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
  The `Status:` stanza should show the `State` of `Pending` if the wrapped object (RayCluster) has been queued. No pods from the second `AppWrapper` were created.
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
    Sender:                        before [backoff] - Rejoining
    State:                         Pending
  Events:                          <none>
  ```

  Dispatching policy out of the box is FIFO which can be augmented as per user needs. The second cluster will be dispatched when additional aggregated resources are available in the cluster or the first AppWrapper Ray cluster is deleted.

