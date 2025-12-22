FROM --platform=$BUILDPLATFORM golang:1.25.1 AS builder

ENV GOPROXY=https://proxy.golang.org,direct
WORKDIR /historyserver

# Copy the go modules and manifests.
COPY go.mod go.mod
COPY go.sum go.sum
# Cache dependencies to avoid re-downloading when only sources change.
RUN go mod download

# Copy the go source.
COPY cmd/collector/main.go cmd/collector/main.go
COPY pkg/collector/ pkg/collector/
COPY pkg/storage/ pkg/storage/
COPY pkg/utils/ pkg/utils/

# Build the collector binary.
COPY Makefile Makefile
RUN make build

FROM ubuntu:22.04
RUN apt-get update && apt-get upgrade -y && rm -rf /var/cache/apt/ && apt-get install -y ca-certificates
COPY --from=builder /historyserver/output/bin/collector /usr/local/bin/collector

ENTRYPOINT ["/usr/local/bin/collector"]
