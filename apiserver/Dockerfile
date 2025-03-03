# Build the backend service
FROM golang:1.23.6-bullseye AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY apiserver/go.mod apiserver/go.mod
COPY apiserver/go.sum apiserver/go.sum

# Copy the go source
COPY proto/ proto/
COPY ray-operator/ ray-operator/
COPY apiserver/ apiserver/

WORKDIR /workspace/apiserver
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Build
USER root
WORKDIR /workspace/apiserver
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o kuberay-apiserver cmd/main.go

FROM scratch
WORKDIR /workspace
COPY --from=builder /workspace/apiserver/kuberay-apiserver apiserver/
# Support serving swagger files
COPY proto/ proto/
USER 65532:65532

ENTRYPOINT ["/workspace/apiserver/kuberay-apiserver"]
