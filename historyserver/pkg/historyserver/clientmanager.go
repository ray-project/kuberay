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
	AuthTokenSecretKey = rayutils.RAY_AUTH_TOKEN_SECRET_KEY
	// svcInfoCacheTTL is how long a cached ServiceInfo entry is considered valid before re-fetching from K8s
	svcInfoCacheTTL = 30 * time.Second
)

type ClientManager struct {
	configs      []*rest.Config
	clients      []client.Client
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

// GetAuthTokenForRayCluster retrieves the auth token for the named RayCluster from its Secret.
// Returns empty string if auth is not enabled; otherwise returns an error when token retrieval fails.
//
// Both the RayCluster spec and the backing Secret are always read fresh from the K8s API (never
// cached) so that enabling/updating auth and rotating the token take effect immediately: a stale
// cached spec could skip the token fetch after auth is enabled, and a stale cached token would keep
// being sent after the operator rotates the Secret — both silently breaking proxying.
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

	// Kubernetes-delegated token auth has no static bearer token to inject (Ray authenticates against
	// the K8s API server directly). Fail explicitly instead of proxying unauthenticated and letting
	// the dashboard reject the call with a confusing auth error.
	if rayCluster.Spec.AuthOptions.EnableK8sTokenAuth != nil && *rayCluster.Spec.AuthOptions.EnableK8sTokenAuth {
		return "", fmt.Errorf("cannot authenticate proxied requests to RayCluster %s/%s: Kubernetes-delegated token auth (enableK8sTokenAuth) is not supported by the history server", namespace, name)
	}

	// Honor a user-supplied secret name when set, matching the operator's
	// SetContainerTokenAuthEnvVars logic; otherwise fall back to the default.
	secretName := rayutils.CheckName(name)
	if secret := rayCluster.Spec.AuthOptions.SecretName; secret != nil && *secret != "" {
		secretName = *secret
	}

	// Read the Secret fresh on every request, using the same client that served the RayCluster
	// (the auth Secret lives alongside its cluster). We deliberately do not cache the token: the
	// history server uses a direct (non-watching) client, so there is no cheap way to invalidate a
	// cached token when the operator rotates or updates the Secret. Reading fresh ensures a rotated
	// token takes effect on the very next request instead of after a TTL.
	secret := &corev1.Secret{}
	if err := cli.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretName}, secret); err != nil {
		return "", fmt.Errorf("failed to get auth secret %s/%s: %w", namespace, secretName, err)
	}

	// Extract the token from the secret.
	tokenBytes, exists := secret.Data[AuthTokenSecretKey]
	if !exists {
		return "", fmt.Errorf("%s key not found in secret %s/%s", AuthTokenSecretKey, namespace, secretName)
	}

	// Auth is enabled, so an empty token is a misconfiguration: fail instead of proxying unauthenticated.
	token := string(tokenBytes)
	if token == "" {
		return "", fmt.Errorf("%s key in secret %s/%s is empty", AuthTokenSecretKey, namespace, secretName)
	}

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
		svcInfoCache: NewTTLCache[ServiceInfo](svcInfoCacheTTL),
	}
	return clientManager, nil
}
