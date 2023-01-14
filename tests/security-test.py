"""Test for the Pod security standard in CI"""
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
        Create a RayCluster pod with proper securityContext configurations and a simple pod without
        securityContext configurations in pod-security namespace. pod-security namespace forces the
        restricted Pod security standard to all pods. Without securityContext configurationsthe,pod
        will be rejected since it violate the policies defined in restricted security standard.
        '''
        K8S_CLUSTER_MANAGER.delete_kind_cluster()
        # kind_config enables audit logging with the audit policy
        # that listen to the Pod events in the namespace pod-security
        kind_config = CONST.REPO_ROOT.joinpath("ray-operator/config/security/kind-config.yaml")
        K8S_CLUSTER_MANAGER.create_kind_cluster(kind_config = kind_config)
        # apply the restricted Pod security standard to all Pods in the namespace pod-security.
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
            CONST.OPERATOR_IMAGE_KEY: 'kuberay/operator:nightly'
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
        try:
            #create a RayCluster with securityContext configurations in namespace pod-security.
            ray_cluster_add_event = RayClusterAddCREvent(
                custom_resource_object = context['cr'],
                rulesets = [],
                timeout = 90,
                namespace=cluster_namespace,
                filepath = context['filepath']
            )
            ray_cluster_add_event.trigger()
        except Exception as ex:
            logger.error(f"RayServiceAddCREvent fails to converge: {str(ex)}")
            raise Exception("create_ray_cluster fails") from ex
        # Create a node without securityContext configurations in namespace pod-security.
        # The pod should be forbidden.
        if shell_subprocess_run(
            f'kubectl run -n {cluster_namespace} busybox --image=busybox --restart=Never',
            check = False) == 0:
            logger.error(f'forceing the restricted Pod security standard fails')
            raise Exception("forceing the restricted Pod security standard fails")

if __name__ == '__main__':
    unittest.main(verbosity=2)
