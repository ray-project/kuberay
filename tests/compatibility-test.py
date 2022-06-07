#!/usr/bin/env python
import logging
import os
import tempfile
import unittest
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
    time.sleep(30)
    rtn = shell_run('kubectl wait --for=condition=ready pod -n kube-system --all --timeout=1200s')
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

    rtn = shell_run('kubectl wait --for=condition=ready pod -n ray-system --all --timeout=1200s')
    if rtn != 0:
        shell_run('kubectl get pods -A')
    assert rtn == 0
    assert raycluster_spec_file is not None
    shell_assert_success('kubectl apply -f {}'.format(raycluster_spec_file))

    time.sleep(180)

    rtn = shell_run(
        'kubectl wait --for=condition=ready pod -l rayCluster=raycluster-compatibility-test --all --timeout=1200s')
    if rtn != 0:
        shell_run('kubectl get pods -A')
        shell_run('kubectl describe pod $(kubectl get pods | grep -e "-head" | awk "{print \$1}")')
        shell_run('kubectl logs $(kubectl get pods | grep -e "-head" | awk "{print \$1}")')
        shell_run('kubectl logs -n $(kubectl get pods -A | grep -e "-operator" | awk \'{print $1 "  " $2}\')')
    assert rtn == 0

    shell_assert_success('kubectl apply -f {}'.format(raycluster_service_file))


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

@ray.remote
def f(x):
    return x * x

futures = [f.remote(i) for i in range(4)]
print(ray.get(futures))
'''],
                                              demux=True)
        stdout_str, _ = output

        container.stop()

        if stdout_str != b'[0, 1, 4, 9]\n':
            logger.error('test_simple_code returns {}'.format(output))
            raise Exception(('test_simple_code returns invalid result. ' +
                             'Expected: {} Actual: {}').format(b'[0, 1, 4, 9]',
                                                               stdout_str))
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
    major, minor, patch = parse_ray_version(ray_version)
    if major * 100 + minor < 113:
        return False
    return True


class RayHATestCase(unittest.TestCase):
    cluster_template_file = 'tests/config/ray-cluster.ray-ha.yaml.template'

    @classmethod
    def setUpClass(cls):
        delete_cluster()
        create_cluster()
        apply_kuberay_resources()
        download_images()
        create_kuberay_cluster(RayHATestCase.cluster_template_file)

    def setUp(self):
        if not ray_ha_supported():
            raise unittest.SkipTest("ray ha is not supported")

    def test_kill_head(self):
        # delete head node
        shell_assert_success('kubectl delete pod $(kubectl get pods -A | grep -e "-head" | awk "{print \$2}")')
        time.sleep(20)
        # wait for new head node to start
        shell_assert_success('kubectl get pod $(kubectl get pods -A | grep -e "-head" | awk "{print \$2}")')

        # make sure the new head is ready
        shell_assert_success('kubectl wait --for=condition=Ready pod/$(kubectl get pods -A | grep -e "-head" | awk "{print \$2}") --timeout=1200s')
        # make sure both head and worker pods are ready
        shell_assert_success('kubectl wait --for=condition=ready pod -l rayCluster=raycluster-compatibility-test --all --timeout=1200s')

    def test_detached_actor(self):
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

        shell_assert_success('kubectl exec -it $(kubectl get pods -A| grep -e "-head" | awk "{print \\$2}") -- /bin/bash -c "ps aux | grep gcs_server | grep -v grep | awk \'{print \$2}\' | xargs kill"')
        # wait for new head node getting created
        time.sleep(10)
        # make sure the new head is ready
        shell_assert_success('kubectl wait --for=condition=Ready pod/$(kubectl get pods -A | grep -e "-head" | awk "{print \$2}") --timeout=1200s')

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
                    raise Exception('test failed')
            except Exception as e:
                pass
            time.sleep(1)
            count += 1
        if count >= 90:
            raise Exception('failed to run script')

        container.stop()
        client.close()

    def test_kill_worker(self):
        # if not ray_ha_supported():
        #   raise unittest.SkipTest("ray ha is not supported")
        # TODO: implement me
        pass


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
