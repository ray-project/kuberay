package support

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
)

// CreateAndExecuteMetricsRequest is a test helper that creates an HTTP GET request to the /metrics endpoint,
// executes it against a Prometheus handler using the provided registry, and returns the request, response recorder, and handler.
func CreateAndExecuteMetricsRequest(t *testing.T, reg *prometheus.Registry) (*http.Request, *httptest.ResponseRecorder, http.Handler) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	handler.ServeHTTP(rr, req)

	return req, rr, handler
}
