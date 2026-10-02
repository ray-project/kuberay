# KubeRay

KubeRay is a Kubernetes operator for managing Ray clusters. It provides three
custom resources: RayCluster, RayJob, and RayService.

## Repository Structure

This is a monorepo. The primary component is `ray-operator/`.

| Directory | Description |
| --- | --- |
| `ray-operator/` | Kubernetes operator (controllers, webhooks, CRDs) |
| `apiserver/` | KubeRay API Server (Alpha) |
| `apiserversdk/` | API Server SDK |
| `kubectl-plugin/` | kubectl plugin for Ray (Beta) |
| `helm-chart/` | Helm charts (kuberay-operator, kuberay-apiserver, ray-cluster) |
| `dashboard/` | KubeRay Dashboard (Experimental, Next.js/TypeScript) |
| `clients/` | Python client libraries |
| `proto/` | Protobuf definitions and Swagger specs |
| `scripts/` | Build, test, and automation scripts |

## Build and Test Commands

All commands run from `ray-operator/` unless noted otherwise.

```sh
# Build
make build                # Build operator binary

# Tests
make test                 # Unit tests (uses envtest for controller tests)

# Lint and format
make lint                 # golangci-lint (via ./scripts/lint.sh)
make fmt                  # gofmt
make vet                  # go vet
make fumpt                # gofumpt

# Code generation (run after modifying API types)
make generate             # Regenerate DeepCopy methods and client code
make manifests            # Regenerate CRDs and RBAC from kubebuilder markers
make helm                 # Sync CRDs into helm-chart/

# API documentation
make api-docs             # Regenerate CRD reference docs
```

### Single-File Commands

```sh
# Lint a single file
golangci-lint run ./path/to/file.go

# Format a single file
gofumpt -w path/to/file.go
```

### Other Components

```sh
# apiserver (from repo root)
cd apiserver && go test ./pkg/... ./cmd/... -race -parallel 4

# apiserversdk (from repo root)
cd apiserversdk && make lint && make test

# kubectl-plugin (from repo root)
cd kubectl-plugin && go test ./pkg/... -race -parallel 4
```

## Code Generation Workflow

After modifying API types in `ray-operator/apis/ray/v1/types.go`:

```sh
cd ray-operator
make generate      # DeepCopy, client code
make manifests     # CRDs, RBAC
make helm          # Sync CRDs to Helm chart
make api-docs      # Update API reference
```

CI will fail if generated files are out of date.

## Coding Conventions

### Go Style

- **Formatter**: gofumpt (stricter than gofmt)
- **Line length**: 120 characters max
- **Import order** (enforced by gci):
  1. Standard library
  2. Third-party packages
  3. `github.com/ray-project/kuberay` packages
- **nolint directives** must include an explanation (`//nolint:errcheck // reason`)

### Pre-Commit Hooks

Pre-commit hooks run automatically and enforce:

- golangci-lint (20 linters enabled)
- shellcheck for shell scripts
- markdownlint (120-char lines)
- YAML formatting for sample files
- Helm chart validation and docs generation
- CRD schema generation for kubeconform

Install hooks with: `pre-commit install`

### Testing

- Unit tests use envtest for controller tests (simulated API server)
- E2E tests require a Kubernetes cluster (not run locally)
- Test files are co-located with source (`*_test.go` alongside `.go` files)

### Pull Requests

- Small, focused PRs with "subject: message" format
- Include unit tests for new functionality
- Run `make generate && make manifests && make helm` if API types changed
- CI runs all checks regardless of files changed (no path filters)
