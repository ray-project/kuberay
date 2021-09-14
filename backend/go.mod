module github.com/ray-project/kuberay/backend

go 1.15

require (
	github.com/cenkalti/backoff v2.2.1+incompatible
	github.com/go-openapi/runtime v0.19.31
	github.com/golang/glog v1.0.0
	github.com/golang/protobuf v1.5.2
	github.com/google/uuid v1.3.0
	github.com/grpc-ecosystem/grpc-gateway v1.16.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	github.com/ray-project/kuberay/api v0.0.0
	github.com/ray-project/kuberay/ray-operator v0.0.0
	google.golang.org/grpc v1.40.0
	google.golang.org/protobuf v1.27.1
	k8s.io/api v0.19.14
	k8s.io/apimachinery v0.19.14
	k8s.io/client-go v0.19.14
	k8s.io/klog/v2 v2.20.0
)

replace (
	github.com/ray-project/kuberay/api => ../api
	github.com/ray-project/kuberay/ray-operator => ../ray-operator
)
