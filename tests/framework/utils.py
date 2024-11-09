"""Utilities for integration tests of KubeRay."""

import os
import subprocess
import logging
from pathlib import Path
import tempfile
from urllib import request
import yaml
import jsonpatch
import time
from kubernetes import client, config
from abc import ABC, abstractmethod

logger = logging.getLogger(__name__)
logging.basicConfig(
    format="%(asctime)s,%(msecs)d %(levelname)-8s [%(filename)s:%(lineno)d] %(message)s",
    datefmt="%Y-%m-%d:%H:%M:%S",
    level=logging.INFO,
)


class CONST:
    """Constants"""

    __slots__ = ()
    # Docker images
    OPERATOR_IMAGE_KEY = "kuberay-operator-image"
    KUBERAY_LATEST_RELEASE = "quay.io/kuberay/operator:v1.1.0"

    # Kubernetes API clients
    K8S_CR_CLIENT_KEY = "k8s-cr-api-client"
    K8S_V1_CLIENT_KEY = "k8s-v1-api-client"

    # Paths
    REPO_ROOT = Path(__file__).absolute().parent.parent.parent
    HELM_CHART_ROOT = REPO_ROOT.joinpath("helm-chart")

    # Decide the config based on the environment
    BUILDKITE_ENV = "BUILDKITE_ENV"
    if os.getenv(BUILDKITE_ENV, default="") == "true":
        DEFAULT_KIND_CONFIG = REPO_ROOT.joinpath("tests/framework/config/kind-config-buildkite.yml")
    else:
        DEFAULT_KIND_CONFIG = REPO_ROOT.joinpath("tests/framework/config/kind-config.yaml")

    # Ray features
    RAY_FT = "RAY_FT"
    RAY_SERVICE = "RAY_SERVICE"
    RAY_SERVE_FT = "RAY_SERVE_FT"

    # Custom Resource Definitions
    RAY_CLUSTER_CRD = "RayCluster"
    RAY_SERVICE_CRD = "RayService"
    RAY_JOB_CRD = "RayJob"

    # Failures
    CREATE_NEW_POD = "CREATE_NEW_POD"
    RESTART_OLD_POD = "RESTART_OLD_POD"


CONST = CONST()

class ClusterManager(ABC):
    EXTERNAL_CLUSTER = "EXTERNAL_CLUSTER"

    @abstractmethod
    def initialize_cluster(self, kind_config=None) -> None:
        pass

    @abstractmethod
    def cleanup(self) -> None:
        pass

    @abstractmethod
    def upload_image():
        pass

    @abstractmethod
    def check_cluster_exist(self) -> bool:
        pass

    @classmethod
    def instance(cls):
        if cls.EXTERNAL_CLUSTER in os.environ:
            return ExternalClusterManager()
        else:
            return KindClusterManager()

class ExternalClusterManager(ClusterManager):
    CLUSTER_CLEANUP_SCRIPT = "CLUSTER_CLEANUP_SCRIPT"

    def __init__(self) -> None:
        self.k8s_client_dict = {}
        self.cleanup_timeout = 120

    def cleanup(self, namespace = "default") -> None:
        if self.CLUSTER_CLEANUP_SCRIPT in os.environ:
            cleanup_script = os.environ[self.CLUSTER_CLEANUP_SCRIPT]
            shell_subprocess_run(cleanup_script)
        else:
            self.__delete_all_crs("ray.io", "v1", namespace, "rayservices")
            self.__delete_all_crs("ray.io", "v1", namespace, "rayjobs")
            self.__delete_all_crs("ray.io", "v1", namespace, "rayclusters")

            k8s_v1_api = self.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
            start_time = time.time()
            while time.time() - start_time < self.cleanup_timeout:
                pods = k8s_v1_api.list_pod_for_all_namespaces(label_selector = 'app.kubernetes.io/created-by=kuberay-operator')
                if len(pods.items) == 0:
                    logger.info("--- Cleanup rayservices, rayjobs, rayclusters %s seconds ---", time.time() - start_time)
                    break

                time.sleep(1)

            shell_subprocess_run("helm uninstall kuberay-operator", check=False)
            start_time = time.time()
            while time.time() - start_time < self.cleanup_timeout:
                pods = k8s_v1_api.list_pod_for_all_namespaces(label_selector = 'app.kubernetes.io/component=kuberay-operator')
                if len(pods.items) == 0:
                    logger.info("--- Cleanup kuberay-operator %s seconds ---", time.time() - start_time)
                    break

                time.sleep(1)

        for _, k8s_client in self.k8s_client_dict.items():
            k8s_client.api_client.rest_client.pool_manager.clear()
            k8s_client.api_client.close()

        self.k8s_client_dict = {}

    def initialize_cluster(self, kind_config=None) -> None:
        config.load_kube_config()
        self.k8s_client_dict.update(
            {
                CONST.K8S_V1_CLIENT_KEY: client.CoreV1Api(),
                CONST.K8S_CR_CLIENT_KEY: client.CustomObjectsApi(),
            }
        )

    def upload_image(self, image):
        pass

    def check_cluster_exist(self) -> bool:
        """Check whether cluster exists or not"""
        return (
            shell_subprocess_run(
                "kubectl cluster-info", check=False
            )
            == 0
        )

    def __delete_all_crs(self, group, version, namespace, plural):
        custom_objects_api = self.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
        try:
            crs = custom_objects_api.list_namespaced_custom_object(group, version, namespace, plural)
            for cr in crs["items"]:
                name = cr["metadata"]["name"]
                custom_objects_api.delete_namespaced_custom_object(group, version, namespace, plural, name)
        except client.exceptions.ApiException:
            logger.info("CRD did not exist during clean up %s", plural)


class KindClusterManager(ClusterManager):
    """
    KindClusterManager controlls the lifecycle of KinD cluster and Kubernetes API client.
    """

    def __init__(self) -> None:
        self.k8s_client_dict = {}

    def cleanup(self) -> None:
        """Delete a KinD cluster"""
        shell_subprocess_run("kind delete cluster")
        for _, k8s_client in self.k8s_client_dict.items():
            k8s_client.api_client.rest_client.pool_manager.clear()
            k8s_client.api_client.close()
        self.k8s_client_dict = {}

    def initialize_cluster(self, kind_config=None) -> None:
        """Create a KinD cluster"""
        # To use NodePort service, `kind_config` needs to set `extraPortMappings` properly.
        kind_config = CONST.DEFAULT_KIND_CONFIG if not kind_config else kind_config
        shell_subprocess_run(f"kind create cluster --wait 900s --config {kind_config}")

        # Adjust the kubeconfig server address if necessary
        self._adjust_kubeconfig_server_address()

        config.load_kube_config()
        self.k8s_client_dict.update(
            {
                CONST.K8S_V1_CLIENT_KEY: client.CoreV1Api(),
                CONST.K8S_CR_CLIENT_KEY: client.CustomObjectsApi(),
            }
        )

    def upload_image(self, image):
        shell_subprocess_run(f"kind load docker-image {image}")

    def check_cluster_exist(self) -> bool:
        """Check whether KinD cluster exists or not"""
        return (
            shell_subprocess_run(
                "kubectl cluster-info --context kind-kind", check=False
            )
            == 0
        )

    def _adjust_kubeconfig_server_address(self) -> None:
        """Modify the server address in kubeconfig to https://docker:6443"""
        if os.getenv(CONST.BUILDKITE_ENV, default="") == "true":
            shell_subprocess_run("kubectl config set clusters.kind-kind.server https://docker:6443")

K8S_CLUSTER_MANAGER = ClusterManager.instance()


class OperatorManager(ABC):
    KUBERAY_OPERATOR_INSTALLATION_SCRIPT = "KUBERAY_OPERATOR_INSTALLATION_SCRIPT"

    @abstractmethod
    def prepare_operator(self):
        pass

    @classmethod
    def instance(cls, namespace=None, patch=jsonpatch.JsonPatch([]),
        cluster_manager = K8S_CLUSTER_MANAGER):
        if cls.KUBERAY_OPERATOR_INSTALLATION_SCRIPT in os.environ:
            if (namespace != None) or (patch != jsonpatch.JsonPatch([])):
                raise Exception("Parameters namespace or patch are not supported in ScriptBasedOperatorManager")
            return ScriptBasedOperatorManager()
        else:
            if namespace == None:
                namespace = "default"
            DEFAULT_IMAGE_DICT = {
                CONST.OPERATOR_IMAGE_KEY: os.getenv('OPERATOR_IMAGE', default='quay.io/kuberay/operator:nightly'),
            }
            default_operator_manager = DefaultOperatorManager(DEFAULT_IMAGE_DICT, namespace, patch, cluster_manager)
            return default_operator_manager

class DefaultOperatorManager(OperatorManager):
    """
    OperatorManager controlls the lifecycle of KubeRay operator. It will download Docker images,
    load images into an existing KinD cluster, and install CRD and KubeRay operator.
    Parameters:
        docker_image_dict : A dict that includes docker images that need to be
                            downloaded and loaded to the cluster.
        namespace         : A namespace(string) that KubeRay operator will be installed in.
        patch             : A jsonpatch that will be applied to the default KubeRay operator config
                            to create the custom config.
        cluster_manager   : Cluster manager instance.
    """

    def __init__(
        self, docker_image_dict, namespace="default", patch=jsonpatch.JsonPatch([]),
        cluster_manager = K8S_CLUSTER_MANAGER
    ) -> None:
        self.docker_image_dict = docker_image_dict
        self.cluster_manager = cluster_manager
        self.namespace = namespace
        self.values_yaml = {}
        for key in [CONST.OPERATOR_IMAGE_KEY]:
            if key not in self.docker_image_dict:
                raise Exception(f"Image {key} does not exist!")
        repo, tag = self.docker_image_dict[CONST.OPERATOR_IMAGE_KEY].split(":")
        if f"{repo}:{tag}" == CONST.KUBERAY_LATEST_RELEASE:
            url = (
                "https://github.com/ray-project/kuberay-helm"
                f"/raw/kuberay-operator-{tag[1:]}/helm-chart/kuberay-operator/values.yaml"
            )
        else:
            url = "file:///" + str(
                CONST.HELM_CHART_ROOT.joinpath("kuberay-operator/values.yaml")
            )
        with request.urlopen(url) as base_fd:
            self.values_yaml = patch.apply(yaml.safe_load(base_fd))

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
                if (
                    shell_subprocess_run(
                        f"docker image inspect {image} > /dev/null", check=False
                    )
                    != 0
                ):
                    shell_subprocess_run(f"docker pull {image}")
                else:
                    logger.info("Image %s exists", image)

        download_images()
        logger.info("Load images into KinD cluster")
        for key in self.docker_image_dict:
            image = self.docker_image_dict[key]
            self.cluster_manager.upload_image(image)

    def __install_crd_and_operator(self):
        """
        Install both CRD and KubeRay operator by kuberay-operator chart.
        """
        with tempfile.NamedTemporaryFile("w", suffix="_values.yaml") as values_fd:
            # dump the config to a temporary file and use the file as values.yaml in the chart.
            yaml.safe_dump(self.values_yaml, values_fd)
            repo, tag = self.docker_image_dict[CONST.OPERATOR_IMAGE_KEY].split(":")
            if f"{repo}:{tag}" == CONST.KUBERAY_LATEST_RELEASE:
                logger.info(
                    "Install both CRD and KubeRay operator with the latest release."
                )
                shell_subprocess_run(
                    "helm repo add kuberay https://ray-project.github.io/kuberay-helm/"
                )
                shell_subprocess_run(
                    f"helm install -n {self.namespace} -f {values_fd.name} kuberay-operator "
                    f"kuberay/kuberay-operator --version {tag[1:]}"
                )
            else:
                logger.info(
                    "Install both CRD and KubeRay operator by kuberay-operator chart"
                )
                shell_subprocess_run(
                    f"helm install -n {self.namespace} -f {values_fd.name} kuberay-operator "
                    f"{CONST.HELM_CHART_ROOT}/kuberay-operator/ "
                    f"--set image.repository={repo},image.tag={tag}"
                )

class ScriptBasedOperatorManager(OperatorManager):
    def __init__(self):
        self.installation_script = os.getenv(self.KUBERAY_OPERATOR_INSTALLATION_SCRIPT)

    def prepare_operator(self):
        return_code = shell_subprocess_run(self.installation_script)
        if return_code != 0:
            raise Exception("Operator installation failed with exit code " + str(return_code))


def shell_subprocess_run(command, check=True, hide_output=False) -> int:
    """Command will be executed through the shell.

    Args:
        check: If true, an error will be raised if the returncode is nonzero
        hide_output: If true, stdout and stderr of the command will be hidden

    Returns:
        Return code of the subprocess.
    """
    logger.info("Execute command: %s", command)
    if hide_output:
        return subprocess.run(
            command,
            shell=True,
            check=check,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        ).returncode
    else:
        return subprocess.run(command, shell=True, check=check).returncode


def shell_subprocess_check_output(command):
    """
    Run command and return STDOUT as encoded bytes.
    """
    logger.info("Execute command (check_output): %s", command)

    try:
        output = subprocess.check_output(command, shell=True)
        logger.info("Output: %s", output)
        return output
    except subprocess.CalledProcessError as e:
        logger.info("Exception: %s", e.output)
        raise


def get_pod(namespace, label_selector):
    """Gets pods in the `namespace`. Returns the first pod that has `label_filter`.
    Returns None if the number of matches is not equal to 1.
    """
    pods = K8S_CLUSTER_MANAGER.k8s_client_dict[
        CONST.K8S_V1_CLIENT_KEY
    ].list_namespaced_pod(namespace=namespace, label_selector=label_selector)
    if len(pods.items) != 1:
        logger.warning(
            "There are %d matches for selector %s in namespace %s, but the expected match is 1.",
            len(pods.items),
            label_selector,
            namespace,
        )
        return None
    return pods.items[0]


def get_head_pod(namespace):
    """Gets a head pod in the `namespace`. Returns None if there are no matches."""
    return get_pod(namespace, "ray.io/node-type=head")


def pod_exec_command(pod_name, namespace, exec_command, check=True):
    """kubectl exec the `exec_command` in the given `pod_name` Pod in the given `namespace`.
    Both STDOUT and STDERR of `exec_command` will be printed.
    """
    return shell_subprocess_run(
        f"kubectl exec {pod_name} -n {namespace} -- {exec_command}", check
    )

def delete_all_cr(crd_name, namespace, check=True):
    return shell_subprocess_run(
        f"kubectl delete {crd_name} --all -n {namespace}", check
    )


def start_curl_pod(name: str, namespace: str, timeout_s: int = -1):
    shell_subprocess_run(
        f"kubectl run {name} --image=rancher/curl -n {namespace} "
        '--command -- /bin/sh -c "while true; do sleep 10;done"'
    )

    # Wait for curl pod to be created
    k8s_v1_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY]
    start_time = time.time()
    while time.time() - start_time < timeout_s or timeout_s < 0:
        resp = k8s_v1_api.read_namespaced_pod(name=name, namespace=namespace)
        if resp.status.phase == "Running":
            return

    raise TimeoutError(f"Curl pod wasn't started in {timeout_s}s.")


def create_custom_object(namespace, cr_object):
    """Create a custom resource based on `cr_object` in the given `namespace`."""
    k8s_cr_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
    crd = cr_object["kind"]
    if crd == CONST.RAY_CLUSTER_CRD:
        k8s_cr_api.create_namespaced_custom_object(
            group="ray.io",
            version="v1",
            namespace=namespace,
            plural="rayclusters",
            body=cr_object,
        )
    elif crd == CONST.RAY_SERVICE_CRD:
        k8s_cr_api.create_namespaced_custom_object(
            group="ray.io",
            version="v1",
            namespace=namespace,
            plural="rayservices",
            body=cr_object,
        )
    elif crd == CONST.RAY_JOB_CRD:
        k8s_cr_api.create_namespaced_custom_object(
            group="ray.io",
            version="v1",
            namespace=namespace,
            plural="rayjobs",
            body=cr_object,
        )


def delete_custom_object(crd, namespace, cr_name):
    """Delete the given `cr_name` custom resource in the given `namespace`."""
    k8s_cr_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
    if crd == CONST.RAY_CLUSTER_CRD:
        k8s_cr_api.delete_namespaced_custom_object(
            group="ray.io",
            version="v1",
            namespace=namespace,
            plural="rayclusters",
            name=cr_name,
        )
    elif crd == CONST.RAY_SERVICE_CRD:
        k8s_cr_api.delete_namespaced_custom_object(
            group="ray.io",
            version="v1",
            namespace=namespace,
            plural="rayservices",
            name=cr_name,
        )
    elif crd == CONST.RAY_JOB_CRD:
        k8s_cr_api.delete_namespaced_custom_object(
            group="ray.io",
            version="v1",
            namespace=namespace,
            plural="rayjobs",
            name=cr_name,
        )


def get_custom_object(crd, namespace, cr_name):
    """Get the given `cr_name` custom resource in the given `namespace`."""
    k8s_cr_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
    if crd == CONST.RAY_CLUSTER_CRD:
        return k8s_cr_api.get_namespaced_custom_object(
            group="ray.io",
            version="v1",
            namespace=namespace,
            plural="rayclusters",
            name=cr_name,
        )
    elif crd == CONST.RAY_SERVICE_CRD:
        return k8s_cr_api.get_namespaced_custom_object(
            group="ray.io",
            version="v1",
            namespace=namespace,
            plural="rayservices",
            name=cr_name,
        )
    elif crd == CONST.RAY_JOB_CRD:
        return k8s_cr_api.get_namespaced_custom_object(
            group="ray.io",
            version="v1",
            namespace=namespace,
            plural="rayjobs",
            name=cr_name,
        )
