package historyserver

import (
	"context"
	"fmt"
	"strings"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Aligned with ray-operator defaults:
	// https://github.com/ray-project/kuberay/blob/178e6c91/ray-operator/apis/config/v1alpha1/defaults.go#L12-L13
	DefaultKubeAPIQPS   = float64(100)
	DefaultKubeAPIBurst = 200
)

type ClientManager struct {
	config *rest.Config
	client client.Client
}

// Client returns the controller-runtime client.
func (c *ClientManager) Client() client.Client {
	return c.client
}

func (c *ClientManager) ListRayClusters(ctx context.Context) ([]*rayv1.RayCluster, error) {
	var clusterList rayv1.RayClusterList
	if err := c.client.List(ctx, &clusterList); err != nil {
		logrus.Errorf("Failed to list RayClusters: %v", err)
		return nil, err
	}
	clusters := make([]*rayv1.RayCluster, 0, len(clusterList.Items))
	for i := range clusterList.Items {
		clusters = append(clusters, &clusterList.Items[i])
	}
	return clusters, nil
}

func (c *ClientManager) GetRayCluster(ctx context.Context, namespace, name string) (*rayv1.RayCluster, error) {
	var rayCluster rayv1.RayCluster
	if err := c.Client().Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &rayCluster); err != nil {
		return nil, err
	}
	return &rayCluster, nil
}

type ClientManagerConfig struct {
	Kubeconfigs        string
	UseKubernetesProxy bool
	QPS                float32
	Burst              int
}

func NewClientManager(cfg ClientManagerConfig) (*ClientManager, error) {
	kubeconfigs := cfg.Kubeconfigs

	var restConfig *rest.Config
	var err error
	if len(kubeconfigs) > 0 {
		stringList := strings.Split(kubeconfigs, ",")
		if len(stringList) > 1 {
			// historyserver is able to get query from live gcs, which is not safe.
			// we hope to replace these apis with one events.
			return nil, fmt.Errorf("only one kubeconfig is supported")
		}

		if stringList[0] == "" {
			return nil, fmt.Errorf("kubeconfig is empty")
		}

		restConfig, err = clientcmd.BuildConfigFromFlags("", stringList[0])
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
		}
	} else {
		if cfg.UseKubernetesProxy {
			// Load Kubernetes REST config from default kubeconfig locations (KUBECONFIG environment variable or ~/.kube/config)
			// without interactive prompts.
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			configOverrides := &clientcmd.ConfigOverrides{}
			clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
			restConfig, err = clientConfig.ClientConfig()
			if err != nil {
				return nil, fmt.Errorf("failed to load default kubeconfig in Kubernetes proxy mode: %w", err)
			}
		} else {
			restConfig, err = rest.InClusterConfig()
			if err != nil {
				return nil, fmt.Errorf("failed to build config from in-cluster kubeconfig: %w", err)
			}
		}
	}
	restConfig.QPS = cfg.QPS
	restConfig.Burst = cfg.Burst

	scheme := runtime.NewScheme()
	utilruntime.Must(rayv1.AddToScheme(scheme))
	cli, err := client.New(restConfig, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	logrus.Info("create client manager successfully")
	return &ClientManager{
		config: restConfig,
		client: cli,
	}, nil
}
