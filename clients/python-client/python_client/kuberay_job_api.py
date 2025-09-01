"""
Set of APIs to manage rayjobs.
"""

import logging
import time
from kubernetes import client, config
from kubernetes.client.rest import ApiException
from typing import Any, Optional
from python_client import constants


log = logging.getLogger(__name__)
if logging.getLevelName(log.level) == "NOTSET":
    logging.basicConfig(format="%(asctime)s %(message)s", level=constants.LOGLEVEL)

TERMINAL_JOB_STATUSES = [
    "STOPPED",
    "SUCCEEDED",
    "FAILED",
]


class RayjobApi:
    """
    RayjobApi provides APIs to list, get, create, build, update, delete rayjobs.
    Methods:
    - submit_job(k8s_namespace: str, job: Any) -> Any: Submit and execute a job asynchronously.
    - stop_job(name: str, k8s_namespace: str) -> bool: Stop a job by suspending it.
    - resubmit_job(name: str, k8s_namespace: str) -> bool: Resubmit a job that has been suspended.
    - get_job_status(name: str, k8s_namespace: str, timeout: int, delay_between_attempts: int) -> Any: Get the most recent status of a job.
    - wait_until_job_finished(name: str, k8s_namespace: str, timeout: int, delay_between_attempts: int) -> bool: Wait until a job is completed.
    - delete_job(name: str, k8s_namespace: str) -> bool: Delete a job and all of its associated data.
    """

    # initial config to setup the kube client
    def __init__(self):
        # loading the config
        try:
            self.kube_config: Optional[Any] = config.load_kube_config()
        except config.ConfigException:
            # No kubeconfig found, try in-cluster config
            try:
                self.kube_config: Optional[Any] = config.load_incluster_config()
            except config.ConfigException:
                log.error("Failed to load both kubeconfig and in-cluster config")
                raise

        self.api = client.CustomObjectsApi()

    def __del__(self):
        self.api = None
        self.kube_config = None

    def submit_job(self, k8s_namespace: str = "default", job: Any = None) -> Any:
        """Submit a Ray job to a given namespace."""
        try:
            rayjob = self.api.create_namespaced_custom_object(
                group=constants.GROUP,
                version=constants.JOB_VERSION,
                plural=constants.JOB_PLURAL,
                body=job,
                namespace=k8s_namespace,
            )
            return rayjob
        except ApiException as e:
            log.error("error submitting ray job: {}".format(e))
            return None

    def get_job_status(
        self,
        name: str,
        k8s_namespace: str = "default",
        timeout: int = 60,
        delay_between_attempts: int = 5,
    ) -> Any:
        """Get a specific Ray job status in a given namespace.

        This method waits until the job has a status field populated by the operator.

        Parameters:
        - name (str): The name of the Ray job custom resource.
        - k8s_namespace (str, optional): The namespace in which to retrieve the Ray job. Defaults to "default".
        - timeout (int, optional): The duration in seconds after which we stop trying to get status. Defaults to 60 seconds.
        - delay_between_attempts (int, optional): The duration in seconds to wait between attempts. Defaults to 5 seconds.

        Returns:
            Any: The custom resource status for the specified Ray job, or None if not found or timeout.
        """
        while timeout > 0:
            try:
                resource: Any = self.api.get_namespaced_custom_object_status(
                    group=constants.GROUP,
                    version=constants.JOB_VERSION,
                    plural=constants.JOB_PLURAL,
                    name=name,
                    namespace=k8s_namespace,
                )
            except ApiException as e:
                if e.status == 404:
                    log.error("rayjob resource is not found. error = {}".format(e))
                    return None
                else:
                    log.error("error fetching custom resource: {}".format(e))
                    return None

            if resource and "status" in resource and resource["status"]:
                return resource["status"]
            else:
                log.info("rayjob {} status not set yet, waiting...".format(name))
                time.sleep(delay_between_attempts)
                timeout -= delay_between_attempts

        log.info("rayjob {} status not set yet, timing out...".format(name))
        return None

    def wait_until_job_finished(
        self,
        name: str,
        k8s_namespace: str = "default",
        timeout: int = 60,
        delay_between_attempts: int = 5,
    ) -> bool:
        """Wait until a Ray job reaches a terminal status.

        This method waits for the job to have a jobStatus field with a terminal value
        (STOPPED, SUCCEEDED, or FAILED).

        Parameters:
        - name (str): The name of the Ray job custom resource.
        - k8s_namespace (str, optional): The namespace in which to retrieve the Ray job. Defaults to "default".
        - timeout (int, optional): The duration in seconds after which we stop trying. Defaults to 60 seconds.
        - delay_between_attempts (int, optional): The duration in seconds to wait between attempts. Defaults to 5 seconds.

        Returns:
            bool: True if the rayjob reaches a terminal status, False otherwise.
        """
        while timeout > 0:
            status = self.get_job_status(
                name, k8s_namespace, timeout, delay_between_attempts
            )

            if status and "jobStatus" in status:
                current_status = status["jobStatus"]
                if current_status in ["", "PENDING"]:
                    log.info("rayjob {} has not started yet".format(name))
                elif current_status == "RUNNING":
                    log.info("rayjob {} is running".format(name))
                elif current_status in TERMINAL_JOB_STATUSES:
                    log.info(
                        "rayjob {} has finished with status {}!".format(
                            name, current_status
                        )
                    )
                    return True
                else:
                    log.info(
                        "rayjob {} has an unknown status: {}".format(
                            name, current_status
                        )
                    )
            else:
                log.info(
                    "rayjob {} jobStatus field not available yet, waiting...".format(
                        name
                    )
                )

            time.sleep(delay_between_attempts)
            timeout -= delay_between_attempts

        log.info(
            "rayjob {} has not reached terminal status before timeout".format(name)
        )
        return False

    def stop_job(self, name: str, k8s_namespace: str = "default") -> bool:
        """Stop a Ray job by setting the suspend field to True.

        This will delete the associated RayCluster and transition the job to 'Suspended' status.
        Only works on jobs in 'Running' or 'Initializing' status.

        Parameters:
        - name (str): The name of the Ray job custom resource.
        - k8s_namespace (str, optional): The namespace in which to stop the Ray job. Defaults to "default".

        Returns:
            bool: True if the job was successfully stopped, False otherwise.
        """
        try:
            # Patch the RayJob to set suspend=true
            patch_body = {
                "spec": {
                    "suspend": True
                }
            }
            self.api.patch_namespaced_custom_object(
                group=constants.GROUP,
                version=constants.JOB_VERSION,
                plural=constants.JOB_PLURAL,
                name=name,
                namespace=k8s_namespace,
                body=patch_body,
            )
            log.info(f"Successfully stopped rayjob {name} in namespace {k8s_namespace}")
            return True
        except ApiException as e:
            if e.status == 404:
                log.error(f"rayjob {name} not found in namespace {k8s_namespace}")
            else:
                log.error(f"error stopping rayjob {name}: {e.reason}")
            return False

    def resubmit_job(self, name: str, k8s_namespace: str = "default") -> bool:
        """Resubmit a suspended Ray job by setting the suspend field to False.

        This will create a new RayCluster and resubmit the job.
        Only works on jobs in 'Suspended' status.

        Parameters:
        - name (str): The name of the Ray job custom resource.
        - k8s_namespace (str, optional): The namespace in which to resubmit the Ray job. Defaults to "default".

        Returns:
            bool: True if the job was successfully resubmitted, False otherwise.
        """
        try:
            # Patch the RayJob to set suspend=false
            patch_body = {
                "spec": {
                    "suspend": False
                }
            }
            self.api.patch_namespaced_custom_object(
                group=constants.GROUP,
                version=constants.JOB_VERSION,
                plural=constants.JOB_PLURAL,
                name=name,
                namespace=k8s_namespace,
                body=patch_body,
            )
            log.info(f"Successfully resubmitted rayjob {name} in namespace {k8s_namespace}")
            return True
        except ApiException as e:
            if e.status == 404:
                log.error(f"rayjob {name} not found in namespace {k8s_namespace}")
            else:
                log.error(f"error resubmitting rayjob {name}: {e.reason}")
            return False

    def delete_job(self, name: str, k8s_namespace: str = "default") -> bool:
        """Delete a Ray job and all of its associated data.

        Parameters:
        - name (str): The name of the Ray job custom resource.
        - k8s_namespace (str, optional): The namespace in which to delete the Ray job. Defaults to "default".

        Returns:
            bool: True if the job was successfully deleted, False otherwise.
        """
        try:
            self.api.delete_namespaced_custom_object(
                group=constants.GROUP,
                version=constants.JOB_VERSION,
                plural=constants.JOB_PLURAL,
                name=name,
                namespace=k8s_namespace,
            )
            log.info(f"Successfully deleted rayjob {name} in namespace {k8s_namespace}")
            return True
        except ApiException as e:
            if e.status == 404:
                log.error(f"rayjob custom resource already deleted. error = {e.reason}")
                return False
            else:
                log.error(f"error deleting the rayjob custom resource: {e.reason}")
                return False
