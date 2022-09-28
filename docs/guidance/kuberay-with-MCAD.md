# Kuberay integration with MCAD (Multi-Cluster-App-Dispatcher)

The multi-cluster-app-dispatcher is a Kubernetes controller providing mechanisms for applications to manage batch jobs in a single or multi-cluster environment. For more details please refer [here](https://github.com/IBM/multi-cluster-app-dispatcher).

## Use case

MCAD allows you to deploy Ray cluster with a guarantee that sufficient resources are available in the cluster prior to actual pod creation in the Kubernetes cluster. It supports features such as:
   
- Integrates with upstream Kubernetes scheduling stack for features such co-scheduling, Packing on GPU dimension etc.
- Ability to wrap any Kubernetes objects.
- Increases control plane stability by JIT (Just-in Time) object creation.
- Queuing with policies.
- Quota management that goes across namespaces.
- Support for multiple Kubernetes clusters; dispatching jobs to any one of a number of Kubernetes clusters.


In order to queue Ray cluster(s) and `gang dispatch` them when aggregated resources are available please refer to the setup [Kuberay-MCAD integration](https://github.com/IBM/multi-cluster-app-dispatcher/blob/quota-management/doc/usage/examples/kuberay/kuberay-mcad.md) with configuration files [here](https://github.com/IBM/multi-cluster-app-dispatcher/tree/quota-management/doc/usage/examples/kuberay/config).

## Submitting kuberay cluster to MCAD

Let's submit two Ray clusters on the same Kubernetes cluster.

- Assuming you have installed all the pre-requisites mentioned in the [Kuberay-MCAD integration](https://github.com/IBM/multi-cluster-app-dispatcher/blob/quota-management/doc/usage/examples/kuberay/kuberay-mcad.md), we submit the first Ray cluster using command `kubectl create -f aw-raycluster.yaml` using config file [here](https://github.com/IBM/multi-cluster-app-dispatcher/blob/quota-management/doc/usage/examples/kuberay/config/aw-raycluster.yaml).

```
  Conditions:
    Last Transition Micro Time:  2022-09-27T21:07:34.252275Z
    Last Update Micro Time:      2022-09-27T21:07:34.252273Z
    Status:                      True
    Type:                        Init
    Last Transition Micro Time:  2022-09-27T21:07:34.252535Z
    Last Update Micro Time:      2022-09-27T21:07:34.252534Z
    Reason:                      AwaitingHeadOfLine
    Status:                      True
    Type:                        Queueing
    Last Transition Micro Time:  2022-09-27T21:07:34.261174Z
    Last Update Micro Time:      2022-09-27T21:07:34.261174Z
    Reason:                      FrontOfQueue.
    Status:                      True
    Type:                        HeadOfLine
    Last Transition Micro Time:  2022-09-27T21:07:34.316208Z
    Last Update Micro Time:      2022-09-27T21:07:34.316208Z
    Reason:                      AppWrapperRunnable
    Status:                      True
    Type:                        Dispatched
  Controllerfirsttimestamp:      2022-09-27T21:07:34.251877Z
  Filterignore:                  true
  Queuejobstate:                 Dispatched
  Sender:                        before manageQueueJob - afterEtcdDispatching
  State:                         Running
Events:                          <none>
(base) asmalvan@mcad-dev:~/mcad-kuberay$ kubectl get pods
NAME                                               READY   STATUS    RESTARTS   AGE
raycluster-autoscaler-1-head-9s4x5                 2/2     Running   0          47s
raycluster-autoscaler-1-worker-small-group-4s6jv   1/1     Running   0          47s
```

- As seen the cluster is dispatched and pods are running.

- Let's submit another Ray cluster and see it queued without creating pending pods using the command `kubectl create -f aw-raycluster.yaml`. To do this, change cluster name from `name: raycluster-autoscaler` to `name: raycluster-autoscaler-1` and re-submit

```
Conditions:
    Last Transition Micro Time:  2022-09-27T21:11:06.162080Z
    Last Update Micro Time:      2022-09-27T21:11:06.162080Z
    Status:                      True
    Type:                        Init
    Last Transition Micro Time:  2022-09-27T21:11:06.162401Z
    Last Update Micro Time:      2022-09-27T21:11:06.162401Z
    Reason:                      AwaitingHeadOfLine
    Status:                      True
    Type:                        Queueing
    Last Transition Micro Time:  2022-09-27T21:11:06.171619Z
    Last Update Micro Time:      2022-09-27T21:11:06.171618Z
    Reason:                      FrontOfQueue.
    Status:                      True
    Type:                        HeadOfLine
    Last Transition Micro Time:  2022-09-27T21:11:06.179694Z
    Last Update Micro Time:      2022-09-27T21:11:06.179689Z
    Message:                     Insufficient resources to dispatch AppWrapper.
    Reason:                      AppWrapperNotRunnable.
    Status:                      True
    Type:                        Backoff
  Controllerfirsttimestamp:      2022-09-27T21:11:06.161797Z
  Filterignore:                  true
  Queuejobstate:                 HeadOfLine
  Sender:                        before ScheduleNext - setHOL
  State:                         Pending
Events:                          <none>
```


- As seen the second Ray cluster is queued with no pending pods created. 

- Dispatching policy out of the box is FIFO which can be augmented as per user needs. The second cluster will be dispatched when additional aggregated resources are available in the cluster or the first AppWrapper Ray cluster is deleted.

