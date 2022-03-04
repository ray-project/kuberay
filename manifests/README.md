# Install KubeRay using Kustomize Manifests

This folder contains KubeRay Kustomize manifests.

## Overview

User facing manifest entrypoints are `cluster-scoped-resources` package, `base` and `overlay` package.

- `cluster-scoped-resources` should collect all cluster-scoped resources.
- `base` should collect base manifest 
- `overlay` should collect specific namespace-scoped resources.
