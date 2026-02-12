# Google Cloud Storage (GCS)

This module is the writer and reader for GCS storage.

It is required for the GKE Cluster running Ray to have workload identity (WI), to setup WI, please follow:
[How-to: Workload Identity](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/workload-identity)

To use it with the History Server, set `--runtime-class-name=gcs`.

```yaml
apiVersion: apps/v1
kind: Deployment
...
spec:
  ...
  template:
    ...
    spec:
      serviceAccountName: historyserver
      containers:
      - name: historyserver
        env:
          - name: GCS_BUCKET
            value: "<GCS_BUCKET_NAME>"
        image: ray-historyserver:v0.1.0
        imagePullPolicy: Always
        command:
        - historyserver
        - --runtime-class-name=gcs
        - --ray-root-dir=log
        ports:
        - containerPort: 8080
        resources:
          limits:
            cpu: "500m"
```

RayCluster will also have the following under both the Rworker and head collector spec

```yaml
  - name: collector
    image: ray-collector:v0.1.0
    imagePullPolicy: Always
    env:
    - name: GCS_BUCKET
      value: "<GCS_BUCKET_NAME>"
      command:
      - collector
      - --role=Headx
      - --runtime-class-name=gcs
      - --ray-cluster-name=raycluster-historyserver
      - --ray-root-dir=log
      - --events-port=8084
```
