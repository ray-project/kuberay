package historyserver

import (
	"context"
	"fmt"
	"strings"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

// GetAuthToken retrieves the auth token from the RayCluster's secret.
// Returns empty string if auth is not enabled or secret not found.
func (c *ClientManager) GetAuthToken(ctx context.Context, clusterName, clusterNamespace string) (string, error) {
	if len(c.clients) == 0 {
		return "", fmt.Errorf("no Kubernetes client available")
	}

	// Currently only one kubeconfig is supported (enforced in NewClientManager)
	client := c.clients[0]

	// First, check if the RayCluster has auth enabled
	rayCluster := &rayv1.RayCluster{}
	err := client.Get(ctx, types.NamespacedName{Namespace: clusterNamespace, Name: clusterName}, rayCluster)
	if err != nil {
		return "", fmt.Errorf("failed to get RayCluster %s/%s: %w", clusterNamespace, clusterName, err)
	}

	// Check if auth is enabled
	if rayCluster.Spec.AuthOptions == nil || rayCluster.Spec.AuthOptions.Mode != rayv1.AuthModeToken {
		logrus.Debugf("Auth not enabled for RayCluster %s/%s", clusterNamespace, clusterName)
		return "", nil
	}

	// Fetch the secret containing the auth token
	secret := &corev1.Secret{}
	err = client.Get(ctx, types.NamespacedName{Namespace: clusterNamespace, Name: clusterName}, secret)
	if err != nil {
		return "", fmt.Errorf("failed to get auth secret %s/%s: %w", clusterNamespace, clusterName, err)
	}

	// Extract the token from the secret
	tokenBytes, exists := secret.Data["auth_token"]
	if !exists {
		return "", fmt.Errorf("auth_token key not found in secret %s/%s", clusterNamespace, clusterName)
	}

	return string(tokenBytes), nil
}

func NewClientManager(kubeconfigs string, useKubernetesProxy bool) (*ClientManager, error) {
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

		c, err := clientcmd.BuildConfigFromFlags("", stringList[0])
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
		}
		c.QPS = 50
		c.Burst = 100
		kubeconfigList = append(kubeconfigList, c)
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
				return nil, fmt.Errorf("failed to load default kubeconfig in Kubernetes proxy mode: %w", err)
			}
		} else {
			c, err = rest.InClusterConfig()
			if err != nil {
				return nil, fmt.Errorf("failed to build config from in-cluster kubeconfig: %w", err)
			}
		}
		c.QPS = 50
		c.Burst = 100
		kubeconfigList = append(kubeconfigList, c)
	}
	scheme := runtime.NewScheme()
	// Registered for the type v1.Secret to fetch auth token for RayCluster with auth enabled.
	utilruntime.Must(corev1.AddToScheme(scheme))

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
