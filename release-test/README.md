# Release test

Validates a KubeRay release candidate on a local kind cluster against the docs on docs.ray.io, the samples in
`ray-operator/config/samples/`, the Helm charts and the Go modules. It replaces the release validation
spreadsheet.

An LLM only plans (turns a doc page into shell steps, once per page) and triages (explains a failure). Running
the steps and deciding pass or fail is Go, so a passing Item costs no tokens.

| File | Written by | Committed |
|---|---|---|
| `items.yaml` | humans: what to validate | yes |
| `manifest.yaml` | the `release-test-plan` skill, reviewed like code: how to validate each Item | yes |
| `results/<tag>/` | `go run ./release-test run` | no |

The skills are in `.claude/skills/`: `release-test` drives a whole run, `release-test-plan` writes manifest
units, `release-test-run` triages one failure.

## Running a release

You need Go, kind, kubectl, helm, git, [yq v4](https://github.com/mikefarah/yq),
[uv](https://github.com/astral-sh/uv) and, for plan and triage, the `claude` CLI. Run the commands from the
repository root; `--help` lists a command's flags.

| Command | Does |
|---|---|
| `go run ./release-test stale --kuberay-version <tag> [ID...]` | lists Items whose unit is missing or whose source changed since it was planned |
| `go run ./release-test lint` | checks `manifest.yaml` |
| `go run ./release-test run --kuberay-version <tag> [ID...]` | runs the manifest, writes the results, triages failures |
| `go run ./release-test summary --kuberay-version <tag>` | rebuilds `results/<tag>/SUMMARY.md` |

1. Run `stale`, plan what it lists with `/release-test-plan <tag> <id>...`, review
   `git diff release-test/manifest.yaml` and commit it.
2. Run the manifest. It resumes where it stopped; pass IDs to re-run single Items. A unit whose source changed
   since it was planned is not run but reported as stale.

   ```sh
   kind create cluster --name release-test
   go run ./release-test run --kuberay-version v1.8.0-rc.0
   ```

3. Read `results/<tag>/SUMMARY.md`, then `results/<tag>/<id>/report.md` for each failure.

`/release-test v1.8.0-rc.0` in Claude Code does all three.

The runner triages each failure by calling `claude` with your Claude Code login; `--triage=false` skips it.
Steps never see `ANTHROPIC_*` or `CLAUDE_CODE_*` variables, and the triage call has no shell or network and
can edit only the unit's `report.md`.

## items.yaml

```yaml
- id: raycluster-quick-start           # stable slug; the unit id and results/<tag>/<id>/
  source: https://docs.ray.io/en/master/kuberay/getting-started/raycluster-quick-start.html
- id: ray-cluster-sample
  source: ray-operator/config/samples/ray-cluster.sample.yaml
- id: ray-cluster-tpu-v6e-singlehost
  source: ray-operator/config/samples/ray-cluster.tpu-v6e-singlehost.yaml
  requires: [tpu, gke]                 # not runnable on kind: listed under Manual, never planned or run
- id: go-get-ray-operator
  procedure: |                         # for Items that are neither a page nor a sample
    In an empty Go module, go get github.com/ray-project/kuberay/ray-operator@$KUBERAY_VERSION succeeds ...
```

## manifest.yaml

Each step is a `run` command and a `check` command; the step passes when both exit 0. Commands use
`$KUBERAY_VERSION` and `$CHART_VERSION`, never a fixed version. `source_sha` is the git blob sha of what
the unit was planned from. For a docs page that is its code blocks, without comment lines and with
KubeRay version references normalized: a prose edit or the version bump the docs get after each release
does not make the unit stale, a changed command or expected output does. For a sample it is the whole
file as it is at the tag, because that is the copy the steps apply. The format is in
`.claude/skills/release-test-plan/SKILL.md`.

## Results

```text
results/<tag>/
├── SUMMARY.md
└── <item-id>/
    ├── report.md        front matter (item, status, version, failed_step, triage, brief) + step table
    ├── steps/NN.log     the command, the check, the output, the exit code
    ├── evidence/        on failure: pods, events, Ray resources, describe, Pod logs
    └── triage.json      on failure: the triage call's output
```

`status` is `pass`, `fail` or `stale`. `triage` is one of `plan-bug` (the manifest is wrong), `doc-bug`,
`sample-bug`, `product-bug`, `env` (kind cannot do it) or `flaky`. Nothing files GitHub issues; a human
decides.
