# Kuberay Kubectl Plugin

Kubectl plugin/extension for Kuberay CLI that provides the ability to manage ray resources.

## Prerequisites

1. Make sure there is a Kubernetes cluster running with KubeRay installed.
2. Make sure `kubectl` has the right context.

## Installation

You can install the Kuberay kubectl plugin using one of the following methods:

### Install using Krew kubectl plugin manager (Recommended)

1. Install [Krew](https://krew.sigs.k8s.io/docs/user-guide/setup/install/).
2. Download the plugin list by running `kubectl krew update`.
3. Install the plugin by running `kubectl krew install ray`.
4. Run `kubectl ray --help` to verify the installation.

### Download from GitHub releases

Go to the [releases page](https://github.com/ray-project/kuberay/releases) and download the binary for your platform.

For example, to install kubectl plugin version 1.2.2 on Linux amd64:

```bash
curl -LO https://github.com/ray-project/kuberay/releases/download/v1.2.2/kubectl-ray_v1.2.2_linux_amd64.tar.gz
tar -xvf kubectl-ray_v1.2.2_linux_amd64.tar.gz
cp kubectl-ray ~/.local/bin
```

Replace `~/.local/bin` with the directory in your `PATH`.

### Compiling from source

1. Run `go build cmd/kubectl-ray.go`
2. Move the binary, which will be named `kubectl-ray` to your `PATH`

## Shell Completion

1. Install [kubectl plugin-completion](https://github.com/marckhouzam/kubectl-plugin_completion) plugin.
2. Run `kubectl plugin-completion generate`.
3. Add `$HOME/.kubectl-plugin-completion` to `PATH` in your shell profile.
