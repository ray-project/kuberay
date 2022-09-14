# Development

This section walks through how to build and test the operator in a running Kubernetes cluster.

## Requirements

software  | version | link
:-------------  | :---------------:| -------------:
kubectl |  v1.21.0+    | [download](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
go  | v1.16,17|[download](https://golang.org/dl/)
docker   | 19.03+|[download](https://docs.docker.com/install/)

The instructions assume you have access to a running Kubernetes cluster via ``kubectl``. If you want to test locally, consider using [Minikube](https://kubernetes.io/docs/tasks/tools/install-minikube/).

### Setup on Kind

For a local [kind](https://kind.sigs.k8s.io/) environment setup, you can follow the Jupyter Notebook example: [KubeRay-on-kind](../docs/notebook/kuberay-on-kind.ipynb).

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