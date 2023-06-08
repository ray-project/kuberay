# Frequently Asked Questions

Welcome to the Frequently Asked Questions page for Kuberay. This document addresses common inquiries. If you don't find an answer to your question here, please don't hesitate to connect with us via our [community channels](https://github.com/ray-project/kuberay#getting-involved).

## Contents

- [How can I disable the default worker init container injection?](#how-can-i-disable-the-default-worker-init-container-injection)
- [How to troubleshoot the worker init container failure?](#how-to-troubleshoot-the-worker-init-container-failure)
- [How can I set a custom cluster domain?](#how-can-i-set-a-custom-cluster-domain)
- [Why don't changes to the RayCluster/RayJob definition apply to the running RayCluster/RayJob?](#why-dont-changes-to-the-rayclusterrayjob-definition-apply-to-the-running-rayclusterrayjob)

## Questions

### How can I disable the default worker init container injection?

The default worker init container is described [here](https://github.com/ray-project/kuberay/blob/2de3fe5ca3cf206c4ebb9912e128295e5cc5db45/ray-operator/controllers/ray/common/pod.go#L207). To disable this injection, set the `ENABLE_INIT_CONTAINER_INJECTION` environment variable in the Kuberay operator to `false`(only for version after 0.5.0). For Helm chart users, this adjustment can be made [here](https://github.com/ray-project/kuberay/blob/2de3fe5ca3cf206c4ebb9912e128295e5cc5db45/helm-chart/kuberay-operator/values.yaml#L74). After disabling, remember to add your own init container to the worker pod template. More details are provided in [PR 1069](https://github.com/ray-project/kuberay/pull/1069).

### How to troubleshoot the worker init container failure?

Please note that for version 0.5.0, the worker init container will not output any logs. To troubleshoot, follow these steps:
- Check the head pod logs for any errors about GCS server.
- Verify that Ray is in the `$PATH`.
- The worker init container shares the same ImagePullPolicy, SecurityContext, Env, VolumeMounts, and Resources as the worker pod template. Check these settings, as any that require a sidecar container could lead to deadlock. See [issue 1130](https://github.com/ray-project/kuberay/issues/1130) for more details.
- If the previous steps don't resolve your issue, disable the default worker init container injection and add your init container to the worker pod template. You can then execute you own test code and view logs with `kubectl logs -f <worker> <init-container-name>`.

### How can I set a custom cluster domain?

To customize the cluster domain (default is `cluster.local`), adjust the `CLUSTER_DOMAIN` environment variable in the Kuberay operator. Helm chart users can make this adjustment [here](https://github.com/ray-project/kuberay/blob/master/helm-chart/kuberay-operator/values.yaml#L78).

### Why don't changes to the RayCluster/RayJob CR apply to the running RayCluster/RayJob?

Currently, only modifications to `replicas` field in `RayCluster/RayJob` CR are supported. Changes to other fields may not take effect or could lead to unexpected results.
