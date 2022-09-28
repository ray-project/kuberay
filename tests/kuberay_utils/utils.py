#!/usr/bin/env python

import os
import logging
import time
import tempfile
import docker

from string import Template

kindcluster_config_file = 'tests/config/cluster-config.yaml'
raycluster_service_file = 'tests/config/raycluster-service.yaml'

logger = logging.getLogger(__name__)
logging.basicConfig(level=logging.INFO)


def parse_ray_version(version_str):
    tmp = version_str.split('.')
    assert len(tmp) == 3
    major = int(tmp[0])
    minor = int(tmp[1])
    patch = int(tmp[2])
    return major, minor, patch


def ray_ft_supported(ray_version):
    if ray_version == "nightly":
        return True
    major, minor, _ = parse_ray_version(ray_version)
    return major * 100 + minor > 113


def ray_service_supported(ray_version):
    if ray_version == "nightly":
        return True
    major, minor, _ = parse_ray_version(ray_version)
    return major * 100 + minor > 113


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
    rtn = shell_run(
        'kubectl wait --for=condition=ready pod -n kube-system --all --timeout=900s')
    if rtn != 0:
        shell_run('kubectl get pods -A')
    assert rtn == 0


def apply_kuberay_resources(images, kuberay_operator_image, kuberay_apiserver_image):
    for image in images:
        shell_assert_success('kind load docker-image {}'.format(image))
    shell_assert_success('kubectl create -k manifests/cluster-scope-resources')
    # use kustomize to build the yaml, then change the image to the one we want to testing.
    shell_assert_success(
        ('rm -f kustomization.yaml && kustomize create --resources manifests/base && ' +
         'kustomize edit set image ' +
         'kuberay/operator:nightly={0} kuberay/apiserver:nightly={1} && ' +
         'kubectl apply -k .').format(kuberay_operator_image, kuberay_apiserver_image))


def create_kuberay_cluster(template_name, ray_version, ray_image):
    template = None
    with open(template_name, mode='r') as f:
        template = Template(f.read())

    raycluster_spec_buf = template.substitute(
        {'ray_image': ray_image, 'ray_version': ray_version})

    raycluster_spec_file = None
    with tempfile.NamedTemporaryFile('w', delete=False) as f:
        f.write(raycluster_spec_buf)
        raycluster_spec_file = f.name

    rtn = shell_run(
        'kubectl wait --for=condition=ready pod -n ray-system --all --timeout=900s')
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
        shell_run(
            'kubectl describe pod $(kubectl get pods | grep -e "-head" | awk "{print \$1}")')
        shell_run(
            'kubectl logs $(kubectl get pods | grep -e "-head" | awk "{print \$1}")')
        shell_run(
            'kubectl logs -n $(kubectl get pods -A | grep -e "-operator" | awk \'{print $1 "  " $2}\')')
    assert rtn == 0

    shell_assert_success('kubectl apply -f {}'.format(raycluster_service_file))


def create_kuberay_service(template_name, ray_version, ray_image):
    template = None
    with open(template_name, mode='r') as f:
        template = Template(f.read())

    rayservice_spec_buf = template.substitute(
        {'ray_image': ray_image, 'ray_version': ray_version})

    service_config_file = None
    with tempfile.NamedTemporaryFile('w', delete=False) as f:
        f.write(rayservice_spec_buf)
        service_config_file = f.name

    rtn = shell_run(
        'kubectl wait --for=condition=ready pod -n ray-system --all --timeout=900s')
    if rtn != 0:
        shell_run('kubectl get pods -A')
    assert rtn == 0
    assert service_config_file is not None
    shell_assert_success('kubectl apply -f {}'.format(service_config_file))

    shell_run('kubectl get pods -A')

    time.sleep(20)

    shell_assert_success('kubectl apply -f {}'.format(raycluster_service_file))

    wait_for_condition(
        lambda: shell_run(
            'kubectl get service rayservice-sample-serve-svc -o jsonpath="{.status}"') == 0,
        timeout=900,
        retry_interval_ms=5000,
    )


def delete_cluster():
    shell_run('kind delete cluster')


def download_images(images):
    client = docker.from_env()
    for image in images:
        if shell_run('docker image inspect {}'.format(image)) != 0:
            # Only pull the image from DockerHub when the image does not
            # exist in the local docker registry.
            client.images.pull(image)
    client.close()


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
