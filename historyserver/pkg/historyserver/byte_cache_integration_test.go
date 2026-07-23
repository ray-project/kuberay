package historyserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emicklei/go-restful/v3"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// TestByteCache_ReadEndpointsRoundTripIsLossless is an in-process HTTP test:
//
// 1. Wire ServerHandler with a fake processor.
// 2. Register routers for /enter_cluster and the dead-session read endpoints.
// 3. GET /enter_cluster to trigger cold load and encode snapshot into the byte cache.
// 4. GET each read endpoint to decode from cache and serve.
// 5. Assert each response still contains the richSnapshot sentinel values (including nested CustomFields).
func TestByteCache_ReadEndpointsRoundTripIsLossless(t *testing.T) {
	restful.DefaultContainer = restful.NewContainer()

	const (
		ns      = "default"
		name    = "cluster-bc"
		session = "session_2026-04-22_10-00-00_000000_1"
	)

	handler := &ServerHandler{
		maxClusters: 100,
		clustersMap: make(map[utils.ClusterKey][]utils.ClusterInfo),
	}
	fp := &fakeProcessor{
		fn: func(_ context.Context, info utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
			key := utils.BuildClusterSessionKey(info.Name, info.Namespace, info.SessionName)
			return SessionStatusProcessed, richSnapshot(key), nil
		},
	}
	handler.sessionLoader = NewSessionLoader(fp, context.Background(), DefaultSessionProcessTimeout, DefaultSessionCacheSize, DefaultSessionCacheMaxBytes, DefaultSessionCacheTTL)
	handler.clustersMap[utils.ClusterKey{Namespace: ns, Name: name}] = []utils.ClusterInfo{
		{Namespace: ns, Name: name, SessionName: session, OwnerKind: "RayJob", OwnerName: "job-bc"},
	}

	routerRayClusterSet(handler)
	routerNodes(handler)
	routerEvents(handler)
	routerAPI(handler)
	routerLogical(handler)
	container := restful.DefaultContainer

	enterURL := "/enter_cluster/" + ns + "/" + name + "/" + session
	enterReq := httptest.NewRequest(http.MethodGet, enterURL, nil)
	enterResp := httptest.NewRecorder()
	container.ServeHTTP(enterResp, enterReq)
	if enterResp.Code != http.StatusOK {
		t.Fatalf("enter_cluster: got %d, body=%s", enterResp.Code, enterResp.Body.String())
	}
	cookies := enterResp.Result().Cookies()

	cases := []struct {
		name       string
		url        string
		wantSubstr string
	}{
		{"nodes", "/nodes", "node-e2e-dead"},
		{"actors", "/logical/actors", "actor-e2e-cafe"},
		{"jobs", "/api/jobs/", "sub-e2e-bbbb"},
		{"tasks", "/api/v0/tasks", "task-e2e-1111"},
		{"events_customFields", "/events/", "e2e-custom-value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			for _, c := range cookies {
				req.AddCookie(c)
			}
			resp := httptest.NewRecorder()
			container.ServeHTTP(resp, req)

			if resp.Code != http.StatusOK {
				t.Fatalf("%s: got %d, body=%s", tc.url, resp.Code, resp.Body.String())
			}
			if !strings.Contains(resp.Body.String(), tc.wantSubstr) {
				t.Fatalf("%s: response missing sentinel %q; body=%s", tc.url, tc.wantSubstr, resp.Body.String())
			}
		})
	}
}
