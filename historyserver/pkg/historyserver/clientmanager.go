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
	configs []*rest.Config
	clients []client.Client
}

// Client returns the primary controller-runtime client.
func (c *ClientManager) Client() client.Client {
	return c.clients[0]
}

func (c *ClientManager) ListRayClusters(ctx context.Context) ([]*rayv1.RayCluster, error) {
	list := []*rayv1.RayCluster{}
	var errs []error
	for _, cl := range c.clients {
		listOfRayCluster := rayv1.RayClusterList{}
		err := cl.List(ctx, &listOfRayCluster)
		if err != nil {
			logrus.Errorf("Failed to list RayClusters: %v", err)
			errs = append(errs, err)
			continue
		}
		for _, rayCluster := range listOfRayCluster.Items {
			list = append(list, &rayCluster)
		}
	}
	// If every client failed, return an error so callers can distinguish
	// "no clusters exist" from "API unreachable".
	if len(errs) > 0 && len(list) == 0 && len(errs) == len(c.clients) {
		return list, fmt.Errorf("all %d client(s) failed to list RayClusters", len(errs))
	}
	return list, nil
}

type ClientManagerConfig struct {
	Kubeconfigs        string
	UseKubernetesProxy bool
	QPS                float32
	Burst              int
}

func NewClientManager(cfg ClientManagerConfig) (*ClientManager, error) {
	kubeconfigs := cfg.Kubeconfigs

	var c *rest.Config
	var err error
	kubeconfigList := []*rest.Config{}
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

		c, err = clientcmd.BuildConfigFromFlags("", stringList[0])
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
			c, err = clientConfig.ClientConfig()
			if err != nil {
				return nil, fmt.Errorf("failed to load default kubeconfig in Kubernetes proxy mode: %w", err)
			}
		} else {
			c, err = rest.InClusterConfig()
			if err != nil {
				return nil, fmt.Errorf("failed to build config from in-cluster kubeconfig: %w", err)
			}
		}
	}
	c.QPS = cfg.QPS
	c.Burst = cfg.Burst
	kubeconfigList = append(kubeconfigList, c)

	scheme := runtime.NewScheme()
	utilruntime.Must(rayv1.AddToScheme(scheme))
	clientList := []client.Client{}
	for _, config := range kubeconfigList {
		c, err := client.New(config, client.Options{
			Scheme: scheme,
		})
		if err != nil {
			logrus.Errorf("Failed to create client: %v", err)
			continue
		}
		clientList = append(clientList, c)
	}

	if len(clientList) == 0 {
		return nil, fmt.Errorf("failed to create any client")
	}

	logrus.Infof("create client manager successfully, clients: %v", len(clientList))
	clientManager := &ClientManager{
		configs: kubeconfigList,
		clients: clientList,
	}
	return clientManager, nil
}
