package manager

import (
	"time"

	"github.com/ray-project/kuberay/apiserver/pkg/client"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	klog "k8s.io/klog/v2"
)

type ClientManagerInterface interface {
	ClusterClient() client.ClusterClientInterface
	JobClient() client.JobClientInterface
	ServiceClient() client.ServiceClientInterface
	KubernetesClient() client.KubernetesClientInterface
	Time() util.TimeInterface
	GetConnectionConfig() (time.Duration, util.ClientOptions)
}

// Container for all service clients
type ClientManager struct {
	// Kubernetes clients
	clusterClient    client.ClusterClientInterface
	jobClient        client.JobClientInterface
	serviceClient    client.ServiceClientInterface
	kubernetesClient client.KubernetesClientInterface
	// auxiliary tools
	time                          util.TimeInterface
	initConnectionTimeout         time.Duration
	defaultKubernetesClientConfig util.ClientOptions
}

func (c *ClientManager) ClusterClient() client.ClusterClientInterface {
	return c.clusterClient
}

func (c *ClientManager) JobClient() client.JobClientInterface {
	return c.jobClient
}

func (c *ClientManager) ServiceClient() client.ServiceClientInterface {
	return c.serviceClient
}

func (c *ClientManager) KubernetesClient() client.KubernetesClientInterface {
	return c.kubernetesClient
}

func (c *ClientManager) Time() util.TimeInterface {
	return c.time
}

func (c *ClientManager) init() {
	// db, kubernetes initialization
	klog.Info("Initializing client manager")

	// configure configs
	c.initConnectionTimeout = 15 * time.Second
	c.defaultKubernetesClientConfig = util.ClientOptions{
		QPS:   5,
		Burst: 10,
	}

	// 1. utils initialization
	c.time = util.NewRealTime()

	// TODO: Potentially, we may need storage layer clients to help persist the data.
	// 2. kubernetes client initialization
	c.clusterClient = client.NewRayClusterClientOrFatal(c.initConnectionTimeout, c.defaultKubernetesClientConfig)
	c.jobClient = client.NewRayJobClientOrFatal(c.initConnectionTimeout, c.defaultKubernetesClientConfig)
	c.serviceClient = client.NewRayServiceClientOrFatal(c.initConnectionTimeout, c.defaultKubernetesClientConfig)
	c.kubernetesClient = client.CreateKubernetesCoreOrFatal(c.initConnectionTimeout, c.defaultKubernetesClientConfig)

	klog.Infof("Client manager initialized successfully")
}

func NewClientManager() ClientManager {
	clientManager := ClientManager{}
	clientManager.init()

	return clientManager
}

func (c *ClientManager) GetConnectionConfig() (time.Duration, util.ClientOptions) {
	return c.initConnectionTimeout, c.defaultKubernetesClientConfig
}
