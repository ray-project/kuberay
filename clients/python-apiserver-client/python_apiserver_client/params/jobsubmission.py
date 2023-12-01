import datetime


class RayJobRequest:
    """
    RayJobRequest used to define job to be submitted to a Ray cluster
    It provides APIs to create, stringify and convert to dict.

    Methods:
    - Create RayJobRequest: gets the following parameters:
        entrypoint - required, the command to start a job on the cluster
        submission_id - optional, submission id for the job submission
        runtime_env - optional, yaml string specifying job runtime environment
        metadata - optional, dictionary of the submission metadata
        num_cpus - optional, number of cpus for job execution
        num_gpus - optional, number of gpus for job execution
        resources - optional, dictionary of the resources for job execution
    """
    def __init__(self, entrypoint: str, submission_id: str = None, runtime_env: str = None,
                 metadata: dict[str, str] = None, num_cpu: float = -1., num_gpu: float = -1.,
                 resources: dict[str, str] = None) -> None:
        self.entrypoint = entrypoint
        self.submission_id = submission_id
        self.runtime_env = runtime_env
        self.metadata = metadata
        self.num_cpu = num_cpu
        self.num_gpu = num_gpu
        self.resources = resources

    def to_string(self) -> str:
        val = f"entrypoint = {self.entrypoint}"
        if self.submission_id is not None:
            val += f", submission_id = {self.submission_id}"
        if self.num_cpu > 0:
            val += f", num_cpu = {self.num_cpu}"
        if self.num_gpu > 0:
            val += f", num_gpu = {self.num_gpu}"
        if self.runtime_env is not None:
            val += f", runtime_env = {self.runtime_env}"
        if self.metadata is not None:
            val += f", metadata = {self.metadata}"
        if self.resources is not None:
            val += f", resources = {self.resources}"
        return val

    def to_dict(self) -> dict[str, any]:
        dct = {"entrypoint": self.entrypoint}
        if self.submission_id is not None:
            dct["submissionId"] = self.submission_id
        if self.runtime_env is not None:
            dct["runtimeEnv"] = self.runtime_env
        if self.metadata is not None:
            dct["metadata"] = self.metadata
        if self.num_cpu > 0:
            dct["numCpus"] = self.num_cpu
        if self.num_gpu > 0:
            dct["numGpus"] = self.num_gpu
        if self.resources is not None:
            dct["resources"] = self.resources
        return dct


class RayJobInfo:
    """
    RayJobInfo used to define information about the job in a Ray cluster
    It provides APIs to create and stringify. Its output only data, so we do not need to implement to_dict

    Methods:
    - Create RayJobRequest: gets the following parameters:
        entrypoint - the command to start a job on the cluster
        job_id - job execution id
        submission_id - submission id for the job submission
        runtime_env - job runtime environment
        status - job execution status
        message - status message
        start_time - job start time
        end-time - job ind time
        error_type - type of error
        metadata - optional, dictionary of the submission metadata
    """
    def __init__(self, dst: dict[str, any]) -> None:
        self.entrypoint = dst.get("entrypoint", "")
        self.job_id = dst.get("jobId", "")
        self.submission_id = dst.get("submissionId", "")
        self.status = dst.get("status", "")
        self.message = dst.get("message", None)
        self.start_time = int(dst.get("startTime", "0"))
        self.end_time = int(dst.get("endTime", "0"))
        self.error_type = dst.get("ErrorType", None)
        self.metadata = dst.get("Metadata", None)
        self.runtime_env = dst.get("runtimeEnv", None)

    def to_string(self) -> str:
        val = (f"entrypoint = {self.entrypoint}, job id {self.job_id}, submission id = {self.submission_id},"
               f" status = {self.status}")
        if self.message is not None:
            val += f" message = {self.message}"
        if self.start_time > 0:
            val += (f" start time = "
                    f"{datetime.datetime.fromtimestamp(self.start_time /1.e3).strftime('%Y-%m-%d %H:%M:%S')}")
        if self.end_time > 0:
            val += (f" end time = "
                    f"{datetime.datetime.fromtimestamp(self.end_time / 1e3).strftime('%Y-%m-%d %H:%M:%S')}")
        if self.error_type is not None:
            val += f" error type = {self.error_type}"
        if self.runtime_env is not None:
            val += f" runtime env = {str(self.runtime_env)}"
        if self.metadata is not None:
            val += f" metadata = {str(self.metadata)}"
        return val
