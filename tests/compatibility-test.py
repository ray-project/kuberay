#!/usr/bin/env python
import logging
import unittest
import os
import random
import string

import kuberay_utils.utils as utils
from framework.prototype import (
    EasyJobRule,
    show_cluster_info
)

from framework.utils import (
    delete_all_cr,
    get_head_pod,
    get_pod,
    pod_exec_command,
    shell_subprocess_run,
    CONST,
    K8S_CLUSTER_MANAGER,
    OperatorManager
)

logger = logging.getLogger(__name__)
logging.basicConfig(
    format='%(asctime)s,%(msecs)d %(levelname)-8s [%(filename)s:%(lineno)d] %(message)s',
    datefmt='%Y-%m-%d:%H:%M:%S',
    level=logging.INFO
)

# parse global variables from env
ray_image = os.getenv('RAY_IMAGE', 'rayproject/ray:2.9.0')
ray_version = ray_image.split(':')[-1]
kuberay_operator_image = os.getenv('OPERATOR_IMAGE')


class BasicRayTestCase(unittest.TestCase):
    """Test the basic functionalities of RayCluster by executing simple jobs."""
    cluster_template = CONST.REPO_ROOT.joinpath("tests/config/ray-cluster.mini.yaml.template")
    ray_cluster_ns = "default"

    @classmethod
    def setUpClass(cls):
        """Create a Kind cluster, a KubeRay operator, and a RayCluster."""
        K8S_CLUSTER_MANAGER.cleanup()
        K8S_CLUSTER_MANAGER.initialize_cluster()
        operator_manager = OperatorManager.instance()
        operator_manager.prepare_operator()
        utils.create_ray_cluster(BasicRayTestCase.cluster_template, ray_version, ray_image)

    def test_simple_code(self):
        """
        Run a simple example in the head Pod to test the basic functionality of the Ray cluster.
        The example is from https://docs.ray.io/en/latest/ray-core/walkthrough.html#running-a-task.
        """
        headpod = get_head_pod(BasicRayTestCase.ray_cluster_ns)
        headpod_name = headpod.metadata.name
        pod_exec_command(
            headpod_name, BasicRayTestCase.ray_cluster_ns, "python samples/simple_code.py")

    def test_cluster_info(self):
        """Execute "print(ray.cluster_resources())" in the head Pod."""
        EasyJobRule().assert_rule()

    def test_probe_injection(self):
        """
        Check whether the readiness and liveness probes are injected into the Ray container.
        """
        def is_probe_injected(pod):
            probes = [
                pod.spec.containers[0].readiness_probe,
                pod.spec.containers[0].liveness_probe
            ]
            for probe in probes:
                if probe is None:
                    return False
            return True
        headpod = get_head_pod(BasicRayTestCase.ray_cluster_ns)
        assert is_probe_injected(headpod)
        # TODO (kevin85421): We only check 1 worker Pod here.
        worker_pod = get_pod(BasicRayTestCase.ray_cluster_ns, "ray.io/node-type=worker")
        assert is_probe_injected(worker_pod)

class RayFTTestCase(unittest.TestCase):
    """Test Ray GCS Fault Tolerance"""
    cluster_template = CONST.REPO_ROOT.joinpath("tests/config/ray-cluster.ray-ft.yaml.template")
    ray_cluster_ns = "default"

    # We need to delete the RayCluster at the end of each test case to test cleanup_redis,
    # therefore, we use setUp, instead of setUpClass, here to re-create RayCluster for each test case.
    def setUp(self):
        if not utils.is_feature_supported(ray_version, CONST.RAY_FT):
            raise unittest.SkipTest(f"{CONST.RAY_FT} is not supported")
        K8S_CLUSTER_MANAGER.cleanup()
        K8S_CLUSTER_MANAGER.initialize_cluster()
        operator_manager = OperatorManager.instance()
        operator_manager.prepare_operator()
        utils.create_ray_cluster(RayFTTestCase.cluster_template, ray_version, ray_image)

    def cleanup_redis(self):
        delete_all_cr(CONST.RAY_CLUSTER_CRD, RayFTTestCase.ray_cluster_ns)
        utils.wait_for_condition(
            lambda: shell_subprocess_run("test $(kubectl exec deploy/redis -- redis-cli --no-auth-warning -a 5241590000000000 DBSIZE) = '0'") == 0,
            timeout=300, retry_interval_ms=1000,
        )

    def test_ray_serve(self):
        """Kill GCS process on the head Pod and then test a deployed Ray Serve model."""
        if not utils.is_feature_supported(ray_version, CONST.RAY_SERVE_FT):
            raise unittest.SkipTest(
                "This test is flaky before Ray 2.6.0, so we only run it for 2.6.0 and later."
            )
        headpod = get_head_pod(RayFTTestCase.ray_cluster_ns)
        headpod_name = headpod.metadata.name

        # In `test_detached_actor`, we create 1 head Pod and 1 worker Pod. Afterward, we kill the
        # GCS process of the head Pod to trigger its restart. Next, we terminate the head Pod,
        # and KubeRay will create a new one in its place. However, Ray may take several seconds to
        # realize that the old head Pod is gone. Therefore, using `ray list nodes` might show more
        # than 1 "ALIVE" head nodes in the cluster temporarily. This may lead to an issue where the
        # Serve controller believes it hasn't been scheduled to the head node, and as a result, it
        # raises an exception. To avoid this issue, we will add a retry logic in `test_ray_serve_1`
        # to wait until only 1 head node is alive. `ray list nodes` is for debugging purpose only.
        pod_exec_command(headpod_name, RayFTTestCase.ray_cluster_ns,
            "ray list nodes",
            check = False
        )

        # Deploy a Ray Serve model.
        exit_code = pod_exec_command(headpod_name, RayFTTestCase.ray_cluster_ns,
            "python samples/test_ray_serve_1.py",
            check = False
        )

        if exit_code != 0:
            show_cluster_info(RayFTTestCase.ray_cluster_ns)
            self.fail(f"Fail to execute test_ray_serve_1.py. The exit code is {exit_code}.")

        old_head_pod = get_head_pod(RayFTTestCase.ray_cluster_ns)
        old_head_pod_name = old_head_pod.metadata.name
        restart_count = old_head_pod.status.container_statuses[0].restart_count

        # Kill the gcs_server process on head node. The head node will crash after 20 seconds
        # because the value of `RAY_gcs_rpc_server_reconnect_timeout_s` is "20" in the
        # `ray-cluster.ray-ft.yaml.template` file.
        pod_exec_command(old_head_pod_name, RayFTTestCase.ray_cluster_ns, "pkill gcs_server")

        # Waiting for all pods become ready and running.
        utils.wait_for_new_head(CONST.RESTART_OLD_POD, old_head_pod_name, restart_count,
            RayFTTestCase.ray_cluster_ns, timeout=300, retry_interval_ms=1000)

        # Try to connect to the deployed model again
        headpod = get_head_pod(RayFTTestCase.ray_cluster_ns)
        headpod_name = headpod.metadata.name
        exit_code = pod_exec_command(headpod_name, RayFTTestCase.ray_cluster_ns,
            "python samples/test_ray_serve_2.py",
            check = False
        )

        if exit_code != 0:
            show_cluster_info(RayFTTestCase.ray_cluster_ns)
            self.fail(f"Fail to execute test_ray_serve_2.py. The exit code is {exit_code}.")

        self.cleanup_redis()

    @unittest.skipIf(
        ray_version == '2.8.0',
        'test_detached_actor is too flaky with Ray 2.8.0 due to '
        'https://github.com/ray-project/ray/issues/41343. '
        'It is fixed in Ray 2.9.0 and the nightly.'
    )
    def test_detached_actor(self):
        """Kill GCS process on the head Pod and then test a detached actor."""

        headpod = get_head_pod(RayFTTestCase.ray_cluster_ns)
        headpod_name = headpod.metadata.name

        # RAY_NAMESPACE is an abstraction in Ray. It is not a Kubernetes namespace.
        ray_namespace = ''.join(random.choices(string.ascii_lowercase, k=10))
        logger.info('Ray namespace: %s', ray_namespace)

        # Register a detached actor
        exit_code = pod_exec_command(headpod_name, RayFTTestCase.ray_cluster_ns,
            f" python samples/test_detached_actor_1.py {ray_namespace}",
            check = False
        )

        if exit_code != 0:
            show_cluster_info(RayFTTestCase.ray_cluster_ns)
            self.fail(f"Fail to execute test_detached_actor_1.py. The exit code is {exit_code}.")

        old_head_pod = get_head_pod(RayFTTestCase.ray_cluster_ns)
        old_head_pod_name = old_head_pod.metadata.name
        restart_count = old_head_pod.status.container_statuses[0].restart_count

        # [Test 1: Kill GCS process to "restart" the head Pod]
        # Kill the gcs_server process on head node. The head node will crash after 20 seconds
        # because the value of `RAY_gcs_rpc_server_reconnect_timeout_s` is "20" in the
        # `ray-cluster.ray-ft.yaml.template` file.
        pod_exec_command(old_head_pod_name, RayFTTestCase.ray_cluster_ns, "pkill gcs_server")

        # Waiting for all pods become ready and running.
        utils.wait_for_new_head(CONST.RESTART_OLD_POD, old_head_pod_name, restart_count,
            RayFTTestCase.ray_cluster_ns, timeout=300, retry_interval_ms=1000)

        # Try to connect to the detached actor again.
        # [Note] When all pods become running and ready, the RayCluster still needs tens of seconds
        # to relaunch actors. Hence, `test_detached_actor_2.py` will retry until a Ray client
        # connection succeeds.
        headpod = get_head_pod(RayFTTestCase.ray_cluster_ns)
        headpod_name = headpod.metadata.name
        expected_output = 3
        exit_code = pod_exec_command(headpod_name, RayFTTestCase.ray_cluster_ns,
            f" python samples/test_detached_actor_2.py {ray_namespace} {expected_output}",
            check = False
        )

        if exit_code != 0:
            show_cluster_info(RayFTTestCase.ray_cluster_ns)
            self.fail(f"Fail to execute test_detached_actor_2.py. The exit code is {exit_code}.")

        # [Test 2: Delete the head Pod and wait for a new head Pod]
        # Delete the head Pod. The `kubectl delete pod` command has a default flag `--wait=true`,
        # which waits for resources to be gone before returning.
        shell_subprocess_run(
            f'kubectl delete pod {headpod_name} -n {RayFTTestCase.ray_cluster_ns}')
        restart_count = headpod.status.container_statuses[0].restart_count

        # Waiting for all pods become ready and running.
        utils.wait_for_new_head(CONST.CREATE_NEW_POD, headpod_name, restart_count,
            RayFTTestCase.ray_cluster_ns, timeout=300, retry_interval_ms=1000)

        # Try to connect to the detached actor again.
        headpod = get_head_pod(RayFTTestCase.ray_cluster_ns)
        headpod_name = headpod.metadata.name
        expected_output = 4
        exit_code = pod_exec_command(headpod_name, RayFTTestCase.ray_cluster_ns,
            f" python samples/test_detached_actor_2.py {ray_namespace} {expected_output}",
            check = False
        )

        if exit_code != 0:
            show_cluster_info(RayFTTestCase.ray_cluster_ns)
            self.fail(f"Fail to execute test_detached_actor_2.py. The exit code is {exit_code}.")

        self.cleanup_redis()

class KubeRayHealthCheckTestCase(unittest.TestCase):
    """Test KubeRay health check"""
    cluster_template = CONST.REPO_ROOT.joinpath("tests/config/ray-cluster.sidecar.yaml.template")
    ray_cluster_ns = "default"

    @classmethod
    def setUpClass(cls):
        K8S_CLUSTER_MANAGER.cleanup()
        K8S_CLUSTER_MANAGER.initialize_cluster()
        operator_manager = OperatorManager.instance()
        operator_manager.prepare_operator()
        utils.create_ray_cluster(
            KubeRayHealthCheckTestCase.cluster_template, ray_version, ray_image)

    def test_terminated_raycontainer(self):
        """
        KubeRay should delete a Pod if its restart policy is "Never" and the Ray container is
        terminated no matter whether the Pod is in the "Running" state.
        """
        old_head_pod = get_head_pod(KubeRayHealthCheckTestCase.ray_cluster_ns)
        old_head_pod_name = old_head_pod.metadata.name
        restart_count = old_head_pod.status.container_statuses[0].restart_count

        # After the Ray container is terminated by `pkill ray`, the head Pod will still be in the
        # "Running" state because there is still a sidecar container running in the Pod. KubeRay
        # should delete the Pod and create a new one.
        pod_exec_command(old_head_pod_name, KubeRayHealthCheckTestCase.ray_cluster_ns, "pkill ray")

        # Set the mode to `CONST.CREATE_NEW_POD` to wait for a new head Pod
        # rather than restarting the old head Pod.
        utils.wait_for_new_head(CONST.CREATE_NEW_POD, old_head_pod_name, restart_count,
            RayFTTestCase.ray_cluster_ns, timeout=300, retry_interval_ms=1000)


if __name__ == '__main__':
    logger.info('Setting Ray image to: {}'.format(ray_image))
    logger.info('Setting Ray version to: {}'.format(ray_version))
    logger.info('Setting KubeRay operator image to: {}'.format(kuberay_operator_image))
    unittest.main(verbosity=2)
