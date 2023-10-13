package client

import (
	"time"

	mcadClient "github.com/project-codeflare/multi-cluster-app-dispatcher/pkg/client/clientset/versioned/typed/controller/v1beta1"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type MCADClientInterface interface {
	MCADClient(namespace string) mcadClient.AppWrapperInterface
}

type MCADClient struct {
	client *mcadClient.WorkloadV1beta1Client
}

func (cc MCADClient) MCADClient(namespace string) mcadClient.AppWrapperInterface {
	return cc.client.AppWrappers(namespace)
}

func NewMCADClientOrFatal(initConnectionTimeout time.Duration, options util.ClientOptions) MCADClientInterface {
	cfg, err := config.GetConfig()
	if err != nil {
		klog.Fatalf("Failed to create RayService client. Error: %v", err)
	}
	cfg.QPS = options.QPS
	cfg.Burst = options.Burst

	workloadClient := mcadClient.NewForConfigOrDie(cfg)
	return &MCADClient{client: workloadClient}
}
