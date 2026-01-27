# KubeRay History Server documentation

> [!NOTE]
> The KubeRay History Server is under active development. This directory contains
> guides to set up and run the History Server components locally for development
> and testing.

## Overview

The KubeRay History Server collects, stores, and displays historical logs and
metadata from Ray clusters. It has two parts:

1. **Collector**: A sidecar container that runs alongside Ray nodes to collect
   logs and metadata, then uploads them to blob storage.
2. **History Server**: A central service that reads data from blob storage and
   provides a web interface to explore the history of Ray jobs, tasks, actors,
   and other cluster activities.

## Guides

| Guide | Description |
|-------|-------------|
| [Collector setup](set_up_collector.md) | How to set up the Collector on a Kind cluster with MinIO or Azure storage |
| [History Server setup](set_up_historyserver.md) | Quick start guide for deploying and using the History Server with API examples |

## Supported storage backends

The History Server supports multiple storage backends:

| Backend | Description | Configuration |
|---------|-------------|---------------|
| S3/MinIO | AWS S3 or MinIO-compatible storage | Use `--runtime-class-name=s3` |
| Azure Blob Storage | Microsoft Azure Blob Storage | Use `--runtime-class-name=azureblob` |
| Aliyun OSS | Alibaba Cloud Object Storage Service | Use `--runtime-class-name=aliyunoss` |
| Local test | For local testing and development | Use `--runtime-class-name=localtest` |

## Running locally

### Prerequisites

- Go v1.24+
- Docker
- Kind
- kubectl
- GNU Make

### Quick start

1. **Set up the environment**: Follow the [Collector setup guide](set_up_collector.md)
   to spin up a Kind cluster, deploy the KubeRay operator, and configure storage.

2. **Build the components**:

   ```bash
   # Build both collector and history server images
   make -C historyserver localimage-build

   # Or build individually
   make -C historyserver localimage-collector
   make -C historyserver localimage-historyserver
   ```

3. **Load images into Kind**:

   ```bash
   kind load docker-image collector:v0.1.0
   kind load docker-image historyserver:v0.1.0
   ```

4. **Deploy and test**: Follow the [History Server setup guide](set_up_historyserver.md)
   to deploy the Ray cluster, submit jobs, and access the History Server API.

## Configuration files

Sample configs are in the `config/` directory:

| File | Description |
|------|-------------|
| `minio.yaml` | MinIO deployment for S3-compatible storage |
| `azurite.yaml` | Azurite deployment for Azure Blob Storage emulation |
| `raycluster.yaml` | Ray cluster with collector sidecar (S3/MinIO) |
| `raycluster-azureblob.yaml` | Ray cluster with collector sidecar (Azure Blob) |
| `rayjob.yaml` | Sample Ray job for testing |
| `historyserver.yaml` | History Server deployment |
| `service_account.yaml` | Service account for History Server |

## Additional resources

- [REP: Ray History Server #62](https://github.com/ray-project/enhancements/pull/62)
- [Design doc](https://docs.google.com/document/d/15Y2bW4uzeUJe84FxRNUnHozoQPqYdLB2yLmgrdF2ZmI/edit?pli=1&tab=t.0#heading=h.xrvvvqarib6g)
- [Slack channel: #ray-history-server](https://app.slack.com/client/TN4768NRM/C09QLLU8HTL)
