package historyserver

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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

const (
	// AuthTokenSecretKey is the key used to store the auth token in a Kubernetes Secret
	AuthTokenSecretKey = "auth_token"
	// authTokenCacheTTL is how long a cached token is considered valid before re-fetching from K8s
	authTokenCacheTTL = 5 * time.Minute
)

// cachedToken holds an auth token along with its expiry time
type cachedToken struct {
	token     string
	expiresAt time.Time
}

type ClientManager struct {
	configs    []*rest.Config
	clients    []client.Client
	tokenCache map[string]cachedToken
	mu         sync.RWMutex
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

// GetAuthTokenForRayCluster uses a pre-fetched RayCluster to avoid an extra GET.
// Returns empty string if auth is not enabled; otherwise returns an error when token retrieval fails.
// Tokens are cached for authTokenCacheTTL to avoid hitting the K8s API on every request
func (c *ClientManager) GetAuthTokenForRayCluster(ctx context.Context, rayCluster *rayv1.RayCluster) (string, error) {
	if len(c.clients) == 0 {
		return "", fmt.Errorf("no Kubernetes client available")
	}
	if rayCluster == nil {
		return "", fmt.Errorf("nil RayCluster provided")
	}

	// Check if auth is enabled
	if rayCluster.Spec.AuthOptions == nil || rayCluster.Spec.AuthOptions.Mode != rayv1.AuthModeToken {
		logrus.Debugf("Auth not enabled for RayCluster %s/%s", rayCluster.Namespace, rayCluster.Name)
		return "", nil
	}

	cacheKey := rayCluster.Namespace + "/" + rayCluster.Name

	// Check the cache first.
	c.mu.RLock()
	if cached, ok := c.tokenCache[cacheKey]; ok && time.Now().Before(cached.expiresAt) {
		c.mu.RUnlock()
		logrus.Debugf("Auth token cache hit for RayCluster %s", cacheKey)
		return cached.token, nil
	}
	c.mu.RUnlock()

	// Cache miss or expired — fetch from K8s.
	client := c.clients[0]
	secret := &corev1.Secret{}
	err := client.Get(ctx, types.NamespacedName{Namespace: rayCluster.Namespace, Name: rayCluster.Name}, secret)
	if err != nil {
		return "", fmt.Errorf("failed to get auth secret %s/%s: %w", rayCluster.Namespace, rayCluster.Name, err)
	}

	// Extract the token from the secret.
	tokenBytes, exists := secret.Data[AuthTokenSecretKey]
	if !exists {
		return "", fmt.Errorf("%s key not found in secret %s/%s", AuthTokenSecretKey, rayCluster.Namespace, rayCluster.Name)
	}

	token := string(tokenBytes)

	// Store in cache
	c.mu.Lock()
	c.tokenCache[cacheKey] = cachedToken{
		token:     token,
		expiresAt: time.Now().Add(authTokenCacheTTL),
	}
	c.mu.Unlock()

	return token, nil
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
		configs:    kubeconfigList,
		clients:    clientList,
		tokenCache: make(map[string]cachedToken),
	}
	return clientManager, nil
}
