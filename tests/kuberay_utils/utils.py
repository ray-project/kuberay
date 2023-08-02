#!/usr/bin/env python
from string import Template

import logging
import time
import tempfile
import yaml

from framework.prototype import (
    RayClusterAddCREvent,
    RayServiceFullCREvent
)

from framework.utils import (
    get_head_pod,
    CONST,
    K8S_CLUSTER_MANAGER
)

logger = logging.getLogger(__name__)
logging.basicConfig(
    format='%(asctime)s,%(msecs)d %(levelname)-8s [%(filename)s:%(lineno)d] %(message)s',
    datefmt='%Y-%m-%d:%H:%M:%S',
    level=logging.INFO
)

def is_feature_supported(ray_version, feature):
    """Return True if `feature` is supported in `ray_version`"""
    if ray_version == "nightly":
        return True
    major, minor, _ = [int(s) for s in ray_version.split('.')]
    if feature in [CONST.RAY_FT, CONST.RAY_SERVICE]:
        return major * 100 + minor > 113
    # Before Ray 2.6.0, there was a bug in Ray Serve that caused the
    # test_ray_serve to be quite unstable. Therefore, we only run
    # `test_ray_serve` for Ray versions 2.6.0 and later.
    if feature == CONST.RAY_SERVE_FT:
        return major * 100 + minor >= 206
    return False

def create_ray_cluster(template_name, ray_version, ray_image):
    """Create a RayCluster and a NodePort service."""
    context = {}
    with open(template_name, encoding="utf-8") as ray_cluster_template:
        template = Template(ray_cluster_template.read())
        yamlfile = template.substitute(
            {'ray_image': ray_image, 'ray_version': ray_version}
        )
    with tempfile.NamedTemporaryFile('w', suffix = '_ray_cluster_yaml') as ray_cluster_yaml:
        ray_cluster_yaml.write(yamlfile)
        ray_cluster_yaml.flush()
        context['filepath'] = ray_cluster_yaml.name

        for k8s_object in yaml.safe_load_all(yamlfile):
            if k8s_object['kind'] == 'RayCluster':
                context['cr'] = k8s_object
                break

        # Create a RayCluster
        ray_cluster_add_event = RayClusterAddCREvent(
            custom_resource_object = context['cr'],
            rulesets = [],
            timeout = 90,
            namespace='default',
            filepath = context['filepath']
        )
        ray_cluster_add_event.trigger()
        return ray_cluster_add_event

def create_ray_service(template_name, ray_version, ray_image):
    """Create a RayService without a NodePort service."""
    context = {}
    with open(template_name, encoding="utf-8") as ray_service_template:
        template = Template(ray_service_template.read())
        yamlfile = template.substitute(
            {'ray_image': ray_image, 'ray_version': ray_version}
        )
    with tempfile.NamedTemporaryFile('w', suffix = '_ray_service_yaml') as ray_service_yaml:
        ray_service_yaml.write(yamlfile)
        ray_service_yaml.flush()
        context['filepath'] = ray_service_yaml.name

        for k8s_object in yaml.safe_load_all(yamlfile):
            if k8s_object['kind'] == 'RayService':
                context['cr'] = k8s_object
                break

        # Create a RayService
        ray_service_add_event = RayServiceFullCREvent(
            custom_resource_object = context['cr'],
            rulesets = [],
            timeout = 90,
            namespace='default',
            filepath = context['filepath']
        )
        ray_service_add_event.trigger()
        return ray_service_add_event

def wait_for_condition(
        condition_predictor, timeout=10, retry_interval_ms=100, **kwargs
):
    """Wait until a condition is met or time out with an exception.

    Args:
        condition_predictor: A function that predicts the condition.
        timeout: Maximum timeout in seconds.
        retry_interval_ms: Retry interval in milliseconds.

    Raises:
        RuntimeError: If the condition is not met before the timeout expires.
    """
    start = time.time()
    last_ex = None
    while time.time() - start <= timeout:
        try:
            if condition_predictor(**kwargs):
                return
        except Exception as ex:
            last_ex = ex
        time.sleep(retry_interval_ms / 1000.0)
    message = "The condition wasn't met before the timeout expired."
    if last_ex is not None:
        message += f" Last exception: {last_ex}"
    raise RuntimeError(message)

def wait_for_new_head(old_head_pod_name, old_restart_count, namespace, timeout, retry_interval_ms):
    """
    `wait_for_new_head` is used to wait for new head is ready and running. For example, `test_detached_actor` kills
    the gcs_server process on the head pod. It takes nearly 1 min to kill the head pod, and the head pod will still
    be in 'Running' and 'Ready' in that minute.
    
    Hence, we need to check `restart_count` or `new_head_pod_name`.
    (1) `restart_count`: If the pod is restarted by the restartPolicy of a Pod, `restart_count` will increase by 1.
                         If the pod is deleted by KubeRay and the reconciler creates a new one, `restart_count` will be 0.
    (2) `new_head_pod_name`: If the reconciler creates a new head pod, `new_head_pod_name` will be different from
                             `old_head_pod_name`.

    Next, we check the status of pods to ensure all pods should be "Running" and "Ready".

    After the cluster state converges (all pods are "Running" and "Ready"), ray processes still need tens of seconds to
    become ready to serve new connections from ray clients. So, users need to retry until a Ray client connection succeeds.

    Args:
        old_head_pod_name: Name of the old head pod.
        old_restart_count: If the Pod is restarted by Kubernetes Pod RestartPolicy, the restart_count will increase by 1.
        namespace: Namespace that the head pod is running in.
        timeout: Same as `wait_for_condition`.
        retry_interval_ms: Same as `wait_for_condition`.

    Raises:
        RuntimeError: If the condition is not met before the timeout expires, raise the RuntimeError.
    """
    k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
    def check_status(old_head_pod_name, old_restart_count, namespace) -> bool:
        all_pods = k8s_v1_api.list_namespaced_pod(namespace = namespace)
        headpod = get_head_pod(namespace)
        new_head_pod_name = headpod.metadata.name
        new_restart_count = headpod.status.container_statuses[0].restart_count
        # The default container restartPolicy of a Pod is `Always`. Hence, when GCS server is killed,
        # the head pod will restart the old one rather than create a new one.
        if new_head_pod_name != old_head_pod_name:
            logger.info(f'If GCS server is killed, the head pod will restart the old one rather than create a new one.' +
                f' new_head_pod_name: {new_head_pod_name}, old_head_pod_name: {old_head_pod_name}')
            # TODO (kevin85421): We should `return False` here, but currently ray:nightly has a high possibility to create
            #                    a new head pod instead of restarting the old one.

        # When GCS server is killed, it takes nearly 1 min to kill the head pod. In the minute, the head
        # pod will still be in 'Running' and 'Ready'. Hence, we need to check `restart_count`.
        else:
            # TODO (kevin85421): We should remove `else` in the future. Currently, ray:nightly has a high possibility to
            #                    create a new head pod instead of restarting the old one. The new pod's `restart_count`
            #                    is 0.
            if new_restart_count != old_restart_count + 1:
                logger.info(f'new_restart_count != old_restart_count + 1 => new_restart_count: {new_restart_count},' +
                    f' old_restart_count: {old_restart_count}')
                return False
        # All pods should be "Running" and "Ready". This check is an overkill. We added this check due to
        # the buggy behaviors of Ray HA. To elaborate, when GCS server is killed, the head pod should restart,
        # but worker pods should not. However, currently, worker pods will also restart.
        # See https://github.com/ray-project/kuberay/issues/634 for more details.
        for pod in all_pods.items:
            if pod.status.phase != 'Running':
                logger.info(f'Pod {pod.metadata.name} is not Running.')
                return False
            for c in pod.status.container_statuses:
                if not c.ready:
                    logger.info(f'Container {c.name} in {pod.metadata.name} is not ready.')
                    return False
        return True
    wait_for_condition(check_status, timeout=timeout, retry_interval_ms=retry_interval_ms,
        old_head_pod_name=old_head_pod_name, old_restart_count=old_restart_count, namespace=namespace)
    # After the cluster state converges, ray processes still need tens of seconds to become ready.
    # TODO (kevin85421): Make ray processes become ready when pods are "Ready" and "Running".
