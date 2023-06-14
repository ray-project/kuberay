''' Test sample RayService YAML files to catch invalid and outdated ones. '''
import json
from kubernetes import client
import logging
import os
import pytest
import sys
from tempfile import NamedTemporaryFile
import time
from typing import Any, Dict, List, Optional
import yaml

from framework.prototype import (
    RuleSet,
    CREvent,
    EasyJobRule,
    CurlServiceRule,
    get_expected_head_pods,
    get_expected_worker_pods,
    show_cluster_info,
    check_pod_running,
)

from framework.utils import (
    start_curl_pod,
    logger,
    shell_subprocess_run,
    shell_subprocess_check_output,
    CONST,
    K8S_CLUSTER_MANAGER,
    OperatorManager
)

logger = logging.getLogger(__name__)

NAMESPACE = 'default'
DEFAULT_IMAGE_DICT = {
    CONST.RAY_IMAGE_KEY: os.getenv('RAY_IMAGE', default='rayproject/ray:2.5.0'),
    CONST.OPERATOR_IMAGE_KEY: os.getenv('OPERATOR_IMAGE', default='kuberay/operator:nightly'),
}

CURL_CMD_FMT = (
    "kubectl exec curl -n {namespace} -- "
    "curl -X POST -H 'Content-Type: application/json' "
    "{name}-serve-svc.{namespace}.svc.cluster.local:8000{path}/ -d '{json}'"
)

def curl_cmd(name: str, namespace: str, path: str, json_args: Any):
    return CURL_CMD_FMT.format(
        name=name,
        namespace=namespace,
        path=path,
        json=json.dumps(json_args),
    )


class RayServiceAddCREvent(CREvent):
    """CREvent for RayService addition"""
    def exec(self):
        shell_subprocess_run(f"kubectl apply -n {self.namespace} -f {self.filepath}")

    def wait(self):
        """Wait for RayService to converge"""
        logger.info("Waiting for pods in ray service to be running...")

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

class RayServiceUpdateCREvent(CREvent):
    """CREvent for RayService update"""
    def __init__(
        self,
        custom_resource_object,
        rulesets: List[RuleSet] = [],
        timeout: int = 90,
        namespace: str = "default",
        filepath: Optional[str] = None,
        query_while_updating: Optional[Dict[str, str]] = None,
    ):
        super().__init__(custom_resource_object, rulesets, timeout, namespace, filepath)
        self.name = self.custom_resource_object["metadata"]["name"]
        self.query_while_updating = query_while_updating

    def query(self):
        if self.query_while_updating:
            for cmd, expected_output in self.query_while_updating.items():
                output = shell_subprocess_check_output(cmd)
                assert output.decode('utf-8') == expected_output

    def exec(self):
        """Update a CR by a `kubectl apply` command."""
        self.start = time.time()
        shell_subprocess_run(f"kubectl apply -n {self.namespace} -f {self.filepath}")
    
    def wait_for_status(self, status: str):
        """Helper function to check for service status."""
        k8s_cr_api: client.CustomObjectsApi = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
        while time.time() - self.start < self.timeout:
            rayservice_info = k8s_cr_api.get_namespaced_custom_object_status(
                group="ray.io",
                namespace=self.namespace,
                name=self.name,
                version="v1alpha1",
                plural="rayservices",
            )
            if rayservice_info["status"]["serviceStatus"] == status:
                break

            self.query()
            time.sleep(0.1)
        else:
            raise TimeoutError(
                f'Ray service "{self.name}" did not transition to status "{status}" '
                f"after {self.timeout}s."
            )

    def wait(self):
        """Wait for deployment to transition -> WaitForServeDeploymentReady -> Running"""

        self.wait_for_status("WaitForServeDeploymentReady")
        logger.info("Ray service transitioned to status WaitForServeDeploymentReady.")
        self.wait_for_status("Running")
        logger.info("Ray service transitioned to status Running.")

class RayServiceDeleteCREvent(CREvent):
    """CREvent for RayService deletion"""
    def exec(self):
        """Delete a CR by a `kubectl delete` command."""
        shell_subprocess_run(f"kubectl delete -n {self.namespace} -f {self.filepath}")

    def wait(self):
        """Wait for pods to be deleted"""
        k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
        start_time = time.time()
        while time.time() - start_time < self.timeout:
            headpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector = 'ray.io/node-type=head')
            workerpods = k8s_v1_api.list_namespaced_pod(
                namespace = self.namespace, label_selector = 'ray.io/node-type=worker')
            if (len(headpods.items) == 0 and len(workerpods.items) == 0):
                converge = True
                logger.info("--- Cleanup RayService %s seconds ---", time.time() - start_time)
                return
            time.sleep(1)

        logger.info(f"RayServiceDeleteCREvent failed to converge in {self.timeout}s.")
        logger.info("expected_head_pods: 0, expected_worker_pods: 0")
        show_cluster_info(self.namespace)
        raise TimeoutError(f"RayServiceDeleteCREvent didn't finish in {self.timeout}s.")


class TestRayService:
    @pytest.fixture
    def set_up_cluster(self):
        SAMPLE_PATH = CONST.REPO_ROOT.joinpath("ray-operator/config/samples/")
        YAMLs = ['ray_v1alpha1_rayservice.yaml']

        sample_yaml_files = []
        for filename in YAMLs:
            filepath = SAMPLE_PATH.joinpath(filename)
            with open(filepath, encoding="utf-8") as cr_yaml:
                for k8s_object in yaml.safe_load_all(cr_yaml):
                    sample_yaml_files.append(
                        {'path': filepath, 'name': filename, 'cr': k8s_object}
                    )
        
        self.cr = sample_yaml_files[0]['cr']
        self.config_path = sample_yaml_files[0]['path']
        self.rayservice_name = self.cr["metadata"]["name"]
        self.default_queries = {
            curl_cmd(self.rayservice_name, NAMESPACE, "/fruit", ["MANGO", 2]): "6",
            curl_cmd(self.rayservice_name, NAMESPACE, "/calc", ["MUL", 3]): '"15 pizzas please!"',
        }

        if K8S_CLUSTER_MANAGER.check_cluster_exist():
            K8S_CLUSTER_MANAGER.delete_kind_cluster()

        operator_manager = OperatorManager(DEFAULT_IMAGE_DICT)
        K8S_CLUSTER_MANAGER.create_kind_cluster()
        operator_manager.prepare_operator()
        start_curl_pod("curl", "default")
        logger.info(DEFAULT_IMAGE_DICT)

        yield

        K8S_CLUSTER_MANAGER.delete_kind_cluster()

    def test_deploy_applications(self, set_up_cluster):
        rs = RuleSet([EasyJobRule(), CurlServiceRule(queries=self.default_queries)])
        cr_events: List[CREvent] = [
            RayServiceAddCREvent(self.cr, [rs], 90, NAMESPACE, self.config_path),
            RayServiceDeleteCREvent(self.cr, [], 90, NAMESPACE, self.config_path)
        ]

        for cr_event in cr_events:
            cr_event.trigger()
    
    def test_in_place_update(self, set_up_cluster):
        # Modify the MangoStand price and Multiplier factor
        with open(self.config_path, 'r') as file:
            yaml_lines = file.readlines()
            yaml_lines[24] = " " * 14 + "price: 4\n"
            yaml_lines[62] = " " * 14 + "factor: 3\n"

        updated_queries = {
            curl_cmd(self.rayservice_name, NAMESPACE, "/fruit", ["MANGO", 2]): "8",
            curl_cmd(self.rayservice_name, NAMESPACE, "/calc", ["MUL", 3]): '"9 pizzas please!"',
        }

        with NamedTemporaryFile(mode="w+", suffix=".yaml") as yaml_copy:
            logger.info(f"Writing modified RayService yaml to {yaml_copy.name}.")
            yaml_copy.writelines(yaml_lines)
            yaml_copy.flush()

            cr_events: List[CREvent] = [
                RayServiceAddCREvent(
                    custom_resource_object=self.cr,
                    rulesets=[RuleSet([EasyJobRule(), CurlServiceRule(queries=self.default_queries)])],
                    timeout=90,
                    namespace=NAMESPACE,
                    filepath=self.config_path
                ),
                RayServiceUpdateCREvent(
                    custom_resource_object=self.cr,
                    rulesets=[RuleSet([EasyJobRule(), CurlServiceRule(queries=updated_queries)])],
                    timeout=90,
                    namespace=NAMESPACE,
                    filepath=yaml_copy.name
                ),
                RayServiceDeleteCREvent(self.cr, [], 90, NAMESPACE, self.config_path),
            ]

            for cr_event in cr_events:
                cr_event.trigger()

    def test_zero_downtime_rollout(self, set_up_cluster):
        # Modify the cluster spec to trigger a rollout
        with open(self.config_path, 'r') as file:
            yaml_lines = file.readlines()
            yaml_lines[103:103] = [
                " " * 14 + "env:\n",
                " " * 16 + "- name: SAMPLE_ENV_VAR\n",
                " " * 18 + 'value: SAMPLE_VALUE\n',
            ]

        with NamedTemporaryFile(mode="w+", suffix=".yaml") as yaml_copy:
            logger.info(f"Writing modified RayService yaml to {yaml_copy.name}.")
            yaml_copy.writelines(yaml_lines)
            yaml_copy.flush()

            cr_events: List[CREvent] = [
                RayServiceAddCREvent(
                    custom_resource_object=self.cr,
                    rulesets=[RuleSet([EasyJobRule(), CurlServiceRule(queries=self.default_queries)])],
                    filepath=self.config_path
                ), 
                RayServiceUpdateCREvent(
                    custom_resource_object=self.cr,
                    rulesets=[RuleSet([CurlServiceRule(queries=self.default_queries)])],
                    filepath=yaml_copy.name,
                    query_while_updating=self.default_queries,
                ), 
                RayServiceDeleteCREvent(custom_resource_object=self.cr, filepath=self.config_path),
            ]

            for cr_event in cr_events:
                cr_event.trigger()

if __name__ == "__main__":
    sys.exit(pytest.main(["-v", "-s", __file__]))
