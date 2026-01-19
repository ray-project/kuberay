package historyserver

import (
	"context"
	"strings"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClientManager struct {
	configs []*rest.Config
	clients []client.Client
}

func (c *ClientManager) ListRayClusters(ctx context.Context) ([]*rayv1.RayCluster, error) {
	list := []*rayv1.RayCluster{}
	for _, c := range c.clients {
		listOfRayCluster := rayv1.RayClusterList{}
		err := c.List(ctx, &listOfRayCluster)
		if err != nil {
			logrus.Errorf("Failed to list RayClusters: %v", err)
			continue
		}
		for _, rayCluster := range listOfRayCluster.Items {
			list = append(list, &rayCluster)
		}
	}
	return list, nil
}

func NewClientManager(kubeconfigs string, useKubernetesProxy bool) *ClientManager {
	kubeconfigList := []*rest.Config{}
	if len(kubeconfigs) > 0 {
		stringList := strings.Split(kubeconfigs, ",")
		if len(stringList) > 1 {
			// historyserver is able to get query from live gcs, which is not safe.
			// we hope to replace these apis with one events.
			logrus.Errorf("Only one kubeconfig is supported.")
		}
		for _, kubeconfig := range stringList {
			if kubeconfig != "" {
				c, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
				if err != nil {
					logrus.Errorf("Failed to build config from kubeconfig: %v", err)
					continue
				}
				c.QPS = 50
				c.Burst = 100
				kubeconfigList = append(kubeconfigList, c)
				logrus.Infof("add config from path: %v", kubeconfig)
				break
			}
		}
	} else {
		var c *rest.Config
		var err error
		if useKubernetesProxy {
			// Load Kubernetes REST config from default kubeconfig locations (KUBECONFIG environment variable or ~/.kube/config)
			// without interactive prompts.
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			configOverrides := &clientcmd.ConfigOverrides{}
			clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
			c, err = clientConfig.ClientConfig()
			if err != nil {
				logrus.Errorf("Failed to load default kubeconfig in Kubernetes proxy mode: %v", err)
			}
		} else {
			c, err = rest.InClusterConfig()
			if err != nil {
				logrus.Errorf("Failed to build config from in-cluster kubeconfig: %v", err)
			}
		}
		c.QPS = 50
		c.Burst = 100
		kubeconfigList = append(kubeconfigList, c)
	}
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
	logrus.Infof("create client manager successfully, clients: %v", len(clientList))
	return &ClientManager{
		configs: kubeconfigList,
		clients: clientList,
	}
}
