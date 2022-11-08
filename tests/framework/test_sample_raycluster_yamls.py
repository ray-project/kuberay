''' Test sample RayCluster YAML files to catch invalid and outdated ones. '''
import unittest
import os
import logging
import yaml

from prototype import (
    RuleSet,
    GeneralTestCase,
    RayClusterAddCREvent,
    HeadPodNameRule,
    EasyJobRule,
    HeadSvcRule,
)

logger = logging.getLogger(__name__)

if __name__ == '__main__':
    NAMESPACE = 'default'
    SAMPLE_PATH = '../../ray-operator/config/samples/'

    sample_yaml_files = []

    # The free plan of GitHub Actions (i.e. KubeRay CI) only support 2-core CPU runners. Most
    # sample YAMLs cannot schedule all pods on Kubernetes nodes due to insufficient CPUs. We
    # decided to just run some tests on KubeRay CI and run all tests in the Ray CI.
    GITHUB_ACTIONS = os.getenv("GITHUB_ACTIONS", default="False").lower() == "true"
    github_action_tests = set([
        "ray-cluster.getting-started.yaml",
        "ray-cluster.ingress.yaml",
        "ray-cluster.mini.yaml"
        ]
    )

    for filename in os.scandir(SAMPLE_PATH):
        if filename.is_file():
            with open(filename, encoding="utf-8") as cr_yaml:
                if GITHUB_ACTIONS and filename.name not in github_action_tests:
                    continue
                for k8s_object in yaml.safe_load_all(cr_yaml):
                    if k8s_object['kind'] == 'RayCluster':
                        sample_yaml_files.append(
                            {'path': filename.path, 'name': filename.name, 'cr': k8s_object}
                        )
                        break

    skip_tests = {
        'ray-cluster.complete.large.yaml': 'Skip this test because it requires a lot of resources.',
        'ray-cluster.external-redis.yaml':
            'It installs multiple Kubernetes resources and cannot clean up by DeleteCREvent.',
        'ray-cluster.autoscaler.large.yaml':
            'Skip this test because it requires a lot of resources.'
    }

    rs = RuleSet([HeadPodNameRule(), EasyJobRule(), HeadSvcRule()])
    images = [
        os.getenv('RAY_IMAGE', default='rayproject/ray:2.0.0'),
        os.getenv('OPERATOR_IMAGE', default='kuberay/operator:nightly'),
        os.getenv('APISERVER_IMAGE', default='kuberay/apiserver:nightly')
    ]
    logger.info(images)
    # Build a test plan
    logger.info("Build a test plan ...")
    test_cases = unittest.TestSuite()
    for index, new_cr in enumerate(sample_yaml_files):
        if new_cr['name'] in skip_tests:
            logger.info('[SKIP TEST %d] %s: %s', index, new_cr['name'], skip_tests[new_cr['name']])
            continue
        logger.info('[TEST %d]: %s', index, new_cr['name'])
        addEvent = RayClusterAddCREvent(new_cr['cr'], [rs], 90, NAMESPACE, new_cr['path'])
        test_cases.addTest(GeneralTestCase('runtest', images, addEvent))

    # Execute all tests
    runner = unittest.TextTestRunner()
    test_result = runner.run(test_cases)

    # Without this line, the exit code will always be 0.
    assert test_result.wasSuccessful()
