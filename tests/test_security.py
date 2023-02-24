"""
https://github.com/ray-project/kuberay/blob/master/docs/guidance/pod-security.md
Test for pod-security.md in CI
"""
import os
import logging
import unittest
import yaml
import jsonpatch
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
    Test for the document for the Pod security standard in CI.
    The differences between this test and pod-security.md is:
    (Step 5.1) Installs a simple Pod without securityContext instead of a RayCluster.
    """
    namespace = "pod-security"

    @classmethod
    def setUpClass(cls):
        K8S_CLUSTER_MANAGER.delete_kind_cluster()
        kind_config = CONST.REPO_ROOT.joinpath("ray-operator/config/security/kind-config.yaml")
        K8S_CLUSTER_MANAGER.create_kind_cluster(kind_config = kind_config)
        # Apply the restricted Pod security standard to all Pods in the namespace pod-security.
        # The label pod-security.kubernetes.io/enforce=restricted means that the Pod that violates
        # the policies will be rejected.
        shell_subprocess_run(f"kubectl create ns {PodSecurityTestCase.namespace}")
        shell_subprocess_run(f"kubectl label --overwrite ns {PodSecurityTestCase.namespace} \
                             {PodSecurityTestCase.namespace}.kubernetes.io/warn=restricted \
                             {PodSecurityTestCase.namespace}.kubernetes.io/warn-version=latest \
                             {PodSecurityTestCase.namespace}.kubernetes.io/audit=restricted \
                             {PodSecurityTestCase.namespace}.kubernetes.io/audit-version=latest \
                             {PodSecurityTestCase.namespace}.kubernetes.io/enforce=restricted \
                             {PodSecurityTestCase.namespace}.kubernetes.io/enforce-version=latest")
        # Install the KubeRay operator in the namespace pod-security.
        image_dict = {
            CONST.RAY_IMAGE_KEY: 'rayproject/ray-ml:2.3.0',
            CONST.OPERATOR_IMAGE_KEY: os.getenv('OPERATOR_IMAGE','kuberay/operator:nightly'),
        }
        logger.info(image_dict)
        patch = jsonpatch.JsonPatch([{
            'op': 'add',
            'path': '/securityContext',
            'value': {
                'allowPrivilegeEscalation': False,
                'capabilities': {'drop':["ALL"]},
                'runAsNonRoot': True,
                'seccompProfile': {'type': 'RuntimeDefault'}
            }
        }])
        operator_manager = OperatorManager(image_dict, PodSecurityTestCase.namespace, patch)
        operator_manager.prepare_operator()

    def test_ray_cluster_with_security_context(self):
        """
        Create a RayCluster with securityContext config under restricted mode.
        """
        context = {}
        cr_yaml = CONST.REPO_ROOT.joinpath(
            "ray-operator/config/security/ray-cluster.pod-security.yaml"
        )
        with open(cr_yaml, encoding="utf-8") as cr_fd:
            context['filepath'] = cr_fd.name
            for k8s_object in yaml.safe_load_all(cr_fd):
                if k8s_object['kind'] == 'RayCluster':
                    context['cr'] = k8s_object
                    break

        logger.info('[TEST]:Create RayCluster with securityContext config under restricted mode')
        ray_cluster_add_event = RayClusterAddCREvent(
            custom_resource_object = context['cr'],
            rulesets = [],
            timeout = 90,
            namespace = PodSecurityTestCase.namespace,
            filepath = context['filepath']
        )
        ray_cluster_add_event.trigger()

    def test_pod_without_security_context(self):
        """
        Create a pod without securityContext config under restricted mode.
        """
        k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
        pod_spec = client.V1PodSpec(containers=[client.V1Container(name='busybox',image='busybox')])
        pod_metadata = client.V1ObjectMeta(name='my-pod', namespace=PodSecurityTestCase.namespace)
        pod_body = client.V1Pod(api_version='v1', kind='Pod', metadata=pod_metadata, spec=pod_spec)
        logger.info('[TEST]:Create pod without securityContext config under restricted mode')
        with self.assertRaises(
            ApiException,
            msg = 'A Pod that violates restricted security policies should be rejected.'
        ) as ex:
            k8s_v1_api.create_namespaced_pod(namespace=PodSecurityTestCase.namespace, body=pod_body)
        # check if raise forbidden error. Only forbidden error is allowed
        self.assertEqual(
            first = ex.exception.status,
            second = 403,
            msg = f'Error code 403 is expected but Pod creation failed with {ex.exception.status}'
        )

if __name__ == '__main__':
    unittest.main(verbosity=2)
