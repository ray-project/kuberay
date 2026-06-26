package historyserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/emicklei/go-restful/v3"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
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
}
