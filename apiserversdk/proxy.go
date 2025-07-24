package apiserversdk

import (
	"bytes"
	"fmt"
	"io"
	"math"
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
		return nil, fmt.Errorf("failed to parse url %s from config: %w", config.KubernetesConfig.Host, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	baseTransport, err := rest.TransportFor(config.KubernetesConfig) // rest.TransportFor provides the auth to the K8s API server.
	if err != nil {
		return nil, fmt.Errorf("failed to get transport for config: %w", err)
	}
	proxy.Transport = newRetryRoundTripper(baseTransport, HTTPClientDefaultMaxRetry)
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

// retryRoundTripper is a custom implementation of http.RoundTripper that retries HTTP requests.
// It verifies retryable HTTP status codes and retries using exponential backoff.
type retryRoundTripper struct {
	base    http.RoundTripper
	retries int
}

func newRetryRoundTripper(base http.RoundTripper, retries int) http.RoundTripper {
	return &retryRoundTripper{base: base, retries: retries}
}

func (rrt *retryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	var resp *http.Response
	var err error
	for attempt := 0; attempt < rrt.retries; attempt++ {
		if attempt == 0 && req.Body != nil && req.GetBody == nil {
			bodyBytes, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read request body for retry support: %w", err)
			}
			err = req.Body.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to close request body: %w", err)
			}
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(bodyBytes)), nil
			}
		}

		if attempt > 0 && req.GetBody != nil {
			var bodyCopy io.ReadCloser
			bodyCopy, err = req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("failed to read request body: %w", err)
			}
			req.Body = bodyCopy
		}

		resp, err = rrt.base.RoundTrip(req)
		if err != nil {
			return resp, fmt.Errorf("request to %s %s failed with error: %w", req.Method, req.URL.String(), err)
		}

		if isSuccessfulStatusCode(resp.StatusCode) {
			return resp, nil
		}

		if !isRetryableHTTPStatusCodes(resp.StatusCode) {
			return resp, nil
		}

		if attempt < rrt.retries-1 && resp.Body != nil {
			if _, err = io.Copy(io.Discard, resp.Body); err != nil {
				return nil, fmt.Errorf("retryRoundTripper internal failure to drain response body: %w", err)
			}
			if err = resp.Body.Close(); err != nil {
				return nil, fmt.Errorf("retryRoundTripper internal failure to close response body: %w", err)
			}
		}

		sleepDuration := HTTPClientDefaultInitBackoff * time.Duration(math.Pow(HTTPClientDefaultBackoffBase, float64(attempt)))
		if sleepDuration > HTTPClientDefaultMaxBackoff {
			sleepDuration = HTTPClientDefaultMaxBackoff
		}

		// TODO: merge common utils for apiserver v1 and v2
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return resp, fmt.Errorf("retry timeout exceeded context deadline")
			}
			if sleepDuration > remaining {
				sleepDuration = remaining
			}
		}

		time.Sleep(sleepDuration)
	}
	return resp, err
}

func isSuccessfulStatusCode(statusCode int) bool {
	return 200 <= statusCode && statusCode < 300
}

// TODO: merge common utils for apiserver v1 and v2
func isRetryableHTTPStatusCodes(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, // 408
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	default:
		return false
	}
}
