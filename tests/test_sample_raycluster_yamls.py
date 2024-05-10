''' Test sample RayCluster YAML files to catch invalid and outdated ones. '''
import logging
import unittest
import os
import git
import yaml
import argparse

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

def parse_args():
    parser = argparse.ArgumentParser(description='Run tests for specified YAML files.')
    parser.add_argument('--yaml-files', nargs='*', help='Use the filename under path `ray-operator/config/samples` to specify which YAML files should be tested.')
    return parser.parse_args()

if __name__ == '__main__':
    NAMESPACE = 'default'
    SAMPLE_PATH = CONST.REPO_ROOT.joinpath("ray-operator/config/samples/")

    sample_yaml_files = []

    # Paths of untracked files, specified as strings, relative to KubeRay
    # git root directory.
    untracked_files = set(
        git.Repo(CONST.REPO_ROOT).untracked_files
    )

    args = parse_args()

    for file in os.scandir(SAMPLE_PATH):
        if not file.is_file():
            continue
        # For local development, skip untracked files.
        if os.path.relpath(file.path, CONST.REPO_ROOT) in untracked_files:
            continue
        # Skip files that don't match the specified YAML files
        if args.yaml_files and file.name not in args.yaml_files:
            continue

        with open(file, encoding="utf-8") as cr_yaml:
            for k8s_object in yaml.safe_load_all(cr_yaml):
                if k8s_object['kind'] == 'RayCluster':
                    sample_yaml_files.append(
                        {'path': file.path, 'name': file.name, 'cr': k8s_object}
                    )
                    break

    skip_tests = {
        'ray-cluster.complete.large.yaml': 'Skip this test because it requires a lot of resources.',
        'ray-cluster.autoscaler.large.yaml':
            'Skip this test because it requires a lot of resources.',
        'ray-cluster.tpu-v4-singlehost.yaml': 'Skip this test because it requires TPU resources.',
        'ray-cluster.tpu-v4-multihost.yaml' : 'Skip this test because it requires TPU resources',
        'ray-cluster.gke-bucket.yaml': 'Skip this test because it requires GKE and k8s service accounts.',
        'ray-service.high-availability-locust.yaml': 'Skip this test because the RayCluster here is only used for testing RayService.',
    }

    rs = RuleSet([HeadPodNameRule(), EasyJobRule(), HeadSvcRule()])

    # Build a test plan
    logger.info("Build a test plan ...")
    test_cases = unittest.TestSuite()
    for index, new_cr in enumerate(sample_yaml_files):
        if new_cr['name'] in skip_tests:
            logger.info('[SKIP TEST %d] %s: %s', index, new_cr['name'], skip_tests[new_cr['name']])
            continue
        logger.info('[TEST %d]: %s', index, new_cr['name'])
        addEvent = RayClusterAddCREvent(new_cr['cr'], [rs], 90, NAMESPACE, new_cr['path'])
        test_cases.addTest(GeneralTestCase('runtest', addEvent))

    # Execute all tests
    runner = unittest.TextTestRunner()
    test_result = runner.run(test_cases)

    # Without this line, the exit code will always be 0.
    assert test_result.wasSuccessful()
