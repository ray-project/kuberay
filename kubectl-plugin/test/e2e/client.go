package e2e

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	rayclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
)

type Client interface {
	Core() kubernetes.Interface
	Ray() rayclient.Interface
	Config() rest.Config
}

type testClient struct {
	core   kubernetes.Interface
	ray    rayclient.Interface
	config rest.Config
}

var _ Client = (*testClient)(nil)

func (t *testClient) Core() kubernetes.Interface {
	return t.core
}

func (t *testClient) Ray() rayclient.Interface {
	return t.ray
}

func (t *testClient) Config() rest.Config {
	return t.config
}

func newTestClient() (Client, error) {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	rayClient, err := rayclient.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &testClient{
		core:   kubeClient,
		ray:    rayClient,
		config: *cfg,
	}, nil
}
