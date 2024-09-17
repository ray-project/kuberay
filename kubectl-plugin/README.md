# Kuberay Kubectl Plugin

Kubectl plugin/extension for Kuberay CLI that provides the ability to manage ray resources.

## Prerequisites

1. Make sure there is a Kubernetes cluster running with KubeRay installed.
2. Make sure `kubectl` has the right context.

## Installation

### Compiling from source

1. Run `go build cmd/kubectl-ray.go`
2. Move the binary, which will be named `kubectl-ray` to your `PATH`

### Using Krew

1. Install [Krew](https://krew.sigs.k8s.io/docs/user-guide/setup/install/).
2. (TODO: Replace this step with the installation command).

## Shell Completion

1. Install [kubectl plugin-completion](https://github.com/marckhouzam/kubectl-plugin_completion) plugin.
2. Run `kubectl plugin-completion generate`.
3. Add `$HOME/.kubectl-plugin-completion` to `PATH` in your shell profile.
