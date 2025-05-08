package apiserversdk

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
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

	kuberayServiceFilterHandler, err := requireKuberayService(handler, config.KubernetesConfig)
	if err != nil {
		return nil, err
	}
	mux.Handle("/api/v1/namespaces/{namespace}/services/{service}/proxy/", kuberayServiceFilterHandler) // allow accessing KubeRay dashboards and job submissions.
	return mux, nil
}

func requireKuberayService(baseHandler http.Handler, config *rest.Config) (http.Handler, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 7 {
			http.Error(w, "invalid service proxy URL", http.StatusBadRequest)
			return
		}
		namespace, rawService := parts[4], parts[6]
		_, serviceName, _, valid := net.SplitSchemeNamePort(rawService)
		if !valid {
			http.Error(w, "invalid service proxy URL", http.StatusBadRequest)
			return
		}

		svc, err := clientset.CoreV1().Services(namespace).Get(r.Context(), serviceName, metav1.GetOptions{})
		if err != nil {
			http.Error(w, "service not found", http.StatusNotFound)
			return
		}
		if name, ok := svc.Labels[utils.KubernetesApplicationNameLabelKey]; !ok || name != utils.ApplicationName {
			http.Error(w, "not a KubeRay service", http.StatusForbidden)
			return
		}

		baseHandler.ServeHTTP(w, r)
	}), nil
}
