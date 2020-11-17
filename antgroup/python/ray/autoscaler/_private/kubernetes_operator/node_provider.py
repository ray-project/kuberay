import logging, copy
from uuid import uuid4
from kubernetes.client.rest import ApiException
from kubernetes.client.models import v1_owner_reference

from ray.autoscaler._private.command_runner import KubernetesCommandRunner
from ray.autoscaler._private.kubernetes_operator import core_api, log_prefix, \
    extensions_beta_api, custom_object_api
from ray.autoscaler._private.kubernetes_operator.config import bootstrap_kubernetes
from ray.autoscaler.node_provider import NodeProvider
from ray.autoscaler.tags import TAG_RAY_CLUSTER_NAME, TAG_RAY_NODE_KIND
from ray.autoscaler._private.kubernetes_operator.ray_cluster_constant import CRD_RAY_GROUP, \
    CRD_RAY_VERSION, CRD_RAY_PLURAL, CRD_RAY_KIND, DASH

logger = logging.getLogger(__name__)


def to_label_selector(tags):
    label_selector = ""
    for k, v in tags.items():
        if label_selector != "":
            label_selector += ","
        label_selector += "{}={}".format(k, v)
    return label_selector


def build_owner_reference(body) -> v1_owner_reference.V1OwnerReference:
    reference = v1_owner_reference.V1OwnerReference(
        controller=True,
        block_owner_deletion=True,
        api_version=CRD_RAY_GROUP + '/' + CRD_RAY_VERSION,
        kind=CRD_RAY_KIND,
        name=body.get('name'),
        uid=body.get('uid'),
    )
    return reference


def append_owner_reference(body, obj):
    owner_reference = build_owner_reference(body)
    refs = obj.setdefault('metadata', {}).setdefault('ownerReferences', [])
    matching = [ref for ref in refs if ref.to_dict()['uid'] == body.get('uid')]
    if not matching:
        refs.append(owner_reference)


class KubernetesOperatorNodeProvider(NodeProvider):
    def __init__(self, provider_config, cluster_name):
        NodeProvider.__init__(self, provider_config, cluster_name)
        self.cluster_name = cluster_name
        self.namespace = provider_config["namespace"]

    def non_terminated_nodes(self, tag_filters):
        # Match pods that are in the 'Pending' or 'Running' phase.
        # Unfortunately there is no OR operator in field selectors, so we
        # have to match on NOT any of the other phases.
        field_selector = ",".join([
            "status.phase!=Failed",
            "status.phase!=Unknown",
            "status.phase!=Succeeded",
            "status.phase!=Terminating",
        ])

        tag_filters[TAG_RAY_CLUSTER_NAME] = self.cluster_name
        label_selector = to_label_selector(tag_filters)
        pod_list = core_api().list_namespaced_pod(
            self.namespace,
            field_selector=field_selector,
            label_selector=label_selector)

        return [pod.metadata.name for pod in pod_list.items]

    def is_running(self, node_id):
        pod = core_api().read_namespaced_pod(node_id, self.namespace)
        return pod.status.phase == "Running"

    def is_terminated(self, node_id):
        pod = core_api().read_namespaced_pod(node_id, self.namespace)
        return pod.status.phase not in ["Running", "Pending"]

    def node_tags(self, node_id):
        pod = core_api().read_namespaced_pod(node_id, self.namespace)
        return pod.metadata.labels

    def external_ip(self, node_id):
        raise NotImplementedError("Must use internal IPs with Kubernetes.")

    def internal_ip(self, node_id):
        pod = core_api().read_namespaced_pod(node_id, self.namespace)
        return pod.status.pod_ip

    def get_node_id(self, ip_address, use_internal_ip=True) -> str:
        if not use_internal_ip:
            raise ValueError("Must use internal IPs with Kubernetes.")
        return super().get_node_id(ip_address, use_internal_ip=use_internal_ip)

    def set_node_tags(self, node_id, tags):
        pod = core_api().read_namespaced_pod(node_id, self.namespace)
        pod.metadata.labels.update(tags)
        core_api().patch_namespaced_pod(node_id, self.namespace, pod)

    def create_node(self, node_config, tags, count):
        # create a raycluster
        conf = node_config.copy()
        pod_spec = conf.get("pod", conf)
        service_spec = conf.get("service")
        ingress_spec = conf.get("ingress")
        tags[TAG_RAY_CLUSTER_NAME] = self.cluster_name
        pod_spec["metadata"]["namespace"] = self.namespace
        if "labels" in pod_spec["metadata"]:
            pod_spec["metadata"]["labels"].update(tags)
        else:
            pod_spec["metadata"]["labels"] = tags

        new_node_names = []

        # init exist_cluster to None
        exist_cluster = None
        try:
            exist_cluster = custom_object_api().get_namespaced_custom_object(
                group=CRD_RAY_GROUP,
                version=CRD_RAY_VERSION,
                namespace=self.namespace,
                plural=CRD_RAY_PLURAL,
                name=self.cluster_name)
            logger.info(log_prefix + "calling get_namespaced_custom_object "
                        "(cluster={}).".format(exist_cluster))
        except ApiException:
            pass

        body = {}
        raycluster = None
        if exist_cluster is None:
            extensions = []
            extension = self.generate_cluster_pod_meta(count, pod_spec, tags,
                                                       new_node_names)
            extensions.append(extension)
            spec = self.generate_cluster_meta(body, pod_spec)
            spec['extensions'] = extensions
            body['spec'] = spec
            logger.info(log_prefix + "calling create_namespaced_custom_object")
            raycluster = custom_object_api().create_namespaced_custom_object(
                group=CRD_RAY_GROUP,
                version=CRD_RAY_VERSION,
                namespace=self.namespace,
                plural=CRD_RAY_PLURAL,
                body=body,
                pretty='true')
        else:
            extensions = exist_cluster['spec']['extensions']
            extensions.append(
                self.generate_cluster_pod_meta(count, pod_spec, tags,
                                               new_node_names))
            exist_cluster['spec']['extensions'] = extensions
            body = exist_cluster
            logger.info(log_prefix + "calling patch_namespaced_custom_object")
            raycluster = custom_object_api().patch_namespaced_custom_object(
                group=CRD_RAY_GROUP,
                version=CRD_RAY_VERSION,
                namespace=self.namespace,
                plural=CRD_RAY_PLURAL,
                name=self.cluster_name,
                body=body)

        new_svcs = []
        if service_spec is not None:
            logger.info(log_prefix + "calling create_namespaced_service "
                        "(count={}).".format(count))

            for new_node_name in new_node_names:
                service_spec_copy = copy.deepcopy(service_spec)
                metadata = service_spec_copy.get("metadata", {})
                metadata["name"] = new_node_name
                service_spec_copy["metadata"] = metadata
                append_owner_reference(body={
                    'name': self.cluster_name,
                    'uid': raycluster['metadata']['uid']
                },
                                       obj=service_spec_copy)
                service_spec_copy["spec"]["selector"] = {
                    "raycluster.component": new_node_name
                }
                svc = core_api().create_namespaced_service(
                    self.namespace, service_spec_copy)
                new_svcs.append(svc)

        if ingress_spec is not None:
            logger.info(log_prefix + "calling create_namespaced_ingress "
                        "(count={}).".format(count))
            for new_svc in new_svcs:
                ingress_spec_copy = copy.deepcopy(ingress_spec)
                metadata = ingress_spec_copy.get("metadata", {})
                metadata["name"] = new_svc.metadata.name
                ingress_spec_copy["metadata"] = metadata
                append_owner_reference(body={
                    'name': self.cluster_name,
                    'uid': raycluster['metadata']['uid']
                },
                                       obj=ingress_spec_copy)
                ingress_spec_copy = _add_service_name_to_service_port(
                    ingress_spec_copy, new_svc.metadata.name)
                extensions_beta_api().create_namespaced_ingress(
                    self.namespace, ingress_spec_copy)

    def generate_cluster_meta(self, body, pod_spec):
        spec = {}
        body['apiVersion'] = CRD_RAY_GROUP + '/' + CRD_RAY_VERSION
        body['kind'] = CRD_RAY_KIND
        body['metadata'] = {}
        body['metadata']['name'] = self.cluster_name
        spec['clusterName'] = self.cluster_name
        spec['images'] = {}
        spec['images']['defaultImage'] = pod_spec['spec']['containers'][0][
            'image']
        spec['imagePullPolicy'] = 'Always'
        return spec

    def generate_cluster_pod_meta(self, count, pod_spec, tags, new_node_names):
        extension = {}
        extension['replicas'] = count
        pod_id_list = []
        for _ in range(count):
            pod_name = self.cluster_name + DASH + tags[
                TAG_RAY_NODE_KIND] + DASH + str(uuid4())[:8]
            pod_id_list.append(pod_name)
            if pod_name not in new_node_names:
                new_node_names.append(pod_name)
        extension['type'] = tags[TAG_RAY_NODE_KIND]
        extension['idList'] = pod_id_list
        extension['image'] = pod_spec['spec']['containers'][0]['image']
        extension['groupName'] = ''
        extension['command'] = ' '.join(
            map(str, pod_spec['spec']['containers'][0]['args']))
        extension['labels'] = pod_spec["metadata"]["labels"]
        extension['serviceAccountName'] = pod_spec["spec"][
            "serviceAccountName"]
        extension['ports'] = pod_spec['spec']['containers'][0].setdefault(
            'ports', [])
        extension['volumes'] = pod_spec['spec']['volumes']
        extension['volumeMounts'] = pod_spec['spec']['containers'][
            0].setdefault('volumeMounts', [])
        extension['resources'] = pod_spec['spec']['containers'][0]['resources']
        extension['containerEnv'] = pod_spec['spec']['containers'][
            0].setdefault('env', [])
        return extension

    def terminate_node(self, node_id):
        # delete raycluster pod
        logger.info(log_prefix + "calling terminate_node")
        self.terminate_pods([node_id])

        try:
            core_api().delete_namespaced_service(node_id, self.namespace)
        except ApiException:
            pass
        try:
            extensions_beta_api().delete_namespaced_ingress(
                node_id,
                self.namespace,
            )
        except ApiException:
            pass

    def terminate_nodes(self, node_ids):
        # delete raycluster pods
        nodes = self.non_terminated_nodes({})
        if len(nodes) == len(node_ids):
            try:
                logger.info(log_prefix +
                            "calling delete_namespaced_custom_object")
                custom_object_api().delete_namespaced_custom_object(
                    group=CRD_RAY_GROUP,
                    version=CRD_RAY_VERSION,
                    namespace=self.namespace,
                    plural=CRD_RAY_PLURAL,
                    name=self.cluster_name)
            except ApiException:
                pass
        else:
            logger.info(log_prefix + "calling terminate_pods")
            self.terminate_pods(node_ids)

    def terminate_pods(self, node_ids):
        try:
            exist_cluster = custom_object_api().get_namespaced_custom_object(
                group=CRD_RAY_GROUP,
                version=CRD_RAY_VERSION,
                namespace=self.namespace,
                plural=CRD_RAY_PLURAL,
                name=self.cluster_name)
            extensions = exist_cluster['spec']['extensions']
            desired_extensions = []
            for extension in extensions:
                pod_id_list = extension['idList']
                for node_id in node_ids:
                    if node_id in pod_id_list:
                        pod_id_list.remove(node_id)
                if len(pod_id_list) > 0:
                    extension['idList'] = pod_id_list
                    desired_extensions.append(extension)
            exist_cluster['spec']['extensions'] = desired_extensions
            logger.info(log_prefix + "calling replace_namespaced_custom_object"
                        " in terminate_nodes")
            custom_object_api().replace_namespaced_custom_object(
                group=CRD_RAY_GROUP,
                version=CRD_RAY_VERSION,
                namespace=self.namespace,
                plural=CRD_RAY_PLURAL,
                name=self.cluster_name,
                body=exist_cluster)
        except ApiException as e:
            logger.info(log_prefix + "calling terminate_pods failed "
                        "(error={})".format(e))
            pass

    def get_command_runner(self,
                           log_prefix,
                           node_id,
                           auth_config,
                           cluster_name,
                           process_runner,
                           use_internal_ip,
                           docker_config=None):
        return KubernetesCommandRunner(log_prefix, self.namespace, node_id,
                                       auth_config, process_runner)

    @staticmethod
    def bootstrap_config(cluster_config):
        return bootstrap_kubernetes(cluster_config)


def _add_service_name_to_service_port(spec, svc_name):
    """Goes recursively through the ingress manifest and adds the
    right serviceName next to every servicePort definition.
    """
    if isinstance(spec, dict):
        dict_keys = list(spec.keys())
        for k in dict_keys:
            spec[k] = _add_service_name_to_service_port(spec[k], svc_name)

            if k == "serviceName" and spec[k] != svc_name:
                raise ValueError(
                    "The value of serviceName must be set to "
                    "${RAY_POD_NAME}. It is automatically replaced "
                    "when using the autoscaler.")

    elif isinstance(spec, list):
        spec = [
            _add_service_name_to_service_port(item, svc_name) for item in spec
        ]

    elif isinstance(spec, str):
        # The magic string ${RAY_POD_NAME} is replaced with
        # the true service name, which is equal to the worker pod name.
        if "${RAY_POD_NAME}" in spec:
            spec = spec.replace("${RAY_POD_NAME}", svc_name)
    return spec

