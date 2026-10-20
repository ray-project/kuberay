from unittest.mock import Mock

import pytest

from python_apiserver_client import KubeRayAPIs
from python_apiserver_client import kuberay_apis


class FakeClock:
    def __init__(self):
        self.now = 0
        self.sleeps = []

    def monotonic(self):
        return self.now

    def sleep(self, seconds):
        self.sleeps.append(seconds)
        self.now += seconds


@pytest.fixture
def clock(monkeypatch):
    clock = FakeClock()
    monkeypatch.setattr(kuberay_apis.time, "monotonic", clock.monotonic)
    monkeypatch.setattr(kuberay_apis.time, "sleep", clock.sleep)
    return clock


def test_wait_cluster_ready_counts_status_request_time(clock):
    apis = KubeRayAPIs(wait_interval=2)

    def get_status(**kwargs):
        clock.now += 6
        return 200, None, "creating"

    apis.get_cluster_status = Mock(side_effect=get_status)

    status, error = apis.wait_cluster_ready("default", "test", wait=5)

    assert status == 408
    assert "Timed out" in error
    assert apis.get_cluster_status.call_count == 1
    assert clock.sleeps == []


def test_wait_cluster_ready_caps_poll_interval_to_remaining_time(clock):
    apis = KubeRayAPIs(wait_interval=10)
    apis.get_cluster_status = Mock(return_value=(200, None, "creating"))

    status, error = apis.wait_cluster_ready("default", "test", wait=5)

    assert status == 408
    assert "Timed out" in error
    assert clock.now == 5
    assert clock.sleeps == [5]
    assert apis.get_cluster_status.call_count == 1


def test_wait_cluster_ready_succeeds_before_timeout(clock):
    apis = KubeRayAPIs(wait_interval=2)
    apis.get_cluster_status = Mock(side_effect=[
        (200, None, "creating"),
        (200, None, "ready"),
    ])

    assert apis.wait_cluster_ready("default", "test", wait=5) == (200, None)
    assert clock.sleeps == [2]


def test_wait_cluster_ready_returns_status_errors(clock):
    apis = KubeRayAPIs()
    apis.get_cluster_status = Mock(return_value=(503, "unavailable", None))

    assert apis.wait_cluster_ready("default", "test", wait=5) == (503, "unavailable")
    assert clock.sleeps == []


def test_wait_cluster_ready_accepts_inflight_ready_response(clock):
    apis = KubeRayAPIs()

    def get_status(**kwargs):
        clock.now += 6
        return 200, None, "ready"

    apis.get_cluster_status = Mock(side_effect=get_status)

    assert apis.wait_cluster_ready("default", "test", wait=5) == (200, None)
    assert apis.get_cluster_status.call_count == 1
    assert clock.sleeps == []


@pytest.mark.parametrize("wait", [-1, 0])
def test_wait_cluster_ready_preserves_unlimited_wait(clock, wait):
    apis = KubeRayAPIs(wait_interval=2)
    apis.get_cluster_status = Mock(side_effect=[
        (200, None, "creating"),
        (200, None, "ready"),
    ])

    assert apis.wait_cluster_ready("default", "test", wait=wait) == (200, None)
    assert clock.sleeps == [2]
