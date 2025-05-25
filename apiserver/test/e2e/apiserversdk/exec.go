package apiserversdk

import (
	"net/http"
	"path"
	"strings"

	"k8s.io/client-go/rest"
)

type ProxyRoundTripper struct {
	Transport http.RoundTripper
}

func newProxyRoundTripper(cfg *rest.Config) (*ProxyRoundTripper, error) {
	transport, err := rest.TransportFor(cfg)
	if err != nil {
		return nil, err
	}

	return &ProxyRoundTripper{
		Transport: transport,
	}, nil
}

func (rt *ProxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	newReq := req.Clone(req.Context())

	newReq.URL.Path = path.Join(
		"/api/v1/namespaces/ray-system/services/kuberay-apiserver:8888/proxy",
		req.URL.Path,
	)

	if !strings.HasSuffix(newReq.URL.Path, "/") {
		newReq.URL.Path = newReq.URL.Path + "/"
	}

	return rt.Transport.RoundTrip(newReq)
}
