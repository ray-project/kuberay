"""Utilities for integration tests of KubeRay."""

import subprocess
import logging
from pathlib import Path

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
