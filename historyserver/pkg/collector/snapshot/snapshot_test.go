package snapshot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memWriter struct {
	files map[string][]byte
	dirs  map[string]bool
}

func newMemWriter() *memWriter {
	return &memWriter{
		files: make(map[string][]byte),
		dirs:  make(map[string]bool),
	}
}

func (m *memWriter) WriteFile(file string, reader io.ReadSeeker) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	m.files[file] = data
	return nil
}

func (m *memWriter) CreateDirectory(path string) error {
	m.dirs[path] = true
	return nil
}

func TestRunSnapshot(t *testing.T) {
	clusterMetadata := map[string]any{
		"result": true,
		"data": map[string]any{
			"ray_version":    "2.54.0",
			"python_version": "3.11.0",
			"session_name":   "session_2026-08-17_10-30-00_123456",
		},
	}
	timezone := map[string]any{"timezone": "UTC"}
	serveApps := map[string]any{"applications": map[string]any{}}
	placementGroups := map[string]any{"result": []any{}}
	jobs := []map[string]any{
		{"job_id": "01000000", "status": "SUCCEEDED"},
		{"job_id": "02000000", "status": "RUNNING"},
	}
	datasets01 := map[string]any{"datasets": []any{map[string]any{"id": "ds1"}}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		query := r.URL.RawQuery
		full := path
		if query != "" {
			full = path + "?" + query
		}

		var resp interface{}
		switch {
		case full == "/api/v0/cluster_metadata":
			resp = clusterMetadata
		case full == "/timezone":
			resp = timezone
		case full == "/api/serve/applications/":
			resp = serveApps
		case full == "/api/v0/placement_groups?detail=1&limit=10000":
			resp = placementGroups
		case full == "/api/jobs/":
			resp = jobs
		case strings.HasPrefix(path, "/api/data/datasets/"):
			jobID := strings.TrimPrefix(path, "/api/data/datasets/")
			if jobID == "01000000" {
				resp = datasets01
			} else {
				resp = map[string]any{"datasets": []any{}}
			}
		default:
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	writer := newMemWriter()
	cfg := Config{
		DashboardAddress:    srv.URL,
		StorageRootDir:      "root",
		RayClusterName:      "my-cluster",
		RayClusterNamespace: "default",
	}

	err := Run(context.Background(), cfg, writer)
	require.NoError(t, err)

	sessionName := "session_2026-08-17_10-30-00_123456"

	// Verify served endpoints are stored
	assertStoredEndpoint(t, writer, "root", sessionName, "/api/v0/cluster_metadata")
	assertStoredEndpoint(t, writer, "root", sessionName, "/timezone")
	assertStoredEndpoint(t, writer, "root", sessionName, "/api/serve/applications/")
	assertStoredEndpoint(t, writer, "root", sessionName, "/api/v0/placement_groups?detail=1&limit=10000")

	// Verify preservation endpoints are stored
	assertStoredEndpoint(t, writer, "root", sessionName, "/api/jobs/")

	// Verify per-job datasets are stored
	assertStoredEndpoint(t, writer, "root", sessionName, "/api/data/datasets/01000000")

	// Verify cluster metadata marker exists
	markerPath := "root/cluster-metadata/raycluster/default_my-cluster/" + sessionName
	_, markerExists := writer.files[markerPath]
	assert.True(t, markerExists, "cluster metadata marker should exist at %s; files: %v", markerPath, keys(writer.files))
}

func TestRunSnapshotFallbackSessionName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch path {
		case "/api/v0/cluster_metadata":
			// Return metadata without session_name
			json.NewEncoder(w).Encode(map[string]any{"result": true, "data": map[string]any{}})
		case "/timezone":
			json.NewEncoder(w).Encode(map[string]any{"timezone": "UTC"})
		case "/api/serve/applications/":
			json.NewEncoder(w).Encode(map[string]any{})
		case "/api/jobs/":
			json.NewEncoder(w).Encode([]any{})
		default:
			json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer srv.Close()

	writer := newMemWriter()
	cfg := Config{
		DashboardAddress:    srv.URL,
		StorageRootDir:      "root",
		RayClusterName:      "my-cluster",
		RayClusterNamespace: "default",
	}

	err := Run(context.Background(), cfg, writer)
	require.NoError(t, err)

	// Verify a session name was generated (should start with "session_")
	foundSession := false
	for path := range writer.files {
		if strings.Contains(path, "session_") {
			foundSession = true
			break
		}
	}
	assert.True(t, foundSession, "should have generated a session name starting with session_")
}

func TestRunSnapshotWithOwner(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch path {
		case "/api/v0/cluster_metadata":
			json.NewEncoder(w).Encode(map[string]any{
				"result": true,
				"data":   map[string]any{"session_name": "session_2026-08-17_10-30-00_123456"},
			})
		case "/api/jobs/":
			json.NewEncoder(w).Encode([]any{})
		default:
			json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer srv.Close()

	writer := newMemWriter()
	cfg := Config{
		DashboardAddress:    srv.URL,
		StorageRootDir:      "root",
		RayClusterName:      "my-cluster",
		RayClusterNamespace: "default",
		OwnerKind:           "rayjob",
		OwnerName:           "my-job",
	}

	err := Run(context.Background(), cfg, writer)
	require.NoError(t, err)

	sessionName := "session_2026-08-17_10-30-00_123456"

	// Verify the cluster metadata marker uses owner path
	markerPath := "root/cluster-metadata/rayjob/default_my-job_my-cluster/" + sessionName
	_, markerExists := writer.files[markerPath]
	assert.True(t, markerExists, "cluster metadata marker with owner should exist at %s; files: %v", markerPath, keys(writer.files))

	// Verify fetched_endpoints use owner path in cluster dir
	expectedPrefix := "root/cluster-history/rayjob/default/my-job/my-cluster/" + sessionName + "/fetched_endpoints/"
	foundFetched := false
	for path := range writer.files {
		if strings.HasPrefix(path, expectedPrefix) {
			foundFetched = true
			break
		}
	}
	assert.True(t, foundFetched, "fetched endpoints should be under owner cluster dir %s; files: %v", expectedPrefix, keys(writer.files))
}

func TestRunSnapshotCancelation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"result": true,
			"data":   map[string]any{"session_name": "session_2026-08-17_10-30-00_123456"},
		})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	writer := newMemWriter()
	cfg := Config{
		DashboardAddress:    srv.URL,
		StorageRootDir:      "root",
		RayClusterName:      "my-cluster",
		RayClusterNamespace: "default",
	}

	err := Run(ctx, cfg, writer)
	assert.Error(t, err, "should fail when context is canceled")
}

func TestDiscoverSessionName(t *testing.T) {
	tests := []struct {
		name     string
		response map[string]any
		wantName string
	}{
		{
			name: "from session_name field",
			response: map[string]any{
				"data": map[string]any{
					"session_name": "session_2026-01-01_12-00-00_000000",
				},
			},
			wantName: "session_2026-01-01_12-00-00_000000",
		},
		{
			name: "from session_dir field",
			response: map[string]any{
				"data": map[string]any{
					"session_dir": "/tmp/ray/session_2026-02-01_13-00-00_111111",
				},
			},
			wantName: "session_2026-02-01_13-00-00_111111",
		},
		{
			name: "session_name takes precedence over session_dir",
			response: map[string]any{
				"data": map[string]any{
					"session_name": "session_2026-01-01_12-00-00_000000",
					"session_dir":  "/tmp/ray/session_2026-02-01_13-00-00_111111",
				},
			},
			wantName: "session_2026-01-01_12-00-00_000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(tt.response)
			}))
			defer srv.Close()

			client := &http.Client{}
			name, err := discoverSessionName(context.Background(), client, srv.URL)
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, name)
		})
	}
}

func TestDiscoverSessionNameFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := &http.Client{}
	name, err := discoverSessionName(context.Background(), client, srv.URL)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(name, "session_"), "generated session name should start with session_")
}

func TestDedup(t *testing.T) {
	result := dedup(
		[]string{"/a", "/b", "/c"},
		[]string{"/b", "/d"},
		[]string{"/a", "/e"},
	)
	assert.Equal(t, []string{"/a", "/b", "/c", "/d", "/e"}, result)
}

func TestFetchAndStore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"key":"value"}`))
	}))
	defer srv.Close()

	writer := newMemWriter()
	err := fetchAndStore(context.Background(), &http.Client{}, srv.URL, "/api/v0/cluster_metadata", "root/cluster-history/raycluster/default/my-cluster", "session_2026-01-01_12-00-00_000000", writer)
	require.NoError(t, err)

	expectedKey := "root/cluster-history/raycluster/default/my-cluster/session_2026-01-01_12-00-00_000000/fetched_endpoints/restful__api__v0__cluster_metadata"
	data, ok := writer.files[expectedKey]
	require.True(t, ok, "expected file at %s; files: %v", expectedKey, keys(writer.files))
	assert.Equal(t, `{"key":"value"}`, string(data))
}

func TestFetchAndStoreSkipsEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return empty body
	}))
	defer srv.Close()

	writer := newMemWriter()
	err := fetchAndStore(context.Background(), &http.Client{}, srv.URL, "/api/v0/cluster_metadata", "root/cluster-history/raycluster/default/my-cluster", "session_2026-01-01_12-00-00_000000", writer)
	require.NoError(t, err)
	assert.Empty(t, writer.files, "should not store empty responses")
}

func TestFetchEndpointAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := fetchEndpoint(context.Background(), &http.Client{}, srv.URL, "/api/v0/cluster_metadata")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
}

func assertStoredEndpoint(t *testing.T, writer *memWriter, rootDir, sessionName, endpoint string) {
	t.Helper()
	storageKey := "restful__" + strings.ReplaceAll(strings.Trim(endpoint, "/"), "/", "__")
	prefix := rootDir + "/cluster-history/raycluster/default/my-cluster/" + sessionName + "/fetched_endpoints/" + storageKey
	_, ok := writer.files[prefix]
	assert.True(t, ok, "endpoint %s should be stored at %s; files: %v", endpoint, prefix, keys(writer.files))
}

func keys(m map[string][]byte) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

