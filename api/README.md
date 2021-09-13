# API

This folder contains api definition written in protocol buffer. The api will be used for building services.

## Prerequisite

In order to run generation commands successfully, please make sure you have following tools installed in your environment. 

- [Make](https://man7.org/linux/man-pages/man1/make.1.html)
- [Docker](https://www.docker.com/)

Currently, docker isnot being used for code generation. This is in the roadmap.
User still have to install protoc, etc to successfully generate the code.

```
# 1. Install Protocol Buffer

$ brew install protobuf
$ protoc --version  # Ensure compiler version is 3+

# 2. Install go plugin for protoc and grpc

$ go get -u google.golang.org/grpc
$ go get -u github.com/golang/protobuf/protoc-gen-go

# 3. Install grpc gateway and openapi plugin for protoc 

$ go get -u github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway
$ go get -u github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2 

```

## Auto Generation

### Go Client and Swagger

```
make generate
```

### Clients

We postpone the client generation until there's external service needs for communication. 

### API reference documentation

Use the tools [bootprint-openapi](https://github.com/bootprint/bootprint-monorepo/tree/master/packages/bootprint-openapi) 
and [html-inline](https://github.com/substack/html-inline) to generate the API reference from the swagger files.

### Third Party Protos

Third party proto dependencies are sync back to `api/third_party` for easy development (IDE friendly).
Actually, we should specify the directory in which to search for imports instead.

```
protoc -I. 
    -I/go/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis \
    -I/go/src/github.com/grpc-ecosystem/grpc-gateway/ \
    -I/go/src/github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger/options/ \
``` 

Source: 
- [googleapis](https://github.com/googleapis/googleapis/tree/master/google/api)
- [protoc-gen-openapiv2](https://github.com/grpc-ecosystem/grpc-gateway/tree/master/protoc-gen-openapiv2/options)

