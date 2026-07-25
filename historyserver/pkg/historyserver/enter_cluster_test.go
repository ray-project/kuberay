package historyserver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emicklei/go-restful/v3"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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

func TestEnterCluster(t *testing.T) {
	// Reset default container to avoid polluting or using duplicate services across runs
	restful.DefaultContainer = restful.NewContainer()

	mockReader := &mockStorageReader{
		clusters: []utils.ClusterInfo{
			// cluster-a (Single session)
			{
				Namespace:   "default",
				Name:        "cluster-a",
				SessionName: "session_2026-04-22_10-00-00_000000_1",
				OwnerKind:   "RayJob",
				OwnerName:   "job-a",
			},
			// cluster-b (Multi-session cluster: past session AND live session)
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
			// cluster-c (session resolved but triggers live resolution)
			{
				Namespace:   "default",
				Name:        "cluster-c",
				SessionName: "session_2026-04-22_10-00-00_000000_2_live",
				OwnerKind:   "RayJob",
				OwnerName:   "job-c",
			},
			// cluster-d (invalid session format)
			{
				Namespace:   "default",
				Name:        "cluster-d",
				SessionName: "invalid-session-name",
			},
		},
	}

	// Initialize fake client manager for tests
	scheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	clientManager := &ClientManager{
		clients: []client.Client{k8sClient},
	}

	// Create ServerHandler with fake sessionLoader and mockStorageReader
	handler := &ServerHandler{
		maxClusters:   100,
		reader:        mockReader,
		clientManager: clientManager,
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
	handler.sessionLoader = NewSessionLoader(fp, DefaultSessionProcessTimeout, DefaultSessionCacheSize, DefaultSessionCacheTTL)

	// Register actual router
	routerRayClusterSet(handler, context.Background())

	container := restful.DefaultContainer

	t.Run("Verify sorted order of multi-session slice puts latest first", func(t *testing.T) {
		clusters := handler.listClusters(100)
		var sessions []utils.ClusterInfo
		for _, c := range clusters {
			if c.Name == "cluster-b" {
				sessions = append(sessions, c)
			}
		}
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

	t.Run("Enter cluster with session that is actually live maps to live sentinel", func(t *testing.T) {
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

	t.Run("Enter cluster with invalid session name format rejects request", func(t *testing.T) {
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
}

func TestEnterClusterLatestFromStorage(t *testing.T) {
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
			{
				Namespace:       "default",
				Name:            "cluster-refresh",
				SessionName:     "session_2026-04-22_10-00-00_000000_2",
				CreateTimeStamp: 2000, // Newer latest!
			},
		},
	}

	handler := &ServerHandler{
		maxClusters:   100,
		reader:        mockReader,
		clientManager: clientManager,
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

	// The mock reader's List() must be called to retrieve sessions from storage!
	if mockReader.listCount == 0 {
		t.Errorf("Expected StorageReader.List() to be called to retrieve sessions, but call count was 0")
	}

	// Verify the cookie is set to the newest session
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

	scheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cluster-prioritize-live"},
	}).Build()
	clientManager := &ClientManager{
		clients: []client.Client{k8sClient},
	}

	mockReader := &mockStorageReader{
		clusters: []utils.ClusterInfo{
			{
				Namespace:       "default",
				Name:            "cluster-prioritize-live",
				SessionName:     "session_2026-04-22_10-00-00_000000_1",
				CreateTimeStamp: 2000, // Newer timestamp on storage!
			},
		},
	}

	handler := &ServerHandler{
		maxClusters:   100,
		clientManager: clientManager,
		reader:        mockReader,
	}

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

func TestEnterClusterReturnsNotFoundWhenRemovedFromStorage(t *testing.T) {
	restful.DefaultContainer = restful.NewContainer()

	// 1. Storage reader returns an empty list (sessions were deleted/cleaned up)
	mockReader := &mockStorageReader{
		clusters: []utils.ClusterInfo{},
	}

	scheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	clientManager := &ClientManager{
		clients: []client.Client{k8sClient},
	}

	handler := &ServerHandler{
		maxClusters:   100,
		reader:        mockReader,
		clientManager: clientManager,
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
	req := httptest.NewRequest("GET", "/enter_cluster/default/cluster-removed-from-storage/latest", nil)
	resp := httptest.NewRecorder()
	container.ServeHTTP(resp, req)

	// Since storage is configured but returned empty matching list, fallback should NOT occur, returning 404
	if resp.Code != http.StatusNotFound {
		t.Fatalf("Expected status 404 (NotFound), got %d: %s", resp.Code, resp.Body.String())
	}
}

type errorClient struct {
	client.Client
	err error
}

func (c *errorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return c.err
}

func TestEnterClusterReturnsErrorOnTransientK8sError(t *testing.T) {
	restful.DefaultContainer = restful.NewContainer()

	// clientManager returns non-NotFound transient error (e.g. timeout)
	transientErr := fmt.Errorf("connection timeout")
	errClient := &errorClient{err: transientErr}
	clientManager := &ClientManager{
		clients: []client.Client{errClient},
	}

	// Storage contains a historical session entry
	mockReader := &mockStorageReader{
		clusters: []utils.ClusterInfo{
			{
				Namespace:       "default",
				Name:            "cluster-transient-err",
				SessionName:     "session_2026-04-22_10-00-00_000000_1",
				CreateTimeStamp: 1000,
			},
		},
	}

	handler := &ServerHandler{
		maxClusters:   100,
		reader:        mockReader,
		clientManager: clientManager,
	}

	fp := &fakeProcessor{
		fn: func(ctx context.Context, info utils.ClusterInfo) (SessionStatus, *eventserver.SessionSnapshot, error) {
			return SessionStatusProcessed, &eventserver.SessionSnapshot{}, nil
		},
	}
	handler.sessionLoader = NewSessionLoader(fp, context.Background(), DefaultSessionProcessTimeout, DefaultSessionCacheSize, DefaultSessionCacheTTL)

	routerRayClusterSet(handler)
	container := restful.DefaultContainer

	// Request "latest" when K8s returns transient error
	req := httptest.NewRequest("GET", "/enter_cluster/default/cluster-transient-err/latest", nil)
	resp := httptest.NewRecorder()
	container.ServeHTTP(resp, req)

	// Since non-NotFound errors are propagated upwards, it should return 500 InternalServerError rather than falling back to storage
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status 500 (StatusInternalServerError), got %d: %s", resp.Code, resp.Body.String())
	}
}
