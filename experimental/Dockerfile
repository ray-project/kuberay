# Build security proxy
FROM golang:1.23.6-bullseye AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY ./ ./

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o security_proxy cmd/main.go

FROM scratch
WORKDIR /workspace

COPY --from=builder /workspace/security_proxy /usr/local/bin/security_proxy

ENTRYPOINT ["/usr/local/bin/security_proxy"]
