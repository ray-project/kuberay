# Google Cloud Storage (GCS)

This module is the writer and reader for GCS storage.

It is required for the GKE Cluster running Ray to have workload identity, to setup WI, please follow:
[How-to: Workload Identity](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/workload-identity)

To use it with the History Server, set `--runtime-class-name=gcs`.
