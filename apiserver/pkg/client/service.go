package client

import (
	"time"

	"k8s.io/klog/v2"

	"github.com/ray-project/kuberay/apiserver/pkg/util"
	rayclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/typed/ray/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type ServiceClientInterface interface {
	RayServiceClient(namespace string) rayiov1alpha1.RayServiceInterface
}

type RayServiceClient struct {
	client rayiov1alpha1.RayV1alpha1Interface
}

func (cc RayServiceClient) RayServiceClient(namespace string) rayiov1alpha1.RayServiceInterface {
	return cc.client.RayServices(namespace)
}

func NewRayServiceClientOrFatal(initConnectionTimeout time.Duration, options util.ClientOptions) ServiceClientInterface {
	cfg, err := config.GetConfig()
	if err != nil {
		klog.Fatalf("Failed to create RayService client. Error: %v", err)
	}
	cfg.QPS = options.QPS
	cfg.Burst = options.Burst

	rayServiceClient := rayclient.NewForConfigOrDie(cfg).RayV1alpha1()
	return &RayServiceClient{client: rayServiceClient}
}
