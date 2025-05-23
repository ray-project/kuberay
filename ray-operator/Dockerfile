# Build the manager binary
FROM golang:1.24.2-bullseye AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.sum ./
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY ray-operator/main.go ray-operator/main.go
COPY ray-operator/apis/ ray-operator/apis/
COPY ray-operator/controllers/ ray-operator/controllers/
COPY ray-operator/pkg/features ray-operator/pkg/features
COPY ray-operator/pkg/utils ray-operator/pkg/utils
COPY ray-operator/pkg/webhooks ray-operator/pkg/webhooks

WORKDIR /workspace/ray-operator

# Build
USER root
RUN CGO_ENABLED=1 GOOS=linux go build -tags strictfipsruntime -a -o manager main.go

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /
COPY --from=builder /workspace/ray-operator/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
