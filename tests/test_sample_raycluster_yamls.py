''' Test sample RayCluster YAML files to catch invalid and outdated ones. '''
import logging
import unittest
import os
import git
import yaml

from framework.prototype import (
    RuleSet,
    GeneralTestCase,
    RayClusterAddCREvent,
    HeadPodNameRule,
    EasyJobRule,
    HeadSvcRule,
)

from framework.utils import (
    CONST
)

logger = logging.getLogger(__name__)

if __name__ == '__main__':
    NAMESPACE = 'default'
    SAMPLE_PATH = CONST.REPO_ROOT.joinpath("ray-operator/config/samples/")

    sample_yaml_files = []

    # The free plan of GitHub Actions (i.e. KubeRay CI) only supports 2-core CPU runners. Most
    # sample YAMLs cannot schedule all pods on Kubernetes nodes due to insufficient CPUs. We
    # decided to just run some tests on KubeRay CI and run all tests in the Ray CI.
    # See https://github.com/ray-project/kuberay/issues/695.
    GITHUB_ACTIONS = os.getenv("GITHUB_ACTIONS", default="False").lower() == "true"
    github_action_tests = {
        "ray-cluster.getting-started.yaml",
        "ray-cluster.ingress.yaml",
        "ray-cluster.mini.yaml",
        "ray-cluster.external-redis.yaml"
    }

    # Paths of untracked files, specified as strings, relative to KubeRay
    # git root directory.
    untracked_files = set(
        git.Repo(CONST.REPO_ROOT).untracked_files
    )

    for file in os.scandir(SAMPLE_PATH):
        if not file.is_file():
            continue
        # For local development, skip untracked files.
        if os.path.relpath(file.path, CONST.REPO_ROOT) in untracked_files:
            continue
        with open(file, encoding="utf-8") as cr_yaml:
            if GITHUB_ACTIONS and file.name not in github_action_tests:
                continue
            for k8s_object in yaml.safe_load_all(cr_yaml):
                if k8s_object['kind'] == 'RayCluster':
                    sample_yaml_files.append(
                        {'path': file.path, 'name': file.name, 'cr': k8s_object}
                    )
                    break

    skip_tests = {
        'ray-cluster.complete.large.yaml': 'Skip this test because it requires a lot of resources.',
        'ray-cluster.autoscaler.large.yaml':
            'Skip this test because it requires a lot of resources.'
    }

    rs = RuleSet([HeadPodNameRule(), EasyJobRule(), HeadSvcRule()])
    image_dict = {
        CONST.RAY_IMAGE_KEY: os.getenv('RAY_IMAGE', default='rayproject/ray:2.3.0'),
        CONST.OPERATOR_IMAGE_KEY: os.getenv('OPERATOR_IMAGE', default='kuberay/operator:nightly'),
    }
    logger.info(image_dict)
    # Build a test plan
    logger.info("Build a test plan ...")
    test_cases = unittest.TestSuite()
    for index, new_cr in enumerate(sample_yaml_files):
        if new_cr['name'] in skip_tests:
            logger.info('[SKIP TEST %d] %s: %s', index, new_cr['name'], skip_tests[new_cr['name']])
            continue
        logger.info('[TEST %d]: %s', index, new_cr['name'])
        addEvent = RayClusterAddCREvent(new_cr['cr'], [rs], 90, NAMESPACE, new_cr['path'])
        test_cases.addTest(GeneralTestCase('runtest', image_dict, addEvent))

    # Execute all tests
    runner = unittest.TextTestRunner()
    test_result = runner.run(test_cases)

    # Without this line, the exit code will always be 0.
    assert test_result.wasSuccessful()
