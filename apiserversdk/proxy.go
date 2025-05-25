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
	mux.Handle("/apis/ray.io/v1/", handler)                                                                                    // forward KubeRay CR requests.
	mux.Handle("GET /api/v1/namespaces/{namespace}/events", withFieldSelector(handler, "involvedObject.apiVersion=ray.io/v1")) // allow querying KubeRay CR events.

	k8sClient := kubernetes.NewForConfigOrDie(config.KubernetesConfig)
	requireKubeRayServiceHandler := requireKubeRayService(handler, k8sClient)
	// Allow accessing KubeRay dashboards and job submissions.
	// Note: We also register "/proxy" to avoid the trailing slash redirection
	// See https://pkg.go.dev/net/http#hdr-Trailing_slash_redirection-ServeMux
	mux.Handle("/api/v1/namespaces/{namespace}/services/{service}/proxy", requireKubeRayServiceHandler)
	mux.Handle("/api/v1/namespaces/{namespace}/services/{service}/proxy/", requireKubeRayServiceHandler)

	return mux, nil
}

func withFieldSelector(handler http.Handler, selectors ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		// Preserve existing field selectors if any
		q.Set("fieldSelector", strings.Join(append(q["fieldSelector"], selectors...), ","))
		r.URL.RawQuery = q.Encode()
		handler.ServeHTTP(w, r)
	})
}

// requireKubeRayService verifies that the requested service has the label "app.kubernetes.io/name=kuberay".
// If the service is not found or does not have the correct label, it returns a 404 Not Found error.
func requireKubeRayService(handler http.Handler, k8sClient *kubernetes.Clientset) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		namespace, serviceSchemeNamePort := r.PathValue("namespace"), r.PathValue("service")
		_, serviceName, _, valid := net.SplitSchemeNamePort(serviceSchemeNamePort)
		if !valid {
			http.Error(w, "invalid service format: "+serviceSchemeNamePort, http.StatusBadRequest)
			return
		}
		services, err := k8sClient.CoreV1().Services(namespace).List(r.Context(), metav1.ListOptions{
			FieldSelector: "metadata.name=" + serviceName,
			LabelSelector: "app.kubernetes.io/name=" + utils.ApplicationName,
		})
		if err != nil {
			http.Error(w, "failed to list kuberay services", http.StatusInternalServerError)
			return
		}
		if len(services.Items) == 0 {
			http.Error(w, "kuberay service not found", http.StatusNotFound)
			return
		}
		handler.ServeHTTP(w, r)
	})
}
