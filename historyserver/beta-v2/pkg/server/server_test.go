// Tests for router.go and server.go.
//
// Scope:
//   - Router registration: inspect container.RegisteredWebServices to confirm
//     the spec §8 endpoint table paths are present.
//   - getTimezone: exercise via httptest against a fake StorageReader to
//     assert the reader.nil and reader.returns-bytes branches.
//   - buildProxyTargetURL: pure function, exhaustive branch coverage for the
//     two URL shapes (kube-apiserver proxy vs in-cluster DNS).
//   - redirectRequest: tested through its "no proxyResolver" guard — a
//     real reverse-proxy test requires a live ClientManager and belongs
//     to Wave 4's E2E suite.
//   - getClusters: construction path is covered by build; behavior is
//     covered via Wave 4 E2E since it needs a real ClientManager.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	restful "github.com/emicklei/go-restful/v3"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// --- helpers -----------------------------------------------------------------

// fakeStorageReader is reused from cache_test.go. Use newFakeStorageReader()
// and its put() helper to populate test content — the storage key layout it
// uses is "clusterID|fileName" internally, but callers only need .put().

// collectRoutePaths flattens container.RegisteredWebServices() into a set of
// "{WebService.RootPath}{Route.Path}" strings — what callers actually hit.
// Returned sorted so failure messages are deterministic.
func collectRoutePaths(container *restful.Container) []string {
	seen := map[string]struct{}{}
	for _, ws := range container.RegisteredWebServices() {
		for _, r := range ws.Routes() {
			seen[r.Path] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// --- router tests ------------------------------------------------------------

// TestRegisterRouter_RegistersExpectedRoutes pins the v1-matching URL set.
// Adding new routes is fine — this test fails only if one of the well-known
// paths disappears, which would break the frontend without a visible signal.
func TestRegisterRouter_RegistersExpectedRoutes(t *testing.T) {
	s := newServerWithLoader(&fakeLoader{})
	container := restful.NewContainer()
	s.RegisterRouter(container)

	got := collectRoutePaths(container)

	// Each entry in this slice is a prefix-match: it must be present as a
	// substring in one of the registered route paths. We use prefix-match
	// (not exact) because go-restful composes the final path from
	// WebService.Path + Route.Path, and specific path-template syntax like
	// {node_id} would force a brittle literal match.
	wantPrefixes := []string{
		"/clusters",
		"/timezone",
		"/nodes",
		"/events",
		"/api/v0/tasks",
		"/api/jobs/",
		"/logical/actors",
		"/enter_cluster",
		"/readz",
		"/livez",
	}

	for _, want := range wantPrefixes {
		found := false
		for _, p := range got {
			if strings.HasPrefix(p, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a route starting with %q, got routes: %v", want, got)
		}
	}
}

// TestEnterCluster_SetsCookiesAndEchoesAck drives /enter_cluster end-to-end
// through the registered container: (a) cookies land on the response; (b) the
// JSON ack body matches v1's shape.
func TestEnterCluster_SetsCookiesAndEchoesAck(t *testing.T) {
	s := newServerWithLoader(&fakeLoader{})
	container := restful.NewContainer()
	s.RegisterRouter(container)

	req := httptest.NewRequest(http.MethodGet, "/enter_cluster/ns1/c1/sess_42", nil)
	rec := httptest.NewRecorder()
	container.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}

	// Cookie assertions.
	gotCookies := map[string]string{}
	for _, c := range rec.Result().Cookies() {
		gotCookies[c.Name] = c.Value
	}
	wantCookies := map[string]string{
		cookieClusterNameKey:      "c1",
		cookieClusterNamespaceKey: "ns1",
		cookieSessionNameKey:      "sess_42",
	}
	for k, v := range wantCookies {
		if gotCookies[k] != v {
			t.Errorf("cookie %q = %q, want %q", k, gotCookies[k], v)
		}
	}

	// Body assertions.
	var ack struct {
		Result    string `json:"result"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Session   string `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ack); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}
	if ack.Result != "success" || ack.Name != "c1" || ack.Namespace != "ns1" || ack.Session != "sess_42" {
		t.Fatalf("unexpected ack: %+v", ack)
	}
}

// --- getTimezone tests -------------------------------------------------------

// TestGetTimezone_ReturnsEmptyJSONWhenMissing covers the fallback path —
// reader.GetContent returns nil and v1 answers with the constant body
// {"offset":"","value":""}. Frontend relies on that exact shape.
func TestGetTimezone_ReturnsEmptyJSONWhenMissing(t *testing.T) {
	s := newServerWithLoader(&fakeLoader{})
	s.reader = newFakeStorageReader() // empty store; every GetContent returns nil

	req := newReqWithCookies(t, "c1", "ns1", "sess_1", "/timezone", nil)
	resp, rec := newResp()

	s.getTimezone(req, resp)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %q", ct)
	}

	// Body must parse and have a "offset" key — exact v1-compatible shape.
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, hasOffset := payload["offset"]; !hasOffset {
		t.Errorf("expected 'offset' key in response, got %v", payload)
	}
	if _, hasValue := payload["value"]; !hasValue {
		t.Errorf("expected 'value' key in response, got %v", payload)
	}
}

// TestGetTimezone_ReturnsStoredBody covers the reader-returns-bytes branch:
// the handler should stream the stored JSON verbatim. We plant a payload
// whose shape differs from the empty fallback so the assertion can tell them
// apart.
func TestGetTimezone_ReturnsStoredBody(t *testing.T) {
	const stored = `{"offset":"+08:00","value":"Asia/Taipei"}`
	storageKey := utils.EndpointPathToStorageKey("/timezone")
	fullPath := "sess_1/" + utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME + "/" + storageKey

	storeReader := newFakeStorageReader()
	storeReader.put("c1_ns1", fullPath, []byte(stored))

	s := newServerWithLoader(&fakeLoader{})
	s.reader = storeReader

	req := newReqWithCookies(t, "c1", "ns1", "sess_1", "/timezone", nil)
	resp, rec := newResp()

	s.getTimezone(req, resp)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != stored {
		t.Fatalf("expected body %q, got %q", stored, body)
	}
}

// TestGetTimezone_MissingCookiesReturns400 mirrors the cookie contract every
// handler shares (see handlers_test.go for the getTasks variant).
func TestGetTimezone_MissingCookiesReturns400(t *testing.T) {
	s := newServerWithLoader(&fakeLoader{})
	req := newReqWithCookies(t, "", "", "", "/timezone", nil)
	resp, rec := newResp()

	s.getTimezone(req, resp)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%q)", rec.Code, rec.Body.String())
	}
}

// --- redirectRequest guard ---------------------------------------------------

// TestRedirectRequest_ReturnsNotImplementedWhenProxyUnwired is the W6 contract:
// when the test-only newServerWithLoader is used (no proxyResolver / httpClient),
// any live-proxy path must answer 501 so existing tests stay green.
func TestRedirectRequest_ReturnsNotImplementedWhenProxyUnwired(t *testing.T) {
	s := newServerWithLoader(&fakeLoader{})
	req := newReqWithCookies(t, "c1", "ns1", liveSessionSentinel, "/api/v0/tasks", nil)
	resp, rec := newResp()

	s.redirectRequest(req, resp)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d (body=%q)", rec.Code, rec.Body.String())
	}
}

// --- buildProxyTargetURL -----------------------------------------------------

// TestBuildProxyTargetURL covers both URL shapes the reverse proxy emits.
// Pure function → exhaustive coverage is cheap and valuable.
func TestBuildProxyTargetURL(t *testing.T) {
	svc := ServiceInfo{ServiceName: "ray-head-svc", Namespace: "ray-ns", Port: 8265}

	tests := []struct {
		name          string
		origURL       string
		useKubeProxy  bool
		apiServerHost string
		want          string
	}{
		{
			name:         "in-cluster DNS path uses http://svc:port",
			origURL:      "/api/v0/tasks?limit=10",
			useKubeProxy: false,
			want:         "http://ray-head-svc:8265/api/v0/tasks?limit=10",
		},
		{
			name:          "kube-apiserver proxy uses services/:dashboard/proxy path",
			origURL:       "/logical/actors",
			useKubeProxy:  true,
			apiServerHost: "https://1.2.3.4:443",
			want:          "https://1.2.3.4:443/api/v1/namespaces/ray-ns/services/ray-head-svc:dashboard/proxy/logical/actors",
		},
		{
			name:          "useKubeProxy=true but empty apiServerHost falls back to DNS",
			origURL:       "/nodes",
			useKubeProxy:  true,
			apiServerHost: "",
			want:          "http://ray-head-svc:8265/nodes",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildProxyTargetURL(tc.origURL, svc, tc.useKubeProxy, tc.apiServerHost)
			if got != tc.want {
				t.Fatalf("buildProxyTargetURL: got %q, want %q", got, tc.want)
			}
		})
	}
}
