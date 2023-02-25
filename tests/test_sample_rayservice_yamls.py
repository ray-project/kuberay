''' Test sample RayService YAML files to catch invalid and outdated ones. '''
import unittest
import os
import logging
import yaml

from framework.prototype import (
    RuleSet,
    GeneralTestCase,
    RayServiceAddCREvent,
    EasyJobRule,
    CurlServiceRule
)

from framework.utils import (
    CONST
)

logger = logging.getLogger(__name__)

if __name__ == '__main__':
    NAMESPACE = 'default'
    SAMPLE_PATH = CONST.REPO_ROOT.joinpath("ray-operator/config/samples/")
    YAMLs = ['ray_v1alpha1_rayservice.yaml']

    sample_yaml_files = []
    for filename in YAMLs:
        filepath = SAMPLE_PATH.joinpath(filename)
        with open(filepath, encoding="utf-8") as cr_yaml:
            for k8s_object in yaml.safe_load_all(cr_yaml):
                sample_yaml_files.append(
                    {'path': filepath, 'name': filename, 'cr': k8s_object}
                )

    rs = RuleSet([EasyJobRule(), CurlServiceRule()])
    image_dict = {
        CONST.RAY_IMAGE_KEY: os.getenv('RAY_IMAGE', default='rayproject/ray:2.3.0'),
        CONST.OPERATOR_IMAGE_KEY: os.getenv('OPERATOR_IMAGE', default='kuberay/operator:nightly'),
    }
    logger.info(image_dict)

    # Build a test plan
    logger.info("Build a test plan ...")
    test_cases = unittest.TestSuite()
    for index, new_cr in enumerate(sample_yaml_files):
        logger.info('[TEST %d]: %s', index, new_cr['name'])
        addEvent = RayServiceAddCREvent(new_cr['cr'], [rs], 90, NAMESPACE, new_cr['path'])
        test_cases.addTest(GeneralTestCase('runtest', image_dict, addEvent))

    # Execute all tests
    runner = unittest.TextTestRunner()
    test_result = runner.run(test_cases)

    # Without this line, the exit code will always be 0.
    assert test_result.wasSuccessful()
