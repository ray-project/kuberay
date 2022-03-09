# ProtoBuf Definitions

This folder contains api definition written in protocol buffer. The api will be used for building services.

## Prerequisite

In order to run generation commands successfully, please make sure you have following tools installed in your environment. 

- [Make](https://man7.org/linux/man-pages/man1/make.1.html)
- [Docker](https://www.docker.com/)

Run following command to build proto-generator image. We will use it to generate apis.

> Note: you can actually skip this step, and it will download a prebuilt image instead.

```
make build-image
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

Third party proto dependencies are sync back to `proto/third_party` for easy development (IDE friendly).
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
