"""Configuration test framework for KubeRay"""
from typing import List
import logging
import unittest
import time
import subprocess
import yaml
from kubernetes import client, config
import jsonpatch

# Global variables
logger = logging.getLogger(__name__)
logging.basicConfig(
    format='%(asctime)s,%(msecs)d %(levelname)-8s [%(filename)s:%(lineno)d] %(message)s',
    datefmt='%Y-%m-%d:%H:%M:%S',
    level=logging.INFO)

class CONST(object):
    """Constants"""
    __slots__ = ()
    OPERATOR_IMAGE_KEY = "kuberay-operator-image"
    RAY_IMAGE_KEY = "ray-image"
    K8S_CR_CLIENT_KEY = "k8s-cr-api-client"
    K8S_V1_CLIENT_KEY = "k8s-v1-api-client"
CONST = CONST()

# Utility functions
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

class KubernetesClusterManager:
    """
    KubernetesClusterManager controlls the lifecycle of KinD cluster and Kubernetes API client.
    """
    def __init__(self) -> None:
        self.k8s_client_dict = {}

    def delete_kind_cluster(self) -> None:
        """Delete a KinD cluster"""
        shell_subprocess_run("kind delete cluster")
        for _, k8s_client in self.k8s_client_dict.items():
            k8s_client.api_client.rest_client.pool_manager.clear()
            k8s_client.api_client.close()
        self.k8s_client_dict = {}

    def create_kind_cluster(self) -> None:
        """Create a KinD cluster"""
        shell_subprocess_run("kind create cluster --wait 900s")
        config.load_kube_config()
        self.k8s_client_dict.update({
            CONST.K8S_V1_CLIENT_KEY: client.CoreV1Api(),
            CONST.K8S_CR_CLIENT_KEY: client.CustomObjectsApi()
        })

    def check_cluster_exist(self) -> bool:
        """Check whether KinD cluster exists or not"""
        return shell_subprocess_run("kubectl cluster-info --context kind-kind", check = False) == 0
K8S_CLUSTER_MANAGER = KubernetesClusterManager()

class OperatorManager:
    """
    OperatorManager controlls the lifecycle of KubeRay operator. It will download Docker images,
    load images into an existing KinD cluster, and install CRD and KubeRay operator.
    """
    def __init__(self, docker_image_dict) -> None:
        for key in [CONST.OPERATOR_IMAGE_KEY, CONST.RAY_IMAGE_KEY]:
            if key not in docker_image_dict:
                raise Exception(f"Image {key} does not exist!")
        self.docker_image_dict = docker_image_dict

    def prepare_operator(self):
        """Prepare KubeRay operator for an existing KinD cluster"""
        self.__kind_prepare_images()
        self.__install_crd_and_operator()

    def __kind_prepare_images(self):
        """Download images and load images into KinD cluster"""
        def download_images():
            """Download Docker images from DockerHub"""
            logger.info("Download Docker images: %s", self.docker_image_dict)
            for key in self.docker_image_dict:
                # Only pull the image from DockerHub when the image does not
                # exist in the local docker registry.
                image = self.docker_image_dict[key]
                if shell_subprocess_run(
                        f'docker image inspect {image} > /dev/null', check = False) != 0:
                    shell_subprocess_run(f'docker pull {image}')

        download_images()
        logger.info("Load images into KinD cluster")
        for key in self.docker_image_dict:
            image = self.docker_image_dict[key]
            shell_subprocess_run(f'kind load docker-image {image}')

    def __install_crd_and_operator(self):
        """Install both CRD and KubeRay operator by kuberay-operator chart"""
        logger.info("Install both CRD and KubeRay operator by kuberay-operator chart")
        repo, tag = self.docker_image_dict[CONST.OPERATOR_IMAGE_KEY].split(':')
        shell_subprocess_run(
            "helm install kuberay-operator ../../helm-chart/kuberay-operator/ "
            f"--set image.repository={repo},image.tag={tag}"
        )

def check_pod_running(pods) -> bool:
    """"Check whether all of the pods are in running state"""
    for pod in pods:
        if pod.status.phase != 'Running':
            return False
    return True

def shell_subprocess_run(command, check = True):
    """
    Command will be executed through the shell. If check=True, it will raise an error when
    the returncode of the execution is not 0.
    """
    logger.info("Execute command: %s", command)
    return subprocess.run(command, shell = True, check = check).returncode

def shell_subprocess_check_output(command):
    """
    Run command and return STDOUT as encoded bytes.
    """
    logger.info("Execute command (check_output): %s", command)
    output = subprocess.check_output(command, shell=True)
    logger.info("Output: %s", output)
    return output

def get_expected_head_pods(custom_resource):
    """Get the number of head pods in custom_resource"""
    resource_kind = custom_resource["kind"]
    head_replica_paths = {
       "RayCluster": "spec.headGroupSpec.replicas",
       "RayService": "spec.rayClusterConfig.headGroupSpec.replicas",
       "RayJob": "spec.rayClusterSpec.headGroupSpec.replicas"
    }
    if resource_kind in head_replica_paths:
        path = head_replica_paths[resource_kind]
        return search_path(custom_resource, path.split('.'), default_value=1)
    raise Exception(f"Unknown resource kind: {resource_kind} in get_expected_head_pods()")

def get_expected_worker_pods(custom_resource):
    """Get the number of head pods in custom_resource"""
    resource_kind = custom_resource["kind"]
    worker_specs_paths = {
       "RayCluster": "spec.workerGroupSpecs",
       "RayService": "spec.rayClusterConfig.workerGroupSpecs",
       "RayJob": "spec.rayClusterSpec.workerGroupSpecs"
    }
    if resource_kind in worker_specs_paths:
        path = worker_specs_paths[resource_kind]
        worker_group_specs = search_path(custom_resource, path.split('.'), default_value=[])
        expected_worker_pods = 0
        for spec in worker_group_specs:
            expected_worker_pods += spec['replicas']
        return expected_worker_pods
    raise Exception(f"Unknown resource kind: {resource_kind} in get_expected_worker_pods()")

def show_cluster_info(cr_namespace):
    """Show system information"""
    shell_subprocess_run(f'kubectl get all -n={cr_namespace}')
    shell_subprocess_run(f'kubectl describe pods -n={cr_namespace}')
    # With "--tail=-1", every line in the log will be printed. The default value of "tail" is not
    # -1 when using selector.
    shell_subprocess_run(f'kubectl logs -n={cr_namespace} -l ray.io/node-type=head --tail=-1')
    operator_namespace = shell_subprocess_check_output('kubectl get pods '
        '-l app.kubernetes.io/component=kuberay-operator -A '
        '-o jsonpath={".items[0].metadata.namespace"}')
    shell_subprocess_run("kubectl logs -l app.kubernetes.io/component=kuberay-operator -n "
        f'{operator_namespace.decode("utf-8")} --tail=-1')

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
    def clean_up(self):
        """Cleanup the CR."""
        raise NotImplementedError

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
        shell_subprocess_run(f"kubectl exec {headpod_name} -n {cr_namespace} --" +
            " python -c \"import ray; ray.init(); print(ray.cluster_resources())\"")

class CurlServiceRule(Rule):
    """"Using curl to access the deployed application on Ray service"""
    def assert_rule(self, custom_resource=None, cr_namespace='default'):
        # Create a pod for running curl command, because the service is not exposed.
        shell_subprocess_run(f"kubectl run curl --image=radial/busyboxplus:curl -n {cr_namespace} "
            "--command -- /bin/sh -c \"while true; do sleep 10;done\"")
        success_create = False
        for _ in range(30):
            resp = client.CoreV1Api().read_namespaced_pod(name="curl", namespace=cr_namespace)
            if resp.status.phase != 'Pending':
                success_create = True
                break
            time.sleep(1)
        if not success_create:
            raise Exception("CurlServiceRule create curl pod timeout")
        output = shell_subprocess_check_output(f"kubectl exec curl -n {cr_namespace} "
            "-- curl -X POST -H 'Content-Type: application/json' "
            f"{custom_resource['metadata']['name']}-serve-svc.{cr_namespace}.svc.cluster.local:8000"
            " -d '[\"MANGO\", 2]'")
        assert output == b'6'
        shell_subprocess_run(f"kubectl delete pod curl -n {cr_namespace}")

class RayClusterAddCREvent(CREvent):
    """CREvent for RayCluster addition"""
    def exec(self):
        if not self.filepath:
            k8s_cr_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
            k8s_cr_api.create_namespaced_custom_object(
                group = 'ray.io',version = 'v1alpha1', namespace = self.namespace,
                plural = 'rayclusters', body = self.custom_resource_object)
        else:
            shell_subprocess_run(f"kubectl apply -n {self.namespace} -f {self.filepath}")

    def wait(self):
        start_time = time.time()
        expected_head_pods = get_expected_head_pods(self.custom_resource_object)
        expected_worker_pods = get_expected_worker_pods(self.custom_resource_object)
        # Wait until:
        #   (1) The number of head pods and worker pods are as expected.
        #   (2) All head pods and worker pods are "Running".
        converge = False
        k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
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

        if not converge:
            logger.info("RayClusterAddCREvent wait() failed to converge in %d seconds.",
                self.timeout)
            logger.info("expected_head_pods: %d, expected_worker_pods: %d",
                expected_head_pods, expected_worker_pods)
            show_cluster_info(self.namespace)
            raise Exception("RayClusterAddCREvent wait() timeout")

    def clean_up(self):
        """Delete added RayCluster"""
        k8s_cr_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
        k8s_cr_api.delete_namespaced_custom_object(
            group = 'ray.io', version = 'v1alpha1', namespace = self.namespace,
            plural = 'rayclusters', name = self.custom_resource_object['metadata']['name'])
        # Wait pods to be deleted
        converge = False
        k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
        start_time = time.time()
        for _ in range(self.timeout):
            headpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=head')
            workerpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=worker')
            if (len(headpods.items) == 0 and len(workerpods.items) == 0):
                converge = True
                logger.info("--- Cleanup RayCluster %s seconds ---", time.time() - start_time)
                break
            time.sleep(1)

        if not converge:
            logger.info("RayClusterAddCREvent clean_up() failed to converge in %d seconds.",
                self.timeout)
            logger.info("expected_head_pods: 0, expected_worker_pods: 0")
            show_cluster_info(self.namespace)
            raise Exception("RayClusterAddCREvent clean_up() timeout")

class RayServiceAddCREvent(CREvent):
    """CREvent for RayService addition"""
    def exec(self):
        """Wait for RayService to converge"""""
        if not self.filepath:
            k8s_cr_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
            k8s_cr_api.create_namespaced_custom_object(
                group = 'ray.io',version = 'v1alpha1', namespace = self.namespace,
                plural = 'rayservices', body = self.custom_resource_object)
        else:
            shell_subprocess_run(f"kubectl apply -n {self.namespace} -f {self.filepath}")

    def wait(self):
        """Wait for RayService to converge"""""
        start_time = time.time()
        expected_head_pods = get_expected_head_pods(self.custom_resource_object)
        expected_worker_pods = get_expected_worker_pods(self.custom_resource_object)
        # Wait until:
        #   (1) The number of head pods and worker pods are as expected.
        #   (2) All head pods and worker pods are "Running".
        #   (3) Service named "rayservice-sample-serve" presents
        converge = False
        k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
        for _ in range(self.timeout):
            headpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=head')
            workerpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=worker')
            head_services = k8s_v1_api.list_namespaced_service(
                namespace = self.namespace, label_selector =
                f"ray.io/serve={self.custom_resource_object['metadata']['name']}-serve")
            if (len(head_services.items) == 1 and len(headpods.items) == expected_head_pods
                    and len(workerpods.items) == expected_worker_pods
                    and check_pod_running(headpods.items) and check_pod_running(workerpods.items)):
                converge = True
                logger.info("--- RayServiceAddCREvent %s seconds ---", time.time() - start_time)
                break
            time.sleep(1)

        if not converge:
            logger.info("RayServiceAddCREvent wait() failed to converge in %d seconds.",
                self.timeout)
            logger.info("expected_head_pods: %d, expected_worker_pods: %d",
                expected_head_pods, expected_worker_pods)
            show_cluster_info(self.namespace)
            raise Exception("RayServiceAddCREvent wait() timeout")

    def clean_up(self):
        """Delete added RayService"""
        k8s_cr_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
        k8s_cr_api.delete_namespaced_custom_object(
            group = 'ray.io', version = 'v1alpha1', namespace = self.namespace,
            plural = 'rayservices', name = self.custom_resource_object['metadata']['name'])
        # Wait pods to be deleted
        converge = False
        k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
        start_time = time.time()
        for _ in range(self.timeout):
            headpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector = 'ray.io/node-type=head')
            workerpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector = 'ray.io/node-type=worker')
            if (len(headpods.items) == 0 and len(workerpods.items) == 0):
                converge = True
                logger.info("--- Cleanup RayService %s seconds ---", time.time() - start_time)
                break
            time.sleep(1)

        if not converge:
            logger.info("RayServiceAddCREvent clean_up() failed to converge in %d seconds.",
                self.timeout)
            logger.info("expected_head_pods: 0, expected_worker_pods: 0")
            show_cluster_info(self.namespace)
            raise Exception("RayServiceAddCREvent clean_up() timeout")

class GeneralTestCase(unittest.TestCase):
    """TestSuite"""
    def __init__(self, methodName, docker_image_dict, cr_event):
        super().__init__(methodName)
        self.cr_event = cr_event
        self.operator_manager = OperatorManager(docker_image_dict)

    @classmethod
    def setUpClass(cls):
        K8S_CLUSTER_MANAGER.delete_kind_cluster()

    def setUp(self):
        if not K8S_CLUSTER_MANAGER.check_cluster_exist():
            K8S_CLUSTER_MANAGER.create_kind_cluster()
            self.operator_manager.prepare_operator()

    def runtest(self):
        """Run a configuration test"""
        self.cr_event.trigger()

    def tearDown(self) -> None:
        try:
            self.cr_event.clean_up()
        except Exception as ex:
            logger.error(str(ex))
            K8S_CLUSTER_MANAGER.delete_kind_cluster()

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
    image_dict = {
        CONST.RAY_IMAGE_KEY: 'rayproject/ray:2.0.0',
        CONST.OPERATOR_IMAGE_KEY: 'kuberay/operator:nightly'
    }

    test_cases = unittest.TestSuite()
    for new_cr in mut.mutate():
        addEvent = RayClusterAddCREvent(new_cr, [rs], 90, NAMESPACE)
        test_cases.addTest(GeneralTestCase('runtest', image_dict, addEvent))
    runner = unittest.TextTestRunner()
    runner.run(test_cases)
