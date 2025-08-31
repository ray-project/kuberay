# KubeRay Weekly Summary - Week of 2025-08-24
*Generated on 2025-08-31 based on GitHub API search results*

## Summary Statistics
- **New Issues**: 15
- **New Pull Requests**: 28
- **Period**: 2025-08-24 to 2025-08-31
- **Total GitHub Activity**: 43 items

## New Issues

### Feature Requests (8)
- 🟢 [#4022](https://github.com/ray-project/kuberay/issues/4022) [Feature] Update User Documentation and API Reference for DeletionStrategy enhancement
  - Labels: `enhancement`, `triage`
  - Author: @seanlaii

- 🟢 [#4021](https://github.com/ray-project/kuberay/issues/4021) [Feature] Implement RayJob DeletionStrategy Logic and E2E Tests
  - Labels: `enhancement`, `triage`
  - Author: @seanlaii

- 🟢 [#4020](https://github.com/ray-project/kuberay/issues/4020) [Feature] Implement Validation Logic in RayJob Controller/Webhook for DeletionStrategy
  - Labels: `enhancement`, `triage`
  - Author: @seanlaii

- 🟢 [#4019](https://github.com/ray-project/kuberay/issues/4019) [Feature] Define new DeletionStrategy in RayJob CRD
  - Labels: `enhancement`, `triage`
  - Author: @seanlaii

- 🟢 [#4018](https://github.com/ray-project/kuberay/issues/4018) [Umbrella] Enhance RayJob DeletionStrategy to Support Multi-Stage Deletion
  - Labels: `enhancement`, `triage`
  - Author: @seanlaii

- 🟢 [#4013](https://github.com/ray-project/kuberay/issues/4013) [Feature] Separate controller namespace and CRD namespaces for KubeRay-Operator Dashboard
  - Labels: `enhancement`, `triage`
  - Author: @win5923

- 🔴 [#4001](https://github.com/ray-project/kuberay/issues/4001) [Feature][kuberay-operator][Helm] Make kubeClient's QPS and Burst configurable
  - Labels: `enhancement`, `1.5.0`
  - Author: @Future-Outlier

- 🟢 [#3987](https://github.com/ray-project/kuberay/issues/3987) [Feature] Add NetworkPolicy support
  - Labels: `enhancement`, `triage`
  - Author: @chipspeak

### Bug Reports (3)
- 🟢 [#4012](https://github.com/ray-project/kuberay/issues/4012) [BUG] Test Apiserver E2E (nightly operator) is flaky
  - Labels: `bug`, `triage`
  - Author: @win5923

- 🟢 [#4005](https://github.com/ray-project/kuberay/issues/4005) [Bug] APIServer SDK is flaky
  - Labels: `bug`, `apiserver`, `flaky`
  - Author: @kevin85421

- 🟢 [#3994](https://github.com/ray-project/kuberay/issues/3994) [Bug] a RayJob with shutdownAfterJobFinishes set to false is not allowed to be suspended
  - Labels: `bug`, `triage`
  - Author: @guofy-ai

- 🟢 [#3984](https://github.com/ray-project/kuberay/issues/3984) [Bug] rayCluster cannot submit a job
  - Labels: `bug`, `triage`
  - Author: @LY-today

### Good First Issues (1)
- 🔴 [#4016](https://github.com/ray-project/kuberay/issues/4016) Refactor testRayJob global variable to avoid test side effects
  - Labels: `good-first-issue`
  - Author: @Future-Outlier

### Umbrella Issues (2)
- 🟢 [#4008](https://github.com/ray-project/kuberay/issues/4008) [Umbrella] Upgrade golangci-lint from v1.64.8 to v2.4.0
  - Author: @seanlaii

- 🟢 [#3993](https://github.com/ray-project/kuberay/issues/3993) [Refactor] Refactor utils package in ray-operator
  - Author: @owenowenisme

## New Pull Requests

### Bug Fixes (2)
- 🔴 [#4024](https://github.com/ray-project/kuberay/pull/4024) [WIP] Could you help me summarize new issues and pull requests since last week?
  - Author: @Copilot

- 🟢 [#4023](https://github.com/ray-project/kuberay/pull/4023) Use ctrl logger in Volcano scheduler to include context
  - Author: @win5923

### Refactoring (8)
- 🟣 [#4017](https://github.com/ray-project/kuberay/pull/4017) [Refactor] Refactor testRayJob global variable to avoid test side effects
  - Author: @400Ping

- 🟣 [#4011](https://github.com/ray-project/kuberay/pull/4011) [Feature] Remove checking CRD in Volcano scheduler initialization
  - Author: @win5923

- 🟣 [#4010](https://github.com/ray-project/kuberay/pull/4010) [refactor][5/N] Refactor `httpproxy_httpclient.go`
  - Author: @owenowenisme

- 🟣 [#4009](https://github.com/ray-project/kuberay/pull/4009) [refactor][4/N] Remove ctrl in dashboard http client in `dashboard-httpclient.go`
  - Author: @owenowenisme

- 🟣 [#4006](https://github.com/ray-project/kuberay/pull/4006) Follow up 3992: Remove logs and add comments
  - Author: @kevin85421

- 🔴 [#3990](https://github.com/ray-project/kuberay/pull/3990) chore: use the same Service name as the owning resource
  - Author: @lowjoel

- 🟣 [#3989](https://github.com/ray-project/kuberay/pull/3989) Add `useKubernetesProxy` to reconciler options
  - Author: @owenowenisme

- 🟣 [#3985](https://github.com/ray-project/kuberay/pull/3985) [1/N] Refactor utils package - Remove unnecessary package from dashboard_http
  - Author: @owenowenisme

### New Features (4)
- 🟢 [#4004](https://github.com/ray-project/kuberay/pull/4004) feat[python-client]: add support for loading kubeconfig from within pod
  - Author: @kryanbeane

- 🟢 [#4003](https://github.com/ray-project/kuberay/pull/4003) [Feature] Include CR UID in kuberay metrics
  - Author: @YuxiaoWang-520

- 🟣 [#4002](https://github.com/ray-project/kuberay/pull/4002) [Helm] Make Kube Client QPS and Burst configurable for kuberay-operator
  - Author: @Future-Outlier

- 🟢 [#3996](https://github.com/ray-project/kuberay/pull/3996) feat: mirror RayService svc object creation for RayJob
  - Author: @lowjoel

### Maintenance (5)
- 🟢 [#4015](https://github.com/ray-project/kuberay/pull/4015) Bump next from 15.2.4 to 15.4.7 in /dashboard
  - Labels: `dependencies`, `javascript`
  - Author: @dependabot[bot]

- 🟢 [#4014](https://github.com/ray-project/kuberay/pull/4014) Fix testifylint & gci lint issues in apiserver/kubectl-plugin
  - Author: @seanlaii

- 🟢 [#4007](https://github.com/ray-project/kuberay/pull/4007) [Chore] Upgrade golangci-lint to v2.4.0 and adjust linting configurations
  - Author: @seanlaii

- 🟣 [#3988](https://github.com/ray-project/kuberay/pull/3988) [apiserver] reduce the possibility of flaky test in TestAPIServerClie…
  - Author: @fscnick

- 🟣 [#3983](https://github.com/ray-project/kuberay/pull/3983) [refactor][1/N] Move `FetchHeadServiceURL` to `util.go` to reduce imported packages in `dashboard_httpclient.go`
  - Author: @kevin85421

### Testing (3)
- 🟣 [#4000](https://github.com/ray-project/kuberay/pull/4000) [Test] Add ReconcileConcurrency Configuration Test
  - Author: @Future-Outlier

- 🟣 [#3999](https://github.com/ray-project/kuberay/pull/3999) [Follow Up][Test] Support to set QPS and burst by configuration
  - Author: @Future-Outlier

- 🟣 [#3997](https://github.com/ray-project/kuberay/pull/3997) Remove unecessary raycluster log in kai-scheduler logger
  - Author: @owenowenisme

### Experimental Features (2)
- 🟢 [#3998](https://github.com/ray-project/kuberay/pull/3998) [POC] Prototype multi-host indexing
  - Author: @chiayi

- 🟢 [#3986](https://github.com/ray-project/kuberay/pull/3986) [WIP] Implement NetworkPolicy support
  - Author: @chipspeak

### General Changes (4)
- 🟣 [#3995](https://github.com/ray-project/kuberay/pull/3995) Use ctrl logger and create logger in function in kai-scheduler
  - Author: @owenowenisme

- 🟣 [#3992](https://github.com/ray-project/kuberay/pull/3992) [refactor][3/N] Refactor dashbpard httpclient
  - Author: @owenowenisme

- 🟢 [#3991](https://github.com/ray-project/kuberay/pull/3991) [refactor][2/N] Move `UnmarshalRuntimeEnvYAML` to utils.go
  - Author: @owenowenisme

- 🟣 [#3982](https://github.com/ray-project/kuberay/pull/3982) [Poc] Refactor dashboard http
  - Author: @owenowenisme

## Key Highlights This Week

### Major Feature Development
- **RayJob DeletionStrategy Enhancement**: A comprehensive umbrella issue (#4018) was created for enhancing RayJob deletion strategies to support multi-stage deletion, with 4 related sub-issues tracking specific implementation aspects.

### Code Quality & Maintenance
- **golangci-lint Upgrade**: Major effort to upgrade from v1.64.8 to v2.4.0 with corresponding lint fixes
- **Utils Package Refactoring**: Multiple PRs focused on cleaning up the utils package to reduce dependencies and improve modularity

### Infrastructure Improvements
- **Kubernetes Integration**: Several PRs focused on improving Kubernetes service creation and proxy handling
- **Monitoring & Observability**: Addition of UID labels to KubeRay metrics for better resource tracking

### Testing & Quality
- **Flaky Test Fixes**: Multiple issues and PRs addressing test reliability in the APIServer components
- **Test Coverage**: New tests added for configuration options like ReconcileConcurrency

## Top Contributors This Week
1. @seanlaii - 6 contributions (1 umbrella issue + 4 sub-issues + 1 PR)
2. @owenowenisme - 6 contributions (1 issue + 5 PRs)
3. @Future-Outlier - 4 contributions (1 issue + 3 PRs)
4. @win5923 - 3 contributions (2 issues + 1 PR)
5. @lowjoel - 2 contributions (2 PRs)
6. @kevin85421 - 2 contributions (1 issue + 1 PR)
7. @chiayi - 1 contribution (1 PR)
8. @chipspeak - 2 contributions (1 issue + 1 PR)
9. @kryanbeane - 1 contribution (1 PR)
10. @YuxiaoWang-520 - 1 contribution (1 PR)

---
*This summary was generated using GitHub API data and the weekly-summary.py script.*
*For the latest updates, visit the [KubeRay repository](https://github.com/ray-project/kuberay).*