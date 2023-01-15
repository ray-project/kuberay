"""Utilities for integration tests of KubeRay."""

import subprocess
import logging
from pathlib import Path
from kubernetes import client, config
import yaml

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
    KUBERAY_LATEST_RELEASE = "kuberay/operator:v0.4.0"

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

    # Custom Resource Definitions
    RAY_CLUSTER_CRD = "RayCluster"
    RAY_SERVICE_CRD = "RayService"
    RAY_JOB_CRD = "RayJob"

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

class OperatorManager:
    """
    OperatorManager controlls the lifecycle of KubeRay operator. It will download Docker images,
    load images into an existing KinD cluster, and install CRD and KubeRay operator.
    """
    def __init__(self, docker_image_dict) -> None:
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
                else:
                    logger.info("Image %s exists", image)

        download_images()
        logger.info("Load images into KinD cluster")
        for key in self.docker_image_dict:
            image = self.docker_image_dict[key]
            shell_subprocess_run(f'kind load docker-image {image}')

    def __install_crd_and_operator(self):
        """Install both CRD and KubeRay operator by kuberay-operator chart"""
        repo, tag = self.docker_image_dict[CONST.OPERATOR_IMAGE_KEY].split(':')
        if f"{repo}:{tag}" == CONST.KUBERAY_LATEST_RELEASE:
            logger.info("Install both CRD and KubeRay operator with the latest release.")
            shell_subprocess_run(
                "helm repo add kuberay https://ray-project.github.io/kuberay-helm/"
            )
            shell_subprocess_run(
                f"helm install kuberay-operator kuberay/kuberay-operator --version {tag[1:]}"
            )
        else:
            logger.info("Install both nightly CRD and KubeRay operator by kuberay-operator chart")
            shell_subprocess_run(
                f"helm install kuberay-operator {CONST.HELM_CHART_ROOT}/kuberay-operator/ "
                f"--set image.repository={repo},image.tag={tag}"
            )

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

def get_pod(namespace, label_selector):
    """Gets pods in the `namespace`. Returns the first pod that has `label_filter`.
    Returns None if the number of matches is not equal to 1.
    """
    pods = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_V1_CLIENT_KEY].list_namespaced_pod(
            namespace = namespace, label_selector = label_selector
        )
    if len(pods.items) != 1:
        logger.warning(
            "There are %d matches for selector %s in namespace %s, but the expected match is 1.",
            len(pods.items), label_selector, namespace
        )
        return None
    return pods.items[0]

def get_head_pod(namespace):
    """Gets a head pod in the `namespace`. Returns None if there are no matches."""
    return get_pod(namespace, 'ray.io/node-type=head')

def pod_exec_command(pod_name, namespace, exec_command, check = True):
    """kubectl exec the `exec_command` in the given `pod_name` Pod in the given `namespace`.
    Both STDOUT and STDERR of `exec_command` will be printed.
    """
    return shell_subprocess_run(f"kubectl exec {pod_name} -n {namespace} -- {exec_command}", check)

def create_custom_object(namespace, cr_object):
    """Create a custom resource based on `cr_object` in the given `namespace`."""
    k8s_cr_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
    crd = cr_object["kind"]
    if crd == CONST.RAY_CLUSTER_CRD:
        k8s_cr_api.create_namespaced_custom_object(
            group = 'ray.io', version = 'v1alpha1', namespace = namespace,
            plural = 'rayclusters', body = cr_object)
    elif crd == CONST.RAY_SERVICE_CRD:
        k8s_cr_api.create_namespaced_custom_object(
            group = 'ray.io', version = 'v1alpha1', namespace = namespace,
            plural = 'rayservices', body = cr_object)
    elif crd == CONST.RAY_JOB_CRD:
        raise NotImplementedError

def delete_custom_object(crd, namespace, cr_name):
    """Delete the given `cr_name` custom resource in the given `namespace`."""
    k8s_cr_api = K8S_CLUSTER_MANAGER.k8s_client_dict[CONST.K8S_CR_CLIENT_KEY]
    if crd == CONST.RAY_CLUSTER_CRD:
        k8s_cr_api.delete_namespaced_custom_object(
            group = 'ray.io', version = 'v1alpha1', namespace = namespace,
            plural = 'rayclusters', name = cr_name)
    elif crd == CONST.RAY_SERVICE_CRD:
        k8s_cr_api.delete_namespaced_custom_object(
            group = 'ray.io', version = 'v1alpha1', namespace = namespace,
            plural = 'rayservices', name = cr_name)
    elif crd == CONST.RAY_JOB_CRD:
        raise NotImplementedError

def retrieve_images(filepaths):
    '''
    retrieve all images needed based on the yaml files. It will return a dict that include all image
    names. The tipical use case is to pass the image dict created by retrieve_images to
    operator_manager, then the operator manager will download all images to cluster:
    image_dict = retrieve_images(filepaths)
    image_dict[CONST.OPERATOR_IMAGE_KEY] = os.getenv(
                                            'OPERATOR_IMAGE',
                                            default='kuberay/operator:nightly'
                                            )
    operator_manager = OperatorManager(image_dict)
    operator_manager.prepare_operator()
    '''
    def prepare_next(cur,step):
        """
        Cur is a list, its element is either a list or a dict that may include images.
        Step is either a dict key, index of a list or '*' indicator(meaning to go through the list)
        prepare_next will return the next cur based on the step.
        """
        if cur == []:
            return []
        # case1: cur element is a list and only cur[step] is needed
        if step.isnumeric() and isinstance(cur[0],list) and int(step)<=len(cur[0]):
            return list(map( lambda x : x[int(step)],cur))
        # case2: cur element is a dic
        # [Example] cur=[{'containers':[{'name':'nginx','image':'nginx:1.14.2'}],step='containers'
        # the new cur should be: [[{'name':'nginx','image':'nginx:1.14.2'}]]
        if step!='*' and isinstance(cur[0],dict) and step in cur[0]:
            return list(map( lambda x : x[step],cur))
        # case3: cur element is a list and every element in the list is needed
        # [Example] cur=[[{'name':'nginx','image':'nginx:1.14.2'},
        #                 {'name':'nginx','image':'nginx:1.14.3'}]],step='*'
        # the new cur should be:[{'name':'nginx','image':'nginx:1.14.2'},
        #                        {'name':'nginx','image':'nginx:1.14.3'}]
        if isinstance(cur[0],list):
            return [ele for ele_list in cur for ele in ele_list]
        # case4: invliad
        return []

    image_dict = {}
    file_dict = {}
    # image_path_dict defines the path of the image for k8s kinds. May add more kind in the future.
    # * indicate it is a list and needs to go through all the elements in the list. examples:
    # 'spec/workerGroupSpecs/* is a list, since multiple workerGroup can be created and each
    # workerGroup can have different images.
    image_path_dict = {
        # Custom resources:
        'RayCluster': [
            'spec/headGroupSpec/template/spec/containers/*/image',
            'spec/workerGroupSpecs/*/template/spec/containers/*/image',
            'spec/workerGroupSpecs/*/template/spec/initContainers/*/image'
        ],
        'RayService':[
            'spec/rayClusterConfig/headGroupSpec/template/spec/containers/*/image',
            'spec/rayClusterConfig/workerGroupSpecs/*/template/spec/containers/*/image',
            'spec/rayClusterConfig/workerGroupSpecs/*/template/spec/initContainers/*/image'
        ],

        'RayJob':[
            'spec/rayClusterSpec/headGroupSpec/template/spec/containers/*/image',
            'spec/rayClusterSpec/workerGroupSpecs/*/template/spec/containers/*/image',
            'spec/rayClusterSpec/workerGroupSpecs/*/template/spec/initContainers/*/image'
        ],
        # Workloads:
        'Pod':['spec/containers/*/image'],
        'Deployment':['spec/template/spec/containers/*/image'],
        'ReplicaSet':['spec/template/spec/containers/*/image'],
        'StatefulSet':['spec/template/spec/containers/*/image'],
        'DaemonSet':['spec/template/spec/containers/*/image'],
        'Job':['spec/template/spec/containers/*/image'],
        'CronJob':['spec/jobTemplate/spec/template/spec/containers/*/image'],
        'ReplicationController':['spec/template/spec/containers/*/image'],


    }
    for filepath in filepaths:
        with open(filepath, encoding="utf-8") as k8s_yaml:
            file_dict[filepath] = set()
            for k8s_object in yaml.safe_load_all(k8s_yaml):
                kind = k8s_object['kind']
                # if kind is not ray cr or Workloads, no knowledge about the image path, hence skip
                if kind not in image_path_dict:
                    continue
                for steps in image_path_dict[kind]:
                    # cur is a list, its element is either a list or a dict that may include images
                    cur = [k8s_object]
                    step_list = steps.split('/')
                    for step in step_list:
                        cur = prepare_next(cur,step)
                        if cur == []:
                            break
                    # After go through the image path, cur is now a list that conatin images
                    for image in cur:
                        file_dict[filepath].add(image)
                        if image not in image_dict:
                            image_dict[image] = image
    for filepath,images in file_dict.items():
        logger.info("finding %s in %s", list(images),filepath.split('/')[-1],)
    return image_dict
