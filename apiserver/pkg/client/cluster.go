package client

import (
	"time"

	"github.com/golang/glog"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	rayclusterclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/typed/raycluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type ClusterClientInterface interface {
	RayClusterClient(namespace string) rayiov1alpha1.RayClusterInterface
}

type RayClusterClient struct {
	client rayiov1alpha1.RayV1alpha1Interface
}

func (cc RayClusterClient) RayClusterClient(namespace string) rayiov1alpha1.RayClusterInterface {
	return cc.client.RayClusters(namespace)
}

func NewRayClusterClientOrFatal(initConnectionTimeout time.Duration, options util.ClientOptions) ClusterClientInterface {
	cfg, err := config.GetConfig()
	if err != nil {
		glog.Fatalf("Failed to create RayCluster client. Error: %v", err)
	}
	cfg.QPS = options.QPS
	cfg.Burst = options.Burst

	rayClusterClient := rayclusterclient.NewForConfigOrDie(cfg).RayV1alpha1()
	return &RayClusterClient{client: rayClusterClient}
}
