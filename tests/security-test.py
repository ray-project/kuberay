#!/usr/bin/env python
import logging
import unittest
import yaml

from framework.prototype import (
    RayClusterAddCREvent
)

from framework.utils import (
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

class PodSecurityTestCase(unittest.TestCase):
    """Test for the document for the Pod security standard in CI """
    def test_pod_security(self):
        '''
        Create a RayCluster with proper securityContext configurations in a namespace 
        that force the restricted Pod security standard to all pods.
        '''
        cluster_with_pod_security = CONST.REPO_ROOT.joinpath("ray-operator/config/security/ray-cluster.pod-security.yaml")
        cluster_without_pod_security = CONST.REPO_ROOT.joinpath("ray-operator/config/samples/ray-cluster.complete.yaml")
        kind_config = CONST.REPO_ROOT.joinpath("ray-operator/config/security/kind-config.yaml")
        image_dict = {
            CONST.RAY_IMAGE_KEY: 'rayproject/ray:2.2.0',
            CONST.OPERATOR_IMAGE_KEY: 'kuberay/operator:nightly'
        }
        cluster_namespace = "pod-security"
        K8S_CLUSTER_MANAGER.delete_kind_cluster()
        # kind_config enables audit logging with the audit policy
        # that listen to the Pod events in the namespace pod-security
        K8S_CLUSTER_MANAGER.create_kind_cluster(kind_config = kind_config)
        # apply the restricted Pod security standard to all Pods in the namespace pod-security.
        # The label pod-security.kubernetes.io/enforce=restricted means that
        # the Pod that violate the policies will be rejected.
        shell_subprocess_run(f"kubectl create ns {cluster_namespace}")
        shell_subprocess_run(f"kubectl label --overwrite ns {cluster_namespace} \
                             {cluster_namespace}.kubernetes.io/warn=restricted \
                             {cluster_namespace}.kubernetes.io/warn-version=latest \
                             {cluster_namespace}.kubernetes.io/audit=restricted \
                             {cluster_namespace}.kubernetes.io/audit-version=latest \
                             {cluster_namespace}.kubernetes.io/enforce=restricted \
                             {cluster_namespace}.kubernetes.io/enforce-version=latest")
        # Install the KubeRay operator in default namespace(for now)
        operator_manager = OperatorManager(image_dict)
        operator_manager.prepare_operator()

        context = {}
        with open(cluster_with_pod_security, encoding="utf-8") as ray_cluster_yaml:
            context['filepath'] = ray_cluster_yaml.name
            for k8s_object in yaml.safe_load_all(ray_cluster_yaml):
                if k8s_object['kind'] == 'RayCluster':
                    context['cr'] = k8s_object
                    break
        try:
            #create a RayCluster with proper securityContext configurations
            ray_cluster_add_event = RayClusterAddCREvent(
                custom_resource_object = context['cr'],
                rulesets = [],
                timeout = 500,
                namespace=cluster_namespace,
                filepath = context['filepath']
            )
            ray_cluster_add_event.trigger()
        except Exception as ex:
            logger.error(f"RayServiceAddCREvent fails to converge: {str(ex)}")
            raise Exception("create_ray_cluster fails") from ex
if __name__ == '__main__':
    unittest.main(verbosity=2)
