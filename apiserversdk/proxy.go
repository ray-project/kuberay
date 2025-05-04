package apiserversdk

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

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
	var eventQueryHandler http.Handler = proxy
	eventQueryHandler = AddQueryFilterToHandler(eventQueryHandler)

	mux := http.NewServeMux()
	// TODO: add template features to specify routes.
	mux.Handle("/apis/ray.io/v1/", handler)                                // forward KubeRay CR requests.
	mux.Handle("/api/v1/namespaces/{namespace}/events", eventQueryHandler) // allow querying KubeRay CR events.
	// TODO: check whether the service belongs to KubeRay first.
	mux.Handle("/api/v1/namespaces/{namespace}/services/{service}/proxy", handler) // allow accessing KubeRay dashboards and job submissions.
	return mux, nil
}

func AddQueryFilterToHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only allow query requests for events
		if r.Method != http.MethodGet {
			http.Error(w, "Only GET requests are allowed for events", http.StatusMethodNotAllowed)
			return
		}

		q := r.URL.Query()

		// Filter for KubeRay resources
		fieldSelectors := []string{
			"involvedObject.apiVersion=ray.io/v1",
		}

		// Preserve existing field selectors if any
		if existing := q.Get("fieldSelector"); existing != "" {
			fieldSelectors = append(fieldSelectors, existing)
		}

		// Set the combined field selectors
		q.Set("fieldSelector", strings.Join(fieldSelectors, ","))

		r.URL.RawQuery = q.Encode()

		handler.ServeHTTP(w, r)
	})
}
