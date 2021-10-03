# Development

This section walks through how to build and test the operator in a running Kubernetes cluster.

## Requirements

software  | version | link
:-------------  | :---------------:| -------------:
kubectl |  v1.18.3+    | [download](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
go  | v1.13+|[download](https://golang.org/dl/)
docker   | 19.03+|[download](https://docs.docker.com/install/)

The instructions assume you have access to a running Kubernetes cluster via ``kubectl``. If you want to test locally, consider using [Minikube](https://kubernetes.io/docs/tasks/tools/install-minikube/).

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
/Users/jiaxin/go/src/github.com/ray-project/ray-contrib/ray-operator/bin/controller-gen "crd:maxDescLen=100,trivialVersions=true,preserveUnknownFields=false" rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
/Users/jiaxin/go/src/github.com/ray-project/ray-contrib/ray-operator/bin/controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
go fmt ./...
go vet ./...
mkdir -p /Users/jiaxin/go/src/github.com/ray-project/ray-contrib/ray-operator/testbin
test -f /Users/jiaxin/go/src/github.com/ray-project/ray-contrib/ray-operator/testbin/setup-envtest.sh || curl -sSLo /Users/jiaxin/go/src/github.com/ray-project/ray-contrib/ray-operator/testbin/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.7.2/hack/setup-envtest.sh
source /Users/jiaxin/go/src/github.com/ray-project/ray-contrib/ray-operator/testbin/setup-envtest.sh; fetch_envtest_tools /Users/jiaxin/go/src/github.com/ray-project/ray-contrib/ray-operator/testbin; setup_envtest_env /Users/jiaxin/go/src/github.com/ray-project/ray-contrib/ray-operator/testbin; go test ./... -coverprofile cover.out
Using cached envtest tools from /Users/jiaxin/go/src/github.com/ray-project/ray-contrib/ray-operator/testbin
setting up env vars
?   	github.com/ray-project/ray-contrib/ray-operator	[no test files]
ok  	github.com/ray-project/ray-contrib/ray-operator/api/v1alpha1	0.023s	coverage: 0.9% of statements
ok  	github.com/ray-project/ray-contrib/ray-operator/controllers	9.587s	coverage: 66.8% of statements
ok  	github.com/ray-project/ray-contrib/ray-operator/controllers/common	0.016s	coverage: 75.6% of statements
ok  	github.com/ray-project/ray-contrib/ray-operator/controllers/utils	0.015s	coverage: 31.4% of statements
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
