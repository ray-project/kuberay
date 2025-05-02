package apiserversdk

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"k8s.io/client-go/rest"
)

type MuxConfig struct {
	KubernetesConfig *rest.Config
	Middleware       func(http.Handler) http.Handler
}

func NewMux(config MuxConfig) (*http.ServeMux, error) {
	u, err := url.Parse(config.KubernetesConfig.Host) // parse the K8s API server URL from the KubernetesConfig.
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	if proxy.Transport, err = rest.TransportFor(config.KubernetesConfig); err != nil { // rest.TransportFor provides the auth to the K8s API server.
		return nil, err
	}
	var handler http.Handler = proxy
	if config.Middleware != nil {
		handler = config.Middleware(proxy)
	}
	mux := http.NewServeMux()
	// TODO: add template features to specify routes.
	mux.Handle("/apis/ray.io/v1/", handler) // forward KubeRay CR requests.
	// TODO: add query filters to make sure only KubeRay events are queried.
	mux.Handle("/api/v1/namespaces/{namespace}/events", handler) // allow querying KubeRay CR events.
	// TODO: check whether the service belongs to KubeRay first.
	mux.Handle("/api/v1/namespaces/{namespace}/services/{service}/proxy", handler) // allow accessing KubeRay dashboards and job submissions.
	return mux, nil
}
