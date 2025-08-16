# Build the manager binary
FROM golang:1.24.0-bullseye AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY apis/ apis/
COPY controllers/ controllers/
COPY pkg/features pkg/features
COPY pkg/utils pkg/utils
COPY pkg/webhooks pkg/webhooks
COPY rayjobsubmitter/ rayjobsubmitter/

# Build
USER root
RUN CGO_ENABLED=1 GOOS=linux go build -tags strictfipsruntime -a -o manager main.go
# Build the submitter binary with -ldflags="-w -s" to reduce the size of the binary.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s"  -trimpath -a -o submitter ./rayjobsubmitter/cmd/main.go

FROM scratch as submitter
WORKDIR /
COPY --from=builder /workspace/submitter .
USER 65532:65532

ENTRYPOINT ["/submitter"]

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
