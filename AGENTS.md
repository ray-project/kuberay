# AGENTS.md

Guidance for AI coding agents working in the KubeRay repo. This file focuses on
one error-prone task: keeping the Helm chart versions correct during a release,
including the versions shown in the chart READMEs. For everything else, see
[CONTRIBUTING.md](./CONTRIBUTING.md) and each component's `DEVELOPMENT.md`.

## kuberay is the single source of truth for Helm charts

The charts under `helm-chart/` live here. The
[kuberay-helm](https://github.com/ray-project/kuberay-helm) repo is a pure copy;
charts flow one way (`kuberay/helm-chart` → `kuberay-helm`). Never hand-edit
charts in `kuberay-helm` — that makes the two repos diverge.

## Chart READMEs are generated — edit the template, not the README

Each chart's `README.md` is generated from `README.md.gotmpl` by `helm-docs`
(the `helm-docs-built` pre-commit hook re-checks it). Never edit a chart
`README.md` by hand; edit its `README.md.gotmpl` instead.

## How to update the version shown in the chart READMEs

The `helm install ... --version X.Y.Z` examples in the chart READMEs show a
version. To update them:

1. Bump the chart version in each `helm-chart/<chart>/Chart.yaml`.
2. Regenerate the READMEs:

   ```bash
   make -C helm-chart helm-docs
   ```

3. If a README still shows the old version, its `README.md.gotmpl` hardcodes the
   version. Today `ray-cluster` and `kuberay-apiserver` hardcode it, while
   `kuberay-operator` uses `{{ template "chart.version" . }}` and updates
   automatically. Edit the hardcoded version in that `.gotmpl`, then re-run
   `make -C helm-chart helm-docs`.
4. Validate: `pre-commit run --all-files`.

## Files to change for a version bump

A KubeRay version bump (on a `release-X.Y` branch) should touch only these
files. Do not bump version strings scattered across other docs.

1. `helm-chart/kuberay-operator/Chart.yaml` — chart `version`
2. `helm-chart/kuberay-operator/values.yaml` — image `tag`
3. `helm-chart/kuberay-apiserver/Chart.yaml` — chart `version`
4. `helm-chart/kuberay-apiserver/values.yaml` — image `tag`
5. `helm-chart/ray-cluster/Chart.yaml` — chart `version`
6. `ray-operator/config/default/kustomization.yaml` — image `tag`
7. `ray-operator/controllers/ray/utils/constant.go` — `KUBERAY_VERSION`

Then regenerate (`make -C helm-chart helm-docs`) and validate
(`pre-commit run --all-files`). Full release procedure:
[docs/development/release.md](./docs/development/release.md).
