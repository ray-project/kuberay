"""
https://github.com/ray-project/kuberay/blob/master/docs/guidance/pod-security.md
Test for pod-security.md in CI
"""
import os
import logging
import unittest
import yaml
from kubernetes import client
from kubernetes.client.rest import ApiException

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
        """
        The differences between this test and pod-security.md are:
        (1) (Step 4) Installs the operator in default namespace rather than pod-security namespace.
        (2) (Step 5.1) Installs a simple Pod without securityContext instead of a RayCluster.
        """
        def create_cluster():
            """Create a KinD cluster"""
            K8S_CLUSTER_MANAGER.delete_kind_cluster()
            kind_config = CONST.REPO_ROOT.joinpath("ray-operator/config/security/kind-config.yaml")
            K8S_CLUSTER_MANAGER.create_kind_cluster(kind_config = kind_config) 
        def create_namespace_with_restricted_mode(cluster_namespace):
            """
            Create a namespace and apply the restricted Pod security standard to all Pods.
            The label pod-security.kubernetes.io/enforce=restricted means that the Pod
            that violates the policies will be rejected.
            """
            shell_subprocess_run(f"kubectl create ns {cluster_namespace}")
            shell_subprocess_run(f"kubectl label --overwrite ns {cluster_namespace} \
                                {cluster_namespace}.kubernetes.io/warn=restricted \
                                {cluster_namespace}.kubernetes.io/warn-version=latest \
                                {cluster_namespace}.kubernetes.io/audit=restricted \
                                {cluster_namespace}.kubernetes.io/audit-version=latest \
                                {cluster_namespace}.kubernetes.io/enforce=restricted \
                                {cluster_namespace}.kubernetes.io/enforce-version=latest")      
        def install_operator():
            """Install the KubeRay operator in default namespace(for now)"""
            image_dict = {
                CONST.RAY_IMAGE_KEY: 'rayproject/ray-ml:2.2.0',
                CONST.OPERATOR_IMAGE_KEY: os.getenv('OPERATOR_IMAGE','kuberay/operator:nightly'),
            }
            logger.info(image_dict)
            operator_manager = OperatorManager(image_dict)
            operator_manager.prepare_operator()
        def create_ray_cluster(cluster_namespace):
            logger.info(
                '[TEST]:Create RayCluster with securityContext config under restricted mode'
            )
            context = {}
            cr_yaml = CONST.REPO_ROOT.joinpath(
                "ray-operator/config/security/ray-cluster.pod-security.yaml"
            )
            with open(cr_yaml, encoding="utf-8") as ray_cluster_yaml:
                context['filepath'] = ray_cluster_yaml.name
                for k8s_object in yaml.safe_load_all(ray_cluster_yaml):
                    if k8s_object['kind'] == 'RayCluster':
                        context['cr'] = k8s_object
                        break
            ray_cluster_add_event = RayClusterAddCREvent(
                custom_resource_object = context['cr'],
                rulesets = [],
                timeout = 90,
                namespace = cluster_namespace,
                filepath = context['filepath']
            )
            ray_cluster_add_event.trigger()

        def install_non_compliant_pod(cluster_namespace):
            logger.info('[TEST]:Create pod without securityContext config under restricted mode')
            k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
            busybox_container = client.V1Container(name='busybox',image='busybox')
            pod_spec = client.V1PodSpec(containers= [busybox_container])
            pod_metadata = client.V1ObjectMeta(name='my-pod', namespace=cluster_namespace)
            pod_body = client.V1Pod(
                api_version='v1',
                kind='Pod',
                metadata=pod_metadata, spec=pod_spec
            )
            with self.assertRaises(
                ApiException,
                msg = 'A Pod that violates restricted security policies should be rejected.'
            ) as ex:
                k8s_v1_api.create_namespaced_pod(namespace=cluster_namespace, body=pod_body)
            # check if raise forbidden error. Only forbidden error is allowed
            error_code = ex.exception.status
            self.assertEqual(
                first = error_code,
                second = 403,
                msg = f'Error code 403 is expected but Pod creation failed with {error_code}'
            )
        cluster_namespace = "pod-security"
        create_cluster()
        create_namespace_with_restricted_mode(cluster_namespace)
        install_operator()
        create_ray_cluster(cluster_namespace)
        install_non_compliant_pod(cluster_namespace)
if __name__ == '__main__':
    unittest.main(verbosity=2)
