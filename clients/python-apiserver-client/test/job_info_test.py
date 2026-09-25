from types import SimpleNamespace

import pytest

from python_apiserver_client import KubeRayAPIs
from python_apiserver_client.params import RayJobInfo


@pytest.mark.parametrize("error_key,metadata_key", [("errorType", "metadata"), ("ErrorType", "Metadata")])
def test_job_info_preserves_error_type_and_metadata(error_key, metadata_key):
    info = RayJobInfo({error_key: "JOB_RUNTIME_ENV_SETUP_FAILED", metadata_key: {"owner": "batch"}})

    assert info.error_type == "JOB_RUNTIME_ENV_SETUP_FAILED"
    assert info.metadata == {"owner": "batch"}
    assert "error type = JOB_RUNTIME_ENV_SETUP_FAILED" in info.to_string()
    assert "metadata = {'owner': 'batch'}" in info.to_string()


def test_job_info_without_optional_error_and_metadata():
    info = RayJobInfo({"status": "SUCCEEDED"})

    assert info.error_type is None
    assert info.metadata is None


@pytest.mark.parametrize("list_jobs", [False, True])
def test_job_api_preserves_error_type_and_metadata(monkeypatch, list_jobs):
    payload = {
        "submissionId": "job-1",
        "status": "FAILED",
        "errorType": "JOB_RUNTIME_ENV_SETUP_FAILED",
        "metadata": {"owner": "batch"},
    }
    response = {"submissions": [payload]} if list_jobs else payload
    monkeypatch.setattr(
        "requests.get",
        lambda *args, **kwargs: SimpleNamespace(status_code=200, json=lambda: response),
    )
    client = KubeRayAPIs()
    if list_jobs:
        status, error, infos = client.list_job_info("default", "cluster")
        assert len(infos) == 1
        info = infos[0]
    else:
        status, error, info = client.get_job_info("default", "cluster", "job-1")

    assert status == 200
    assert error is None
    assert info.error_type == "JOB_RUNTIME_ENV_SETUP_FAILED"
    assert info.metadata == {"owner": "batch"}
