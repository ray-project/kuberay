package historyserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayutils "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
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
	// Aligned with ray-operator defaults:
	// https://github.com/ray-project/kuberay/blob/178e6c91/ray-operator/apis/config/v1alpha1/defaults.go#L12-L13
	DefaultKubeAPIQPS   = float64(100)
	DefaultKubeAPIBurst = 200

	// AuthTokenSecretKey is the key used to store the auth token in a Kubernetes Secret
	AuthTokenSecretKey = "auth_token"
	// authTokenCacheTTL is how long a cached token is considered valid before re-fetching from K8s
	authTokenCacheTTL = 5 * time.Minute
	// svcInfoCacheTTL is how long a cached ServiceInfo entry is considered valid before re-fetching from K8s
	svcInfoCacheTTL = 30 * time.Second
)

type ClientManager struct {
	configs      []*rest.Config
	clients      []client.Client
	tokenCache   *TTLCache[string]
	svcInfoCache *TTLCache[ServiceInfo]
}

// Client returns the primary controller-runtime client.
func (c *ClientManager) Client() client.Client {
	return c.clients[0]
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

type ClientManagerConfig struct {
	Kubeconfigs        string
	UseKubernetesProxy bool
	QPS                float32
	Burst              int
}

// GetAuthTokenForRayCluster retrieves the auth token for the named RayCluster from its Secret.
// Returns empty string if auth is not enabled; otherwise returns an error when token retrieval fails.
//
// The RayCluster spec is always read fresh from the K8s API (never from the svcInfo cache) so that
// enabling or updating auth is picked up immediately: a stale cached spec could otherwise skip the
// token fetch after auth is enabled and silently break proxying.
// Tokens are cached for authTokenCacheTTL to avoid hitting the K8s API on every request.
func (c *ClientManager) GetAuthTokenForRayCluster(ctx context.Context, namespace, name string) (string, error) {
	if len(c.clients) == 0 {
		return "", fmt.Errorf("no Kubernetes client available")
	}

	// Read the current spec fresh so the auth decision is never based on stale cached data.
	rayCluster, cli, err := getRayCluster(ctx, c.clients, namespace, name)
	if err != nil {
		return "", err
	}

	// Check if auth is enabled
	if rayCluster.Spec.AuthOptions == nil || rayCluster.Spec.AuthOptions.Mode != rayv1.AuthModeToken {
		logrus.Debugf("Auth not enabled for RayCluster %s/%s", namespace, name)
		return "", nil
	}

	cacheKey := namespace + "/" + name

	// Check the cache first.
	if token, ok := c.tokenCache.Get(cacheKey); ok {
		logrus.Debugf("Auth token cache hit for RayCluster %s", cacheKey)
		return token, nil
	}

	// Honor a user-supplied secret name when set, matching the operator's
	// SetContainerTokenAuthEnvVars logic; otherwise fall back to the default.
	secretName := rayutils.CheckName(name)
	if secret := rayCluster.Spec.AuthOptions.SecretName; secret != nil && *secret != "" {
		secretName = *secret
	}

	// Cache miss or expired — fetch from K8s using the same client that served the RayCluster,
	// since the auth Secret lives alongside its cluster.
	secret := &corev1.Secret{}
	if err := cli.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretName}, secret); err != nil {
		return "", fmt.Errorf("failed to get auth secret %s/%s: %w", namespace, secretName, err)
	}

	// Extract the token from the secret.
	tokenBytes, exists := secret.Data[AuthTokenSecretKey]
	if !exists {
		return "", fmt.Errorf("%s key not found in secret %s/%s", AuthTokenSecretKey, namespace, secretName)
	}

	token := string(tokenBytes)
	c.tokenCache.Set(cacheKey, token)

	return token, nil
}

// getRayCluster fetches the named RayCluster fresh from the K8s API, trying each client in turn
// and returning the first successful response together with the client that served it (so callers
// can reuse the same client for related objects, e.g. the head service or the auth Secret).
func getRayCluster(ctx context.Context, clients []client.Client, namespace, name string) (*rayv1.RayCluster, client.Client, error) {
	var err error
	for _, cli := range clients {
		rc := &rayv1.RayCluster{}
		if getErr := cli.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, rc); getErr == nil {
			return rc, cli, nil
		} else {
			err = getErr
		}
	}
	return nil, nil, fmt.Errorf("failed to get RayCluster %s/%s: %w", namespace, name, err)
}

// GetSvcInfo looks up the cluster's head service routing info, using a short-lived cache to reduce
// K8s API calls. The cache is invalidated after svcInfoCacheTTL (30s) to pick up changes while
// avoiding excessive network overhead on every request. This info is only used for request routing
// and is not security-sensitive; auth decisions read the RayCluster spec fresh (see
// GetAuthTokenForRayCluster).
func (c *ClientManager) GetSvcInfo(name, namespace string) (ServiceInfo, error) {
	cacheKey := namespace + "/" + name

	// Check the cache first.
	if cached, ok := c.svcInfoCache.Get(cacheKey); ok {
		logrus.Debugf("svcInfo cache hit for cluster %s", cacheKey)
		return cached, nil
	}

	// Cache miss or expired — fetch from K8s.
	svcInfo, err := fetchSvcInfo(c.clients, name, namespace)
	if err != nil {
		return ServiceInfo{}, err
	}

	c.svcInfoCache.Set(cacheKey, svcInfo)

	return svcInfo, nil
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
		configs:      kubeconfigList,
		clients:      clientList,
		tokenCache:   NewTTLCache[string](authTokenCacheTTL),
		svcInfoCache: NewTTLCache[ServiceInfo](svcInfoCacheTTL),
	}
	return clientManager, nil
}
