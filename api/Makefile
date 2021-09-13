# Makefile to generate api clients from proto.

OUTPUT_MODE=import
TMP_OUTPUT=/tmp

.PHONY: generate
generate:
	mkdir -p go_client && mkdir -p swagger && \
	protoc -I ./ \
		-I ./third_party/ --go_out ${TMP_OUTPUT} --go_opt paths=${OUTPUT_MODE} \
		--go-grpc_out ${TMP_OUTPUT} --go-grpc_opt paths=${OUTPUT_MODE} \
		--grpc-gateway_out ${TMP_OUTPUT}  --grpc-gateway_opt paths=${OUTPUT_MODE} \
		--openapiv2_opt logtostderr=true --openapiv2_out=:swagger ./*.proto && \
	cp ${TMP_OUTPUT}/github.com/ray-project/kuberay/api/go_client/* ./go_client

.PHONY: clean
clean:
	rm -rf go_client && rm -rf swagger
