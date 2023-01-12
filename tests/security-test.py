#!/usr/bin/env python
import logging
import unittest
import time
import os
import random
import string
import jsonpatch
import yaml

import kuberay_utils.utils as utils
from framework.prototype import (
    CurlServiceRule,
    EasyJobRule,
    RuleSet,
    show_cluster_info
)

from framework.utils import (
    get_head_pod,
    pod_exec_command,
    shell_subprocess_run,
    CONST,
    K8S_CLUSTER_MANAGER,
    OperatorManager
)

logger = logging.getLogger(__name__)
logging.basicConfig(
    format='%(asctime)s,%(msecs)d %(levelname)-8s [%(filename)s:%(lineno)d] %(message)s',
    datefmt='%Y-%m-%d:%H:%M:%S',
    level=logging.INFO
)

# Default Ray version
ray_version = '2.2.0'

# Default docker images
ray_image = 'rayproject/ray:2.2.0'
kuberay_operator_image = 'kuberay/operator:nightly'


class PodSecurityTestCase(unittest.TestCase):
    """Test for the document for the Pod security standard in CI """

    def setUp(self):
        K8S_CLUSTER_MANAGER.delete_kind_cluster()
                
        

    def test_pod_security(self):
        '''
        Create a RayCluster with proper securityContext configurations in a namespace that force the restricted Pod security standard to all pods.
        '''
        cluster_template_with_pod_security = CONST.REPO_ROOT.joinpath("ray-operator/config/security/ray-cluster.pod-security.yaml.template")
        cluster_template_without_pod_security = CONST.REPO_ROOT.joinpath("tests/config/ray-cluster.mini.yaml.template")
        helm_chart_values_yaml_template = CONST.REPO_ROOT.joinpath("helm-chart/kuberay-operator/values.yaml")
        helm_chart_values_yaml = CONST.REPO_ROOT.joinpath("ray-operator/config/security/security-values.yaml")
        image_dict = {
            CONST.RAY_IMAGE_KEY: ray_image,
            CONST.OPERATOR_IMAGE_KEY: kuberay_operator_image
        }
        cluster_namespace = "pod-security"

        # copy helm-chart/kuberay-operator/values.yaml and update the field securityContext in the new security-values.yaml
        with open(helm_chart_values_yaml_template) as base_values:
            with open(helm_chart_values_yaml,'w') as security_values:
                base_values = yaml.load(base_values, Loader=yaml.FullLoader)
                base_values['securityContext']['allowPrivilegeEscalation'] = False
                base_values['securityContext']['capabilities'] = {'drop':["ALL"]}
                base_values['securityContext']['runAsNonRoot'] = True
                base_values['securityContext']['seccompProfile'] = {'type': 'RuntimeDefault'}
                yaml.dump(base_values,security_values)

        K8S_CLUSTER_MANAGER.delete_kind_cluster()
        K8S_CLUSTER_MANAGER.create_kind_cluster(kind_config = CONST.REPO_ROOT.joinpath("ray-operator/config/security/kind-config.yaml"))

        # apply the restricted Pod security standard to all Pods in the namespace pod-security. 
        # The label pod-security.kubernetes.io/enforce=restricted means that the Pod that violate the policies will be rejected 
        shell_subprocess_run(f"kubectl create ns {cluster_namespace}")
        time.sleep(1) 
        shell_subprocess_run( f" kubectl label --overwrite ns {cluster_namespace} \
                                {cluster_namespace}.kubernetes.io/warn=restricted \
                                {cluster_namespace}.kubernetes.io/warn-version=latest \
                                {cluster_namespace}.kubernetes.io/audit=restricted \
                                {cluster_namespace}.kubernetes.io/audit-version=latest \
                                {cluster_namespace}.kubernetes.io/enforce=restricted \
                                {cluster_namespace}.kubernetes.io/enforce-version=latest" )
      
        # Install the KubeRay operator in pod-security namespace and create a RayCluster with proper securityContext configurations
        time.sleep(1) 
        operator_manager = OperatorManager(image_dict)
        operator_manager.prepare_operator(namespace=cluster_namespace,helm_chart_values = helm_chart_values_yaml)
        utils.create_ray_cluster(cluster_template_with_pod_security, ray_version, ray_image, namespace=cluster_namespace)                        
   
def parse_environment():
    global ray_version, ray_image, kuberay_operator_image
    for k, v in os.environ.items():
        if k == 'RAY_IMAGE':
            ray_image = v
            ray_version = ray_image.split(':')[-1]
        elif k == 'OPERATOR_IMAGE':
            kuberay_operator_image = v


if __name__ == '__main__':
    parse_environment()
    logger.info('Setting Ray image to: {}'.format(ray_image))
    logger.info('Setting Ray version to: {}'.format(ray_version))
    logger.info('Setting KubeRay operator image to: {}'.format(kuberay_operator_image))
    unittest.main(verbosity=2)
