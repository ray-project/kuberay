# Build security proxy
FROM registry.access.redhat.com/ubi8/go-toolset:1.19.13 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY ../apiserver/go.mod go.sum ./
# Copy the go source
COPY ../apiserver/cmd cmd/
COPY ../apiserver/pkg pkg/

# We are only using a subset of the libraries in go.mod, so do not load it upfront

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o security_proxy cmd/proxy/main.go

FROM scratch
WORKDIR /workspace

COPY --from=builder /workspace/security_proxy /usr/local/bin/security_proxy

ENTRYPOINT ["/usr/local/bin/security_proxy"]
