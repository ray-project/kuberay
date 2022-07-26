#!/usr/bin/env python
import logging
import os
import tempfile
import unittest
import subprocess
from string import Template

import docker
import time

ray_version = '1.9.0'
ray_image = "rayproject/ray:1.9.0"

kindcluster_config_file = 'tests/config/cluster-config.yaml'
raycluster_service_file = 'tests/config/raycluster-service.yaml'

logger = logging.getLogger(__name__)
logging.basicConfig(level=logging.INFO)

kuberay_sha = 'nightly'


def parse_ray_version(version_str):
    tmp = version_str.split('.')
    assert len(tmp) == 3
    major = int(tmp[0])
    minor = int(tmp[1])
    patch = int(tmp[2])
    return major, minor, patch


def shell_run(cmd):
    logger.info('executing cmd: {}'.format(cmd))
    return os.system(cmd)


def shell_assert_success(cmd):
    assert shell_run(cmd) == 0


def shell_assert_failure(cmd):
    assert shell_run(cmd) != 0


def create_cluster():
    shell_assert_success(
        'kind create cluster --config {}'.format(kindcluster_config_file))
    time.sleep(60)
    rtn = shell_run('kubectl wait --for=condition=ready pod -n kube-system --all --timeout=900s')
    if rtn != 0:
        shell_run('kubectl get pods -A')
    assert rtn == 0


def apply_kuberay_resources():
    shell_assert_success('kind load docker-image kuberay/operator:{}'.format(kuberay_sha))
    shell_assert_success('kind load docker-image kuberay/apiserver:{}'.format(kuberay_sha))
    shell_assert_success(
        'kubectl create -k manifests/cluster-scope-resources')
    # use kustomize to build the yaml, then change the image to the one we want to testing.
    shell_assert_success(
        ('rm -f kustomization.yaml && kustomize create --resources manifests/base && ' +
         'kustomize edit set image ' +
         'kuberay/operator:nightly=kuberay/operator:{0} kuberay/apiserver:nightly=kuberay/apiserver:{0} && ' +
         'kubectl apply -k .').format(kuberay_sha))


def create_kuberay_cluster(template_name):
    template = None
    with open(template_name, mode='r') as f:
        template = Template(f.read())

    raycluster_spec_buf = template.substitute(
        {'ray_image': ray_image, 'ray_version': ray_version})

    raycluster_spec_file = None
    with tempfile.NamedTemporaryFile('w', delete=False) as f:
        f.write(raycluster_spec_buf)
        raycluster_spec_file = f.name

    rtn = shell_run('kubectl wait --for=condition=ready pod -n ray-system --all --timeout=900s')
    if rtn != 0:
        shell_run('kubectl get pods -A')
    assert rtn == 0
    assert raycluster_spec_file is not None
    shell_assert_success('kubectl apply -f {}'.format(raycluster_spec_file))

    time.sleep(180)

    shell_run('kubectl get pods -A')

    rtn = shell_run(
        'kubectl wait --for=condition=ready pod -l rayCluster=raycluster-compatibility-test --all --timeout=900s')
    if rtn != 0:
        shell_run('kubectl get pods -A')
        shell_run('kubectl describe pod $(kubectl get pods | grep -e "-head" | awk "{print \$1}")')
        shell_run('kubectl logs $(kubectl get pods | grep -e "-head" | awk "{print \$1}")')
        shell_run('kubectl logs -n $(kubectl get pods -A | grep -e "-operator" | awk \'{print $1 "  " $2}\')')
    assert rtn == 0

    shell_assert_success('kubectl apply -f {}'.format(raycluster_service_file))


def create_kuberay_service(template_name):
    template = None
    with open(template_name, mode='r') as f:
        template = Template(f.read())

    rayservice_spec_buf = template.substitute(
        {'ray_image': ray_image, 'ray_version': ray_version})

    service_config_file = None
    with tempfile.NamedTemporaryFile('w', delete=False) as f:
        f.write(rayservice_spec_buf)
        service_config_file = f.name

    rtn = shell_run('kubectl wait --for=condition=ready pod -n ray-system --all --timeout=900s')
    if rtn != 0:
        shell_run('kubectl get pods -A')
    assert rtn == 0
    assert service_config_file is not None
    shell_assert_success('kubectl apply -f {}'.format(service_config_file))

    shell_run('kubectl get pods -A')

    time.sleep(20)

    wait_for_condition(
        lambda: shell_run('kubectl get service rayservice-sample-serve-svc -o jsonpath="{.status}"') == 0,
        timeout=900,
        retry_interval_ms=5000,
    )


def delete_cluster():
    shell_run('kind delete cluster')


def download_images():
    client = docker.from_env()
    client.images.pull(ray_image)
    # not enabled for now
    # shell_assert_success('kind load docker-image \"{}\"'.format(ray_image))
    client.close()


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
        apply_kuberay_resources()
        download_images()
        create_kuberay_cluster(BasicRayTestCase.cluster_template_file)

    def test_simple_code(self):
        # connect from a ray containter client to ray cluster
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
        # connect from a ray containter client to ray cluster
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


def ray_ha_supported():
    if ray_version == "nightly":
        return True
    major, minor, patch = parse_ray_version(ray_version)
    if major * 100 + minor < 113:
        return False
    return True

def ray_service_supported():
    if ray_version == "nightly":
        return True
    major, minor, patch = parse_ray_version(ray_version)
    if major * 100 + minor < 113:
        return False
    return True


class RayHATestCase(unittest.TestCase):
    cluster_template_file = 'tests/config/ray-cluster.ray-ha.yaml.template'

    @classmethod
    def setUpClass(cls):
        if not ray_ha_supported():
            return
        delete_cluster()
        create_cluster()
        apply_kuberay_resources()
        download_images()
        create_kuberay_cluster(RayHATestCase.cluster_template_file)

    def setUp(self):
        if not ray_ha_supported():
            raise unittest.SkipTest("ray ha is not supported")

    def test_kill_head(self):
        # This test will delete head node and wait for a new replacement to
        # come up.
        shell_assert_success('kubectl delete pod $(kubectl get pods -A | grep -e "-head" | awk "{print \$2}")')

        # wait for new head node to start
        time.sleep(80)
        shell_assert_success('kubectl get pods -A')

        # make sure the new head is ready
        # shell_assert_success('kubectl wait --for=condition=Ready pod/$(kubectl get pods -A | grep -e "-head" | awk "{print \$2}") --timeout=900s')
        # make sure both head and worker pods are ready
        rtn = shell_run('kubectl wait --for=condition=ready pod -l rayCluster=raycluster-compatibility-test --all --timeout=900s')
        if rtn != 0:
            shell_run('kubectl get pods -A')
            shell_run('kubectl describe pod $(kubectl get pods | grep -e "-head" | awk "{print \$1}")')
            shell_run('kubectl logs $(kubectl get pods | grep -e "-head" | awk "{print \$1}")')
            shell_run('kubectl logs -n $(kubectl get pods -A | grep -e "-operator" | awk \'{print $1 "  " $2}\')')
        assert rtn == 0

    def test_ray_serve(self):
        client = docker.from_env()
        container = client.containers.run(ray_image, remove=True, detach=True, stdin_open=True, tty=True,
                                          network_mode='host', command=["/bin/sh", "-c", "python"])
        s = container.attach_socket(params={'stdin': 1, 'stream': 1, 'stdout': 1, 'stderr': 1})
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
        shell_assert_success('kubectl exec -it $(kubectl get pods -A| grep -e "-head" | awk "{print \\$2}") -- /bin/bash -c "ps aux | grep gcs_server | grep -v grep | awk \'{print \$2}\' | xargs kill"')
        # wait for new head node getting created
        time.sleep(10)
        # make sure the new head is ready
        shell_assert_success('kubectl wait --for=condition=Ready pod/$(kubectl get pods -A | grep -e "-head" | awk "{print \$2}") --timeout=900s')

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
        s = container.attach_socket(params={'stdin': 1, 'stream': 1, 'stdout': 1, 'stderr': 1})
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
        shell_assert_success('kubectl exec -it $(kubectl get pods -A| grep -e "-head" | awk "{print \\$2}") -- /bin/bash -c "ps aux | grep gcs_server | grep -v grep | awk \'{print \$2}\' | xargs kill"')
        # wait for new head node getting created
        time.sleep(10)
        # make sure the new head is ready
        shell_assert_success('kubectl wait --for=condition=Ready pod/$(kubectl get pods -A | grep -e "-head" | awk "{print \$2}") --timeout=900s')

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
        if not ray_service_supported():
            return
        # Ray Service is running inside a local Kind environment.
        # We use the Ray nightly version now.
        # We wait for the serve service ready.
        # The test will check the successful response from serve service.
        delete_cluster()
        create_cluster()
        apply_kuberay_resources()
        download_images()
        create_kuberay_service(RayServiceTestCase.service_template_file)

    def setUp(self):
        if not ray_service_supported():
            raise unittest.SkipTest("ray service is not supported")

    def test_ray_serve_work(self):
        port_forwarding_proc = subprocess.Popen('kubectl port-forward service/rayservice-sample-serve-svc 8000', shell=True)
        time.sleep(5)
        curl_cmd = 'curl  -X POST -H \'Content-Type: application/json\' localhost:8000 -d \'["MANGO", 2]\''
        wait_for_condition(
            lambda: shell_run(curl_cmd) == 0,
            timeout=5,
        )
        create_kuberay_service(RayServiceTestCase.service_serve_update_template_file)
        curl_cmd = 'curl  -X POST -H \'Content-Type: application/json\' localhost:8000 -d \'["MANGO", 2]\''
        time.sleep(5)
        wait_for_condition(
            lambda: shell_run(curl_cmd) == 0,
            timeout=60,
        )
        create_kuberay_service(RayServiceTestCase.service_cluster_update_template_file)
        time.sleep(5)
        port_forwarding_proc.kill()
        time.sleep(5)
        port_forwarding_proc = subprocess.Popen('kubectl port-forward service/rayservice-sample-serve-svc 8000', shell=True)
        time.sleep(5)
        curl_cmd = 'curl  -X POST -H \'Content-Type: application/json\' localhost:8000 -d \'["MANGO", 2]\''

        wait_for_condition(
            lambda: shell_run(curl_cmd) == 0,
            timeout=180,
        )
        port_forwarding_proc.kill()

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


if __name__ == '__main__':
    parse_environment()
    unittest.main(verbosity=2)
