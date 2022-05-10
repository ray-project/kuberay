#!/usr/bin/env python
import logging
import os
import sys
import tempfile
import time
import unittest
from string import Template

import docker

ray_version = '1.9.0'
ray_image = "rayproject/ray:1.9.0"

kindcluster_config_file = 'tests/config/cluster-config.yaml'

raycluster_spec_template = 'tests/config/ray-cluster.mini.yaml.template'
raycluster_service_file = 'tests/config/raycluster-service.yaml'

logger = logging.getLogger(__name__)

kuberay_sha = 'nightly'

def shell_run(cmd):
    logger.info(cmd)
    return os.system(cmd)


def shell_assert_success(cmd):
    assert shell_run(cmd) == 0


def shell_assert_failure(cmd):
    assert shell_run(cmd) != 0


def create_cluster():
    shell_assert_success(
        'kind create cluster --config {}'.format(kindcluster_config_file))


def apply_kuberay_resources():
    shell_assert_success('kind load docker-image kuberay/operator:{}'.format(kuberay_sha))
    shell_assert_success('kind load docker-image kuberay/apiserver:{}'.format(kuberay_sha))
    shell_assert_success(
        'kubectl apply -k manifests/cluster-scope')

def create_kuberay_cluster():
    template = None
    with open(raycluster_spec_template, mode='r') as f:
        template = Template(f.read())

    raycluster_spec_buf = template.substitute(
        {'ray_image': ray_image, 'ray_version': ray_version})

    raycluster_spec_file = None
    with tempfile.NamedTemporaryFile('w', delete=False) as f:
        f.write(raycluster_spec_buf)
        raycluster_spec_file = f.name

    assert raycluster_spec_file is not None
    shell_assert_success('kubectl apply -f {}'.format(raycluster_spec_file))

    time.sleep(180)

    shell_assert_success(
        'kubectl wait --for=condition=ready pod -l rayCluster=raycluster-compatibility-test --timeout=1600s')
    shell_assert_success('kubectl apply -f {}'.format(raycluster_service_file))


def delete_cluster():
    shell_assert_success('kind delete cluster')


def download_images():
    client = docker.from_env()
    client.images.pull(ray_image)
    # not enabled for now
    # shell_assert_success('kind load docker-image \"{}\"'.format(ray_image))
    client.close()


class BasicRayTestCase(unittest.TestCase):
    def setUp(self):
        create_cluster()
        apply_kuberay_resources()
        download_images()
        create_kuberay_cluster()

    def test_simple_code(self):
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
            print(output, file=sys.stderr)
            raise Exception('invalid result.')
        if rtn_code != 0:
            msg = 'invalid return code {}'.format(rtn_code)
            print(msg, file=sys.stderr)
            raise Exception(msg)

        client.close()

    def tearDown(self):
        delete_cluster()


def parse_environment():
    global ray_version, ray_image, kuberay_sha
    for k, v in os.environ.items():
        if k == 'RAY_VERSION':
            print('Setting Ray image to: {}'.format(v), file=sys.stderr)
            ray_version = v
            ray_image = 'rayproject/ray:{}'.format(ray_version)
        if k == 'KUBERAY_IMG_SHA':
            print('Using KubeRay docker build SHA: {}'.format(v))
            kuberay_sha = v


if __name__ == '__main__':
    parse_environment()
    unittest.main()
