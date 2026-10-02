# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Breaking:** `metrics.serviceMonitor.selector` is renamed to `metrics.serviceMonitor.additionalLabels`, and empty labels are no longer emitted ([ray-project/kuberay#4979](https://github.com/ray-project/kuberay/pull/4979)). Update any values file that sets it.
- Synced the fork with upstream `ray-project/kuberay` `v1.7.1`, the first release that supports Kubernetes 1.35 ([#4399](https://github.com/ray-project/kuberay/pull/4399) adds RayJob sidecar retry handling for 1.35+, [#4703](https://github.com/ray-project/kuberay/pull/4703) moves client-go to 1.36).
- Refreshed `helm/kuberay/` (CRDs, templates, `values.yaml`, chart tests) from `helm-chart/kuberay-operator/` at `v1.7.1`, and pinned the operator image to `v1.7.1`.
- `RayServiceIncrementalUpgrade` is beta and enabled by default upstream from `v1.7.0`; the GS chart follows that default.

### Fixed

- Bumped the `architect` orb from `5.11.5` to `10.11.1`. Orbs below 9 push to `giantswarmpublic.azurecr.io`, which no longer resolves, so releases never reached the catalog.
- `.abs/main.yaml` built `helm-chart/kuberay-operator`, the upstream-tracking copy, instead of `helm/kuberay`, the chart CI actually publishes.
- `helm/kuberay/Chart.yaml` now declares `appVersion: v1.7.1`, with `override_app_version: false` in CI, so the published chart names the KubeRay release it deploys rather than the GS chart tag.

### Added

- Feature gates `RayClusterMTLS`, `RayClusterNetworkPolicy` and `RayClusterHistoryServer`, all disabled by default.
- Operator configuration values `configuration.defaultPodAnnotations` and `configuration.defaultPodLabels`, applied to every Ray pod.

### Deprecated

- `ray.io/v1alpha1` is marked deprecated upstream ([#5122](https://github.com/ray-project/kuberay/pull/5122)). Migrate to `ray.io/v1`.

## [1.1.0] - 2026-05-19

### Changed

- Updated Chart annotations for OCI repositories.
- Synced fork with upstream `ray-project/kuberay` master (through `v1.6.1`).
- Synced CRDs in `helm/kuberay/crds/` with upstream (`helm-chart/kuberay-operator/crds/`), including the new `ray.io_raycronjobs.yaml`.
- Refreshed templates and `values.yaml` in `helm/kuberay/` from the upstream-tracking `helm-chart/kuberay-operator/`. Brings missing RBAC for `secrets`, `pods/resize`, `services/proxy`, leases, and the new `ray.io/v1alpha1.RayCronJob` editor/viewer ClusterRoles. Preserves the GS `application.giantswarm.io/team` label injection.

### Fixed

- Pinned the operator image tag in `helm/kuberay/values.yaml` to `v1.6.1` (was `nightly`). The moving `nightly` tag had drifted past the chart-shipped RBAC, leaving the operator running but unable to reconcile.
- Added the `io.giantswarm.application.audience` / `io.giantswarm.application.team` OCI annotations to `helm/kuberay/Chart.yaml` (the actual published chart). Previously they were only on `helm-chart/kuberay-operator/Chart.yaml`, which is not the chart shipped by the CircleCI pipeline.

## [1.0.0] - 2025-10-07

### Added

- Create first GS artifact.

[Unreleased]: https://github.com/giantswarm/kuberay/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/giantswarm/kuberay/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/giantswarm/kuberay/releases/tag/v1.0.0

