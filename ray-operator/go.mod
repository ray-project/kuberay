module github.com/ray-project/kuberay/ray-operator

go 1.15

require (
	github.com/go-logr/logr v0.3.0
	github.com/google/uuid v1.1.2 // indirect
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/sirupsen/logrus v1.6.0
	github.com/stretchr/testify v1.7.0
	google.golang.org/grpc v1.42.0
	google.golang.org/protobuf v1.25.0
	k8s.io/api v0.19.14
	k8s.io/apimachinery v0.19.14
	k8s.io/client-go v0.19.14
	k8s.io/code-generator v0.19.14
	k8s.io/utils v0.0.0-20200912215256-4140de9c8800
	sigs.k8s.io/controller-runtime v0.7.2
)

replace github.com/ray-project/kuberay/ray-operator/rpc => ./rpc
