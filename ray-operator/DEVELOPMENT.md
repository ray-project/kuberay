# Development

This section walks through how to build and test the operator in a running Kubernetes cluster.

## Requirements

| software | version  |                                                                link |
|:---------|:--------:|--------------------------------------------------------------------:|
| kubectl  | v1.23.0+ | [download](https://kubernetes.io/docs/tasks/tools/install-kubectl/) |
| go       |  v1.23   |                                  [download](https://golang.org/dl/) |
| docker   |  19.03+  |                        [download](https://docs.docker.com/install/) |

Alternatively, you can use podman (version 4.5+) instead of docker. See [podman.io](https://podman.io/getting-started/installation) for installation instructions. The Makefile allows you to specify the container engine to use via the `ENGINE` variable. For example, to use podman, you can run `ENGINE=podman make docker-build`.

The instructions assume you have access to a running Kubernetes cluster via `kubectl`. If you want to test locally, consider using [Kind](https://kind.sigs.k8s.io/) or [Minikube](https://kubernetes.io/docs/tasks/tools/install-minikube/).

### Setup on Kind

For local development, we recommend using [Kind](https://kind.sigs.k8s.io/) to create a Kubernetes cluster.

### Use go v1.23

Currently, KubeRay uses go v1.23 for development.

```bash
go install golang.org/dl/go1.23.2@latest
go1.23.2 download
export GOROOT=$(go1.23.2 env GOROOT)
export PATH="$GOROOT/bin:$PATH"
```

## Development

### IDE Setup (VS Code)

* Step 1: Install the [VS Code Go extension](https://marketplace.visualstudio.com/items?itemName=golang.go).
* Step 2: Import the KubeRay workspace configuration by using the file `kuberay.code-workspace` in the root of the KubeRay git repo:
  * "File" -> "Open Workspace from File" -> "kuberay.code-workspace"

Setting up workspace configuration is required because KubeRay contains multiple Go modules. See the [VS Code Go documentation](https://github.com/golang/vscode-go/blob/master/README.md#setting-up-your-workspace) for details.

### IMPORTANT: Change your working directory to `ray-operator`

All the following guidance require you to switch your working directory to the `ray-operator`.

```bash
cd ray-operator
```

### Cleanup local binaries, such as controller-gen and kustomize

To keep consistent results of code generation and testing, you need to remove outdated binaries installed by the Makefile.

```bash
rm -rf bin
# or
make clean
```

### End-to-end local development process on Kind

#### Run the operator inside the cluster

```bash
# Step 1: Create a Kind cluster
kind create cluster --image=kindest/node:v1.24.0

# Step 2: Modify KubeRay source code
# For example, add a log by adding setupLog.Info("Hello KubeRay") in the function `main` in `main.go`.


# Step 3: Build an image
#         This command will copy the source code directory into the image, and build it.
# Command: IMG={IMG_REPO}:{IMG_TAG} make docker-build
IMG=kuberay/operator:nightly make docker-build

# To skip running unit tests, run the following command instead:
# IMG=kuberay/operator:nightly make docker-image

# Step 4: Load the custom KubeRay image into the Kind cluster.
# Command: kind load docker-image {IMG_REPO}:{IMG_TAG}
kind load docker-image kuberay/operator:nightly

# Step 5: Keep consistency
# If you update RBAC or CRD, you need to synchronize them.
# See the section "Consistency check" for more information.

# Step 6: Install KubeRay operator with the custom image via local Helm chart
# (Path: helm-chart/kuberay-operator)
# Command: helm install kuberay-operator --set image.repository={IMG_REPO} --set image.tag={IMG_TAG} ../helm-chart/kuberay-operator
helm install kuberay-operator --set image.repository=kuberay/operator --set image.tag=nightly ../helm-chart/kuberay-operator

# Step 7: Check the log of KubeRay operator
kubectl logs {YOUR_OPERATOR_POD} | grep "Hello KubeRay"
# {"level":"info","ts":"2024-12-25T11:08:07.046Z","logger":"setup","msg":"Hello KubeRay"}
# ...
```

* Replace `{IMG_REPO}` and `{IMG_TAG}` with your own repository and tag.
* The command `make docker-build` (Step 3) will also run `make test` (unit tests).
* Step 6 also installs the custom resource definitions (CRDs) used by the KubeRay operator.

#### Run the operator outside the cluster

> Note: Running the operator outside the cluster allows you to debug the operator using your IDE. For example, you can set breakpoints in the code and inspect the state of the operator.

```bash
# Step 1: Create a Kind cluster
kind create cluster --image=kindest/node:v1.24.0

# Step 2: Install CRDs
make -C ray-operator install

# Step 3: Compile the source code
make -C ray-operator build

# Step 4: Run the KubeRay operator
./ray-operator/bin/manager -leader-election-namespace default -use-kubernetes-proxy
```

### Running the tests

The unit tests can be run by executing the following command:

```bash
make test
```

Example output:

```
✗ make test
...
go fmt ./...
go vet ./...
...
setting up env vars
?    github.com/ray-project/kuberay/ray-operator [no test files]
ok   github.com/ray-project/kuberay/ray-operator/api/v1alpha1 0.023s coverage: 0.9% of statements
ok   github.com/ray-project/kuberay/ray-operator/controllers 9.587s coverage: 66.8% of statements
ok   github.com/ray-project/kuberay/ray-operator/controllers/common 0.016s coverage: 75.6% of statements
ok   github.com/ray-project/kuberay/ray-operator/controllers/utils 0.015s coverage: 31.4% of statements
```

The e2e tests can be run by executing the following command:

```bash
# Reinstall the kuberay-operator to make sure it use the latest nightly image you just built.
helm uninstall kuberay-operator; helm install kuberay-operator --set image.repository=kuberay/operator --set image.tag=nightly ../helm-chart/kuberay-operator
make test-e2e
```

Example output:

```asciidoc
go test -timeout 30m -v ./test/e2e
=== RUN   TestRayJobWithClusterSelector
    rayjob_cluster_selector_test.go:41: Created ConfigMap test-ns-jtlbd/jobs successfully
    rayjob_cluster_selector_test.go:159: Created RayCluster test-ns-jtlbd/raycluster successfully
    rayjob_cluster_selector_test.go:161: Waiting for RayCluster test-ns-jtlbd/raycluster to become ready
=== RUN   TestRayJobWithClusterSelector/Successful_RayJob
=== PAUSE TestRayJobWithClusterSelector/Successful_RayJob
=== RUN   TestRayJobWithClusterSelector/Failing_RayJob
=== PAUSE TestRayJobWithClusterSelector/Failing_RayJob
=== CONT  TestRayJobWithClusterSelector/Successful_RayJob
=== CONT  TestRayJobWithClusterSelector/Failing_RayJob
=== NAME  TestRayJobWithClusterSelector
    rayjob_cluster_selector_test.go:213: Created RayJob test-ns-jtlbd/counter successfully
    rayjob_cluster_selector_test.go:215: Waiting for RayJob test-ns-jtlbd/counter to complete
    rayjob_cluster_selector_test.go:268: Created RayJob test-ns-jtlbd/fail successfully
    rayjob_cluster_selector_test.go:270: Waiting for RayJob test-ns-jtlbd/fail to complete
    test.go:118: Retrieving Pod Container test-ns-jtlbd/counter-zs9s8/ray-job-submitter logs
    test.go:106: Creating ephemeral output directory as KUBERAY_TEST_OUTPUT_DIR env variable is unset
    test.go:109: Output directory has been created at: /var/folders/mx/kpgdgdqd5j56ynylglgn0nvh0000gn/T/TestRayJobWithClusterSelector2055000419/001
    test.go:118: Retrieving Pod Container test-ns-jtlbd/fail-gdws6/ray-job-submitter logs
    test.go:118: Retrieving Pod Container test-ns-jtlbd/raycluster-head-gnhlw/ray-head logs
    test.go:118: Retrieving Pod Container test-ns-jtlbd/raycluster-worker-small-group-9dffx/ray-worker logs
--- PASS: TestRayJobWithClusterSelector (12.19s)
    --- PASS: TestRayJobWithClusterSelector/Failing_RayJob (16.11s)
    --- PASS: TestRayJobWithClusterSelector/Successful_RayJob (19.14s)
PASS
ok      github.com/ray-project/kuberay/ray-operator/test/e2e    32.066s
```

Note you can set the `KUBERAY_TEST_OUTPUT_DIR` environment to specify the test output directory.
If not set, it defaults to a temporary directory that's removed once the tests execution completes.

Alternatively, You can run the e2e test(s) from your preferred IDE / debugger.

### Manually test new image in running cluster

Build and apply the CRD:
```bash
make install
```

Deploy the manifests and controller
```bash
helm uninstall kuberay-operator; helm install kuberay-operator --set image.repository=kuberay/operator --set image.tag=nightly ../helm-chart/kuberay-operator
```

> Note: remember to replace with your own image

## pre-commit hooks

See [main development documentation][main-dev-doc].

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

### Generating API Reference

We use [elastic/crd-ref-docs](https://github.com/elastic/crd-ref-docs) to generate API reference for CRDs of KubeRay. The configuration file of `crd-ref-docs` is located at `hack/config.yaml`. Please refer to the documenation for more details.

Generate API refernece:

```bash
make api-docs
```

The file will be generated at `docs/reference/api.md` as configured.

### Consistency check

We have several [consistency checks](https://github.com/ray-project/kuberay/blob/master/.github/workflows/consistency-check.yaml) on GitHub Actions. There are several files which need synchronization.

1. `ray-operator/apis/ray/v1alpha1/*_types.go` should be synchronized with the CRD YAML files (`ray-operator/config/crd/bases/`)
2. `ray-operator/apis/ray/v1alpha1/*_types.go` should be synchronized with generated API (`ray-operator/pkg/client`)
3. `ray-operator/apis/ray/v1alpha1/*_types.go` should be synchronized with generated API reference (`docs/reference/api.md`)
4. CRD YAML files in `ray-operator/config/crd/bases/` and `helm-chart/kuberay-operator/crds/` should be the same.
5. Kubebuilder markers in `ray-operator/controllers/ray/*_controller.go` should be synchronized with RBAC YAML files in `ray-operator/config/rbac`.
6. RBAC YAML files in `helm-chart/kuberay-operator/templates` and `ray-operator/config/rbac` should be synchronized. **Currently, we need to synchronize this manually.** See [#631](https://github.com/ray-project/kuberay/pull/631) as an example.
7. `multiple_namespaces_role.yaml` and `multiple_namespaces_rolebinding.yaml` should be synchronized with `role.yaml` and `rolebinding.yaml` in the `helm-chart/kuberay-operator/templates` directory. The only difference is that the former creates namespaced RBAC resources, while the latter creates cluster-scoped RBAC resources.

```bash
# Synchronize consistency 1 and 5:
make manifests

# Synchronize consistency 2:
./hack/update-codegen.sh

# Synchronize consistency 3:
make api-docs

# Synchronize consistency 4:
make helm

# Synchronize 1, 2, 3, 4 and 5 in one command
# [Note]: Currently, we need to synchronize consistency 5 and 6 manually.
make sync

# Reproduce CI error for job "helm-chart-verify-rbac" (consistency 5)
python3 ../scripts/rbac-check.py
```

### Building Multi architecture images locally

Most of image repositories supports multiple architectures container images. When running an image from a device, the docker client automatically pulls the correct the image with a matching architectures. The easiest way to build multi-arch images is to utilize Docker `Buildx` plug-in which allows easily building multi-arch images using Qemu emulation from a single machine. Buildx plugin is readily available when you install the [Docker Desktop](https://docs.docker.com/desktop/) on your machine.
Verify Buildx installation and make sure it does not return error
```
docker buildx version
```
Verify the builder instance has a default(with *) DRIVER/ENDPOINT starting with `docker-container` by running:
```
docker buildx ls
```
You may see something:
```
NAME/NODE    DRIVER/ENDPOINT             STATUS  BUILDKIT             PLATFORMS
sad_brown *  docker-container
  sad_brown0 unix:///var/run/docker.sock running v0.12.4              linux/amd64, linux/amd64/v2, linux/amd64/v3, linux/amd64/v4, linux/arm64, linux/riscv64, linux/ppc64le, linux/s390x, linux/386, linux/mips64le, linux/mips64, linux/arm/v7, linux/arm/v6
default      docker
  default    default                     running v0.11.7+d3e6c1360f6e linux/amd64, linux/amd64/v2, linux/amd64/v3, linux/amd64/v4, linux/386, linux/arm64, linux/riscv64, linux/ppc64le, linux/s390x, linux/mips64le, linux/mips64, linux/arm/v7, linux/arm/v6
```

If not, create the instance by running:
```
docker buildx create --use --bootstrap
```

Run the following `docker buildx build` command to build and push linux/arm64 and linux/amd64 images(manifests) in a single command:
```
cd ray-operator
docker buildx build --tag quay.io/<my org>/operator:latest --tag docker.io/<my org>/operator:latest --platform linux/amd64,linux/arm64 --push --provenance=false .
```
* --platform is a comma separated list of targeted platforms to build.
* --tag is a remote repo_name:tag to push.
* --push/--load optionally Push to remote registry or Load into local docker.
* Some registry such as Quay.io dashboard displays attestation manifests as unknown platforms. Setting --provenance=false to avoid this issue.

[main-dev-doc]: ../docs/development/development.md#pre-commit-hooks
