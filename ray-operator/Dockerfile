# Build the manager binary
FROM golang:1.17 as builder

# DELETE

# Proxy Configuration
ENV http_proxy 'http://proxy-zsgov.external.lmco.com:80'
ENV https_proxy 'http://proxy-zsgov.external.lmco.com:80'
ENV no_proxy '.lmco.com,.local,localhost,172.16.0.0/12,192.168.0.0/16'

ENV HTTP_PROXY 'http://proxy-zsgov.external.lmco.com:80'
ENV HTTPS_PROXY 'http://proxy-zsgov.external.lmco.com:80'
ENV NO_PROXY '.lmco.com,.local,localhost,172.16.0.0/12,192.168.0.0/16'


# Fetch the LM CA and install the cert
ADD https://crl.external.lmco.com/trust/pem/combined/Combined_pem.pem /usr/local/share/ca-certificates/
RUN mv /usr/local/share/ca-certificates/Combined_pem.pem /usr/local/share/ca-certificates/lm_ca_combined.crt && update-ca-certificates



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
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
