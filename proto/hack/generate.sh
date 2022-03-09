#!/bin/bash

set -ex

export TMP_OUTPUT=/tmp
export OUTPUT_MODE=import

# Change directory.
cd /go/src/github.com/ray-project/kuberay/proto

# Delete currently generated code and create new folder.
rm -rf ./go_client/ ./swagger && mkdir -p ./go_client && mkdir -p ./swagger

protoc -I. \
  -I ./third_party/ --go_out ${TMP_OUTPUT} --go_opt paths=${OUTPUT_MODE} \
  --go-grpc_out ${TMP_OUTPUT} --go-grpc_opt paths=${OUTPUT_MODE} \
  --grpc-gateway_out ${TMP_OUTPUT}  --grpc-gateway_opt paths=${OUTPUT_MODE} \
  --openapiv2_opt logtostderr=true --openapiv2_out=:swagger ./*.proto

# Move *.pb.go and *.gw.go to go_client folder.
cp ${TMP_OUTPUT}/github.com/ray-project/kuberay/proto/go_client/* ./go_client
