# Frequently Asked Questions

Welcome to the Frequently Asked Questions page for KubeRay. This document addresses common inquiries.
If you don't find an answer to your question here, please don't hesitate to connect with us via our [community channels](https://github.com/ray-project/kuberay#getting-involved).

## Contents
- [Worker Init Container](#worker-init-container)
- [cluster domain](#cluster-domain)
- [RayService](#rayservice)

### Worker Init Container

When starting a RayCluster, the worker Pod needs to wait until the head Pod is started in order to connect to the head successfully.
To achieve this, the KubeRay operator will automatically inject an [init container](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/) into the worker Pod to wait for the head Pod to be ready before starting the worker container. The init container will continuously check if the head's GCS server is ready or not.

Related questions:
- [Why are my worker Pods stuck in `Init:0/1` status, how can I troubleshoot the worker init container?](#why-are-my-worker-pods-stuck-in-init01-status-how-can-i-troubleshoot-the-worker-init-container)
- [I do not want to use the default worker init container, how can I disable the auto-injection and add my own?](#i-do-not-want-to-use-the-default-worker-init-container-how-can-i-disable-the-auto-injection-and-add-my-own)

### Cluster Domain

Each Kubernetes cluster is assigned a unique cluster domain during installation. This domain helps differentiate between names local to the cluster and external names. The `cluster_domain` can be customized as outlined in the [Kubernetes documentation](https://kubernetes.io/docs/tasks/administer-cluster/dns-custom-nameservers/#introduction). The default value for `cluster_domain` is `cluster.local`.

The cluster domain plays a critical role in service discovery and inter-service communication within the cluster. It is part of the Fully Qualified Domain Name (FQDN) for services within the cluster. See [here](https://github.com/kubernetes/website/blob/main/content/en/docs/concepts/services-networking/dns-pod-service.md#aaaaa-records-1) for examples. In the context of KubeRay, workers use the FQDN of the head service to establish a connection to the head.

Related questions:
- [How can I set a custom cluster domain if mine is not `cluster.local`?](#how-can-i-set-a-custom-cluster-domain-if-mine-is-not-clusterlocal)

### RayService

RayService is a Custom Resource Definition (CRD) designed for Ray Serve. In KubeRay, creating a RayService will first create a RayCluster and then
create Ray Serve applications once the RayCluster is ready. If the issue pertains to the data plane, specifically your Ray Serve scripts 
or Ray Serve configurations (`serveConfigV2`), troubleshooting may be challenging. See [rayservice-troubleshooting](https://github.com/ray-project/kuberay/blob/master/docs/guidance/rayservice-troubleshooting.md) for more details.

## Questions

### Why are my worker Pods stuck in `Init:0/1` status, how can I troubleshoot the worker init container?

Worker Pods might be stuck in `Init:0/1` status for several reasons. The default worker init container only progresses when the GCS server in the head Pod is ready. Here are some common causes for the issue:
- The GCS server process failed in the head Pod. Inspect the head Pod logs for errors related to the GCS server.
- Ray is not included in the `$PATH` in the worker init container. The init container uses `ray health-check` to check the GCS server status.
- The cluster domain is not set correctly. See [cluster-domain](#cluster-domain) for more details. The init container uses the Fully Qualified Domain Name (FQDN) of the head service to connect to the GCS server.
- The worker init container shares the same ImagePullPolicy, SecurityContext, Env, VolumeMounts, and Resources as the worker Pod template. Any setting requiring a sidecar container could lead to a deadlock. Refer to [issue 1130](https://github.com/ray-project/kuberay/issues/1130) for additional details.

If none of the above reasons apply, you can troubleshoot by disabling the default worker init container injection and adding your test init container to the worker Pod template.


### I do not want to use the default worker init container, how can I disable the auto-injection and add my own?

The default worker init container is used to wait for the GCS server in the head Pod to be ready. It is defined [here](https://github.com/ray-project/kuberay/blob/master/ray-operator/controllers/ray/common/pod.go#L207). To disable the injection, set the `ENABLE_INIT_CONTAINER_INJECTION` environment variable in the KubeRay operator to `false` (applicable only for versions after 0.5.0). Helm chart users can make this change [here](https://github.com/ray-project/kuberay/blob/master/helm-chart/kuberay-operator/values.yaml#L74). Once disabled, you can add your custom init container to the worker Pod template. More details can be found in [PR 1069](https://github.com/ray-project/kuberay/pull/1069).


### How can I set the custom cluster domain if mine is not `cluster.local`?

To set a custom cluster domain, adjust the `CLUSTER_DOMAIN` environment variable in the KubeRay operator. Helm chart users can make this modification [here](https://github.com/ray-project/kuberay/blob/master/helm-chart/kuberay-operator/values.yaml#L78).

### Why are my changes to RayCluster/RayJob CR not taking effect?

Currently, only modifications to the `replicas` field in `RayCluster/RayJob` CR are supported. Changes to other fields may not take effect or could lead to unexpected results.
