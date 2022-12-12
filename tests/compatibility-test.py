#!/usr/bin/env python
import logging
import unittest
import time
import os
import random
import string
import docker

import kuberay_utils.utils as utils
from framework.prototype import (
    CurlServiceRule,
    K8S_CLUSTER_MANAGER,
    OperatorManager,
    RuleSet,
    show_cluster_info
)

from framework.utils import (
    shell_subprocess_run,
    CONST
)

logger = logging.getLogger(__name__)
logging.basicConfig(
    format='%(asctime)s,%(msecs)d %(levelname)-8s [%(filename)s:%(lineno)d] %(message)s',
    datefmt='%Y-%m-%d:%H:%M:%S',
    level=logging.INFO
)

# Default Ray version
ray_version = '2.1.0'

# Default docker images
ray_image = 'rayproject/ray:2.1.0'
kuberay_operator_image = 'kuberay/operator:nightly'


class BasicRayTestCase(unittest.TestCase):
    cluster_template_file = CONST.REPO_ROOT.joinpath("tests/config/ray-cluster.mini.yaml.template")

    @classmethod
    def setUpClass(cls):
        # Ray cluster is running inside a local Kind environment.
        # We use port mapping to connect to the Kind environment
        # from another local ray container. The local ray container
        # outside Kind environment has the same ray version as the
        # ray cluster running inside Kind environment.
        K8S_CLUSTER_MANAGER.delete_kind_cluster()
        K8S_CLUSTER_MANAGER.create_kind_cluster()
        image_dict = {
            CONST.RAY_IMAGE_KEY: ray_image,
            CONST.OPERATOR_IMAGE_KEY: kuberay_operator_image
        }
        operator_manager = OperatorManager(image_dict)
        operator_manager.prepare_operator()
        utils.create_ray_cluster(BasicRayTestCase.cluster_template_file,
                                     ray_version, ray_image)

    def test_simple_code(self):
        # connect from a ray container client to ray cluster
        # inside a local Kind environment and run a simple test
        client = docker.from_env()
        container = client.containers.run(ray_image,
                                          remove=True,
                                          detach=True,
                                          tty=True,
                                          network_mode='host')
        rtn_code, output = container.exec_run(['python',
                                               '-c', '''
import ray
ray.init(address='ray://127.0.0.1:10001')

def retry_with_timeout(func, count=90):
    tmp = 0
    err = None
    while tmp < count:
        try:
            return func()
        except Exception as e:
            err = e
            tmp += 1
    assert err is not None
    raise err

@ray.remote
def f(x):
    return x * x

def get_result():
    futures = [f.remote(i) for i in range(4)]
    print(ray.get(futures))
    return 0
rtn = retry_with_timeout(get_result)
assert rtn == 0
'''],
                                              demux=True)
        stdout_str, stderr_str = output

        container.stop()

        if stdout_str != b'[0, 1, 4, 9]\n':
            logger.error('test_simple_code returns {}'.format(output))
            raise Exception(('test_simple_code returns invalid result. ' +
                             'Expected: {} Actual: {} Stderr: {}').format(
                b'[0, 1, 4, 9]', stdout_str, stderr_str))
        if rtn_code != 0:
            msg = 'invalid return code {}'.format(rtn_code)
            logger.error(msg)
            raise Exception(msg)

        client.close()

    def test_cluster_info(self):
        # connect from a ray container client to ray cluster
        # inside a local Kind environment and run a test that
        # gets the amount of nodes in the ray cluster.
        client = docker.from_env()
        container = client.containers.run(ray_image,
                                          remove=True,
                                          detach=True,
                                          tty=True,
                                          network_mode='host')
        rtn_code, output = container.exec_run(['python',
                                               '-c', '''
import ray
ray.init(address='ray://127.0.0.1:10001')

print(len(ray.nodes()))
'''],
                                              demux=True)
        stdout_str, _ = output

        container.stop()

        if stdout_str != b'2\n':
            logger.error('test_cluster_info returns {}'.format(output))
            raise Exception(('test_cluster_info returns invalid result. ' +
                             'Expected: {} Actual: {}').format(b'2',
                                                               stdout_str))
        if rtn_code != 0:
            msg = 'invalid return code {}'.format(rtn_code)
            logger.error(msg)
            raise Exception(msg)

        client.close()


class RayFTTestCase(unittest.TestCase):
    cluster_template_file = CONST.REPO_ROOT.joinpath("tests/config/ray-cluster.ray-ft.yaml.template")

    @classmethod
    def setUpClass(cls):
        if not utils.is_feature_supported(ray_version, CONST.RAY_FT):
            raise unittest.SkipTest(f"{CONST.RAY_FT} is not supported")
        K8S_CLUSTER_MANAGER.delete_kind_cluster()
        K8S_CLUSTER_MANAGER.create_kind_cluster()
        image_dict = {
            CONST.RAY_IMAGE_KEY: ray_image,
            CONST.OPERATOR_IMAGE_KEY: kuberay_operator_image
        }
        operator_manager = OperatorManager(image_dict)
        operator_manager.prepare_operator()
        utils.create_ray_cluster(RayFTTestCase.cluster_template_file,
                                     ray_version, ray_image)

    @unittest.skip("Skip test_kill_head due to its flakiness.")
    def test_kill_head(self):
        # This test will delete head node and wait for a new replacement to
        # come up.
        shell_subprocess_run(
            'kubectl delete pod $(kubectl get pods -A | grep -e "-head" | awk "{print \$2}")')

        # wait for new head node to start
        time.sleep(80)
        shell_subprocess_run('kubectl get pods -A')

        # make sure the new head is ready
        # shell_assert_success('kubectl wait --for=condition=Ready pod/$(kubectl get pods -A | grep -e "-head" | awk "{print \$2}") --timeout=900s')
        # make sure both head and worker pods are ready
        rtn = shell_subprocess_run(
                'kubectl wait --for=condition=ready pod -l rayCluster=raycluster-compatibility-test --all --timeout=900s', check = False)
        if rtn != 0:
            show_cluster_info("default")
            raise Exception(f"Nonzero return code {rtn} in test_kill_head()")

    def test_ray_serve(self):
        cluster_namespace = "default"
        docker_client = docker.from_env()
        container = docker_client.containers.run(ray_image, remove=True, detach=True, stdin_open=True, tty=True,
                                          network_mode='host', command=["/bin/sh"])
        # Deploy a model with ray serve
        ray_namespace = ''.join(random.choices(string.ascii_lowercase, k=10))
        logger.info(f'namespace: {ray_namespace}')

        utils.copy_to_container(container, 'tests/scripts', '/usr/local/', 'test_ray_serve_1.py')
        exit_code, _ = utils.exec_run_container(container, f'python3 /usr/local/test_ray_serve_1.py {ray_namespace}', timeout_sec = 180)

        if exit_code != 0:
            show_cluster_info(cluster_namespace)
            raise Exception(f"There was an exception during the execution of test_ray_serve_1.py. The exit code is {exit_code}." +
                "See above for command output. The output will be printed by the function exec_run_container.")

        # KubeRay only allows at most 1 head pod per RayCluster instance at the same time. In addition,
        # if we have 0 head pods at this moment, it indicates that the head pod crashes unexpectedly.
        headpods = utils.get_pod(namespace=cluster_namespace,
            label_selector='ray.io/node-type=head')
        assert(len(headpods.items) == 1)
        old_head_pod = headpods.items[0]
        old_head_pod_name = old_head_pod.metadata.name
        restart_count = old_head_pod.status.container_statuses[0].restart_count

        # Kill the gcs_server process on head node. If fate sharing is enabled, the whole head node pod
        # will terminate.
        exec_command = ['pkill gcs_server']
        utils.pod_exec_command(pod_name=old_head_pod_name,
            namespace=cluster_namespace, exec_command=exec_command)

        # Waiting for all pods become ready and running.
        utils.wait_for_new_head(old_head_pod_name, restart_count,
            cluster_namespace, timeout=300, retry_interval_ms=1000)

        # Try to connect to the deployed model again
        utils.copy_to_container(container, 'tests/scripts', '/usr/local/', 'test_ray_serve_2.py')
        exit_code, _ = utils.exec_run_container(container, f'python3 /usr/local/test_ray_serve_2.py {ray_namespace}', timeout_sec = 180)

        if exit_code != 0:
            show_cluster_info(cluster_namespace)
            raise Exception(f"There was an exception during the execution of test_ray_serve_2.py. The exit code is {exit_code}." +
                "See above for command output. The output will be printed by the function exec_run_container.")

        container.stop()
        docker_client.close()

    def test_detached_actor(self):
        cluster_namespace = "default"
        docker_client = docker.from_env()
        container = docker_client.containers.run(ray_image, remove=True, detach=True, stdin_open=True, tty=True,
                                            network_mode='host', command=["/bin/sh"])
        ray_namespace = ''.join(random.choices(string.ascii_lowercase, k=10))
        logger.info(f'namespace: {ray_namespace}')

        # Register a detached actor
        utils.copy_to_container(container, 'tests/scripts', '/usr/local/', 'test_detached_actor_1.py')
        exit_code, _ = utils.exec_run_container(container, f'python3 /usr/local/test_detached_actor_1.py {ray_namespace}', timeout_sec = 180)

        if exit_code != 0:
            show_cluster_info(cluster_namespace)
            raise Exception(f"There was an exception during the execution of test_detached_actor_1.py. The exit code is {exit_code}." +
                "See above for command output. The output will be printed by the function exec_run_container.")

        # KubeRay only allows at most 1 head pod per RayCluster instance at the same time. In addition,
        # if we have 0 head pods at this moment, it indicates that the head pod crashes unexpectedly.
        headpods = utils.get_pod(namespace=cluster_namespace,
            label_selector='ray.io/node-type=head')
        assert(len(headpods.items) == 1)
        old_head_pod = headpods.items[0]
        old_head_pod_name = old_head_pod.metadata.name
        restart_count = old_head_pod.status.container_statuses[0].restart_count

        # Kill the gcs_server process on head node. If fate sharing is enabled, the whole head node pod
        # will terminate.
        exec_command = ['pkill gcs_server']
        utils.pod_exec_command(pod_name=old_head_pod_name,
            namespace=cluster_namespace, exec_command=exec_command)

        # Waiting for all pods become ready and running.
        utils.wait_for_new_head(old_head_pod_name, restart_count,
            cluster_namespace, timeout=300, retry_interval_ms=1000)

        # Try to connect to the detached actor again.
        # [Note] When all pods become running and ready, the RayCluster still needs tens of seconds to relaunch actors. Hence,
        #        `test_detached_actor_2.py` will retry until a Ray client connection succeeds.
        utils.copy_to_container(container, 'tests/scripts', '/usr/local/', 'test_detached_actor_2.py')
        exit_code, _ = utils.exec_run_container(container, f'python3 /usr/local/test_detached_actor_2.py {ray_namespace}', timeout_sec = 180)

        if exit_code != 0:
            show_cluster_info(cluster_namespace)
            raise Exception(f"There was an exception during the execution of test_detached_actor_2.py. The exit code is {exit_code}." +
                "See above for command output. The output will be printed by the function exec_run_container.")

        container.stop()
        docker_client.close()

class RayServiceTestCase(unittest.TestCase):
    """Integration tests for RayService"""
    service_template_file = 'tests/config/ray-service.yaml.template'

    # The previous logic for testing updates was problematic.
    # We need to test RayService updates.
    @classmethod
    def setUpClass(cls):
        if not utils.is_feature_supported(ray_version, CONST.RAY_SERVICE):
            raise unittest.SkipTest(f"{CONST.RAY_SERVICE} is not supported")
        K8S_CLUSTER_MANAGER.delete_kind_cluster()
        K8S_CLUSTER_MANAGER.create_kind_cluster()
        image_dict = {
            CONST.RAY_IMAGE_KEY: ray_image,
            CONST.OPERATOR_IMAGE_KEY: kuberay_operator_image
        }
        operator_manager = OperatorManager(image_dict)
        operator_manager.prepare_operator()

    def test_ray_serve_work(self):
        """Create a RayService, send a request to RayService via `curl`, and compare the result."""
        cr_event = utils.create_ray_service(
            RayServiceTestCase.service_template_file, ray_version, ray_image)
        # When Pods are READY and RUNNING, RayService still needs tens of seconds to be ready
        # for serving requests. This `sleep` function is a workaround, and should be removed
        # when https://github.com/ray-project/kuberay/pull/730 is merged.
        time.sleep(60)
        cr_event.rulesets = [RuleSet([CurlServiceRule()])]
        cr_event.check_rule_sets()

def parse_environment():
    global ray_version, ray_image, kuberay_operator_image
    for k, v in os.environ.items():
        if k == 'RAY_IMAGE':
            ray_image = v
            ray_version = ray_image.split(':')[-1]
        elif k == 'OPERATOR_IMAGE':
            kuberay_operator_image = v


if __name__ == '__main__':
    parse_environment()
    logger.info('Setting Ray image to: {}'.format(ray_image))
    logger.info('Setting Ray version to: {}'.format(ray_version))
    logger.info('Setting KubeRay operator image to: {}'.format(kuberay_operator_image))
    unittest.main(verbosity=2)
