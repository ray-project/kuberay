---
name: bump-helm-charts
description: Bump KubeRay Helm chart versions for a release. Use when performing a KubeRay version bump on a release-X.Y branch — edits the 7 source-of-truth version files (Chart.yaml, values.yaml, kustomization.yaml, constant.go) and the 9 hardcoded version spots in 3 README.md.gotmpl templates, then regenerates the chart READMEs.
---

# Bump KubeRay Helm chart versions

Use when performing a KubeRay version bump on a `release-X.Y` branch.

## Source-of-truth files (7 files)

Edit these directly with the new version `X.Y.Z`:

1. `helm-chart/kuberay-operator/Chart.yaml` — chart `version`
2. `helm-chart/kuberay-operator/values.yaml` — image `tag`
3. `helm-chart/kuberay-apiserver/Chart.yaml` — chart `version`
4. `helm-chart/kuberay-apiserver/values.yaml` — image `tag`
5. `helm-chart/ray-cluster/Chart.yaml` — chart `version`
6. `ray-operator/config/default/kustomization.yaml` — image `tag`
7. `ray-operator/controllers/ray/utils/constant.go` — `KUBERAY_VERSION` constant

## Hardcoded README versions (9 spots in 3 gotmpl files)

These do not follow from `Chart.yaml` automatically. Always edit the `.gotmpl`,
never the generated `.md`:

| File | Spots | Version type |
| --- | --- | --- |
| `helm-chart/kuberay-apiserver/README.md.gotmpl` | `--version X.Y.Z` ×2, `helm ls` line `kuberay-apiserver-X.Y.Z` | apiserver chart version (3 spots) |
| `helm-chart/ray-cluster/README.md.gotmpl` | `# Step 3` comment + `kuberay-operator --version X.Y.Z` | kuberay-operator chart version (2 spots) |
| `helm-chart/ray-cluster/README.md.gotmpl` | `raycluster --version X.Y.Z` ×2 | ray-cluster chart version (2 spots) |
| `helm-chart/kuberay-operator/README.md.gotmpl` | `targetRevision: vX.Y.Z` ×2 | git tag — note the `v` prefix |

`kuberay-operator`'s own `--version` lines use `{{ template "chart.version" . }}`
and update automatically — leave them as is.

## Commands

```bash
make -C helm-chart helm-docs   # regenerate README.md from templates
pre-commit run --all-files     # the helm-docs-built hook verifies they match
```

## Example

[PR #4924](https://github.com/ray-project/kuberay/pull/4924/files) bumped
`kuberay-apiserver` 1.4.2 → 1.6.2, `ray-cluster`/`kuberay-operator` 1.1.0 → 1.6.2,
and `targetRevision` v1.0.0-rc.0 → v1.6.2.
