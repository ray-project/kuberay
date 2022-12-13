"""Utilities for integration tests of KubeRay."""

import subprocess
import logging
from pathlib import Path
from kubernetes import client, config

logger = logging.getLogger(__name__)
logging.basicConfig(
    format='%(asctime)s,%(msecs)d %(levelname)-8s [%(filename)s:%(lineno)d] %(message)s',
    datefmt='%Y-%m-%d:%H:%M:%S',
    level=logging.INFO)

class CONST:
    """Constants"""
    __slots__ = ()
    # Docker images
    OPERATOR_IMAGE_KEY = "kuberay-operator-image"
    RAY_IMAGE_KEY = "ray-image"

    # Kubernetes API clients
    K8S_CR_CLIENT_KEY = "k8s-cr-api-client"
    K8S_V1_CLIENT_KEY = "k8s-v1-api-client"

    # Paths
    REPO_ROOT = Path(__file__).absolute().parent.parent.parent
    HELM_CHART_ROOT = REPO_ROOT.joinpath("helm-chart")
    DEFAULT_KIND_CONFIG = REPO_ROOT.joinpath("tests/framework/config/kind-config.yaml")

    # Ray features
    RAY_FT = "RAY_FT"
    RAY_SERVICE = "RAY_SERVICE"
CONST = CONST()

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

    def create_kind_cluster(self, kind_config = None) -> None:
        """Create a KinD cluster"""
        # To use NodePort service, `kind_config` needs to set `extraPortMappings` properly.
        kind_config = CONST.DEFAULT_KIND_CONFIG if not kind_config else kind_config
        shell_subprocess_run(f"kind create cluster --wait 900s --config {kind_config}")
        config.load_kube_config()
        self.k8s_client_dict.update({
            CONST.K8S_V1_CLIENT_KEY: client.CoreV1Api(),
            CONST.K8S_CR_CLIENT_KEY: client.CustomObjectsApi()
        })

    def check_cluster_exist(self) -> bool:
        """Check whether KinD cluster exists or not"""
        return shell_subprocess_run("kubectl cluster-info --context kind-kind", check = False) == 0

K8S_CLUSTER_MANAGER = KubernetesClusterManager()

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
