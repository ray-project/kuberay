"""Track Buildkite's flaky tests in GitHub issues; writes are opt-in."""

import hashlib
import html
import json
import math
import os
import re
import subprocess
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode, urljoin, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener

START = "<!-- kuberay-flaky-tracker:report:start -->"
END = "<!-- kuberay-flaky-tracker:report:end -->"
LABELS = ["flaky-tracker", "bug", "ci", "flaky", "P0", "triage"]


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def buildkite_get(url, token):
    request = Request(
        url,
        headers={
            "Authorization": "Bearer " + token,
            "Buildkite-Version": "2026-08-01",
        },
    )
    try:
        with build_opener(NoRedirect()).open(request, timeout=30) as response:
            return json.load(response), response.headers.get("Link", "")
    except HTTPError as error:
        error.close()
        raise RuntimeError(f"Buildkite Tests API returned HTTP {error.code}") from None
    except (URLError, OSError, ValueError):
        raise RuntimeError(
            "Buildkite transport or JSON error; check the GitHub Actions run."
        ) from None


def github_api(endpoint, *, method="GET", payload=None, paginate=False):
    # gh follows GitHub's Link aliases, including /repositories/{id}/... .
    command = ["gh", "api", "--method", method, endpoint]
    if paginate:
        command.extend(["--paginate", "--slurp"])
    if payload is not None:
        command.extend(["--input", "-"])
    try:
        result = subprocess.run(
            command,
            input=json.dumps(payload) if payload is not None else None,
            capture_output=True,
            text=True,
            timeout=60,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        raise RuntimeError("GitHub CLI could not complete the API request.") from None
    if result.returncode:
        raise RuntimeError("GitHub API request failed; check token and issue permissions.")
    try:
        data = json.loads(result.stdout)
    except ValueError:
        raise RuntimeError("GitHub CLI did not return valid JSON.") from None
    if paginate:
        if not isinstance(data, list) or any(not isinstance(page, list) for page in data):
            raise RuntimeError("GitHub issues API did not return paginated arrays.")
        return [issue for page in data for issue in page]
    return data


def code(value):
    escaped = html.escape(str(value) if value is not None else "unknown", quote=False)
    return "<code>" + escaped.replace('"', "&quot;").replace("@", "&#64;") + "</code>"


def merge_report(body, report, marker):
    start, end = body.find(START), body.find(END)
    identities = re.findall(r"<!-- kuberay-flaky-tracker:v1:[0-9a-f]{64} -->", body)
    if (
        start < 0
        or end < start
        or body.count(START) != 1
        or body.count(END) != 1
        or identities != [marker]
        or start < body.find(marker) < end
    ):
        raise RuntimeError(
            "Existing issue has invalid bot report markers; review it before rerunning."
        )
    return body[:start] + report + body[end + len(END) :]


def flaky_tests(token, organization, suite, branch):
    endpoint = (
        "https://api.buildkite.com/v2/analytics/organizations/"
        + quote(organization, safe="")
        + "/suites/"
        + quote(suite, safe="")
        + "/tests"
    )
    url = endpoint + "?" + urlencode({"labels": "flaky", "branch": branch, "per_page": 100})
    collection = urlsplit(endpoint)
    seen, tests = set(), {}
    while url:
        if url in seen or len(seen) >= 100:
            raise RuntimeError("Buildkite pagination repeated or exceeded 100 pages.")
        seen.add(url)
        page, links = buildkite_get(url, token)
        if not isinstance(page, list):
            raise RuntimeError("Buildkite Tests API did not return an array.")
        for item in page:
            if (
                not isinstance(item, dict)
                or not isinstance(item.get("id"), str)
                or not item["id"]
                or not isinstance(item.get("name"), str)
                or not item["name"]
                or not isinstance(item.get("labels"), list)
                or "flaky" not in item["labels"]
            ):
                raise RuntimeError("Buildkite returned an invalid flaky test.")
            if item["id"] in tests and tests[item["id"]] != item:
                raise RuntimeError("Buildkite returned conflicting records for a test ID.")
            tests[item["id"]] = item
        next_link = next(
            (
                link
                for link, relation in re.findall(r'<([^>]+)>\s*;\s*rel="([^"]+)"', links)
                if "next" in relation.split()
            ),
            None,
        )
        url = urljoin(url, next_link) if next_link else None
        if url:
            target = urlsplit(url)
            if (
                target.scheme != collection.scheme
                or target.netloc != collection.netloc
                or target.path != collection.path
                or target.username
                or target.password
                or target.fragment
            ):
                raise RuntimeError("Buildkite pagination left the requested collection.")
    return list(tests.values())


def track_flaky_tests(env=None):
    env = os.environ if env is None else env
    required = (
        "BUILDKITE_API_TOKEN",
        "BUILDKITE_ORGANIZATION_SLUG",
        "BUILDKITE_TEST_ENGINE_SUITE_SLUG",
        "GH_TOKEN",
        "GITHUB_REPOSITORY",
        "FLAKY_TRACKER_BRANCH",
    )
    if any(not env.get(name, "").strip() for name in required):
        raise RuntimeError("Missing API token or target in the GitHub Actions workflow.")
    token, organization, suite, _, repository, branch = (env[name].strip() for name in required)
    apply = env.get("FLAKY_TRACKER_APPLY") == "true"
    tests = flaky_tests(token, organization, suite, branch)
    print(f"Buildkite Tests API query succeeded: {len(tests)} flaky test(s).")
    if not tests:
        return

    endpoint = "repos/" + repository + "/issues"
    issues = [
        issue
        for issue in github_api(
            endpoint + "?state=open&sort=created&direction=asc&per_page=100", paginate=True
        )
        if "pull_request" not in issue
    ]
    plans = []
    for item in tests:
        # Match the former JavaScript JSON.stringify fingerprint, including Unicode.
        identity = hashlib.sha256(
            json.dumps(
                ["v1", organization, suite, item["id"]], ensure_ascii=False, separators=(",", ":")
            ).encode("utf-8")
        ).hexdigest()
        marker = "<!-- kuberay-flaky-tracker:v1:" + identity + " -->"
        matches = [issue for issue in issues if marker in (issue.get("body") or "")]
        if len(matches) > 1:
            raise RuntimeError("Multiple open issues match one flaky test.")
        test_url = (
            "https://buildkite.com/organizations/"
            + quote(organization, safe="")
            + "/analytics/suites/"
            + quote(suite, safe="")
            + "/tests/"
            + quote(item["id"], safe="")
        )
        reliability = item.get("reliability")
        reliability = (
            f"{reliability * 100:.1f}%"
            if type(reliability) in (int, float) and math.isfinite(reliability)
            else "unknown"
        )
        report = "\n".join(
            [
                START,
                "## Latest Test Engine report",
                "",
                "Buildkite identifies this test as flaky; this bot does not classify tests.",
                "",
                "- Test: " + code((item.get("scope") or "") + "::" + item["name"]),
                "- Location: " + code(item.get("location")),
                "- Branch: " + code(branch),
                "- Reliability: " + reliability,
                "- Executions: "
                + code(item.get("executions_count"))
                + "; failed: "
                + code((item.get("executions_count_by_result") or {}).get("failed")),
                "- [View test in Buildkite Test Engine](" + test_url + ")",
                "",
                "Bot-managed section; add investigation notes outside it.",
                END,
            ]
        )
        issue = matches[0] if matches else None
        body = (
            merge_report(issue["body"], report, marker)
            if issue
            else marker + "\n\n" + report + "\n"
        )
        plans.append(
            {
                "issue": issue,
                "report": report,
                "body": body,
                "marker": marker,
                "title": ("[Flaky test] " + re.sub(r"\s+", " ", item["name"].replace("@", "＠")))[
                    :200
                ],
                "action": "create" if not issue else "noop" if body == issue["body"] else "update",
            }
        )
    print(
        ("Apply plan" if apply else "Dry run")
        + ": "
        + json.dumps(
            {
                action: sum(plan["action"] == action for plan in plans)
                for action in ("create", "update", "noop")
            }
        )
    )
    print(
        json.dumps(
            [
                {
                    "action": plan["action"],
                    "title": plan["title"],
                    "issue": plan["issue"]["number"] if plan["issue"] else None,
                }
                for plan in plans
            ]
        )
    )
    if not apply:
        return

    created = 0
    for plan in plans:
        # Never retry mutations: a lost response may mean the write already succeeded.
        if plan["action"] == "create" and created < 10:
            github_api(
                endpoint,
                method="POST",
                payload={
                    "title": plan["title"],
                    "body": plan["body"],
                    "labels": LABELS,
                },
            )
            created += 1
        elif plan["action"] == "update":
            issue_endpoint = endpoint + "/" + str(plan["issue"]["number"])
            latest = github_api(issue_endpoint)
            if latest["state"] != "open":
                continue
            body = merge_report(latest.get("body") or "", plan["report"], plan["marker"])
            if body != latest.get("body"):
                github_api(issue_endpoint, method="PATCH", payload={"body": body})


if __name__ == "__main__":
    try:
        track_flaky_tests()
    except RuntimeError as error:
        print(f"::error::{error}", file=sys.stderr)
        sys.exit(1)
