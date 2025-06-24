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
    - submit_job(entrypoint: str, ...) -> str: Submit and execute a job asynchronously.
    - TODO: stop_job(job_id: str) -> (bool, str): Request a job to exit asynchronously.
    - get_job_status(job_id: str) -> str: Get the most recent status of a job.
    - wait_until_job_finished(job_id: str) -> bool: Wait until a job is completed.
    - get_job_info(job_id: str): Get the latest status and other information associated with a job.
    - TODO: list_jobs() -> List[JobDetails]: List all jobs along with their status and other information.
    - TODO: get_job_logs(job_id: str) -> str: Get all logs produced by a job.
    - TODO: tail_job_logs(job_id: str) -> Iterator[str]: Get an iterator that follows the logs of a job.
    - delete_job(job_id: str) -> (bool, str): Delete a job in a terminal state and all of its associated data.
    """

    # initial config to setup the kube client
    def __init__(self):
        # loading the config
        self.kube_config: Optional[Any] = config.load_kube_config()
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

    def delete_job(self, name: str, k8s_namespace: str = "default") -> Any:
        try:
            resource: Any = self.api.delete_namespaced_custom_object(
                group=constants.GROUP,
                version=constants.JOB_VERSION,
                plural=constants.JOB_PLURAL,
                name=name,
                namespace=k8s_namespace,
            )
            return resource
        except ApiException as e:
            if e.status == 404:
                log.error(f"rayjob custom resource already deleted. error = {e.reason}")
                return False
            else:
                log.error(f"error deleting the rayjob custom resource: {e.reason}")
                return False
