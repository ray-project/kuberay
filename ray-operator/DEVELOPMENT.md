# Development

This section walks through how to build and test the operator in a running Kubernetes cluster.

## Requirements

software  | version | link
:-------------  | :---------------:| -------------:
kubectl |  v1.21.0+    | [download](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
go  | v1.17|[download](https://golang.org/dl/)
docker   | 19.03+|[download](https://docs.docker.com/install/)

The instructions assume you have access to a running Kubernetes cluster via ``kubectl``. If you want to test locally, consider using [Minikube](https://kubernetes.io/docs/tasks/tools/install-minikube/).

### Setup on Kind

For a local [kind](https://kind.sigs.k8s.io/) environment setup, you can follow the Jupyter Notebook example: [KubeRay-on-kind](../docs/notebook/kuberay-on-kind.ipynb).

### Use go v1.17

Currently, Kuberay does not support go v1.16 ([#568](https://github.com/ray-project/kuberay/issues/568)) or go v1.18 ([#518](https://github.com/ray-project/kuberay/issues/518)).
Hence, we strongly recommend you to use go v1.17. The following commands can help you switch to go v1.17.6.

```bash
go install golang.org/dl/go1.17.6@latest
go1.17.6 download
export GOROOT=$(go1.17.6 env GOROOT)
export PATH="$GOROOT/bin:$PATH"
```

## Development

### IDE Setup (VS Code)
* Step 1: Install the [VS Code Go extension](https://marketplace.visualstudio.com/items?itemName=golang.go).
* Step 2: Import the KubeRay workspace configuration by using the file `kuberay.code-workspace` in the root of the KubeRay git repo:
  * "File" -> "Open Workspace from File" -> "kuberay.code-workspace"

Setting up workspace configuration is required because KubeRay contains multiple Go modules. See the [VS Code Go documentation](https://github.com/golang/vscode-go/blob/master/README.md#setting-up-your-workspace) for details.

### End-to-end local development process on Kind

```bash
# Step 1: Create a Kind cluster
kind create cluster --image=kindest/node:v1.24.0

# Step 2: Modify KubeRay source code
# For example, add a log "Hello KubeRay" in the function `Reconcile` in `raycluster_controller.go`.

# Step 3: Build a Docker image
#         This command will copy the source code directory into the image, and build it.
# Command: IMG={IMG_REPO}:{IMG_TAG} make docker-build
IMG=kuberay/operator:nightly make docker-build

# Step 4: Load the custom KubeRay image into the Kind cluster.
# Command: kind load docker-image {IMG_REPO}:{IMG_TAG}
kind load docker-image kuberay/operator:nightly

# Step 5: Keep consistency
# If you update RBAC or CRD, you need to synchronize them.
# See the section "Consistency check" for more information.

# Step 6: Install KubeRay operator with the custom image via local Helm chart
# (Path: helm-chart/kuberay-operator)
# Command: helm install kuberay-operator --set image.repository={IMG_REPO} --set image.tag={IMG_TAG} .
helm install kuberay-operator --set image.repository=kuberay/operator --set image.tag=nightly .

# Step 7: Check the log of KubeRay operator
kubectl logs {YOUR_OPERATOR_POD} | grep "Hello KubeRay"
# 2022-12-09T04:41:59.946Z        INFO    controllers.RayCluster  Hello KubeRay
# ...
```

* Replace `{IMG_REPO}` and `{IMG_TAG}` with your own repository and tag.
* The command `make docker-build` (Step 3) will also run `make test` (unit tests).
* Step 6 also installs the custom resource definitions (CRDs) used by the KubeRay operator.

### Running the tests

```
make test
```

example results:
```
✗ make test
...
go fmt ./...
go vet ./...
...
setting up env vars
?   	github.com/ray-project/kuberay/ray-operator	[no test files]
ok  	github.com/ray-project/kuberay/ray-operator/api/v1alpha1	0.023s	coverage: 0.9% of statements
ok  	github.com/ray-project/kuberay/ray-operator/controllers	9.587s	coverage: 66.8% of statements
ok  	github.com/ray-project/kuberay/ray-operator/controllers/common	0.016s	coverage: 75.6% of statements
ok  	github.com/ray-project/kuberay/ray-operator/controllers/utils	0.015s	coverage: 31.4% of statements
```

### Manually test new image in running cluster

Build and apply the CRD:
```
make install
```

Deploy the manifests and controller
```
IMG=kuberay/operator:nightly make deploy 
```

> Note: remember to replace with your own image

## CI/CD

### Helm chart linter

We have [chart lint tests](https://github.com/ray-project/kuberay/blob/master/.github/workflows/helm-lint.yaml) with Helm v3.4.1 and Helm v3.9.4 on GitHub Actions. We also provide a script to execute the lint tests on your laptop. If you cannot reproduce the errors on GitHub Actions, the possible reason is the different version of Helm. Issue [#537](https://github.com/ray-project/kuberay/issues/537) is an example that some errors only happen in old helm versions.

Run tests with docker
```bash
./helm-chart/script/chart-test.sh
```
Run tests on your local environment 
* Step1: Install `ct` (chart-testing) and related dependencies. See https://github.com/helm/chart-testing for more details.
* Step2: `./helm-chart/script/chart-test.sh local`

### Consistency check

We have several [consistency checks](https://github.com/ray-project/kuberay/blob/master/.github/workflows/consistency-check.yaml) on GitHub Actions. There are several files which need synchronization.

1. `ray-operator/apis/ray/v1alpha1/*_types.go` should be synchronized with the CRD YAML files (`ray-operator/config/crd/bases/`)
2. `ray-operator/apis/ray/v1alpha1/*_types.go` should be synchronized with generated API (`ray-operator/pkg/client`)
3. CRD YAML files in `ray-operator/config/crd/bases/` and `helm-chart/kuberay-operator/crds/` should be the same.
4. Kubebuilder markers in `ray-operator/controllers/ray/*_controller.go` should be synchronized with RBAC YAML files in `ray-operator/config/rbac`. 
5. RBAC YAML files in `helm-chart/kuberay-operator/templates` and `ray-operator/config/rbac` should be synchronized. **Currently, we need to synchronize this manually.** See [#631](https://github.com/ray-project/kuberay/pull/631) as an example.

```bash
# Synchronize consistency 1 and 4:
make manifests

# Synchronize consistency 2:
./hack/update-codegen.sh

# Synchronize consistency 3:
make helm

# Synchronize 1, 2, 3, and 4 in one command
# [Note]: Currently, we need to synchronize consistency 5 manually.
make sync

# Reproduce CI error for job "helm-chart-verify-rbac" (consistency 5)
python3 ../scripts/rbac-check.py
```

### Run end-to-end tests locally

We have some [end-to-end tests](https://github.com/ray-project/kuberay/blob/master/.github/workflows/actions/compatibility/action.yaml) on GitHub Actions.
These tests operate small Ray clusters running within a [kind](https://kind.sigs.k8s.io/) (Kubernetes-in-docker) environment. To run the tests yourself, follow these steps:

* Step1: Install related dependencies, including [kind](https://kind.sigs.k8s.io/) and [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/).

* Step2: You must be in `/path/to/your/kuberay/`.
  ```bash
  # [Usage]: RAY_IMAGE=$RAY_IMAGE OPERATOR_IMAGE=$OPERATOR_IMAGE python3 tests/compatibility-test.py
  #          These 3 environment variables are optional.
  # [Example]:
  RAY_IMAGE=rayproject/ray:2.3.0 OPERATOR_IMAGE=kuberay/operator:nightly python3 tests/compatibility-test.py
  ```
### Running configuration tests locally.

The sample RayCluster and RayService CRs under `ray-operator/config/samples` are tested in `tests/test_sample_raycluster_yamls.py`
and `tests/test_sample_rayservice_yamls.py`. Currently, only a few of these sample configurations are tested in the CI. See
[KubeRay issue #695](https://github.com/ray-project/kuberay/issues/695). 

```bash
# Test RayCluster doc examples.
RAY_IMAGE=rayproject/ray:2.3.0 OPERATOR_IMAGE=kuberay/operator:nightly python3 tests/test_sample_raycluster_yamls.py
# Test RayService doc examples.
RAY_IMAGE=rayproject/ray:2.3.0 OPERATOR_IMAGE=kuberay/operator:nightly python3 tests/test_sample_rayservice_yamls.py
```

See [KubeRay PR #605](https://github.com/ray-project/kuberay/pull/605) for more details about the test framework.
