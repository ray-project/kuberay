# GCS Bucket Access

This module is the reader and writer for Google Cloud Storage (GCS)

To properly use GCS, please set up workload identity. Workload identity removes the need for any local key files.

To use, set `--runtime-class-name=gcs`
