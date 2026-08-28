package historyserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emicklei/go-restful/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRedirectRequestUsesNamespaceQualifiedServiceName(t *testing.T) {
	tests := []struct {
		name        string
		serviceInfo ServiceInfo
		requestURI  string
		wantURL     string
	}{
		{
			name: "cross-namespace dashboard request",
			serviceInfo: ServiceInfo{
				ServiceName: "raycluster-sample-head-svc",
				Namespace:   "ray-workloads",
				Port:        8265,
			},
			requestURI: "/api/jobs/?limit=10",
			wantURL:    "http://raycluster-sample-head-svc.ray-workloads.svc:8265/api/jobs/?limit=10",
		},
		{
			name: "custom dashboard port",
			serviceInfo: ServiceInfo{
				ServiceName: "custom-head",
				Namespace:   "default",
				Port:        9265,
			},
			requestURI: "/nodes",
			wantURL:    "http://custom-head.default.svc:9265/nodes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotURL string
			handler := &ServerHandler{
				httpClient: &http.Client{
					Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						gotURL = req.URL.String()
						return &http.Response{
							StatusCode: http.StatusOK,
							Header:     make(http.Header),
							Body:       io.NopCloser(strings.NewReader("ok")),
						}, nil
					}),
				},
			}

			httpReq := httptest.NewRequest(http.MethodGet, tt.requestURI, nil)
			req := restful.NewRequest(httpReq)
			req.SetAttribute(ATTRIBUTE_SERVICE_NAME, tt.serviceInfo)
			recorder := httptest.NewRecorder()

			handler.redirectRequest(req, restful.NewResponse(recorder))

			require.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, tt.wantURL, gotURL)
			assert.Equal(t, "ok", recorder.Body.String())
		})
	}
}
