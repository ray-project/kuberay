# Ray Job (alpha)

> Note: This is the alpha version of Ray Job Support in KubeRay. There will be ongoing improvements for Ray Job in the future releases.

## Prerequisites

* Ray 1.10 or higher
* KubeRay v0.3.0+. (v0.5.0+ is recommended)

## What is a RayJob?

A RayJob manages 2 things:

* Ray Cluster: Manages resources in a Kubernetes cluster.
* Job: Manages jobs in a Ray Cluster.

### What does the RayJob provide?

* **Kubernetes-native support for Ray clusters and Ray Jobs.** You can use a Kubernetes config to define a Ray cluster and job, and use `kubectl` to create them. The cluster can be deleted automatically once the job is finished.

## Deploy KubeRay

Make sure your KubeRay operator version is at least v0.3.0.
The latest released KubeRay version is recommended.
For installation instructions, please follow [the documentation](../deploy/installation.md).

## Run an example Job

There is one example config file to deploy a RayJob included here:
[ray_v1alpha1_rayjob.yaml](https://github.com/ray-project/kuberay/blob/master/ray-operator/config/samples/ray_v1alpha1_rayjob.yaml)

```shell
# Create a RayJob.
$ kubectl apply -f config/samples/ray_v1alpha1_rayjob.yaml
```

```shell
# List running RayJobs.
$ kubectl get rayjob
NAME            AGE
rayjob-sample   7s
```

```shell
# RayJob sample will also create a raycluster.
# raycluster will create few resources including pods and services. You can use the following commands to check them:
$ kubectl get rayclusters
$ kubectl get pod
```

## RayJob Configuration

* `entrypoint` - The shell command to run for this job.
* `rayClusterSpec` - The spec for the Ray cluster to run the job on.
* `jobId` - _(Optional)_ Job ID to specify for the job. If not provided, one will be generated.
* `metadata` - _(Optional)_ Arbitrary user-provided metadata for the job.
* `runtimeEnv` - _(Optional)_ base64-encoded string of the runtime env json string.
* `shutdownAfterJobFinishes` - _(Optional)_ whether to recycle the cluster after the job finishes.
* `ttlSecondsAfterFinished` - _(Optional)_ TTL to clean up the cluster. This only works if `shutdownAfterJobFinishes` is set.
* `submitterPodTemplate` - _(Optional)_ Pod template spec for the pod that runs `ray job submit` against the Ray cluster.

## RayJob Observability

You can use `kubectl logs` to check the operator logs or the head/worker nodes logs.
You can also use `kubectl describe rayjobs rayjob-sample` to check the states and event logs of your RayJob instance:

```text
Status:
  Dashboard URL:          rayjob-sample-raycluster-vnl8w-head-svc.ray-system.svc.cluster.local:8265
  End Time:               2022-07-24T02:04:56Z
  Job Deployment Status:  Complete
  Job Id:                 test-hehe
  Job Status:             SUCCEEDED
  Message:                Job finished successfully.
  Ray Cluster Name:       rayjob-sample-raycluster-vnl8w
  Ray Cluster Status:
    Available Worker Replicas:  1
    Endpoints:
      Client:          32572
      Dashboard:       32276
      Gcs - Server:    30679
    Last Update Time:  2022-07-24T02:04:43Z
    State:             ready
  Start Time:          2022-07-24T02:04:49Z
Events:
  Type    Reason     Age   From               Message
  ----    ------     ----  ----               -------
  Normal  Created    90s   rayjob-controller  Created cluster rayjob-sample-raycluster-vnl8w
  Normal  Submitted  82s   rayjob-controller  Submit Job test-hehe
  Normal  Deleted    15s   rayjob-controller  Deleted cluster rayjob-sample-raycluster-vnl8w
```

If the job doesn't run successfully, the above `describe` command will provide information about that too:

```text
Status:
  Dashboard URL:          rayjob-sample-raycluster-nrdm8-head-svc.ray-system.svc.cluster.local:8265
  End Time:               2022-07-24T02:01:39Z
  Job Deployment Status:  Complete
  Job Id:                 test-hehe
  Job Status:             FAILED
  Message:                Job failed due to an application error, last available logs:
python: can't open file '/tmp/code/script.ppy': [Errno 2] No such file or directory

  Ray Cluster Name:  rayjob-sample-raycluster-nrdm8
  Ray Cluster Status:
    Available Worker Replicas:  1
    Endpoints:
      Client:          31852
      Dashboard:       32606
      Gcs - Server:    32436
    Last Update Time:  2022-07-24T02:01:30Z
    State:             ready
  Start Time:          2022-07-24T02:01:38Z
Events:
  Type    Reason     Age   From               Message
  ----    ------     ----  ----               -------
  Normal  Created    2m9s  rayjob-controller  Created cluster rayjob-sample-raycluster-nrdm8
  Normal  Submitted  2m    rayjob-controller  Submit Job test-hehe
  Normal  Deleted    58s   rayjob-controller  Deleted cluster rayjob-sample-raycluster-nrdm8
```

## Delete the RayJob instance

```shell
kubectl delete -f config/samples/ray_v1alpha1_rayjob.yaml
```
