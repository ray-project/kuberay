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

### Build the source code

```
make build
```

### Building the container image

Once building is finished, push it to DockerHub so it can be pulled down and run in the Kubernetes cluster.

```shell script
IMG=kuberay/operator:nightly make docker-build
```

> Note: replace `kuberay/operator:nightly` with your own registry, image name and tag.  

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

* Step1: Install `ct` (chart-testing) and related dependencies. See https://github.com/helm/chart-testing for more details.
* Step2: `./helm-chart/script/chart-test.sh`

### Consistency check

We have several [consistency checks](https://github.com/ray-project/kuberay/blob/master/.github/workflows/consistency-check.yaml) on GitHub Actions. There are several files which need synchronization.

1. `ray-operator/apis/ray/v1alpha1/*_types.go` should be synchronized with the CRD YAML files (`ray-operator/config/crd/bases/`)
2. `ray-operator/apis/ray/v1alpha1/*_types.go` should be synchronized with generated API (`ray-operator/pkg/client`)
3. CRD YAML files in `ray-operator/config/crd/bases/` and `helm-chart/kuberay-operator/crds/` should be the same.
4. Kubebuilder markers in `ray-operator/controllers/ray/*_controller.go` should be synchronized with RBAC YAML files in `ray-operator/config/rbac`. 
5. RBAC YAML files in `helm-chart/kuberay-operator/templates` and `ray-operator/config/rbac` should be synchronized.

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

* Step1: Install related dependencies, including [kind](https://kind.sigs.k8s.io/), [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/), and [kustomize](https://kustomize.io/).

* Step2: `python3 tests/compatibility-test.py` (You must be in `/path/to/your/kuberay/`.)
