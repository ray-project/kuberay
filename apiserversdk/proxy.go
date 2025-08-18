package apiserversdk

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	apiserverutil "github.com/ray-project/kuberay/apiserversdk/util"
	rayutil "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
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
	proxy.Transport = newRetryRoundTripper(baseTransport)
	var handler http.Handler = proxy
	if config.Middleware != nil {
		handler = config.Middleware(proxy)
	}

	mux := http.NewServeMux()
	// TODO: add template features to specify routes.
	mux.Handle("/apis/ray.io/v1/", handler)                                                                                    // forward KubeRay CR requests.
	mux.Handle("GET /api/v1/namespaces/{namespace}/events", withFieldSelector(handler, "involvedObject.apiVersion=ray.io/v1")) // allow querying KubeRay CR events.

	k8sClient := kubernetes.NewForConfigOrDie(config.KubernetesConfig)

	// Compute Template middleware
	ctMiddleware := apiserverutil.NewComputeTemplateMiddleware(k8sClient)
	mux.Handle("POST /apis/ray.io/v1/namespaces/{namespace}/rayclusters", ctMiddleware(handler))
	mux.Handle("PUT /apis/ray.io/v1/namespaces/{namespace}/rayclusters/{name}", ctMiddleware(handler))

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
			LabelSelector: "app.kubernetes.io/name=" + rayutil.ApplicationName,
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
	base http.RoundTripper

	// Num of retries after the initial attempt
	maxRetries int

	// Retry backoff settings
	initBackoff time.Duration
	backoffBase float64
	maxBackoff  time.Duration
}

func newRetryRoundTripper(base http.RoundTripper) http.RoundTripper {
	return &retryRoundTripper{
		base:        base,
		maxRetries:  apiserverutil.HTTPClientDefaultMaxRetry,
		initBackoff: apiserverutil.HTTPClientDefaultInitBackoff,
		backoffBase: apiserverutil.HTTPClientDefaultBackoffBase,
		maxBackoff:  apiserverutil.HTTPClientDefaultMaxBackoff,
	}
}

func (rrt *retryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	var bodyBytes []byte
	var resp *http.Response
	var err error

	if req.Body != nil {
		/* Reuse request body in each attempt */
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body for retry support: %w", err)
		}
		err = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to close request body: %w", err)
		}
	}

	for attempt := 0; attempt <= rrt.maxRetries; attempt++ {
		/* Try up to (rrt.maxRetries + 1) times: initial attempt + retries */

		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err = rrt.base.RoundTrip(req)
		if err != nil {
			return resp, fmt.Errorf("request to %s %s failed with error: %w", req.Method, req.URL.String(), err)
		}

		if apiserverutil.IsSuccessfulStatusCode(resp.StatusCode) {
			return resp, nil
		}

		if !apiserverutil.IsRetryableHTTPStatusCodes(resp.StatusCode) {
			return resp, nil
		}

		if attempt == rrt.maxRetries {
			return resp, nil
		}

		if resp.Body != nil {
			/* If not last attempt, drain response body */
			if _, err = io.Copy(io.Discard, resp.Body); err != nil {
				return nil, fmt.Errorf("retryRoundTripper internal failure to drain response body: %w", err)
			}
			if err = resp.Body.Close(); err != nil {
				return nil, fmt.Errorf("retryRoundTripper internal failure to close response body: %w", err)
			}
		}

		sleepDuration := apiserverutil.GetRetryBackoff(attempt, rrt.initBackoff, rrt.backoffBase, rrt.maxBackoff)

		// TODO: merge common utils for apiserver v1 and v2
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if sleepDuration > remaining {
				return resp, fmt.Errorf("retry timeout exceeded context deadline")
			}
		}

		select {
		case <-time.After(sleepDuration):
		case <-ctx.Done():
			return resp, fmt.Errorf("retry canceled during backoff: %w", ctx.Err())
		}
	}
	return resp, err
}
