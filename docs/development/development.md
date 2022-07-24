## KubeRay Development Guidance

Download this repo locally

```
mkdir -p $GOPATH/src/github.com/ray-project
cd $GOPATH/src/github.com/ray-project
git clone https://github.com/ray-project/kuberay.git
```

### Develop proto and OpenAPI

Generate go clients and swagger file

```
make generate
```

### Develop KubeRay Operator

```
cd ray-operator

# Build codes
make build

# Run test
make test

# Build container image
make docker-build
```

### Develop KubeRay APIServer

```
cd apiserver

# Build code
go build cmd/main.go
```

### Develop KubeRay CLI

```
cd cli
go build -o kuberay -a main.go
./kuberay help
```

### Deploy Docs locally

We don't need to configure `mkdocs` environment, to check static website locally, run command

```
docker run --rm -it -p 8000:8000 -v ${PWD}:/docs squidfunk/mkdocs-material build
```
