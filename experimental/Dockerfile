# Build security proxy
FROM golang:1.24.2-bullseye AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.sum ./

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY experimental/ experimental/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o experimental cmd/main.go

FROM scratch
WORKDIR /workspace

COPY --from=builder /workspace/experimental /usr/local/bin/experimental

ENTRYPOINT ["/usr/local/bin/experimental"]
