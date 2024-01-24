#!/usr/bin/env python
from string import Template

import logging
import time
import tempfile
import yaml

from framework.prototype import RayClusterAddCREvent

from framework.utils import (
    get_head_pod,
    CONST,
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
            timeout = 180,
            namespace='default',
            filepath = context['filepath']
        )
        ray_cluster_add_event.trigger()
        return ray_cluster_add_event

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

def wait_for_new_head(mode, old_head_pod_name, old_restart_count, namespace, timeout, retry_interval_ms):
    """
    `wait_for_new_head` is used to wait for the head Pod is ready and running.

    [Case 1]
    KILL_GCS_SERVER: The head Pod should be restarted rather than creating a new head Pod.
    [Case 2]
    KILL_HEAD_POD: The new head Pod should be created.

    Args:
        mode: KILL_GCS_SERVER or KILL_HEAD_POD.
        old_head_pod_name: Name of the old head pod.
        old_restart_count: The restart count of the old head pod.
        namespace: Namespace that the head pod is running in.
        timeout: Same as `wait_for_condition`.
        retry_interval_ms: Same as `wait_for_condition`.

    Raises:
        RuntimeError: Raise a RuntimeError if a timeout occurs.
    """
    def check_status(mode, old_head_pod_name, old_restart_count, namespace) -> bool:
        headpod = get_head_pod(namespace)
        if headpod is None:
            logger.info(
                "There is no head Pod. We will only check the following conditions " +
                "after the head Pod is created."
            )
            return False
        new_head_pod_name = headpod.metadata.name
        new_restart_count = headpod.status.container_statuses[0].restart_count

        logger.info("Failure mode: %s", mode)
        if mode == CONST.RESTART_OLD_POD:
            if new_head_pod_name != old_head_pod_name:
                logger.warning(
                    "GCS server process is killed. The head Pod should be restarted "
                    "rather than creating a new head Pod. There is something wrong. "
                    "new_head_pod_name: %s, old_head_pod_name: %s",
                    new_head_pod_name, old_head_pod_name
                )
                return False
            if new_restart_count != old_restart_count + 1:
                logger.info(
                    "new_restart_count != old_restart_count + 1 =>"
                    "new_restart_count: %s; old_restart_count: %s",
                    new_restart_count, old_restart_count
                )
                return False
        elif mode == CONST.CREATE_NEW_POD:
            if new_head_pod_name == old_head_pod_name:
                logger.info("The old head Pod %s is not killed.", old_head_pod_name)
                return False
        else:
            raise ValueError(f"Invalid failure mode: {mode}")

        if headpod.status.phase != "Running":
            logger.info(
                "The head Pod %s is not running. The status is %s",
                headpod.metadata.name, headpod.status.phase
            )
            return False
        for container_status in headpod.status.container_statuses:
            if not container_status.ready:
                logger.info(
                    "The container %s is not ready. The status is %s",
                    container_status.name, container_status.ready
                )
                return False
        return True
    wait_for_condition(
        check_status, timeout=timeout, retry_interval_ms=retry_interval_ms,
        mode=mode, old_head_pod_name=old_head_pod_name, old_restart_count=old_restart_count,
        namespace=namespace
        )
    # After the cluster state converges, ray processes still need tens of seconds to become ready.
    # TODO (kevin85421): Make ray processes become ready when pods are "Ready" and "Running".
