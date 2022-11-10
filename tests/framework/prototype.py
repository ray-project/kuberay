"""Configuration test framework for KubeRay"""
from typing import List
import logging
import unittest
import time
import os
import yaml
from kubernetes import client, config
import docker
import jsonpatch

# Utility functions
logger = logging.getLogger(__name__)
logging.basicConfig(
    format='%(asctime)s,%(msecs)d %(levelname)-8s [%(filename)s:%(lineno)d] %(message)s',
    datefmt='%Y-%m-%d:%H:%M:%S',
    level=logging.INFO)

def search_path(yaml_object, steps, default_value = None):
    """
    Search the position in `yaml_object` based on steps. The following example uses
    `search_path` to get the name of the first container in the head pod. If the field does
    not exist, return `default_value`.

    [Example]
    name = search_path(cr, "spec.headGroupSpec.template.spec.containers.0.name".split('.'))
    """
    curr = yaml_object
    for step in steps:
        if step.isnumeric():
            int_step = int(step)
            if int_step >= len(curr) or int_step < 0:
                return default_value
            curr = curr[int(step)]
        elif step in curr:
            curr = curr[step]
        else:
            return default_value
    return curr

# Functions for cluster preparation.
def delete_kind_cluster() -> None:
    """Delete a KinD cluster"""
    os.system("kind delete cluster")

def create_kind_cluster():
    """Create a KinD cluster"""
    os.system("kind create cluster")
    os.system("kubectl wait --for=condition=ready pod -n kube-system --all --timeout=900s")

def install_crd():
    """Install Custom Resource Definition (CRD)"""
    os.system("kubectl create -k ../../manifests/cluster-scope-resources")

def download_images(docker_images):
    """Download Docker images from DockerHub"""
    docker_client = docker.from_env()
    for image in docker_images:
        # Only pull the image from DockerHub when the image does not
        # exist in the local docker registry.
        if os.system(f'docker image inspect {image} > /dev/null') != 0:
            docker_client.images.pull(image)
    docker_client.close()

def kind_load_images(docker_images):
    """Load downloaded images into KinD cluster"""
    for image in docker_images:
        os.system(f'kind load docker-image {image}')

def install_kuberay_operator():
    """Install kuberay operator with image kuberay/operator:nightly"""
    os.system('kubectl apply -k ../../manifests/base')

def check_cluster_exist():
    """Check whether KinD cluster exists or not"""
    return os.system("kubectl cluster-info --context kind-kind") == 0

# Configuration Test Framework Abstractions: (1) Mutator (2) Rule (3) RuleSet (4) CREvent
class Mutator:
    """
    Mutator will start to mutate from `base_cr`. `patch_list` is a list of JsonPatch, and you
    can specify multiple fields that want to mutate in a single JsonPatch.
    """
    def __init__(self, base_custom_resource, json_patch_list: List[jsonpatch.JsonPatch]):
        self.base_cr = base_custom_resource
        self.patch_list = json_patch_list
    def mutate(self):
        """ Generate a new cr by applying the json patch to `cr`. """
        for patch in self.patch_list:
            yield patch.apply(self.base_cr)

class Rule:
    """
    Rule is used to check whether the actual cluster state is the same as our expectation after
    a CREvent. We can infer the expected state by CR YAML file, and get the actual cluster state
    by Kubernetes API.
    """
    def __init__(self):
        pass
    def trigger_condition(self, custom_resource=None) -> bool:
        """
        The rule will only be checked when `trigger_condition` is true. For example, we will only
        check "HeadPodNameRule" when "spec.headGroupSpec" is defined in CR YAML file.
        """
        return True
    def assert_rule(self, custom_resource=None, cr_namespace='default'):
        """Check whether the actual cluster state fulfills the rule or not."""
        raise NotImplementedError

class RuleSet:
    """A set of Rule"""
    def __init__(self, rules: List[Rule]):
        self.rules = rules
    def check_rule_set(self, custom_resource, namespace):
        """Check all rules that the trigger conditions are fulfilled."""
        for rule in self.rules:
            if rule.trigger_condition(custom_resource):
                rule.assert_rule(custom_resource, namespace)

class CREvent:
    """
    CREvent: Custom Resource Event can be mainly divided into 3 categories.
    (1) Add (create) CR (2) Update CR (3) Delete CR
    """
    def __init__(self, custom_resource_object,
        rulesets: List[RuleSet], timeout, namespace, filepath = None):
        self.rulesets = rulesets
        self.timeout = timeout
        self.namespace = namespace
        self.custom_resource_object = custom_resource_object
        # A file may consists of multiple Kubernetes resources (ex: ray-cluster.external-redis.yaml)
        self.filepath = filepath

    def trigger(self):
        """
        The member functions integrate together in `trigger()`.
        [Step1] exec(): Execute a command to trigger the CREvent.
        [Step2] wait(): Wait for the system to converge.
        [Step3] check_rule_sets(): When the system converges, check all registered RuleSets.
        """
        self.exec()
        self.wait()
        self.check_rule_sets()
    def exec(self):
        """
        Execute a command to trigger the CREvent. For example, create a CR by a
        `kubectl apply` command.
        """
        raise NotImplementedError
    def wait(self):
        """Wait for the system to converge."""
        time.sleep(self.timeout)
    def check_rule_sets(self):
        """When the system converges, check all registered RuleSets."""
        for ruleset in self.rulesets:
            ruleset.check_rule_set(self.custom_resource_object, self.namespace)

# My implementations
class HeadPodNameRule(Rule):
    """Check head pod's name"""
    def trigger_condition(self, custom_resource=None) -> bool:
        steps = "spec.headGroupSpec".split('.')
        return search_path(custom_resource, steps) is not None

    def assert_rule(self, custom_resource=None, cr_namespace='default'):
        expected_val = search_path(custom_resource,
            "spec.headGroupSpec.template.spec.containers.0.name".split('.'))
        headpods = client.CoreV1Api().list_namespaced_pod(
            namespace = cr_namespace, label_selector='ray.io/node-type=head')
        assert headpods.items[0].spec.containers[0].name == expected_val

class HeadSvcRule(Rule):
    """The labels of the head pod and the selectors of the head service must match."""
    def assert_rule(self, custom_resource=None, cr_namespace='default'):
        head_services = client.CoreV1Api().list_namespaced_service(
            namespace= cr_namespace, label_selector="ray.io/node-type=head")
        assert len(head_services.items) == 1
        selector_dict = head_services.items[0].spec.selector
        selector = ','.join(map(lambda key: f"{key}={selector_dict[key]}", selector_dict))
        headpods = client.CoreV1Api().list_namespaced_pod(
            namespace =cr_namespace, label_selector=selector)
        assert len(headpods.items) == 1

class EasyJobRule(Rule):
    """Submit a very simple Ray job to test the basic functionality of the Ray cluster."""
    def assert_rule(self, custom_resource=None, cr_namespace='default'):
        headpods = client.CoreV1Api().list_namespaced_pod(
            namespace = cr_namespace, label_selector='ray.io/node-type=head')
        headpod_name = headpods.items[0].metadata.name
        rtn = os.system(
            f"kubectl exec {headpod_name} --" +
            " python -c \"import ray; ray.init(); print(ray.cluster_resources())\"")
        assert rtn == 0

class RayClusterAddCREvent(CREvent):
    """CREvent for RayCluster addition"""
    def exec(self):
        if not self.filepath:
            k8s_cr_api = client.CustomObjectsApi()
            k8s_cr_api.create_namespaced_custom_object(
                group = 'ray.io',version = 'v1alpha1', namespace = self.namespace,
                plural = 'rayclusters', body = self.custom_resource_object)
            k8s_cr_api.api_client.rest_client.pool_manager.clear()
            k8s_cr_api.api_client.close()
        else:
            os.system(f"kubectl apply -n {self.namespace} -f {self.filepath}")

    def wait(self):
        def check_pod_running(pods) -> bool:
            for pod in pods:
                if pod.status.phase != 'Running':
                    return False
            return True
        start_time = time.time()
        expected_head_pods = search_path(self.custom_resource_object,
            "spec.headGroupSpec.replicas".split('.'), default_value=1)
        worker_group_specs = search_path(self.custom_resource_object,
            "spec.workerGroupSpecs".split('.'), default_value=[])
        expected_worker_pods = 0
        for spec in worker_group_specs:
            expected_worker_pods += spec['replicas']
        # Wait until:
        #   (1) The number of head pods and worker pods are as expected.
        #   (2) All head pods and worker pods are "Running".
        converge = False
        k8s_v1_api = client.CoreV1Api()
        for _ in range(self.timeout):
            headpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=head')
            workerpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=worker')
            if (len(headpods.items) == expected_head_pods
                    and len(workerpods.items) == expected_worker_pods
                    and check_pod_running(headpods.items) and check_pod_running(workerpods.items)):
                converge = True
                logger.info("--- RayClusterAddCREvent %s seconds ---", time.time() - start_time)
                break
            time.sleep(1)
        # I hope to move k8s_v1_api to constructor and close it in the destructor,
        # but test_sample_raycluster_yamls.py will fail with unknown reasons.
        k8s_v1_api.api_client.rest_client.pool_manager.clear()
        k8s_v1_api.api_client.close()

        if not converge:
            # Fail to converge. Print some information to debug.
            logger.info("RayClusterAddCREvent failed to converge in %d seconds.", self.timeout)
            logger.info("expected_head_pods: %d, expected_worker_pods: %d",
                expected_head_pods, expected_worker_pods)
            os.system(f'kubectl get all -n={self.namespace}')
            os.system(f'kubectl describe pods -n={self.namespace}')
            # Raise an exception
            raise Exception("RayClusterAddCREvent wait() timeout")

class RayClusterDeleteCREvent(CREvent):
    """CREvent for RayCluster deletion"""
    def exec(self):
        k8s_cr_api = client.CustomObjectsApi()
        k8s_cr_api.delete_namespaced_custom_object(
            group = 'ray.io', version = 'v1alpha1', namespace = self.namespace,
            plural = 'rayclusters', name = self.custom_resource_object['metadata']['name'])
        k8s_cr_api.api_client.rest_client.pool_manager.clear()
        k8s_cr_api.api_client.close()

    def wait(self):
        converge = False
        k8s_v1_api = client.CoreV1Api()
        start_time = time.time()
        for _ in range(self.timeout):
            headpods = client.CoreV1Api().list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=head')
            workerpods = client.CoreV1Api().list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=worker')
            if (len(headpods.items) == 0 and len(workerpods.items) == 0):
                converge = True
                logger.info("--- RayClusterDeleteCREvent %s seconds ---", time.time() - start_time)
                break
            time.sleep(1)
        k8s_v1_api.api_client.rest_client.pool_manager.clear()
        k8s_v1_api.api_client.close()
        if not converge:
            raise Exception("RayClusterDeleteCREvent wait() timeout")

class GeneralTestCase(unittest.TestCase):
    """TestSuite"""
    def __init__(self, methodName, docker_images, cr_event):
        super().__init__(methodName)
        self.cr_event = cr_event
        self.images = docker_images

    @classmethod
    def setUpClass(cls):
        delete_kind_cluster()

    def setUp(self):
        if not check_cluster_exist():
            create_kind_cluster()
            install_crd()
            download_images(self.images)
            kind_load_images(self.images)
            install_kuberay_operator()
            config.load_kube_config()

    def runtest(self):
        """Run a configuration test"""
        self.cr_event.trigger()

    def tearDown(self) -> None:
        try:
            delete_event = RayClusterDeleteCREvent(self.cr_event.custom_resource_object,
                [], self.cr_event.timeout, self.cr_event.namespace)
            delete_event.trigger()
        except Exception as ex:
            logger.error(str(ex))
            delete_kind_cluster()

if __name__ == '__main__':
    TEMPLATE_NAME = 'config/ray-cluster.mini.yaml.template'
    NAMESPACE = 'default'
    with open(TEMPLATE_NAME, encoding="utf-8") as base_yaml:
        base_cr = yaml.load(base_yaml, Loader=yaml.FullLoader)
    patch_list = [
        # Pass
        jsonpatch.JsonPatch([{'op': 'replace',
            'path': '/spec/headGroupSpec/template/spec/containers/0/name','value': 'ray-head-1'}
        ]),
        # Pass
        jsonpatch.JsonPatch([{'op': 'replace',
            'path': '/spec/headGroupSpec/template/spec/containers/0/name', 'value': 'ray-head-2'}
        ]),
        # Reproduce #587: https://github.com/ray-project/kuberay/pull/587
        jsonpatch.JsonPatch([
            {'op': 'replace', 'path': '/spec/workerGroupSpecs/0/replicas', 'value': 2},
            {'op': 'add', 'path': '/spec/workerGroupSpecs/0/template/metadata/name', 'value': 'ha'}
        ]),
        # Reproduce #585: https://github.com/ray-project/kuberay/pull/585
        jsonpatch.JsonPatch([{'op': 'add',
            'path': '/spec/headGroupSpec/rayStartParams/object-manager-port', 'value': '12345'}
        ]),
        # Reproduce: (Fixed by pull request #572. Use v0.3.0 to reproduce.)
        #   #572: https://github.com/ray-project/kuberay/pull/572
        #   #530: https://github.com/ray-project/kuberay/pull/530
        jsonpatch.JsonPatch([{'op': 'add',
            'path': '/spec/headGroupSpec/template/metadata/labels/app.kubernetes.io~1name',
            'value': 'ray'}
        ]),
        # Reproduce #529: https://github.com/ray-project/kuberay/pull/529
        jsonpatch.JsonPatch([
            {'op': 'replace',
                'path': '/spec/headGroupSpec/template/spec/containers/0/resources/requests/memory',
                'value': '256Mi'},
            {'op': 'replace',
                'path': '/spec/headGroupSpec/template/spec/containers/0/resources/limits/memory',
                'value': '512Mi'}
        ])
    ]

    rs = RuleSet([HeadPodNameRule(), EasyJobRule(), HeadSvcRule()])
    mut = Mutator(base_cr, patch_list)
    images = ['rayproject/ray:2.0.0', 'kuberay/operator:nightly', 'kuberay/apiserver:nightly']

    test_cases = unittest.TestSuite()
    for new_cr in mut.mutate():
        addEvent = RayClusterAddCREvent(new_cr, [rs], 90, NAMESPACE)
        test_cases.addTest(GeneralTestCase('runtest', images, addEvent))
    runner = unittest.TextTestRunner()
    runner.run(test_cases)
