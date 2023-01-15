"""Test for the Pod security standard in CI"""
import os
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
    """
    https://github.com/ray-project/kuberay/blob/master/docs/guidance/pod-security.md
    Test for the document for the Pod security standard in CI
    """
    def test_pod_security(self):
        '''
        The differences between this test and pod-security.md are:
        (1) (Step 4) Installs the operator in the default namespace rather than the pod-security namespace.
        (2) (Step 5.1) Installs a simple Pod without securityContext instead of a RayCluster.
        '''
        K8S_CLUSTER_MANAGER.delete_kind_cluster()
        kind_config = CONST.REPO_ROOT.joinpath("ray-operator/config/security/kind-config.yaml")
        K8S_CLUSTER_MANAGER.create_kind_cluster(kind_config = kind_config)
        # Apply the restricted Pod security standard to all Pods in the namespace pod-security.
        # The label pod-security.kubernetes.io/enforce=restricted means that
        # the Pod that violate the policies will be rejected.
        cluster_namespace = "pod-security"
        shell_subprocess_run(f"kubectl create ns {cluster_namespace}")
        shell_subprocess_run(f"kubectl label --overwrite ns {cluster_namespace} \
                             {cluster_namespace}.kubernetes.io/warn=restricted \
                             {cluster_namespace}.kubernetes.io/warn-version=latest \
                             {cluster_namespace}.kubernetes.io/audit=restricted \
                             {cluster_namespace}.kubernetes.io/audit-version=latest \
                             {cluster_namespace}.kubernetes.io/enforce=restricted \
                             {cluster_namespace}.kubernetes.io/enforce-version=latest")
        # Install the KubeRay operator in default namespace(for now)
        image_dict = {
            CONST.RAY_IMAGE_KEY: 'rayproject/ray-ml:2.2.0',
            CONST.OPERATOR_IMAGE_KEY: os.getenv('OPERATOR_IMAGE', default='kuberay/operator:nightly'),
        }
        operator_manager = OperatorManager(image_dict)
        operator_manager.prepare_operator()

        context = {}
        cr_yaml = CONST.REPO_ROOT.joinpath("ray-operator/config/security/ray-cluster.pod-security.yaml")
        with open(cr_yaml, encoding="utf-8") as ray_cluster_yaml:
            context['filepath'] = ray_cluster_yaml.name
            for k8s_object in yaml.safe_load_all(ray_cluster_yaml):
                if k8s_object['kind'] == 'RayCluster':
                    context['cr'] = k8s_object
                    break
        # Create a RayCluster with securityContext configurations in namespace pod-security.
        ray_cluster_add_event = RayClusterAddCREvent(
            custom_resource_object = context['cr'],
            rulesets = [],
            timeout = 90,
            namespace=cluster_namespace,
            filepath = context['filepath']
        )
        ray_cluster_add_event.trigger()
        # Create a pod without securityContext configurations in namespace pod-security.
        if shell_subprocess_run(
            f'kubectl run -n {cluster_namespace} busybox --image=busybox',
            check = False) == 0:
            logger.error(f'A Pod that violates restricted security policies should be rejected.')
            raise Exception("A Pod that violates restricted security policies should be rejected.")

if __name__ == '__main__':
    unittest.main(verbosity=2)
