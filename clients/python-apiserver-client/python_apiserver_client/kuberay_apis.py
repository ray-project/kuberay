import requests
import time

from .params import *


_headers = {"Content-Type": "application/json", "accept": "application/json"}


class KubeRayAPIs:
    """
        This class implements KubeRay APIs based on the API server.
        To create a class, the following parameters are required:
            base - the URL of the API server (default is set to the standalone API server)
            wait interval - the amount of sec to wait between checking for cluster ready
    """
    def __init__(self, base: str = "http://localhost:31888", token: str = None,
                 wait_interval: int = 2) -> None:
        self.base = base
        if token is not None:
            _headers["Authorization"] = token
        self.wait_interval = wait_interval
        self.api_base = "/apis/v1/"

    """
        List compute templates across all namespaces of the k8 cluster
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            list of compute templates
    """
    def list_compute_templates(self) -> tuple[int, str, list[Template]]:
        # Execute HTTP request
        url = self.base + self.api_base + "compute_templates"
        response = requests.get(url, headers=_headers, timeout=None)
        # Check execution status
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"], None
        return response.status_code, None, templates_decoder(response.json())

    """
        List compute templates across for a given namespaces of the k8 cluster
        Parameter:
            namespace to query
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            list of compute templates
    """
    def list_compute_templates_namespace(self, ns: str) -> tuple[int, str, list[Template]]:
        # Execute HTTP request
        url = self.base + self.api_base + f"namespaces/{ns}/compute_templates"
        response = requests.get(url, headers=_headers, timeout=(10, 10))
        # Check execution status
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"], None
        return response.status_code, None, templates_decoder(response.json())

    """
        get a compute template
        Parameter:
            namespace
            template name
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            compute templates
    """
    def get_compute_template(self, ns: str, name: str) -> tuple[int, str, Template]:
        # Execute HTTP request
        url = self.base + self.api_base + f"namespaces/{ns}/compute_templates/{name}"
        response = requests.get(url, headers=_headers, timeout=(10, 10))
        # Check execution status
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"], None
        return response.status_code, None, template_decoder(response.json())

    """
        Create a compute template
        Parameter:
            template - definition of a template
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
    """
    def create_compute_template(self, template: Template) -> tuple[int, str]:
        # Execute HTTP request
        url = self.base + self.api_base + f"namespaces/{template.namespace}/compute_templates"
        response = requests.post(url, json=template.to_dict(), headers=_headers, timeout=(10, 10))
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"]
        return response.status_code, None

    """
        delete a compute template
        Parameter:
            namespace
            template name
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
    """
    def delete_compute_template(self, ns: str, name: str) -> tuple[int, str]:
        # Execute HTTP request
        url = self.base + self.api_base + f"namespaces/{ns}/compute_templates/{name}"
        response = requests.delete(url, headers=_headers, timeout=(10, 10))
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"]
        return response.status_code, None

    """
        List clusters across all namespaces of the k8 cluster
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            list of clusters
    """
    def list_clusters(self) -> tuple[int, str, list[Cluster]]:
        # Execute HTTP request
        url = self.base + self.api_base + "clusters"
        response = requests.get(url, headers=_headers, timeout=None)
        # Check execution status
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"], None
        return response.status_code, None, clusters_decoder(response.json())

    """
        List clusters across for a given namespaces of the k8 cluster
        Parameter:
            namespace to query
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            list of clusters
    """
    def list_clusters_namespace(self, ns: str) -> tuple[int, str, list[Cluster]]:
        # Execute HTTP request
        url = self.base + self.api_base + f"namespaces/{ns}/clusters"
        response = requests.get(url, headers=_headers, timeout=(10, 10))
        # Check execution status
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"], None
        return response.status_code, None, clusters_decoder(response.json())

    """
        get cluster
        Parameter:
            namespace to query
            name of the cluster
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            clusters definition
    """
    def get_cluster(self, ns: str, name: str) -> tuple[int, str, Cluster]:
        # Execute HTTP request
        url = self.base + self.api_base + f"namespaces/{ns}/clusters/{name}"
        response = requests.get(url, headers=_headers, timeout=(10, 10))
        # Check execution status
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"], None
        return response.status_code, None, cluster_decoder(response.json())

    """
        create cluster
        Parameter:
            cluster definition
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
    """
    def create_cluster(self, cluster: Cluster) -> tuple[int, str]:
        # Execute HTTP request
        url = self.base + self.api_base + f"namespaces/{cluster.namespace}/clusters"
        response = requests.post(url, json=cluster.to_dict(), headers=_headers, timeout=(10, 10))
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"]
        return response.status_code, None

    """
        get cluster status
        Parameter:
            namespace of the cluster
            name of the cluster
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            cluster status
    """
    def get_cluster_status(self, ns: str, name: str) -> tuple[int, str, str]:
        # Execute HTTP request
        status, error, cluster = self.get_cluster(ns=ns, name=name)
        # Check execution status
        if status // 100 != 2:
            return status, error, None
        cluster_status = "creating"
        if cluster.cluster_status is not None:
            cluster_status = cluster.cluster_status
        return status, None, cluster_status

    """
        wait cluster ready
        Parameter:
            namespace of the cluster
            name of the cluster
            wait time (-1 waits forever)
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            cluster status
    """
    def wait_cluster_ready(self, ns: str, name: str, wait: int = -1) -> tuple[int, str]:
        current_wait = 0
        while True:
            status, error, c_status = self.get_cluster_status(ns=ns, name=name)
            # Check execution status
            if status // 100 != 2:
                return status, error
            if c_status == "ready":
                return status, None
            if current_wait > wait > 0:
                return 408, f"Timed out waiting for cluster ready in {current_wait} sec"
            time.sleep(self.wait_interval)
            current_wait += self.wait_interval

    """
        get cluster endpoint
        Parameter:
            namespace of the cluster
            name of the cluster
            wait time (-1 waits forever) for cluster to be ready
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            endpoint (service for dashboard endpoint)
    """
    def get_cluster_endpoints(self, ns: str, name: str, wait: int = -1) -> tuple[int, str, str]:
        # Ensure that the cluster is ready
        status, error = self.wait_cluster_ready(ns=ns, name=name, wait=wait)
        if status // 100 != 2:
            return status, error, None
        # Get cluster
        status, error, cluster = self.get_cluster(ns=ns, name=name)
        if status // 100 != 2:
            return status, error, None
        return status, None, f"{name}-head-svc.{ns}.svc.cluster.local:{cluster.service_endpoint['dashboard']}"

    """
        delete cluster
        Parameter:
            namespace of the cluster
            name of the cluster
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
    """
    def delete_cluster(self, ns: str, name: str) -> tuple[int, str]:
        # Execute HTTP request
        url = self.base + self.api_base + f"namespaces/{ns}/clusters/{name}"
        response = requests.delete(url, headers=_headers)
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"]
        return response.status_code, None

    """
        submit Ray job
        Parameter:
            namespace of the cluster
            name of the cluster
            job submission
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            submission id
    """
    def submit_job(self, ns: str, name: str, jobrequest: RayJobRequest) -> tuple[int, str, str]:
        url = self.base + self.api_base + f"namespaces/{ns}/jobsubmissions/{name}"
        response = requests.post(url, json=jobrequest.to_dict(), headers=_headers, timeout=(10, 10))
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"], None
        return response.status_code, None, response.json()["submissionId"]

    """
        get Ray job details
        Parameter:
            namespace of the cluster
            name of the cluster
            job submission id
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            RayJobInfo object
    """
    def get_job_info(self, ns: str, name: str, sid: str) -> tuple[int, str, RayJobInfo]:
        url = self.base + self.api_base + f"namespaces/{ns}/jobsubmissions/{name}/{sid}"
        response = requests.get(url, headers=_headers, timeout=(10, 10))
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"], None
        return response.status_code, None, RayJobInfo(response.json())

    """
        list Ray job details
        Parameter:
            namespace of the cluster
            name of the cluster
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            list of RayJobInfo object
    """
    def list_job_info(self, ns: str, name: str) -> tuple[int, str, list[RayJobInfo]]:
        url = self.base + self.api_base + f"namespaces/{ns}/jobsubmissions/{name}"
        response = requests.get(url, headers=_headers, timeout=(10, 10))
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"], None
        infos = response.json().get("submissions", None)
        if infos is None:
            return response.status_code, None, []
        return response.status_code, None, [RayJobInfo(i) for i in infos]

    """
        get Ray job log
        Parameter:
            namespace of the cluster
            name of the cluster
            job submission id
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
            log
    """
    def get_job_log(self, ns: str, name: str, sid: str) -> tuple[int, str, str]:
        url = self.base + self.api_base + f"namespaces/{ns}/jobsubmissions/{name}/log/{sid}"
        response = requests.get(url, headers=_headers, timeout=(10, 10))
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"], None
        return response.status_code, None, response.json().get("log", "")

    """
        stop Ray job
        Parameter:
            namespace of the cluster
            name of the cluster
            job submission id
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
    """
    def stop_ray_job(self, ns: str, name: str, sid: str) -> tuple[int, str]:
        url = self.base + self.api_base + f"namespaces/{ns}/jobsubmissions/{name}/{sid}"
        response = requests.post(url, headers=_headers, timeout=(10, 10))
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"]
        return response.status_code, None

    """
        delete Ray job
        Parameter:
            namespace of the cluster
            name of the cluster
            job submission id
        Returns:
            http return code
            message - only returned if http return code is not equal to 200
    """
    def delete_ray_job(self, ns: str, name: str, sid: str) -> tuple[int, str]:
        url = self.base + self.api_base + f"namespaces/{ns}/jobsubmissions/{name}/{sid}"
        response = requests.delete(url, headers=_headers, timeout=(10, 10))
        if response.status_code // 100 != 2:
            return response.status_code, response.json()["message"]
        return response.status_code, None
