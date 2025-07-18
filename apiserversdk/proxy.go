package apiserversdk

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

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
	baseTransport, err := rest.TransportFor(config.KubernetesConfig)
	if err != nil { // rest.TransportFor provides the auth to the K8s API server.
		return nil, err
	}
	proxy.Transport = newRetryRoundTripper(baseTransport, 3)
	var handler http.Handler = proxy
	if config.Middleware != nil {
		handler = config.Middleware(proxy)
	}
	handler = bodyPreserveMiddleware(handler)

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

type retryRoundTripper struct {
	base    http.RoundTripper
	retries int
}

func newRetryRoundTripper(base http.RoundTripper, retries int) http.RoundTripper {
	return &retryRoundTripper{base: base, retries: retries}
}

func (rrt *retryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	timeoutCtx, cancel := context.WithTimeout(req.Context(), 30*time.Second)
	defer cancel()

	req = req.WithContext(timeoutCtx)

	var resp *http.Response
	var err error
	for i := 0; i < rrt.retries; i++ {
		if i > 0 && req.GetBody != nil {
			var bodyCopy io.ReadCloser
			bodyCopy, err = req.GetBody()
			if err != nil {
				log.Printf("[retry %d/%d] failed to read request body: %v", i+1, rrt.retries, err)
				return nil, err
			}
			req.Body = bodyCopy
		}

		resp, err = rrt.base.RoundTrip(req)
		if err == nil {
			if 200 <= resp.StatusCode && resp.StatusCode < 300 {
				// Successful status code
				return resp, nil
			} else if shouldRetry(resp.StatusCode) {
				// Error status code that should retry
				log.Printf("[retry %d/%d] request failed with retryable error status: %s", i+1, rrt.retries, resp.Status)
			} else {
				// Error status code that should not retry
				log.Printf("[retry %d/%d] request failed with received non-retryable error status: %s", i+1, rrt.retries, resp.Status)
				return resp, nil
			}
		} else {
			log.Printf("[retry %d/%d] request failed with error: %v", i+1, rrt.retries, err)
			return resp, err
		}

		if i < rrt.retries {
			time.Sleep(time.Duration(1<<i) * time.Second)
		} else {
			log.Printf("[retry %d/%d] maximum retries reached", i+1, rrt.retries)
		}
	}
	return resp, err
}

func shouldRetry(statusCode int) bool {
	switch statusCode {
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func bodyPreserveMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.GetBody == nil {
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "failed to read request body", http.StatusInternalServerError)
				return
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			r.ContentLength = int64(len(bodyBytes))
			r.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(bodyBytes)), nil
			}
		}
		h.ServeHTTP(w, r)
	})
}
