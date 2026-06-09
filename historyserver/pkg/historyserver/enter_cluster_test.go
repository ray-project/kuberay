package historyserver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/emicklei/go-restful/v3"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnterCluster(t *testing.T) {
	// Reset default container to avoid polluting or using duplicate services across runs
	restful.DefaultContainer = restful.NewContainer()

	// Create ServerHandler with fake sessionLoader
	handler := &ServerHandler{
		maxClusters: 100,
		clustersMap: make(map[utils.ClusterKey][]utils.ClusterInfo),
	}

	// Setup fake processor for sessionLoader
	fp := &fakeProcessor{
		fn: func(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
			if info.SessionName == "session_2026-04-22_10-00-00_000000_1" {
				return SessionStatusProcessed, &eventserver.SessionSnapshot{}, nil
			}
			if info.SessionName == "session_2026-04-22_10-00-00_000000_2_live" {
				return SessionStatusLive, nil, nil
			}
			return SessionStatusEventsErr, nil, fmt.Errorf("unknown session")
		},
	}
	handler.sessionLoader = NewSessionLoader(fp, context.Background(), DefaultSessionProcessTimeout, DefaultSessionCacheSize, DefaultSessionCacheTTL)

	// Single session cluster
	keyA := utils.ClusterKey{
		Namespace: "default",
		Name:      "cluster-a",
	}
	handler.clustersMap[keyA] = []utils.ClusterInfo{
		{
			Namespace:   "default",
			Name:        "cluster-a",
			SessionName: "session_2026-04-22_10-00-00_000000_1",
			OwnerKind:   "RayJob",
			OwnerName:   "job-a",
		},
	}

	// Multi-session cluster (past session AND live session)
	keyB := utils.ClusterKey{
		Namespace: "default",
		Name:      "cluster-b",
	}
	handler.clustersMap[keyB] = []utils.ClusterInfo{
		{
			Namespace:       "default",
			Name:            "cluster-b",
			SessionName:     "session_2026-04-22_10-00-00_000000_1",
			OwnerKind:       "RayService",
			OwnerName:       "svc-b",
			CreateTimeStamp: 1000, // Older
		},
		{
			Namespace:       "default",
			Name:            "cluster-b",
			SessionName:     "live",
			OwnerKind:       "RayService",
			OwnerName:       "svc-b",
			CreateTimeStamp: 2000, // Newer (latest)
		},
	}

	// Cluster with session that is resolved but will trigger live resolution
	keyC := utils.ClusterKey{
		Namespace: "default",
		Name:      "cluster-c",
	}
	handler.clustersMap[keyC] = []utils.ClusterInfo{
		{
			Namespace:   "default",
			Name:        "cluster-c",
			SessionName: "session_2026-04-22_10-00-00_000000_2_live",
			OwnerKind:   "RayJob",
			OwnerName:   "job-c",
		},
	}

	// Cluster with invalid session format
	keyD := utils.ClusterKey{
		Namespace: "default",
		Name:      "cluster-d",
	}
	handler.clustersMap[keyD] = []utils.ClusterInfo{
		{
			Namespace:   "default",
			Name:        "cluster-d",
			SessionName: "invalid-session-name",
		},
	}

	// Explicitly sort `Multi-session cluster` to simulate listClusters post-sorting logic
	sort.Sort(utils.ClusterInfoList(handler.clustersMap[keyB]))

	// Register actual router
	routerRayClusterSet(handler)

	container := restful.DefaultContainer

	t.Run("Verify sorted order of multi-session slice puts latest first", func(t *testing.T) {
		sessions := handler.clustersMap[keyB]
		if len(sessions) != 2 {
			t.Fatalf("Expected 2 sessions, got %d", len(sessions))
		}
		// Index 0 must be the newer session (live, timestamp 2000)
		if sessions[0].SessionName != "live" {
			t.Errorf("Expected latest session 'live' at index 0, got %s", sessions[0].SessionName)
		}
	})

	t.Run("Enter existing single-session cluster with explicit session (Successful Dead Session Loading)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/enter_cluster/default/cluster-a/session_2026-04-22_10-00-00_000000_1", nil)
		resp := httptest.NewRecorder()
		container.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", resp.Code, resp.Body.String())
		}

		cookies := resp.Result().Cookies()
		cookieMap := make(map[string]*http.Cookie)
		for _, cookie := range cookies {
			cookieMap[cookie.Name] = cookie
		}

		if c, ok := cookieMap[COOKIE_SESSION_NAME_KEY]; !ok || c.Value != "session_2026-04-22_10-00-00_000000_1" {
			t.Errorf("Expected cookie %s to be 'session_2026-04-22_10-00-00_000000_1', got %v", COOKIE_SESSION_NAME_KEY, c)
		}
	})

	t.Run("Enter cluster with session that is resolved but triggers live resolution (Line 326 path)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/enter_cluster/default/cluster-c/session_2026-04-22_10-00-00_000000_2_live", nil)
		resp := httptest.NewRecorder()
		container.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d: %s", resp.Code, resp.Body.String())
		}

		cookies := resp.Result().Cookies()
		cookieMap := make(map[string]*http.Cookie)
		for _, cookie := range cookies {
			cookieMap[cookie.Name] = cookie
		}

		// Since the fake processor returns SessionStatusLive for this session, resolvedSession is set to "live"
		if c, ok := cookieMap[COOKIE_SESSION_NAME_KEY]; !ok || c.Value != "live" {
			t.Errorf("Expected cookie %s to be 'live' (resolved from timestamp because cluster is live), got %v", COOKIE_SESSION_NAME_KEY, c)
		}
	})

	t.Run("Enter cluster with invalid session name format (Line 310 path)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/enter_cluster/default/cluster-d/invalid-session-name", nil)
		resp := httptest.NewRecorder()
		container.ServeHTTP(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("Expected status 400 (BadRequest), got %d", resp.Code)
		}
	})
	t.Run("Enter cluster with 'latest' keyword resolves to newest session in the list", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/enter_cluster/default/cluster-b/latest", nil)
		resp := httptest.NewRecorder()
		container.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.Code)
		}

		// Verify cookies are set to the ACTUAL newest session name ("live") rather than literal "latest"
		cookies := resp.Result().Cookies()
		cookieMap := make(map[string]*http.Cookie)
		for _, cookie := range cookies {
			cookieMap[cookie.Name] = cookie
		}

		if c, ok := cookieMap[COOKIE_CLUSTER_NAME_KEY]; !ok || c.Value != "cluster-b" {
			t.Errorf("Expected cookie %s to be 'cluster-b', got %v", COOKIE_CLUSTER_NAME_KEY, c)
		}
		if c, ok := cookieMap[COOKIE_CLUSTER_NAMESPACE_KEY]; !ok || c.Value != "default" {
			t.Errorf("Expected cookie %s to be 'default', got %v", COOKIE_CLUSTER_NAMESPACE_KEY, c)
		}
		if c, ok := cookieMap[COOKIE_SESSION_NAME_KEY]; !ok || c.Value != "live" {
			t.Errorf("Expected cookie %s to be 'live' (actual latest session name), got %v", COOKIE_SESSION_NAME_KEY, c)
		}
	})

	t.Run("Enter cluster with no session parameter defaults to latest", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/enter_cluster/default/cluster-b", nil)
		resp := httptest.NewRecorder()
		container.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.Code)
		}

		// Verify cookies are set to the ACTUAL newest session name ("live") rather than literal "latest"
		cookies := resp.Result().Cookies()
		cookieMap := make(map[string]*http.Cookie)
		for _, cookie := range cookies {
			cookieMap[cookie.Name] = cookie
		}

		if c, ok := cookieMap[COOKIE_SESSION_NAME_KEY]; !ok || c.Value != "live" {
			t.Errorf("Expected cookie %s to default to 'live' (actual latest session name), got %v", COOKIE_SESSION_NAME_KEY, c)
		}
	})

	t.Run("Enter cluster with empty session parameter defaults to latest", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/enter_cluster/default/cluster-b/", nil)
		resp := httptest.NewRecorder()
		container.ServeHTTP(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.Code)
		}

		// Verify cookies are set to the ACTUAL newest session name ("live") rather than empty
		cookies := resp.Result().Cookies()
		cookieMap := make(map[string]*http.Cookie)
		for _, cookie := range cookies {
			cookieMap[cookie.Name] = cookie
		}

		if c, ok := cookieMap[COOKIE_SESSION_NAME_KEY]; !ok || c.Value != "live" {
			t.Errorf("Expected cookie %s to default to 'live' (actual latest session name), got %v", COOKIE_SESSION_NAME_KEY, c)
		}
	})
}

type mockStorageReader struct {
	listCount int
	clusters  []utils.ClusterInfo
}

func (m *mockStorageReader) List() []utils.ClusterInfo {
	m.listCount++
	return m.clusters
}

func (m *mockStorageReader) GetContent(clusterId string, fileName string) io.Reader {
	return strings.NewReader("")
}

func (m *mockStorageReader) ListFiles(clusterId string, dir string) []string {
	return nil
}

func TestEnterClusterLatestRefreshesCache(t *testing.T) {
	restful.DefaultContainer = restful.NewContainer()

	// Initialize fake client manager
	scheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	clientManager := &ClientManager{
		clients: []client.Client{k8sClient},
	}

	// Initialize mock reader with dummy data
	mockReader := &mockStorageReader{
		clusters: []utils.ClusterInfo{
			{
				Namespace:       "default",
				Name:            "cluster-refresh",
				SessionName:     "session_2026-04-22_10-00-00_000000_1",
				CreateTimeStamp: 1000,
			},
		},
	}

	handler := &ServerHandler{
		maxClusters:   100,
		clustersMap:   make(map[utils.ClusterKey][]utils.ClusterInfo),
		reader:        mockReader,
		clientManager: clientManager,
	}

	// Preset cache with an OLD state of the cluster (only has session_1)
	key := utils.ClusterKey{Namespace: "default", Name: "cluster-refresh"}
	handler.clustersMap[key] = []utils.ClusterInfo{
		{
			Namespace:       "default",
			Name:            "cluster-refresh",
			SessionName:     "session_2026-04-22_10-00-00_000000_1",
			CreateTimeStamp: 1000,
		},
	}

	// Now a new session appears on the backend! (represented by mockReader returning a newer session_2)
	mockReader.clusters = []utils.ClusterInfo{
		{
			Namespace:       "default",
			Name:            "cluster-refresh",
			SessionName:     "session_2026-04-22_10-00-00_000000_1",
			CreateTimeStamp: 1000,
		},
		{
			Namespace:       "default",
			Name:            "cluster-refresh",
			SessionName:     "session_2026-04-22_10-00-00_000000_2",
			CreateTimeStamp: 2000, // Newer latest!
		},
	}

	fp := &fakeProcessor{
		fn: func(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
			return SessionStatusProcessed, &eventserver.SessionSnapshot{}, nil
		},
	}
	handler.sessionLoader = NewSessionLoader(fp, context.Background(), DefaultSessionProcessTimeout, DefaultSessionCacheSize, DefaultSessionCacheTTL)

	routerRayClusterSet(handler)
	container := restful.DefaultContainer

	// Call enter_cluster with "latest"
	req := httptest.NewRequest("GET", "/enter_cluster/default/cluster-refresh/latest", nil)
	resp := httptest.NewRecorder()
	container.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// The mock reader's List() must be called to refresh the cache!
	if mockReader.listCount == 0 {
		t.Errorf("Expected StorageReader.List() to be called to refresh cache, but call count was 0")
	}

	// Verify the cookie is set to the NEW session_2 instead of the cached session_1
	cookies := resp.Result().Cookies()
	cookieMap := make(map[string]*http.Cookie)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie
	}

	if c, ok := cookieMap[COOKIE_SESSION_NAME_KEY]; !ok || c.Value != "session_2026-04-22_10-00-00_000000_2" {
		t.Errorf("Expected cookie %s to resolve to the new latest session 'session_2026-04-22_10-00-00_000000_2', got %v", COOKIE_SESSION_NAME_KEY, c)
	}
}

func TestEnterClusterLatestPrioritizesLive(t *testing.T) {
	restful.DefaultContainer = restful.NewContainer()

	handler := &ServerHandler{
		maxClusters: 100,
		clustersMap: make(map[utils.ClusterKey][]utils.ClusterInfo),
	}

	key := utils.ClusterKey{
		Namespace: "default",
		Name:      "cluster-prioritize-live",
	}

	// Preset cache where the live session has timestamp 1000,
	// but an archived session has timestamp 2000 (created/updated slightly later on storage).
	handler.clustersMap[key] = []utils.ClusterInfo{
		{
			Namespace:       "default",
			Name:            "cluster-prioritize-live",
			SessionName:     "session_2026-04-22_10-00-00_000000_1",
			CreateTimeStamp: 2000, // Newer timestamp on storage!
		},
		{
			Namespace:       "default",
			Name:            "cluster-prioritize-live",
			SessionName:     "live",
			CreateTimeStamp: 1000, // Older Kubernetes cluster creation timestamp
		},
	}

	// Sort them by CreateTimeStamp descending to simulate listClusters sorting
	sort.Sort(utils.ClusterInfoList(handler.clustersMap[key]))

	fp := &fakeProcessor{
		fn: func(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
			return SessionStatusLive, nil, nil
		},
	}
	handler.sessionLoader = NewSessionLoader(fp, context.Background(), DefaultSessionProcessTimeout, DefaultSessionCacheSize, DefaultSessionCacheTTL)

	routerRayClusterSet(handler)
	container := restful.DefaultContainer

	// Call enter_cluster with "latest"
	req := httptest.NewRequest("GET", "/enter_cluster/default/cluster-prioritize-live/latest", nil)
	resp := httptest.NewRecorder()
	container.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// Verify the cookie resolves to "live" (due to live prioritization) instead of the newer archived session
	cookies := resp.Result().Cookies()
	cookieMap := make(map[string]*http.Cookie)
	for _, cookie := range cookies {
		cookieMap[cookie.Name] = cookie
	}

	if c, ok := cookieMap[COOKIE_SESSION_NAME_KEY]; !ok || c.Value != "live" {
		t.Errorf("Expected cookie %s to prioritize 'live', got %v", COOKIE_SESSION_NAME_KEY, c)
	}
}
