''' Test sample RayService YAML files to catch invalid and outdated ones. '''
import unittest
import os
import logging
import yaml

from prototype import (
    RuleSet,
    GeneralTestCase,
    RayServiceAddCREvent,
    EasyJobRule,
    CurlServiceRule,
)


logger = logging.getLogger(__name__)

if __name__ == '__main__':
    NAMESPACE = 'default'
    SAMPLE_PATH = '../../ray-operator/config/samples/'

    sample_yaml_files = []
    for filename in os.scandir(SAMPLE_PATH):
        if filename.is_file():
            with open(filename, encoding="utf-8") as cr_yaml:
                for k8s_object in yaml.safe_load_all(cr_yaml):
                    if k8s_object['kind'] == 'RayService':
                        sample_yaml_files.append(
                            {'path': filename.path, 'name': filename.name, 'cr': k8s_object}
                        )
                        break
    for val in sample_yaml_files:
        print(val['path'], val['name'], val['cr']['kind'])

    rs = RuleSet([EasyJobRule(), CurlServiceRule()])
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
        logger.info('[TEST %d]: %s', index, new_cr['name'])
        addEvent = RayServiceAddCREvent(new_cr['cr'], [rs], 90, NAMESPACE, new_cr['path'])
        test_cases.addTest(GeneralTestCase('runtest', images, addEvent))

    # Execute all tests
    runner = unittest.TextTestRunner()
    test_result = runner.run(test_cases)

    # Without this line, the exit code will always be 0.
    assert test_result.wasSuccessful()
