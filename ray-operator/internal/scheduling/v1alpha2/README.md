# Kubernetes scheduling v1alpha2 compatibility types

Kubernetes 1.37 removed the `scheduling.k8s.io/v1alpha2` Go types from
`k8s.io/api`. KubeRay still supports Workload Aware Scheduling on Kubernetes
1.36 clusters, where the Kubernetes Workload API uses this version. The
operator therefore needs these types even though it builds with the Kubernetes
1.37 client libraries.

This directory contains a frozen copy of the following files from
`k8s.io/api/scheduling/v1alpha2` at `v0.36.0`:

- `doc.go`
- `register.go`
- `types.go`
- `zz_generated.deepcopy.go`

Only the runtime types, scheme registration, and deepcopy implementations are
vendored. Protobuf, OpenAPI, and generated client files are intentionally
omitted. KubeRay accesses these resources through the dynamic client because
the Kubernetes 1.37 clientset no longer includes a typed v1alpha2 client.

Treat this package as compatibility code. Do not add new API fields or
regenerate it from the current Kubernetes dependencies. Any required update
should be compared against the Kubernetes 1.36 API definitions and must
preserve compatibility with Kubernetes 1.36 clusters.
