# RayJob Batch Inference Example

This page demonstrates how to use the RayJob custom resource to run a batch inference job on a Ray cluster.

We will use an image classification workload.  The example is based on <https://docs.ray.io/en/latest/data/examples/huggingface_vit_batch_prediction.html>. Please see that page for a full explanation of the code.

## Prerequisites

You must have a Kubernetes cluster running and `kubectl` configured to use it.  It is useful but not necessary to have GPU nodes available.

## Deploy KubeRay

Make sure your KubeRay operator version is at least v0.6.0.

The latest released KubeRay version is recommended.

For installation instructions, please follow [the documentation](../deploy/installation.md).

