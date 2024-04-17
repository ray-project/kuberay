"""Configuration test framework for KubeRay"""
import json
import time
import unittest
from typing import Dict, List, Optional
import jsonpatch

from framework.utils import (
    create_custom_object,
    delete_custom_object,
    get_custom_object,
    get_pod,
    get_head_pod,
    start_curl_pod,
    logger,
    pod_exec_command,
    shell_subprocess_run,
    shell_subprocess_check_output,
    CONST,
    K8S_CLUSTER_MANAGER,
    OperatorManager
)

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

def check_pod_running(pods) -> bool:
    """"Check whether all of the pods are in running state"""
    for pod in pods:
        if pod.status.phase != 'Running':
            return False
        for container in pod.status.container_statuses:
            if not container.ready:
                return False
    return True

def get_expected_head_pods(custom_resource):
    """Get the number of head pods in custom_resource"""
    resource_kind = custom_resource["kind"]
    head_replica_paths = {
       CONST.RAY_CLUSTER_CRD: "spec.headGroupSpec.replicas",
       CONST.RAY_SERVICE_CRD: "spec.rayClusterConfig.headGroupSpec.replicas",
       CONST.RAY_JOB_CRD: "spec.rayClusterSpec.headGroupSpec.replicas"
    }
    if resource_kind in head_replica_paths:
        path = head_replica_paths[resource_kind]
        return search_path(custom_resource, path.split('.'), default_value=1)
    raise Exception(f"Unknown resource kind: {resource_kind} in get_expected_head_pods()")

def get_expected_worker_pods(custom_resource):
    """Get the number of head pods in custom_resource"""
    resource_kind = custom_resource["kind"]
    worker_specs_paths = {
       CONST.RAY_CLUSTER_CRD: "spec.workerGroupSpecs",
       CONST.RAY_SERVICE_CRD: "spec.rayClusterConfig.workerGroupSpecs",
       CONST.RAY_JOB_CRD: "spec.rayClusterSpec.workerGroupSpecs"
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
    k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
    head_pods = k8s_v1_api.list_namespaced_pod(
        namespace=cr_namespace,
        label_selector='ray.io/node-type=head'
    )
    worker_pods = k8s_v1_api.list_namespaced_pod(
        namespace=cr_namespace,
        label_selector='ray.io/node-type=worker'
    )
    logger.info(
        f"Number of head pods: {len(head_pods.items)}, "
        f"number of worker pods: {len(worker_pods.items)}"
    )
    shell_subprocess_run(f'kubectl get all -n={cr_namespace}')
    shell_subprocess_run(f'kubectl describe pods -n={cr_namespace}')
    # With "--tail=-1", every line in the log will be printed. The default value of "tail" is not
    # -1 when using selector.
    shell_subprocess_run(f'kubectl logs -n={cr_namespace} -l ray.io/node-type=head --tail=-1')
    operator_namespace = shell_subprocess_check_output('kubectl get pods '
        '-l app.kubernetes.io/component=kuberay-operator -A '
        '-o jsonpath={".items[0].metadata.namespace"}')
    shell_subprocess_check_output("kubectl logs -l app.kubernetes.io/component=kuberay-operator -n "
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
    def __init__(
        self,
        custom_resource_object,
        rulesets: List[RuleSet] = [],
        timeout: int = 300,
        namespace: str = "default",
        filepath: Optional[str] = None,
    ):
        self.rulesets = rulesets
        self.timeout = timeout
        self.namespace = namespace
        self.custom_resource_object = custom_resource_object
        # A file may consists of multiple Kubernetes resources (ex: ray-cluster.external-redis.yaml)
        self.filepath = filepath
        self.num_pods = 0

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
        k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
        pods = k8s_v1_api.list_namespaced_pod(namespace=self.namespace).items
        self.num_pods = len(pods)
        logger.info("Number of Pods before CREvent: %d", self.num_pods)
        for pod in pods:
            logger.info("[%s] Pod name: %s", self.namespace, pod.metadata.name)

        if not self.filepath:
            create_custom_object(self.namespace, self.custom_resource_object)
        else:
            shell_subprocess_run(f"kubectl apply -n {self.namespace} -f {self.filepath}")

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
        headpod = get_head_pod(cr_namespace)
        assert headpod.spec.containers[0].name == expected_val

class HeadSvcRule(Rule):
    """The labels of the head pod and the selectors of the head service must match."""
    def assert_rule(self, custom_resource=None, cr_namespace='default'):
        k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
        head_services = k8s_v1_api.list_namespaced_service(
            namespace= cr_namespace, label_selector="ray.io/node-type=head")
        assert len(head_services.items) == 1
        selector_dict = head_services.items[0].spec.selector
        selector = ','.join(map(lambda key: f"{key}={selector_dict[key]}", selector_dict))
        headpods = k8s_v1_api.list_namespaced_pod(
            namespace =cr_namespace, label_selector=selector)
        assert len(headpods.items) == 1

class EasyJobRule(Rule):
    """Submit a very simple Ray job to test the basic functionality of the Ray cluster."""
    def assert_rule(self, custom_resource=None, cr_namespace='default'):
        headpod = get_head_pod(cr_namespace)
        headpod_name = headpod.metadata.name
        pod_exec_command(headpod_name, cr_namespace,
            "python -c \"import ray; ray.init(); print(ray.cluster_resources())\"")

class ShutdownJobRule(Rule):
    """Check the Ray cluster is shutdown when setting `spec.shutdownAfterJobFinishes` to true."""
    def assert_rule(self, custom_resource=None, cr_namespace='default'):
        custom_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
        # Wait for there to be no RayClusters
        logger.info("Waiting for RayCluster to be deleted...")
        for i in range(30):
            rayclusters = custom_api.list_namespaced_custom_object(
                group = 'ray.io', version = 'v1', namespace = cr_namespace,
                plural = 'rayclusters')
            # print debug log
            if i != 0:
                logger.info("ShutdownJobRule wait() hasn't converged yet.")
                logger.info("Number of RayClusters: %d", len(rayclusters["items"]))
            if len(rayclusters["items"]) == 0:
                break
            time.sleep(1)
        else:
            raise TimeoutError("RayCluster hasn't been deleted in 30 seconds.")

        logger.info("RayCluster has been deleted.")



    def trigger_condition(self, custom_resource=None) -> bool:
        # Trigger if shutdownAfterJobFinishes is set to true
        steps = "spec.shutdownAfterJobFinishes".split('.')
        value = search_path(custom_resource, steps)
        logger.info("ShutdownJobRule trigger_condition(): %s", value)
        assert isinstance(value, bool) or value is None
        return value is not None and value

class CurlServiceRule(Rule):
    """Using curl to access the deployed application(s) on RayService"""
    CURL_CMD_FMT = (
        "kubectl exec curl -n {namespace} -- "
        "curl -X POST -H 'Content-Type: application/json' "
        "{name}-serve-svc.{namespace}.svc.cluster.local:8000{path}/ -d '{json}'"
    )

    def __init__(self, queries: List[Dict[str, str]], start_in_background: bool = False):
        self.queries = queries
        self.start_in_background = start_in_background

    def assert_rule(self, custom_resource, cr_namespace):
        # If curl pod doesn't exist, create one
        if get_pod("default", "run=curl") is None:
            start_curl_pod("curl", cr_namespace, timeout_s=30)

        for query in self.queries:
            cmd = self.CURL_CMD_FMT.format(
                name=custom_resource["metadata"]["name"],
                namespace=cr_namespace,
                path=query.get("path").rstrip("/"),
                json=json.dumps(query["json_args"])
            )

            if self.start_in_background:
                shell_subprocess_run(f"{cmd} &", hide_output=False)

            else:
                output = shell_subprocess_check_output(cmd)
                logger.info("curl output: %s", output.decode('utf-8'))
                if hasattr(query.get("expected_output"), "__iter__"):
                    assert output.decode('utf-8') in query["expected_output"]
                else:
                    assert output.decode('utf-8') == query["expected_output"]
            time.sleep(1)

class AutoscaleRule(Rule):
    def __init__(
        self,
        query: Dict[str, str],
        num_repeat: int,
        expected_worker_pods: int,
        timeout: int,
        message: str = "",
    ):
        self.query: Dict[str, str] = query
        self.num_repeat: int = num_repeat
        self.expected_worker_pods = expected_worker_pods
        self.query_rule = CurlServiceRule(queries=[query], start_in_background=True)
        self.timeout = timeout
        self.message = message

    def assert_rule(self, custom_resource, cr_namespace):
        logger.info(self.message)
        for _ in range(self.num_repeat):
            self.query_rule.assert_rule(custom_resource, cr_namespace)

        start_time = time.time()
        while time.time() - start_time < self.timeout:
            k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
            pods = k8s_v1_api.list_namespaced_pod(
                namespace=cr_namespace, label_selector='ray.io/node-type=worker'
            )
            logger.info("Number of worker pods: %d", len(pods.items))
            if len(pods.items) == self.expected_worker_pods:
                logger.info(
                    "Cluster has successfully scaled to the expected number of "
                    f"{self.expected_worker_pods} worker pods after "
                    f"{time.time() - start_time} seconds."
                )
                break
            time.sleep(2)
        else:
            show_cluster_info(cr_namespace)
            raise TimeoutError(
                "Cluster did not scale to the expected number of "
                f"{self.expected_worker_pods} worker pod(s) within {self.timeout} "
                f"seconds. Cluster is currently at {len(pods.items)} worker pods."
            )


class RayClusterAddCREvent(CREvent):
    """CREvent for RayCluster addition"""
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
        delete_custom_object(CONST.RAY_CLUSTER_CRD,
            self.namespace, self.custom_resource_object['metadata']['name'])

        # Wait pods to be deleted
        converge = False
        k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
        start_time = time.time()
        for _ in range(self.timeout):
            headpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=head')
            workerpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=worker')
            rediscleanuppods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=redis-cleanup')
            if (len(headpods.items) == 0 and len(workerpods.items) == 0 and len(rediscleanuppods.items) == 0):
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

        """Delete other resources in the yaml"""
        if self.filepath:
            logger.info("Delete other resources in the YAML")
            shell_subprocess_run(f"kubectl delete -n {self.namespace} -f {self.filepath} --ignore-not-found=true")

        start_time = time.time()
        converge = False
        for _ in range(self.timeout):
            k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
            pods = k8s_v1_api.list_namespaced_pod(namespace=self.namespace).items
            if len(pods) == self.num_pods:
                converge = True
                logger.info("--- Cleanup other resources %s seconds ---", time.time() - start_time)
                break
            logger.info("#Pods: (1) before CREvent: %d (2) now: %d", self.num_pods, len(pods))
            time.sleep(10)

        if not converge:
            show_cluster_info(self.namespace)
            raise Exception("RayClusterAddCREvent clean_up() timeout")

class RayJobAddCREvent(CREvent):
    """CREvent for RayJob addition"""
    def wait(self):
        """Wait for RayJob to converge"""
        start_time = time.time()
        expected_head_pods = get_expected_head_pods(self.custom_resource_object)
        expected_worker_pods = get_expected_worker_pods(self.custom_resource_object)
        expected_rayclusters = 1
        expected_job_pods = 1
        # Wait until:
        #   (1) The number of head pods and worker pods are as expected.
        #   (2) All head pods and worker pods are "Running".
        #   (3) A RayCluster has been created.
        #   (4) Exactly one Job pod has been created.
        #   (5) RayJob named "rayjob-sample" has status "SUCCEEDED".
        # We check the `expected_job_pods = 1` condition to catch situations described in
        # https://github.com/ray-project/kuberay/issues/1381
        converge = False
        k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
        custom_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
        for i in range(self.timeout):
            rayclusters = custom_api.list_namespaced_custom_object(
                group = 'ray.io', version = 'v1', namespace = self.namespace,
                plural = 'rayclusters')["items"]
            headpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=head')
            workerpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector='ray.io/node-type=worker')
            rayjob = get_custom_object(CONST.RAY_JOB_CRD, self.namespace,
                self.custom_resource_object['metadata']['name'])
            jobpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector='job-name='+self.custom_resource_object['metadata']['name'])

            if (len(headpods.items) == expected_head_pods
                    and len(workerpods.items) == expected_worker_pods
                    and len(jobpods.items) == expected_job_pods
                    and check_pod_running(headpods.items) and check_pod_running(workerpods.items)
                    and rayjob.get('status') is not None
                    and rayjob.get('status').get('jobStatus') == "SUCCEEDED"
                    and len(rayclusters) == expected_rayclusters):
                converge = True
                logger.info("--- RayJobAddCREvent converged in %s seconds ---",
                            time.time() - start_time)
                break
            else:
                # Print debug logs every 10 seconds.
                if i % 10 == 0 and i != 0:
                    logger.info("RayJobAddCREvent wait() hasn't converged yet.")
                    # Print out the delta between expected and actual for the parts that are not
                    # converged yet.
                    if len(headpods.items) != expected_head_pods:
                        logger.info("expected_head_pods: %d, actual_head_pods: %d",
                            expected_head_pods, len(headpods.items))
                    if len(workerpods.items) != expected_worker_pods:
                        logger.info("expected_worker_pods: %d, actual_worker_pods: %d",
                            expected_worker_pods, len(workerpods.items))
                    if len(jobpods.items) != expected_job_pods:
                        logger.info("expected_job_pods: %d, actual_job_pods: %d",
                            expected_job_pods, len(jobpods.items))
                    if not check_pod_running(headpods.items):
                        logger.info("head pods are not running yet.")
                    if not check_pod_running(workerpods.items):
                        logger.info("worker pods are not running yet.")
                    if rayjob.get('status') is None:
                        logger.info("rayjob status is None.")
                    elif rayjob.get('status').get('jobStatus') != "SUCCEEDED":
                        logger.info("rayjob status is not SUCCEEDED yet.")
                        logger.info("rayjob status: %s", rayjob.get('status').get('jobStatus'))
                    if len(rayclusters) != expected_rayclusters:
                        logger.info("expected_rayclusters: %d, actual_rayclusters: %d",
                            expected_rayclusters, len(rayclusters))

                if (rayjob.get("status") is not None and
                    rayjob.get("status").get("jobStatus") in ["STOPPED", "FAILED"]):
                    logger.info("Job Status: %s", rayjob.get("status").get("jobStatus"))
                    logger.info("Job Message: %s", rayjob.get("status").get("message"))
                    break
            time.sleep(1)

        if not converge:
            logger.info("RayJobAddCREvent wait() failed to converge in %d seconds.",
                self.timeout)
            logger.info("expected_head_pods: %d, expected_worker_pods: %d",
                expected_head_pods, expected_worker_pods)
            logger.info("rayjob: %s", rayjob)
            show_cluster_info(self.namespace)
            raise Exception("RayJobAddCREvent wait() timeout")

    def clean_up(self):
        """Delete added RayJob"""
        if not self.filepath:
            delete_custom_object(CONST.RAY_JOB_CRD,
                self.namespace, self.custom_resource_object['metadata']['name'])
        else:
            shell_subprocess_run(f"kubectl delete -n {self.namespace} -f {self.filepath}")
        # Wait for pods to be deleted
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
                logger.info("--- Cleanup RayJob %s seconds ---", time.time() - start_time)
                break
            time.sleep(1)

        if not converge:
            logger.info("RayJobAddCREvent clean_up() failed to converge in %d seconds.",
                self.timeout)
            logger.info("expected_head_pods: 0, expected_worker_pods: 0")
            show_cluster_info(self.namespace)
            raise Exception("RayJobAddCREvent clean_up() timeout")

class GeneralTestCase(unittest.TestCase):
    """TestSuite"""
    def __init__(self, methodName, cr_event):
        super().__init__(methodName)
        self.cr_event = cr_event
        self.operator_manager = OperatorManager.instance()

    @classmethod
    def setUpClass(cls):
        K8S_CLUSTER_MANAGER.cleanup()

    def setUp(self):
        if not K8S_CLUSTER_MANAGER.check_cluster_exist():
            K8S_CLUSTER_MANAGER.initialize_cluster()
            self.operator_manager.prepare_operator()

    def runtest(self):
        """Run a configuration test"""
        self.cr_event.trigger()

    def tearDown(self) -> None:
        try:
            self.cr_event.clean_up()
        except Exception as ex:
            logger.error(str(ex))
            K8S_CLUSTER_MANAGER.cleanup()
