"""Credential-free tests; Buildkite HTTP and GitHub CLI calls are mocked."""

import copy
import io
import json
import subprocess
import unittest
from unittest import mock
from urllib.error import HTTPError, URLError
from urllib.parse import parse_qs, urlsplit

import flaky_tracker as tracker

ENV = {
    "BUILDKITE_API_TOKEN": "fake-buildkite-token",
    "BUILDKITE_ORGANIZATION_SLUG": "ray-project",
    "BUILDKITE_TEST_ENGINE_SUITE_SLUG": "kuberay-e2e",
    "GH_TOKEN": "fake-github-token",
    "GITHUB_REPOSITORY": "ray-project/kuberay",
    "FLAKY_TRACKER_BRANCH": "master",
}


def candidate(**overrides):
    return {
        "id": "test-1",
        "name": "TestRayService",
        "scope": "test/e2e",
        "labels": ["flaky"],
        "reliability": 0.8,
        "executions_count": 10,
        "executions_count_by_result": {"failed": 2},
        **overrides,
    }


class Harness:
    def __init__(
        self, *, tests=None, pages=None, issues=None, apply=False, latest=None, write_error=None
    ):
        self.pages = (
            pages if pages is not None else [(tests if tests is not None else [candidate()], "")]
        )
        self.issues = copy.deepcopy(issues or [])
        self.apply, self.latest, self.write_error = apply, latest, write_error
        self.requests, self.reads, self.writes, self.logs = [], [], [], []

    def get_page(self, url, token):
        self.requests.append((url, token))
        page = copy.deepcopy(self.pages[self.page])
        self.page += 1
        return page

    def github(self, endpoint, *, method="GET", payload=None, paginate=False):
        if method == "GET":
            self.reads.append((endpoint, paginate))
            if paginate:
                return copy.deepcopy(self.issues)
            number = int(endpoint.rsplit("/", 1)[1])
            return copy.deepcopy(
                self.latest or next(issue for issue in self.issues if issue["number"] == number)
            )
        self.writes.append({"method": method, "endpoint": endpoint, **payload})
        if self.write_error:
            raise self.write_error
        if method == "POST":
            issue = {"number": len(self.issues) + 1, "state": "open", **copy.deepcopy(payload)}
            self.issues.append(issue)
        else:
            number = int(endpoint.rsplit("/", 1)[1])
            issue = next(issue for issue in self.issues if issue["number"] == number)
            issue.update(payload)
        return copy.deepcopy(issue)

    def run(self, **environment):
        self.page = 0
        with (
            mock.patch.object(tracker, "buildkite_get", side_effect=self.get_page),
            mock.patch.object(tracker, "github_api", side_effect=self.github),
            mock.patch("sys.stdout", new_callable=io.StringIO) as output,
        ):
            try:
                tracker.track_flaky_tests(
                    {**ENV, "FLAKY_TRACKER_APPLY": str(self.apply).lower(), **environment}
                )
            finally:
                self.logs = output.getvalue().splitlines()


class TrackerTests(unittest.TestCase):
    def test_dry_run_queries_flaky_tests_without_writing(self):
        h = Harness()
        h.run()
        url, token = h.requests[0]
        self.assertEqual(
            urlsplit(url).path, "/v2/analytics/organizations/ray-project/suites/kuberay-e2e/tests"
        )
        self.assertEqual(
            parse_qs(urlsplit(url).query),
            {
                "labels": ["flaky"],
                "branch": ["master"],
                "per_page": ["100"],
            },
        )
        self.assertEqual(token, ENV["BUILDKITE_API_TOKEN"])
        self.assertEqual(h.writes, [])
        self.assertTrue(h.reads[0][1])
        self.assertIn("state=open", h.reads[0][0])
        self.assertTrue(any('"action": "create"' in line for line in h.logs))
        for name in ("BUILDKITE_API_TOKEN", "GH_TOKEN"):
            self.assertNotIn(ENV[name], "\n".join(h.logs))

    def test_empty_response_does_not_query_github(self):
        h = Harness(tests=[], apply=True)
        h.run()
        self.assertEqual(h.reads, [])
        self.assertEqual(h.writes, [])

    def test_buildkite_pagination_deduplicates_test_ids(self):
        h = Harness(
            apply=True,
            pages=[
                ([candidate()], '<?page=2>; rel="next", <?page=4>; rel="last"'),
                ([candidate(), candidate(id="test-2")], ""),
            ],
        )
        h.run()
        self.assertEqual(len(h.requests), 2)
        self.assertEqual(urlsplit(h.requests[1][0]).query, "page=2")
        self.assertEqual(len(h.writes), 2)

    def test_untrusted_links_are_rejected_before_sending_credentials(self):
        for link in (
            "https://attacker.test/tests",
            "http://api.buildkite.com/v2/analytics/organizations/ray-project/suites/kuberay-e2e/tests",
            "https://api.buildkite.com/v2/analytics/organizations/other/suites/other/tests",
            "https://user@api.buildkite.com/v2/analytics/organizations/ray-project/suites/kuberay-e2e/tests",
            "?page=2#fragment",
        ):
            with self.subTest(link=link):
                h = Harness(apply=True, pages=[([candidate()], f'<{link}>; rel="next"')])
                with self.assertRaisesRegex(RuntimeError, "pagination left"):
                    h.run()
                self.assertEqual(len(h.requests), 1)
                self.assertEqual(h.writes, [])

    def test_repeated_or_excessive_pagination_fails_before_writes(self):
        for pages, expected in (
            ([([], '<?page=2>; rel="next"')] * 2, 2),
            ([([], f'<?page={i + 2}>; rel="next"') for i in range(100)], 100),
        ):
            h = Harness(apply=True, pages=pages)
            with self.assertRaisesRegex(RuntimeError, "pagination repeated or exceeded"):
                h.run()
            self.assertEqual(len(h.requests), expected)
            self.assertEqual(h.writes, [])

    def test_invalid_test_records_fail_before_writes(self):
        for data in (
            {},
            [None],
            [candidate(id="")],
            [candidate(labels=[])],
            [candidate(), candidate(name="conflicting record")],
        ):
            with self.subTest(data=data):
                h = Harness(apply=True, pages=[(data, "")])
                with self.assertRaisesRegex(RuntimeError, "Buildkite"):
                    h.run()
                self.assertEqual(h.writes, [])

    def test_missing_configuration_fails_before_requests(self):
        for name in ENV:
            with self.subTest(name=name):
                h = Harness()
                with self.assertRaisesRegex(RuntimeError, "Missing"):
                    h.run(**{name: "  "})
                self.assertEqual(h.requests, [])
                self.assertEqual(h.reads, [])

    def test_creation_rerun_and_rename_preserve_human_notes(self):
        h = Harness(apply=True)
        h.run()
        self.assertEqual(h.writes[0]["method"], "POST")
        self.assertEqual(h.writes[0]["labels"], tracker.LABELS)
        h.issues[0]["body"] += "\nHuman investigation notes"
        h.writes.clear()
        h.run()
        self.assertEqual(h.writes, [])
        updated = Harness(
            apply=True, issues=h.issues, tests=[candidate(name="RenamedTest", reliability=0.9)]
        )
        updated.run()
        self.assertEqual(len(updated.writes), 1)
        write = updated.writes[0]
        self.assertEqual(write["method"], "PATCH")
        self.assertTrue(write["body"].endswith("Human investigation notes"))
        self.assertIn("90.0%", write["body"])
        self.assertNotIn("title", write)
        self.assertNotIn("labels", write)

    def test_fingerprints_match_previous_javascript_including_unicode(self):
        for identifier, fingerprint in (
            ("test-1", "ea7d7867b9cc24012689f7e06952beffe2e135acee10bb1672fcef6a718af0b9"),
            ("测试-1", "6a7c285eba2ddb54f46871096292de5b31e368a32cdfff6702d9fbd3478530fe"),
        ):
            h = Harness(apply=True, tests=[candidate(id=identifier)])
            h.run()
            self.assertTrue(
                h.writes[0]["body"].startswith(
                    "<!-- kuberay-flaky-tracker:v1:" + fingerprint + " -->"
                )
            )

    def test_buildkite_classification_has_no_custom_threshold(self):
        h = Harness(apply=True, tests=[candidate(executions_count_by_result={"failed": 0})])
        h.run()
        self.assertEqual(len(h.writes), 1)

    def test_matching_pr_is_not_an_existing_issue(self):
        seed = Harness(apply=True)
        seed.run()
        h = Harness(apply=True, issues=[{**seed.issues[0], "pull_request": {}}])
        h.run()
        self.assertEqual(h.writes[0]["method"], "POST")

    def test_duplicate_issues_or_damaged_markers_fail_before_writes(self):
        seed = Harness(apply=True)
        seed.run()
        issue = seed.issues[0]
        for issues in (
            [issue, {**issue, "number": 2}],
            [{**issue, "body": issue["body"].replace(":report:end", ":report:removed")}],
            [{**issue, "body": issue["body"] + issue["body"]}],
        ):
            h = Harness(apply=True, issues=issues)
            with self.assertRaisesRegex(
                RuntimeError, "Multiple open issues|invalid bot report markers"
            ):
                h.run()
            self.assertEqual(h.writes, [])

    def test_updates_preserve_recent_notes_and_do_not_reopen_issues(self):
        seed = Harness(apply=True)
        seed.run()
        for state in ("open", "closed"):
            with self.subTest(state=state):
                h = Harness(
                    apply=True,
                    issues=seed.issues,
                    tests=[candidate(reliability=0.5)],
                    latest={
                        **seed.issues[0],
                        "state": state,
                        "body": seed.issues[0]["body"] + "\nRecent human note",
                    },
                )
                h.run()
                self.assertEqual(len(h.writes), 1 if state == "open" else 0)
                if state == "open":
                    self.assertTrue(h.writes[0]["body"].endswith("Recent human note"))

    def test_new_issues_are_batched_and_failed_writes_are_not_retried(self):
        h = Harness(apply=True, tests=[candidate(id=str(i)) for i in range(11)])
        h.run()
        self.assertEqual(len(h.writes), 10)
        h.writes.clear()
        h.run()
        self.assertEqual(len(h.writes), 1)
        failed = Harness(apply=True, write_error=RuntimeError("write failed"))
        with self.assertRaisesRegex(RuntimeError, "write failed"):
            failed.run()
        self.assertEqual(len(failed.writes), 1)

    def test_api_text_cannot_inject_mentions_html_or_report_links(self):
        h = Harness(
            apply=True,
            tests=[candidate(name="@team <script>\n::error::bad", web_url="https://attacker.test")],
        )
        h.run()
        write = h.writes[0]
        self.assertNotIn("@", write["title"])
        self.assertNotIn("<script>", write["body"])
        self.assertNotIn("https://attacker.test", write["body"])
        self.assertIn("&#64;team &lt;script&gt;", write["body"])
        self.assertNotIn("\n::error::", "\n".join(h.logs))

    def test_unknown_reliability_and_execution_counts_are_safe(self):
        for reliability in (None, True, float("nan"), float("inf")):
            h = Harness(
                apply=True,
                tests=[candidate(reliability=reliability, executions_count_by_result=None)],
            )
            h.run()
            self.assertIn("- Reliability: unknown", h.writes[0]["body"])
            self.assertIn("failed: <code>unknown</code>", h.writes[0]["body"])


class TransportTests(unittest.TestCase):
    @mock.patch.object(tracker, "build_opener")
    def test_buildkite_request_uses_version_timeout_and_no_redirects(self, opener):
        response = opener.return_value.open.return_value.__enter__.return_value
        response.read.return_value = b"[]"
        response.headers.get.return_value = '<?page=2>; rel="next"'
        self.assertEqual(
            tracker.buildkite_get("https://api.buildkite.com/tests", "fake-token"),
            ([], '<?page=2>; rel="next"'),
        )
        handler = opener.call_args.args[0]
        self.assertIsInstance(handler, tracker.NoRedirect)
        self.assertIsNone(
            handler.redirect_request(None, None, 302, None, None, "https://attacker.test")
        )
        request = opener.return_value.open.call_args.args[0]
        self.assertEqual(request.get_header("Authorization"), "Bearer fake-token")
        self.assertEqual(request.get_header("Buildkite-version"), "2026-08-01")
        self.assertEqual(opener.return_value.open.call_args.kwargs["timeout"], 30)

    @mock.patch.object(tracker, "build_opener")
    def test_http_errors_do_not_expose_raw_bodies(self, opener):
        for status in (302, 401, 403, 404, 429, 500):
            with self.subTest(status=status):
                opener.return_value.open.side_effect = HTTPError(
                    "https://api.buildkite.com/tests", status, "fake-token raw error", {}, None
                )
                with self.assertRaisesRegex(RuntimeError, f"HTTP {status}") as error:
                    tracker.buildkite_get("https://api.buildkite.com/tests", "fake-token")
                self.assertNotIn("fake-token", str(error.exception))

    @mock.patch.object(tracker, "build_opener")
    def test_transport_and_json_errors_are_sanitized(self, opener):
        for failure in (URLError("fake-token"), TimeoutError("fake-token")):
            opener.return_value.open.side_effect = failure
            with self.assertRaisesRegex(RuntimeError, "transport or JSON") as error:
                tracker.buildkite_get("https://api.buildkite.com/tests", "fake-token")
            self.assertNotIn("fake-token", str(error.exception))
        opener.return_value.open.side_effect = None
        response = opener.return_value.open.return_value.__enter__.return_value
        for data in (b"not JSON fake-token", b"\xff"):
            response.read.return_value = data
            with self.assertRaisesRegex(RuntimeError, "transport or JSON"):
                tracker.buildkite_get("https://api.buildkite.com/tests", "fake-token")

    @mock.patch.object(tracker.subprocess, "run")
    def test_github_cli_owns_pagination_and_flattens_pages(self, run):
        run.return_value = mock.Mock(returncode=0, stdout='[[{"number":1}],[{"number":2}]]')
        self.assertEqual(
            tracker.github_api("repos/ray-project/kuberay/issues", paginate=True),
            [{"number": 1}, {"number": 2}],
        )
        self.assertEqual(
            run.call_args.args[0],
            [
                "gh",
                "api",
                "--method",
                "GET",
                "repos/ray-project/kuberay/issues",
                "--paginate",
                "--slurp",
            ],
        )
        self.assertTrue(run.call_args.kwargs["capture_output"])
        self.assertEqual(run.call_args.kwargs["timeout"], 60)

    @mock.patch.object(tracker.subprocess, "run")
    def test_github_mutations_send_json_on_stdin_without_shell_or_retry(self, run):
        run.return_value = mock.Mock(returncode=0, stdout='{"number":1}')
        payload = {"body": 'Human text with "quotes"\n@name'}
        tracker.github_api("repos/ray-project/kuberay/issues/1", method="PATCH", payload=payload)
        self.assertEqual(json.loads(run.call_args.kwargs["input"]), payload)
        self.assertEqual(run.call_args.args[0][-2:], ["--input", "-"])
        self.assertNotIn("shell", run.call_args.kwargs)
        self.assertEqual(run.call_count, 1)

    @mock.patch.object(tracker.subprocess, "run")
    def test_github_cli_failures_are_sanitized_and_not_retried(self, run):
        for failure in (
            OSError("fake-token"),
            subprocess.TimeoutExpired("gh", 60, output="fake-token"),
        ):
            run.reset_mock()
            run.side_effect = failure
            with self.assertRaisesRegex(RuntimeError, "GitHub CLI") as error:
                tracker.github_api("repos/ray-project/kuberay/issues")
            self.assertNotIn("fake-token", str(error.exception))
            self.assertEqual(run.call_count, 1)
        run.side_effect = None
        run.return_value = mock.Mock(returncode=1, stdout="fake-token", stderr="fake-token")
        with self.assertRaisesRegex(RuntimeError, "GitHub API request failed") as error:
            tracker.github_api("repos/ray-project/kuberay/issues")
        self.assertNotIn("fake-token", str(error.exception))

    @mock.patch.object(tracker.subprocess, "run")
    def test_invalid_github_json_and_pagination_shapes_fail(self, run):
        for stdout in ("invalid fake-token", "{}", "[{}]"):
            run.return_value = mock.Mock(returncode=0, stdout=stdout)
            with self.assertRaisesRegex(RuntimeError, "JSON|paginated arrays"):
                tracker.github_api("repos/ray-project/kuberay/issues", paginate=True)


if __name__ == "__main__":
    unittest.main()
