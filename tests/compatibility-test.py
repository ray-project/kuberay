#!/usr/bin/env python
import logging
import unittest
import docker

from kuberay_utils.utils import *


logger = logging.getLogger(__name__)
logging.basicConfig(level=logging.INFO)

# kuberay_sha, ray_version & ray_image are default values that
# will be changed by parse_environment()
kuberay_sha = 'nightly'
ray_version = '1.9.0'
ray_image = "rayproject/ray:1.9.0"


class BasicRayTestCase(unittest.TestCase):
    cluster_template_file = 'tests/config/ray-cluster.mini.yaml.template'

    @classmethod
    def setUpClass(cls):
        # Ray cluster is running inside a local Kind environment.
        # We use port mapping to connect to the Kind environment
        # from another local ray container. The local ray container
        # outside Kind environment has the same ray version as the
        # ray cluster running inside Kind environment.
        delete_cluster()
        create_cluster()
        apply_kuberay_resources(kuberay_sha)
        download_images(ray_image)
        create_kuberay_cluster(BasicRayTestCase.cluster_template_file,
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
    cluster_template_file = 'tests/config/ray-cluster.ray-ft.yaml.template'

    @classmethod
    def setUpClass(cls):
        if not ray_ft_supported(ray_version):
            return
        delete_cluster()
        create_cluster()
        apply_kuberay_resources(kuberay_sha)
        download_images(ray_image)
        create_kuberay_cluster(RayFTTestCase.cluster_template_file,
                               ray_version, ray_image)

    def setUp(self):
        if not ray_ft_supported(ray_version):
            raise unittest.SkipTest("ray ft is not supported")

    def test_kill_head(self):
        # This test will delete head node and wait for a new replacement to
        # come up.
        shell_assert_success(
            'kubectl delete pod $(kubectl get pods -A | grep -e "-head" | awk "{print \$2}")')

        # wait for new head node to start
        time.sleep(80)
        shell_assert_success('kubectl get pods -A')

        # make sure the new head is ready
        # shell_assert_success('kubectl wait --for=condition=Ready pod/$(kubectl get pods -A | grep -e "-head" | awk "{print \$2}") --timeout=900s')
        # make sure both head and worker pods are ready
        rtn = shell_run(
            'kubectl wait --for=condition=ready pod -l rayCluster=raycluster-compatibility-test --all --timeout=900s')
        if rtn != 0:
            shell_run('kubectl get pods -A')
            shell_run(
                'kubectl describe pod $(kubectl get pods | grep -e "-head" | awk "{print \$1}")')
            shell_run(
                'kubectl logs $(kubectl get pods | grep -e "-head" | awk "{print \$1}")')
            shell_run(
                'kubectl logs -n $(kubectl get pods -A | grep -e "-operator" | awk \'{print $1 "  " $2}\')')
        assert rtn == 0

    def test_ray_serve(self):
        client = docker.from_env()
        container = client.containers.run(ray_image, remove=True, detach=True, stdin_open=True, tty=True,
                                          network_mode='host', command=["/bin/sh", "-c", "python"])
        s = container.attach_socket(
            params={'stdin': 1, 'stream': 1, 'stdout': 1, 'stderr': 1})
        s._sock.setblocking(0)
        s._sock.sendall(b'''
import ray
import time
import ray.serve as serve
import os
import requests
from ray._private.test_utils import wait_for_condition

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

ray.init(address='ray://127.0.0.1:10001')

@serve.deployment
def d(*args):
    return f"{os.getpid()}"

d.deploy()
pid1 = ray.get(d.get_handle().remote())

print('ready')
        ''')

        count = 0
        while count < 90:
            try:
                buf = s._sock.recv(4096)
                logger.info(buf.decode())
                if buf.decode().find('ready') != -1:
                    break
            except Exception as e:
                pass
            time.sleep(1)
            count += 1
        if count >= 90:
            raise Exception('failed to run script')

        # kill the gcs on head node. If fate sharing is enabled
        # the whole head node pod will terminate.
        shell_assert_success(
            'kubectl exec -it $(kubectl get pods -A| grep -e "-head" | awk "{print \\$2}") -- /bin/bash -c "ps aux | grep gcs_server | grep -v grep | awk \'{print \$2}\' | xargs kill"')
        # wait for new head node getting created
        time.sleep(10)
        # make sure the new head is ready
        shell_assert_success(
            'kubectl wait --for=condition=Ready pod/$(kubectl get pods -A | grep -e "-head" | awk "{print \$2}") --timeout=900s')

        s._sock.sendall(b'''
def get_new_value():
    return ray.get(d.get_handle().remote())
pid2 = retry_with_timeout(get_new_value)

if pid1 == pid2:
    print('successful: {} {}'.format(pid1, pid2))
    sys.exit(0)
else:
    print('failed: {} {}'.format(pid1, pid2))
    raise Exception('failed')
        ''')

        count = 0
        while count < 90:
            try:
                buf = s._sock.recv(4096)
                logger.info(buf.decode())
                if buf.decode().find('successful') != -1:
                    break
                if buf.decode().find('failed') != -1:
                    raise Exception('test failed {}'.format(buf.decode()))
            except Exception as e:
                pass
            time.sleep(1)
            count += 1
        if count >= 90:
            raise Exception('failed to run script')

        container.stop()
        client.close()

    def test_detached_actor(self):
        # This test will run a ray client and start a detached actor at first.
        # Then we will kill the head node and kuberay will start a new head node
        # replacement. Finally, we will try to connect to the detached actor again.
        client = docker.from_env()
        container = client.containers.run(ray_image, remove=True, detach=True, stdin_open=True, tty=True,
                                          network_mode='host', command=["/bin/sh", "-c", "python"])
        s = container.attach_socket(
            params={'stdin': 1, 'stream': 1, 'stdout': 1, 'stderr': 1})
        s._sock.setblocking(0)
        s._sock.sendall(b'''
import ray
import time

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

ray.init(address='ray://127.0.0.1:10001')

@ray.remote
class A:
    def ready(self):
        import os
        return os.getpid()

a = A.options(name="a", lifetime="detached", max_restarts=-1).remote()
res1 = ray.get(a.ready.remote())
print('ready')

        ''')

        count = 0
        while count < 90:
            try:
                buf = s._sock.recv(4096)
                logger.info(buf.decode())
                if buf.decode().find('ready') != -1:
                    break
            except Exception as e:
                pass
            time.sleep(1)
            count += 1
        if count >= 90:
            raise Exception('failed to run script')

        # kill the gcs on head node. If fate sharing is enabled
        # the whole head node pod will terminate.
        shell_assert_success(
            'kubectl exec -it $(kubectl get pods -A| grep -e "-head" | awk "{print \\$2}") -- /bin/bash -c "ps aux | grep gcs_server | grep -v grep | awk \'{print \$2}\' | xargs kill"')
        # wait for new head node getting created
        time.sleep(10)
        # make sure the new head is ready
        shell_assert_success(
            'kubectl wait --for=condition=Ready pod/$(kubectl get pods -A | grep -e "-head" | awk "{print \$2}") --timeout=900s')

        s._sock.sendall(b'''
def get_detached_actor():
    return ray.get_actor("a")
a = retry_with_timeout(get_detached_actor)

def get_new_value():
    return ray.get(a.ready.remote())
res2 = retry_with_timeout(get_new_value)

if res1 != res2:
    print('successful: {} {}'.format(res1, res2))
    sys.exit(0)
else:
    print('failed: {} {}'.format(res1, res2))
    raise Exception('failed')
        ''')

        count = 0
        while count < 90:
            try:
                buf = s._sock.recv(4096)
                logger.info(buf.decode())
                if buf.decode().find('successful') != -1:
                    break
                if buf.decode().find('failed') != -1:
                    raise Exception('test failed {}'.format(buf.decode()))
            except Exception as e:
                pass
            time.sleep(1)
            count += 1
        if count >= 90:
            raise Exception('failed to run script')

        container.stop()
        client.close()


class RayServiceTestCase(unittest.TestCase):
    service_template_file = 'tests/config/ray-service.yaml.template'
    service_serve_update_template_file = 'tests/config/ray-service-serve-update.yaml.template'
    service_cluster_update_template_file = 'tests/config/ray-service-cluster-update.yaml.template'

    @classmethod
    def setUpClass(cls):
        if not ray_service_supported(ray_version):
            return
        # Ray Service is running inside a local Kind environment.
        # We use the Ray nightly version now.
        # We wait for the serve service ready.
        # The test will check the successful response from serve service.
        delete_cluster()
        create_cluster()
        apply_kuberay_resources(kuberay_sha)
        download_images(ray_image)
        create_kuberay_service(
            RayServiceTestCase.service_template_file, ray_version, ray_image)

    def setUp(self):
        if not ray_service_supported(ray_version):
            raise unittest.SkipTest("ray service is not supported")

    def test_ray_serve_work(self):
        time.sleep(5)
        curl_cmd = 'curl  -X POST -H \'Content-Type: application/json\' localhost:8000 -d \'["MANGO", 2]\''
        wait_for_condition(
            lambda: shell_run(curl_cmd) == 0,
            timeout=15,
        )
        create_kuberay_service(
            RayServiceTestCase.service_serve_update_template_file,
            ray_version, ray_image)
        curl_cmd = 'curl  -X POST -H \'Content-Type: application/json\' localhost:8000 -d \'["MANGO", 2]\''
        time.sleep(5)
        wait_for_condition(
            lambda: shell_run(curl_cmd) == 0,
            timeout=60,
        )
        create_kuberay_service(
            RayServiceTestCase.service_cluster_update_template_file,
            ray_version, ray_image)
        time.sleep(5)
        curl_cmd = 'curl  -X POST -H \'Content-Type: application/json\' localhost:8000 -d \'["MANGO", 2]\''
        wait_for_condition(
            lambda: shell_run(curl_cmd) == 0,
            timeout=180,
        )


def parse_environment():
    global ray_version, ray_image, kuberay_sha
    for k, v in os.environ.items():
        if k == 'RAY_VERSION':
            logger.info('Setting Ray image to: {}'.format(v))
            ray_version = v
            ray_image = 'rayproject/ray:{}'.format(ray_version)
        if k == 'KUBERAY_IMG_SHA':
            logger.info('Using KubeRay docker build SHA: {}'.format(v))
            kuberay_sha = v


if __name__ == '__main__':
    parse_environment()
    unittest.main(verbosity=2)
