# Frequently Asked Questions

Welcome to the Frequently Asked Questions page for Kuberay. This document addresses common inquiries.
If you don't find an answer to your question here, please don't hesitate to connect with us via our [community channels](https://github.com/ray-project/kuberay#getting-involved).

## Contents

- [I do not want to use the default worker init container, How can I disable the auto injection and add my owns?](#i-do-not-want-to-use-the-default-worker-init-container-how-can-i-disable-the-auto-injection-and-add-my-owns)
- [Why are my worker pods stuck in `Init:0/1` status?](#why-are-my-worker-pods-stuck-in-init01-status)
- [How can I set the custom cluster domain if mine is not `cluster.local`?](#how-can-i-set-the-custom-cluster-domain-if-mine-is-not-clusterlocal)
- [Why are my changes to RayCluster/RayJob CR not taking effect?](#why-are-my-changes-to-rayclusterrayjob-cr-not-taking-effect)


## Questions

### I do not want to use the default worker init container, How can I disable the auto injection and add my owns?

The default worker init container is used to wait for the GCS server in the head pod to be ready. It is defined [here](https://github.com/ray-project/kuberay/blob/master/ray-operator/controllers/ray/common/pod.go#L207). To disable the injection, set the `ENABLE_INIT_CONTAINER_INJECTION` environment variable in the Kuberay operator to `false` (applicable only for versions after 0.5.0). Helm chart users can make this change [here](https://github.com/ray-project/kuberay/blob/master/helm-chart/kuberay-operator/values.yaml#L74). Once disabled, you can add your custom init container to the worker pod template. More details can be found in [PR 1069](https://github.com/ray-project/kuberay/pull/1069).

### Why are my worker pods stuck in `Init:0/1` status?

Worker pods might be stuck in `Init:0/1` status for several reasons. The default worker init container only progresses when the GCS server in the head pod is ready. Here are some common causes for the issue:
- The GCS server process failed in the head pod. Inspect the head pod logs for errors related to the GCS server.
- Ray is not included in the `$PATH` in the worker init container. Ensure Ray is in the `$PATH`.
- The worker init container shares the same ImagePullPolicy, SecurityContext, Env, VolumeMounts, and Resources as the worker pod template. Any setting requiring a sidecar container could lead to a deadlock. Refer to [issue 1130](https://github.com/ray-project/kuberay/issues/1130) for additional details.  
If none of the above reasons apply, you can troubleshoot by disabling the default worker init container injection, adding your test init container to the worker pod template.

### How can I set the custom cluster domain if mine is not `cluster.local`?

To set a custom cluster domain, adjust the `CLUSTER_DOMAIN` environment variable in the Kuberay operator. Helm chart users can make this modification [here](https://github.com/ray-project/kuberay/blob/master/helm-chart/kuberay-operator/values.yaml#L78).

### Why are my changes to RayCluster/RayJob CR not taking effect?

Currently, only modifications to the `replicas` field in `RayCluster/RayJob` CR are supported. Changes to other fields may not take effect or could lead to unexpected results.
