"""
Set of helper methods to manage rayclusters. Requires Python 3.6 and higher
"""

import logging
import copy
import re
from typing import Any, Tuple
import os
import subprocess
import time
from python_client import constants
from kubernetes import client, config


log = logging.getLogger(__name__)
if logging.getLevelName(log.level) == "NOTSET":
    logging.basicConfig(format="%(asctime)s %(message)s", level=constants.LOGLEVEL)

"""
ClusterUtils contains methods to facilitate modifying/populating the config of a raycluster
"""


class ClusterUtils:
    """
    ClusterUtils - Utility class for populating cluster information

    Methods:
    - populate_meta(cluster: dict, name: str, k8s_namespace: str, labels: dict, ray_version: str) -> dict:
    - populate_ray_head(cluster: dict, ray_image: str,service_type: str, cpu_requests: str, memory_requests: str, cpu_limits: str, memory_limits: str, ray_start_params: dict) -> Tuple[dict, bool]:
    - populate_worker_group(cluster: dict, group_name: str, ray_image: str, ray_command: Any, init_image: str, cpu_requests: str, memory_requests: str, cpu_limits: str, memory_limits: str, replicas: int, min_replicas: int, max_replicas: int, ray_start_params: dict) -> Tuple[dict, bool]:
    - update_worker_group_replicas(cluster: dict, group_name: str, max_replicas: int, min_replicas: int, replicas: int) -> Tuple[dict, bool]:
    """

    # initial config to setup the kube client
    def __init__(self):
        # loading the config
        self.kube_config = config.load_kube_config()
        self.core_v1_api = client.CoreV1Api()

    def populate_meta(
        self,
        cluster: dict,
        name: str,
        k8s_namespace: str,
        labels: dict,
        ray_version: str,
    ) -> dict:
        """Populate the metadata and ray version of the cluster.

        Parameters:
        - cluster (dict): A dictionary representing a cluster.
        - name (str): The name of the cluster.
        - k8s_namespace (str): The namespace of the cluster.
        - labels (dict): A dictionary of labels to be applied to the cluster.
        - ray_version (str): The version of Ray to use in the cluster.

        Returns:
            dict: The updated cluster dictionary with metadata and ray version populated.
        """

        assert self.is_valid_name(name)

        cluster["apiVersion"] = "{group}/{version}".format(
            group=constants.GROUP, version=constants.VERSION
        )
        cluster["kind"] = constants.KIND
        cluster["metadata"] = {
            "name": name,
            "namespace": k8s_namespace,
            "labels": labels,
        }
        cluster["spec"] = {"rayVersion": ray_version}
        return cluster

    def populate_ray_head(
        self,
        cluster: dict,
        ray_image: str,
        service_type: str,
        cpu_requests: str,
        memory_requests: str,
        cpu_limits: str,
        memory_limits: str,
        ray_start_params: dict,
    ) -> Tuple[dict, bool]:
        """Populate the ray head specs of the cluster
        Parameters:
        - cluster (dict): The dictionary representation of the cluster.
        - ray_image (str): The name of the ray image to use for the head node.
        - service_type (str): The type of service to run for the head node.
        - cpu_requests (str): The CPU resource requests for the head node.
        - memory_requests (str): The memory resource requests for the head node.
        - cpu_limits (str): The CPU resource limits for the head node.
        - memory_limits (str): The memory resource limits for the head node.
        - ray_start_params (dict): The parameters for starting the Ray cluster.

        Returns:
        - Tuple (dict, bool): The updated cluster, and a boolean indicating whether the update was successful.
        """
        # validate arguments
        try:
            arguments = locals()
            for k, v in arguments.items():
                assert v
        except AssertionError as e:
            log.error(
                "error creating ray head, the parameters are not fully defined. {} = {}".format(
                    k, v
                )
            )
            return cluster, False

        # make sure metadata exists
        if "spec" in cluster.keys():
            if "headGroupSpec" not in cluster.keys():
                log.info(
                    "setting the headGroupSpec for cluster {}".format(
                        cluster["metadata"]["name"]
                    )
                )
                cluster["spec"]["headGroupSpec"] = []
        else:
            log.error("error creating ray head, the spec and/or metadata is not define")
            return cluster, False

        # populate headGroupSpec
        cluster["spec"]["headGroupSpec"] = {
            "serviceType": service_type,
            "rayStartParams": ray_start_params,
            "template": {
                "spec": {
                    "containers": [
                        {
                            "image": ray_image,
                            "name": "ray-head",
                            "ports": [
                                {
                                    "containerPort": 6379,
                                    "name": "gcs-server",
                                    "protocol": "TCP",
                                },
                                {
                                    "containerPort": 8265,
                                    "name": "dashboard",
                                    "protocol": "TCP",
                                },
                                {
                                    "containerPort": 10001,
                                    "name": "client",
                                    "protocol": "TCP",
                                },
                            ],
                            "resources": {
                                "requests": {
                                    "cpu": cpu_requests,
                                    "memory": memory_requests,
                                },
                                "limits": {"cpu": cpu_limits, "memory": memory_limits},
                            },
                            "volumeMounts": [
                                {"mountPath": "/tmp/ray", "name": "ray-logs"}
                            ],
                        }
                    ],
                    "volumes": [{"emptyDir": {}, "name": "ray-logs"}],
                }
            },
        }

        return cluster, True

    def populate_worker_group(
        self,
        group_name: str,
        ray_image: str,
        ray_command: Any,
        init_image: str,
        cpu_requests: str,
        memory_requests: str,
        cpu_limits: str,
        memory_limits: str,
        replicas: int,
        min_replicas: int,
        max_replicas: int,
        ray_start_params: dict,
    ) -> Tuple[dict, bool]:
        """Populate the worker group specification in the cluster dictionary.

        Parameters:
        - cluster (dict): Dictionary representing the cluster spec.
        - group_name (str): The name of the worker group.
        - ray_image (str): The image to use for the Ray worker containers.
        - ray_command (Any): The command to run in the Ray worker containers.
        - init_image (str): The init container image to use.
        - cpu_requests (str): The requested CPU resources for the worker containers.
        - memory_requests (str): The requested memory resources for the worker containers.
        - cpu_limits (str): The limit on CPU resources for the worker containers.
        - memory_limits (str): The limit on memory resources for the worker containers.
        - replicas (int): The desired number of replicas for the worker group.
        - min_replicas (int): The minimum number of replicas for the worker group.
        - max_replicas (int): The maximum number of replicas for the worker group.
        - ray_start_params (dict): The parameters to pass to the Ray worker start command.

        Returns:
        - Tuple[dict, bool]: A tuple of the cluster specification and a boolean indicating
            whether the worker group was successfully populated.
        """
        # validate arguments
        try:
            arguments = locals()
            for k, v in arguments.items():
                if k != "min_replicas":
                    assert v
        except AssertionError as e:
            log.error(
                "error populating worker group, the parameters are not fully defined. {} = {}".format(
                    k, v
                )
            )
            return None, False

        assert self.is_valid_name(group_name)
        assert max_replicas >= min_replicas

        worker_group: dict[str, Any] = {
            "groupName": group_name,
            "maxReplicas": max_replicas,
            "minReplicas": min_replicas,
            "rayStartParams": ray_start_params,
            "replicas": replicas,
            "template": {
                "spec": {
                    "containers": [
                        {
                            "image": ray_image,
                            "command": ray_command,
                            "lifecycle": {
                                "preStop": {
                                    "exec": {"command": ["/bin/sh", "-c", "ray stop"]}
                                }
                            },
                            "name": "ray-worker",
                            "resources": {
                                "requests": {
                                    "cpu": cpu_requests,
                                    "memory": memory_requests,
                                },
                                "limits": {
                                    "cpu": cpu_limits,
                                    "memory": memory_limits,
                                },
                            },
                            "volumeMounts": [
                                {"mountPath": "/tmp/ray", "name": "ray-logs"}
                            ],
                        }
                    ],
                    "volumes": [{"emptyDir": {}, "name": "ray-logs"}],
                }
            },
        }

        return worker_group, True

    def update_worker_group_replicas(
        self,
        cluster: dict,
        group_name: str,
        max_replicas: int,
        min_replicas: int,
        replicas: int,
    ) -> Tuple[dict, bool]:
        """Update the number of replicas for a worker group in the cluster.

        Parameters:
        - cluster (dict): The cluster to update.
        - group_name (str): The name of the worker group to update.
        - max_replicas (int): The maximum number of replicas for the worker group.
        - min_replicas (int): The minimum number of replicas for the worker group.
        - replicas (int): The desired number of replicas for the worker group.

        Returns:
        Tuple[dict, bool]: A tuple containing the updated cluster and a flag indicating whether the update was successful.
        """
        try:
            arguments = locals()
            for k, v in arguments.items():
                if k != "min_replicas":
                    assert v
        except AssertionError as e:
            log.error(
                "error updating worker group, the parameters are not fully defined. {} = {}".format(
                    k, v
                )
            )
            return cluster, False

        assert cluster["spec"]["workerGroupSpecs"]
        assert max_replicas >= min_replicas

        for i in range(len(cluster["spec"]["workerGroupSpecs"])):
            if cluster["spec"]["workerGroupSpecs"][i]["groupName"] == group_name:

                cluster["spec"]["workerGroupSpecs"][i]["maxReplicas"] = max_replicas
                cluster["spec"]["workerGroupSpecs"][i]["minReplicas"] = min_replicas
                cluster["spec"]["workerGroupSpecs"][i]["replicas"] = replicas
                return cluster, True

        return cluster, False

    def update_worker_group_resources(
        self,
        cluster: dict,
        group_name: str,
        cpu_requests: str,
        memory_requests: str,
        cpu_limits: str,
        memory_limits: str,
        container_name="unspecified",
    ) -> Tuple[dict, bool]:
        """Update the resources for a worker group pods in the cluster.

        Parameters:
        - cluster (dict): The cluster to update.
        - group_name (str): The name of the worker group to update.
        - cpu_requests (str): CPU requests for the worker pods.
        - memory_requests (str): Memory requests for the worker pods.
        - cpu_limits (str): CPU limits for the worker pods.
        - memory_limits (str): Memory limits for the worker pods.

        Returns:
        Tuple[dict, bool]: A tuple containing the updated cluster and a flag indicating whether the update was successful.
        """
        try:
            arguments = locals()
            for k, v in arguments.items():
                if k != "min_replicas":
                    assert v
        except AssertionError as e:
            log.error(
                "error updating worker group, the parameters are not fully defined. {} = {}".format(
                    k, v
                )
            )
            return cluster, False

        assert cluster["spec"]["workerGroupSpecs"]

        worker_groups = cluster["spec"]["workerGroupSpecs"]

        def add_values(group_index: int, container_index: int):
            worker_groups[group_index]["template"]["spec"]["containers"][
                container_index
            ]["resources"]["requests"]["cpu"] = cpu_requests
            worker_groups[group_index]["template"]["spec"]["containers"][
                container_index
            ]["resources"]["requests"]["memory"] = memory_requests
            worker_groups[group_index]["template"]["spec"]["containers"][
                container_index
            ]["resources"]["limits"]["cpu"] = cpu_limits
            worker_groups[group_index]["template"]["spec"]["containers"][
                container_index
            ]["resources"]["limits"]["memory"] = memory_limits

        for group_index, worker_group in enumerate(worker_groups):
            if worker_group["groupName"] != group_name:
                continue

            containers = worker_group["template"]["spec"]["containers"]
            container_names = [container["name"] for container in containers]

            if len(containers) == 0:
                log.error(
                    f"error updating container resources, the worker group {group_name} has no containers"
                )
                return cluster, False

            if container_name == "unspecified":
                add_values(group_index, 0)
                return cluster, True
            elif container_name == "all_containers":
                for container_index in range(len(containers)):
                    add_values(group_index, container_index)
                return cluster, True
            elif container_name in container_names:
                container_index = container_names.index(container_name)
                add_values(group_index, container_index)
                return cluster, True

        return cluster, False

    def duplicate_worker_group(
        self,
        cluster: dict,
        group_name: str,
        new_group_name: str,
    ) -> Tuple[dict, bool]:
        """Duplicate a worker group in the cluster.

        Parameters:
        - cluster (dict): The cluster definition.
        - group_name (str): The name of the worker group to be duplicated.
        - new_group_name (str): The name for the duplicated worker group.

        Returns:
        Tuple[dict, bool]: A tuple containing the updated cluster definition and a boolean indicating the success of the operation.
        """
        try:
            arguments = locals()
            for k, v in arguments.items():
                assert v
        except AssertionError as e:
            log.error(
                f"error duplicating worker group, the parameters are not fully defined. {k} = {v}"
            )
            return cluster, False
        assert self.is_valid_name(new_group_name)
        assert cluster["spec"]["workerGroupSpecs"]

        worker_groups = cluster["spec"]["workerGroupSpecs"]
        for _, worker_group in enumerate(worker_groups):
            if worker_group["groupName"] == group_name:
                duplicate_group = copy.deepcopy(worker_group)
                duplicate_group["groupName"] = new_group_name
                worker_groups.append(duplicate_group)
                return cluster, True

        log.error(
            f"error duplicating worker group, no match was found for {group_name}"
        )
        return cluster, False

    def delete_worker_group(
        self,
        cluster: dict,
        group_name: str,
    ) -> Tuple[dict, bool]:
        """Deletes a worker group in the cluster.

        Parameters:
        - cluster (dict): The cluster definition.
        - group_name (str): The name of the worker group to be duplicated.

        Returns:
        Tuple[dict, bool]: A tuple containing the updated cluster definition and a boolean indicating the success of the operation.
        """
        try:
            arguments = locals()
            for k, v in arguments.items():
                assert v
        except AssertionError as e:
            log.error(
                f"error creating ray head, the parameters are not fully defined. {k} = {v}"
            )
            return cluster, False

        assert cluster["spec"]["workerGroupSpecs"]

        worker_groups = cluster["spec"]["workerGroupSpecs"]
        first_or_none = next((x for x in worker_groups if x["groupName"] == group_name), None)
        if first_or_none:
            worker_groups.remove(first_or_none)
            return cluster, True

        log.error(
            f"error removing worker group, no match was found for {group_name}"
        )
        return cluster, False

    def exec_file(
        self,
        file_path,
        cluster_name,
        cluster_namespace = "default",
    ) -> str:
        """Execute Python file in the cluster.

        Parameters:
        - file_path (str): The file path for the python file which will be executed in the ray cluster.
        - cluster_name (str): The name of the ray cluster where the python file will be executed.
        - cluster_name (str): The name of the namespace which the ray cluster is in.

        Returns:
        - Tuple (str, bool): execution ouput of the python file, and a boolean indicating whether the update was successful.
        """
        head_pod = self.get_head_pod(cluster_name, cluster_namespace)
        if len(head_pod) == 0:
            return "", False
        if not os.path.isfile(file_path):
            log.error(
                f"file not found: {file_path}"
            )
            return "", False

        target_path = "/tmp/" +  str(time.time()) + os.path.basename(file_path)

        copy_command = f"kubectl cp {file_path} {head_pod}:{target_path}"
        result = self.shell_subprocess_run(copy_command, "copy file into ray cluster")
        if result.returncode != 0:
            return "", False

        exec_command = f"kubectl exec {head_pod} -- python {target_path}"
        result = self.shell_subprocess_run(exec_command, "exec file in ray cluster")
        if result.returncode != 0:
            return "", False
        output = result.stdout

        delete_command = f"kubectl exec {head_pod} -- rm {target_path}"
        result = self.shell_subprocess_run(delete_command, "delete file in ray cluster")
        if result.returncode != 0:
            return "", False
        
        return output, True

    def shell_subprocess_run(self, command, debug_msg=""):
        result = subprocess.run(command, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        if result.returncode != 0:
            if debug_msg == "":
                log.error(
                    f"error exec command: \"{command}\"\nError message: {result.stderr}"
                )
            else:
                log.error(
                    f"Error when {debug_msg}. Command:\"{command}\"\nError message: {result.stderr}"
                )
        return result
    
    def get_head_pod(
        self,
        cluster_name,
        cluster_namespace = "default"
    ) -> str:
        label_selector = f"ray.io/identifier={cluster_name}-head"
        pods = self.core_v1_api.list_namespaced_pod(
            namespace = cluster_namespace, label_selector = label_selector
        )
        if len(pods.items) != 1:
            log.error(
                f"error cannot find the head pod with the select {label_selector}"
            )
            return ""
        return pods.items[0].metadata.name

    def is_valid_name(self, name: str) -> bool:
        msg = "The name must be 63 characters or less, begin and end with an alphanumeric character, and contain only dashes, dots, and alphanumerics."
        if len(name) > 63 or not bool(re.match("^[a-z0-9]([-.]*[a-z0-9])+$", name)):
            log.error(msg)
            return False
        return True

    def is_valid_label(self, name: str) -> bool:
        msg = "The label name must be 63 characters or less, begin and end with an alphanumeric character, and contain only dashes, underscores, dots, and alphanumerics."
        if len(name) > 63 or  not bool(re.match("^[a-z0-9]([-._]*[a-z0-9])+$", name)):
            log.error(msg)
            return False
        return True

