package manager

import (
	"time"

	"github.com/ray-project/kuberay/backend/pkg/client"
	"github.com/ray-project/kuberay/backend/pkg/util"
	"k8s.io/klog/v2"
)

type ClientManagerInterface interface {
	ClusterClient() client.ClusterClientInterface
	KubernetesClient() client.KubernetesClientInterface
	Time() util.TimeInterface
	UUID() util.UUIDGeneratorInterface
}

// Container for all service clients
type ClientManager struct {
	// Kubernetes clients
	clusterClient    client.ClusterClientInterface
	kubernetesClient client.KubernetesClientInterface
	// auxiliary tools
	time util.TimeInterface
	uuid util.UUIDGeneratorInterface
}

func (c *ClientManager) ClusterClient() client.ClusterClientInterface {
	return c.clusterClient
}

func (c *ClientManager) KubernetesClient() client.KubernetesClientInterface {
	return c.kubernetesClient
}

func (c *ClientManager) Time() util.TimeInterface {
	return c.time
}

func (c *ClientManager) UUID() util.UUIDGeneratorInterface {
	return c.uuid
}

func (c *ClientManager) init() {
	// db, kubernetes initialization
	klog.Info("Initializing client manager")

	// configure configs
	initConnectionTimeout := 15 * time.Second
	defaultKubernetesClientConfig := util.ClientOptions{
		QPS:   5,
		Burst: 10,
	}

	// 1. utils initialization
	c.time = util.NewRealTime()
	c.uuid = util.NewUUIDGenerator()

	// TODO: Potentially, we may need storage layer clients to help persist the data.

	// 2. kubernetes client initialization
	c.clusterClient = client.NewRayClusterClientOrFatal(initConnectionTimeout, defaultKubernetesClientConfig)
	c.kubernetesClient = client.CreateKubernetesCoreOrFatal(initConnectionTimeout, defaultKubernetesClientConfig)

	klog.Infof("Client manager initialized successfully")
}

func NewClientManager() ClientManager {
	clientManager := ClientManager{}
	clientManager.init()

	return clientManager
}
