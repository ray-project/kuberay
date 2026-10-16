import unittest
from unittest.mock import Mock, patch

from python_client.kuberay_job_api import RayjobApi


class TestJobTimeouts(unittest.TestCase):
    def setUp(self):
        self.elapsed = 0
        self.sleeps = []
        self.api = RayjobApi.__new__(RayjobApi)
        self.api.api = Mock()
        self.api.api.get_namespaced_custom_object_status.return_value = {}
        self.monotonic = patch(
            "python_client.kuberay_job_api.time.monotonic",
            side_effect=lambda: self.elapsed,
        )
        self.sleep = patch(
            "python_client.kuberay_job_api.time.sleep", side_effect=self.advance
        )
        self.monotonic.start()
        self.sleep.start()
        self.addCleanup(self.monotonic.stop)
        self.addCleanup(self.sleep.stop)

    def advance(self, seconds):
        self.sleeps.append(seconds)
        self.elapsed += seconds

    def test_missing_status_respects_timeout(self):
        for method, expected in [
            (self.api.get_job_status, None),
            (self.api.wait_until_job_running, False),
            (self.api.wait_until_job_finished, False),
        ]:
            with self.subTest(method=method.__name__):
                self.elapsed = 0
                self.sleeps.clear()
                self.assertEqual(
                    method("job", timeout=12, delay_between_attempts=5), expected
                )
                self.assertEqual(self.elapsed, 12)

    def test_existing_status_respects_timeout(self):
        self.api.api.get_namespaced_custom_object_status.return_value = {
            "status": {"jobDeploymentStatus": "Initializing"}
        }
        for method in [
            self.api.wait_until_job_running,
            self.api.wait_until_job_finished,
        ]:
            with self.subTest(method=method.__name__):
                self.elapsed = 0
                self.assertFalse(method("job", timeout=12, delay_between_attempts=5))
                self.assertEqual(self.elapsed, 12)

    def test_status_wait_consumes_outer_timeout(self):
        for method in [
            self.api.wait_until_job_running,
            self.api.wait_until_job_finished,
        ]:
            with self.subTest(method=method.__name__):
                self.elapsed = 0

                def status_after_first_poll(**kwargs):
                    if self.elapsed < 5:
                        return {}
                    return {"status": {"jobDeploymentStatus": "Initializing"}}

                self.api.api.get_namespaced_custom_object_status.side_effect = (
                    status_after_first_poll
                )
                self.assertFalse(method("job", timeout=12, delay_between_attempts=5))
                self.assertEqual(self.elapsed, 12)

    def test_terminal_status_before_timeout(self):
        for method, status in [
            (self.api.wait_until_job_running, "Running"),
            (self.api.wait_until_job_finished, "Complete"),
        ]:
            with self.subTest(method=method.__name__):
                self.elapsed = 0

                def completed_after_first_poll(**kwargs):
                    if self.elapsed < 5:
                        return {}
                    return {"status": {"jobDeploymentStatus": status}}

                self.api.api.get_namespaced_custom_object_status.side_effect = (
                    completed_after_first_poll
                )
                self.assertTrue(method("job", timeout=12, delay_between_attempts=5))
                self.assertEqual(self.elapsed, 5)

    def test_api_call_time_consumes_timeout(self):
        def slow_status(**kwargs):
            self.elapsed += 6
            return {}

        self.api.api.get_namespaced_custom_object_status.side_effect = slow_status
        self.assertIsNone(
            self.api.get_job_status("job", timeout=12, delay_between_attempts=5)
        )
        self.assertEqual(sum(self.sleeps), 5)
        self.assertEqual(self.api.api.get_namespaced_custom_object_status.call_count, 2)


if __name__ == "__main__":
    unittest.main()
