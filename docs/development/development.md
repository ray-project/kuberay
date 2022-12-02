## KubeRay Development Guidance

### Develop KubeRay Operator

See [ray-operator/DEVELOPMENT.md](https://github.com/ray-project/kuberay/blob/master/ray-operator/DEVELOPMENT.md) for more details.

### Develop KubeRay APIServer

```sh
cd apiserver

# Build code
go build cmd/main.go
```

### Develop KubeRay CLI

See [cli/README.md](https://github.com/ray-project/kuberay/blob/master/cli/README.md) for more details.

### Develop proto and OpenAPI

See [proto/README.md](https://github.com/ray-project/kuberay/blob/master/proto/README.md) for more details.

### Deploy Docs locally

Run the following command in the root directory of your KubeRay repo to deploy Docs locally:

```sh
docker run --rm -it -p 8000:8000 -v ${PWD}:/docs squidfunk/mkdocs-material
```

Then access `http://0.0.0.0:8000/kuberay/` in your web browser.
