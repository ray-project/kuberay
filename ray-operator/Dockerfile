# Build the manager binary
FROM golang:1.22.4-bullseye as builder

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

# Build
USER root
RUN CGO_ENABLED=1 GOOS=linux go build -tags strictfipsruntime -a -o manager main.go

FROM gcr.io/distroless/base-debian11:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
