''' Test sample RayJob YAML files to catch invalid and outdated ones. '''
import unittest
import os
import logging
import yaml

from framework.prototype import (
    RuleSet,
    GeneralTestCase,
    RayJobAddCREvent,
    EasyJobRule,
    ShutdownJobRule,
)

from framework.utils import (
    CONST
)

logger = logging.getLogger(__name__)

if __name__ == '__main__':
    NAMESPACE = 'default'
    SAMPLE_PATH = CONST.REPO_ROOT.joinpath("ray-operator/config/samples/")
    YAMLs = ['ray-job.sample.yaml', 'ray-job.shutdown.yaml', 'ray-job.custom-head-svc.yaml', 'ray-job.resources.yaml']

    sample_yaml_files = []
    for filename in YAMLs:
        filepath = SAMPLE_PATH.joinpath(filename)
        with open(filepath, encoding="utf-8") as cr_yaml:
            for k8s_object in yaml.safe_load_all(cr_yaml):
                if k8s_object['kind'] == 'RayJob':
                    sample_yaml_files.append(
                        {'path': filepath, 'name': filename, 'cr': k8s_object}
                    )
                    break
    # NOTE: The Ray Job "SUCCEEDED" status is checked in the `RayJobAddCREvent` itself.
    # (The event is not considered "converged" until the job has succeeded.) The EasyJobRule
    # is only used to additionally check that the Ray Cluster remains alive and functional.
    rs = RuleSet([EasyJobRule(), ShutdownJobRule()])

    # Build a test plan
    logger.info("Building a test plan ...")
    test_cases = unittest.TestSuite()
    for index, new_cr in enumerate(sample_yaml_files):
        logger.info('[TEST %d]: %s', index, new_cr['name'])
        addEvent = RayJobAddCREvent(new_cr['cr'], [rs], 300, NAMESPACE, new_cr['path'])
        test_cases.addTest(GeneralTestCase('runtest', addEvent))

    # Execute all testsCRs
    runner = unittest.TextTestRunner()
    test_result = runner.run(test_cases)

    # Without this line, the exit code will always be 0.
    assert test_result.wasSuccessful()
