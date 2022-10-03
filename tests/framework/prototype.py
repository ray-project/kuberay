import yaml
from typing import Optional
from kubernetes import client, config, utils
import os
import time
import docker
import logging
import unittest
import jsonpatch

# Utility functions
logger = logging.getLogger(__name__)
logging.basicConfig(level=logging.INFO)

def search_path(cr, steps):
    curr = cr
    for step in steps:
        if step.isnumeric():
            int_step = int(step)
            if int_step >= len(curr) or int_step < 0:
                return None
            curr = curr[int(step)]
        elif step in curr:
            curr = curr[step]
        else:
            return None
    return curr

'''
Functions for cluster preparation. Typical workflow:
  Delete KinD cluster -> Create KinD cluster -> Install CRD -> Download Images (from DockerHub) ->
  Load images into KinD cluster -> Install KubeRay operator
'''
def delete_kind_cluster() -> None:
    """Delete a KinD cluster"""
    os.system("kind delete cluster")

def create_kind_cluster():
    """Create a KinD cluster"""
    os.system("kind create cluster")
    os.system("kubectl wait --for=condition=ready pod -n kube-system --all --timeout=900s")

def install_crd():
    """Install Custom Resource Definition (CRD)"""
    KUBERAY_VERSION = "v0.3.0"
    os.system("kubectl create -k \"github.com/ray-project/kuberay/manifests/cluster-scope-resources?" +
              "ref={}&timeout=90s\"".format(KUBERAY_VERSION))

def download_images(images):
    """Download Docker images from DockerHub"""
    docker_client = docker.from_env()
    for image in images:
        docker_client.images.pull(image)
    docker_client.close()

def kind_load_images(images):
    """Load downloaded images into KinD cluster"""
    for image in images:
        os.system(f'kind load docker-image {image}')

def install_kuberay_operator():
    KUBERAY_VERSION = "v0.3.0"
    os.system("kubectl apply -k \"github.com/ray-project/kuberay/manifests/base?" +
              "ref={}&timeout=90s\"".format(KUBERAY_VERSION))

'''
Configuration Test Framework Abstractions: (1) DeltaSet (2) Mutator (3) Rule (4) RuleSet (5) CREvent
'''

# Mutator: Mutator will start to mutate from `baseCR`. `deltaSets` is a list of DeltaSets, and each DeltaSet
#          specifies a field that wants to mutate with multiple candidate values.
class Mutator:
    def __init__(self, baseCR, patch_list: list[jsonpatch.JsonPatch]):
        self.baseCR = baseCR
        self.patch_list = patch_list
    # Generate a new cr by applying the json patch to `cr`. 
    def mutate(self):
        for patch in self.patch_list:
            yield patch.apply(self.baseCR)

# Rule: Rule is used to check whether the actual cluster state is the same as our expectation after a CREvent.
#       We can infer the expected state by CR YAML file, and get the actual cluster state by Kubernetes API.
# Example: "HeadPodNameRule"
class Rule:
    def __init__(self):
        pass
    # The rule will only be checked when `trigger_condition` is true. For example, we will only check
    # "HeadPodNameRule" when "spec.headGroupSpec" is defined in CR YAML file.
    def trigger_condition(self, cr=None) -> bool:
        return True
    def assertRule(self, cr=None, namespace='default'):
        raise NotImplementedError

# RuleSet: A set of Rule
class RuleSet:
    def __init__(self, rules: list[Rule]):
        self.rules = rules
    def checkRuleSet(self, cr, namespace):
        for rule in self.rules:
            if rule.trigger_condition(cr):
                rule.assertRule(cr, namespace)

# CREvent: Custom Resource Event can be mainly divided into 3 categories.
#   (1) Add (create) CR (2) Update CR (3) Delete CR
#
# The member functions integrate together in `trigger()`.
#   [Step1] exec(): Execute a command to trigger the CREvent. For example, create a CR by a
#                  `kubectl apply` command.
#   [Step2] wait(): Wait for the system to converge.
#   [Step3] checkRuleSets(): When the system converges, check all registered RuleSets.
class CREvent:
    def __init__(self, cr, ruleSets: list[RuleSet], timeout, namespace):
        self.ruleSets = ruleSets
        self.timeout = timeout
        self.namespace = namespace
        self.cr = cr
    def trigger(self):
        self.exec()
        self.wait()
        self.checkRuleSets()
    def exec(self):
        raise NotImplementedError
    def wait(self):
        time.sleep(self.timeout)
    def checkRuleSets(self):
        for rs in self.ruleSets:
            rs.checkRuleSet(self.cr, self.namespace)

'''
My implementations
'''
class HeadPodNameRule(Rule):
    def trigger_condition(self, cr) -> bool:
        steps = "spec.headGroupSpec".split('.')
        return (search_path(cr, steps) != None)

    def assertRule(self, cr, namespace):
        expected_val = search_path(cr, "spec.headGroupSpec.template.spec.containers.0.name".split('.'))
        headpods = client.CoreV1Api().list_namespaced_pod(namespace = namespace, label_selector='rayNodeType=head')
        assert(headpods.items[0].spec.containers[0].name == expected_val)

class EasyJobRule(Rule):
    def assertRule(self, cr=None, namespace='default'):
        headpods = client.CoreV1Api().list_namespaced_pod(namespace = namespace, label_selector='rayNodeType=head')
        headpodName = headpods.items[0].metadata.name
        rtn = os.system("kubectl exec {} -- python -c \"import ray; ray.init(); print(ray.cluster_resources())\""
                        .format(headpodName))
        assert(rtn == 0)

class RayClusterAddCREvent(CREvent):
    def exec(self):
        client.CustomObjectsApi().create_namespaced_custom_object(
            group = 'ray.io',version = 'v1alpha1', namespace = self.namespace, plural = 'rayclusters', body = self.cr,
            pretty = 'true')

    def wait(self):
        def check_pod_running(pods) -> bool:
            for pod in pods:
                if pod.status.phase != 'Running':
                    return False
            return True
        start_time = time.time()
        expected_head_pods = search_path(self.cr, "spec.headGroupSpec.replicas".split('.'))
        expected_worker_pods = search_path(self.cr, "spec.workerGroupSpecs.0.replicas".split('.'))
        # Wait until:
        #   (1) The number of head pods and worker pods are as expected.
        #   (2) All head pods and worker pods are "Running".
        for _ in range(self.timeout):
            headpods = client.CoreV1Api().list_namespaced_pod(namespace = self.namespace,
                                                              label_selector='rayNodeType={}'.format('head'))
            workerpods = client.CoreV1Api().list_namespaced_pod(namespace = self.namespace,
                                                                label_selector='rayNodeType={}'.format('worker'))
            if (len(headpods.items) == expected_head_pods and len(workerpods.items) == expected_worker_pods
                    and check_pod_running(headpods.items) and check_pod_running(workerpods.items)):
                logger.info("--- %s seconds ---" % (time.time() - start_time))
                return
            time.sleep(1)
        raise Exception("wait() timeout")

# TestSuite
class GeneralTestCase(unittest.TestCase):
    def __init__(self, methodName, images, crEvent):
        super(GeneralTestCase, self).__init__(methodName)
        self.crEvent = crEvent
        self.images = images

    def runTest(self):
        logging.info("Prepare KinD cluster")
        delete_kind_cluster()
        create_kind_cluster()
        install_crd()
        download_images(self.images)
        kind_load_images(self.images)
        install_kuberay_operator()
        config.load_kube_config()
        self.crEvent.trigger()

if __name__ == '__main__':
    template_name = 'config/ray-cluster.mini.yaml.template'
    namespace = 'default'
    with open(template_name) as base_yaml:
        baseCR = yaml.load(base_yaml, Loader=yaml.FullLoader)

    patch_list = [
        jsonpatch.JsonPatch([{'op': 'replace', 'path': '/spec/headGroupSpec/template/spec/containers/0/name', 'value': 'ray-head-1'}]),
        jsonpatch.JsonPatch([{'op': 'replace', 'path': '/spec/headGroupSpec/template/spec/containers/0/name', 'value': 'ray-head-2'}]),
        jsonpatch.JsonPatch([{'op': 'replace', 'path': '/spec/headGroupSpec/template/spec/containers/0/name', 'value': 'ray-head-3'}])
    ]
    
    rs = RuleSet([HeadPodNameRule(), EasyJobRule()])
    mut = Mutator(baseCR, patch_list)
    images = ['rayproject/ray:2.0.0', 'kuberay/operator:v0.3.0', 'kuberay/apiserver:v0.3.0']

    test_cases = unittest.TestSuite()
    for cr in mut.mutate():
        addEvent = RayClusterAddCREvent(cr, [rs], 90, namespace)
        test_cases.addTest(GeneralTestCase('runTest', images, addEvent))
    runner=unittest.TextTestRunner()
    runner.run(test_cases)