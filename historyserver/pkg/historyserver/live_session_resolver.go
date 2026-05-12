package historyserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"k8s.io/client-go/transport"
)

const (
	defaultSessionResolveTimeout = 5 * time.Second
	dashboardVersionEndpoint     = "/api/version"
)

// HTTPLiveSessionResolver returns the current session name of a live RayCluster.
type HTTPLiveSessionResolver struct {
	client             *http.Client
	useKubernetesProxy bool
	apiServerHost      string
}

func NewHTTPLiveSessionResolver(cm *ClientManager, useKubernetesProxy bool) (*HTTPLiveSessionResolver, error) {
	httpClient := &http.Client{}
	apiServerHost := ""

	if useKubernetesProxy {
		k8sRestConfig := cm.configs[0]
		transportConfig, err := k8sRestConfig.TransportConfig()
		if err != nil {
			return nil, fmt.Errorf("get transport config: %w", err)
		}
		rt, err := transport.New(transportConfig)
		if err != nil {
			return nil, fmt.Errorf("create Kubernetes-aware round tripper: %w", err)
		}
		httpClient.Transport = rt
		apiServerHost = k8sRestConfig.Host
	}

	return &HTTPLiveSessionResolver{
		client:             httpClient,
		useKubernetesProxy: useKubernetesProxy,
		apiServerHost:      apiServerHost,
	}, nil
}

func (r *HTTPLiveSessionResolver) FetchSessionName(ctx context.Context, namespace, headSvcName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultSessionResolveTimeout)
	defer cancel()

	var url string
	if r.useKubernetesProxy {
		url = fmt.Sprintf("%s/api/v1/namespaces/%s/services/%s:dashboard/proxy%s",
			r.apiServerHost, namespace, headSvcName, dashboardVersionEndpoint)
	} else {
		url = fmt.Sprintf("http://%s:8265%s", headSvcName, dashboardVersionEndpoint)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request for %s: %w", url, err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("call %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s returned status %d", url, resp.StatusCode)
	}

	var body struct {
		SessionName string `json:"session_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode response from %s: %w", url, err)
	}
	if body.SessionName == "" {
		return "", fmt.Errorf("empty session_name from %s", url)
	}
	return body.SessionName, nil
}
