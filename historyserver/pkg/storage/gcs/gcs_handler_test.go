package gcs

import (
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	gstorage "cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/google/go-cmp/cmp"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// setupFakeGCS creates a fake GCS server and a client connected to it.
func setupFakeGCS(t *testing.T, initialObjects ...fakestorage.Object) (*fakestorage.Server, *gstorage.Client, string) {
	t.Helper()

	// The bucket name(s) must be specified within the initialObjects
	// We can extract a bucket name if needed, assuming they all use the same one in this helper
	var bucketName string
	if len(initialObjects) > 0 {
		bucketName = initialObjects[0].BucketName
	} else {
		bucketName = "default-test-bucket" // Fallback if no initial objects
	}

	server, err := fakestorage.NewServerWithOptions(fakestorage.Options{
		InitialObjects: initialObjects,
	})
	if err != nil {
		t.Fatalf("Failed to create fake GCS server: %v", err)
	}
	t.Cleanup(func() { server.Stop() })

	// If no initial objects were provided, create the fallback bucket
	if len(initialObjects) == 0 {
		server.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: bucketName})
	}

	client := server.Client()
	return server, client, bucketName
}

// createRayLogsHandler creates a RayLogsHandler instance with the fake client.
func createRayLogsHandler(client *gstorage.Client, bucketName string) *RayLogsHandler {
	return &RayLogsHandler{
		StorageClient: client,
		GCSBucket:     bucketName,
		RootDir:       "ray_historyserver",
	}
}

func TestCreateDirectory(t *testing.T) {
	server, client, bucketName := setupFakeGCS(t)
	handler := createRayLogsHandler(client, bucketName)

	tests := []struct {
		name        string
		path        string
		expectedObj string
	}{
		{
			name:        "new_directory",
			path:        "new/dir",
			expectedObj: "new/dir/",
		},
		{
			name:        "existing_directory",
			path:        "new/dir",
			expectedObj: "new/dir/",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler.CreateDirectory(tc.path)

			_, err := server.GetObject(bucketName, tc.expectedObj)
			if err != nil {
				t.Errorf("Expected directory object %q not found: %v", tc.expectedObj, err)
			}
		})
	}
}

func TestWriteFile(t *testing.T) {
	server, client, bucketName := setupFakeGCS(t)
	handler := createRayLogsHandler(client, bucketName)

	fileName := "test/file.txt"
	fileContent := "hello world"
	reader := strings.NewReader(fileContent)

	err := handler.WriteFile(fileName, reader)
	if err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", fileName, err)
	}

	obj, err := server.GetObject(bucketName, fileName)
	if err != nil {
		t.Fatalf("Failed to get object %q from fake server: %v", fileName, err)
	}

	if string(obj.Content) != fileContent {
		t.Errorf("File content mismatch: got %q, want %q", string(obj.Content), fileContent)
	}
}

func TestListFiles(t *testing.T) {
	initialObjects := []fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "ray_historyserver/cluster1/logs/file1.txt",
			},
			Content: []byte("a"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "ray_historyserver/cluster1/logs/file2.log",
			},
			Content: []byte("b"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "ray_historyserver/cluster1/logs/subdir/",
			},
			Content: []byte(""),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "ray_historyserver/cluster1/logs/subdir/file3.txt",
			},
			Content: []byte("c"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "ray_historyserver/cluster1/other/file4.txt",
			},
			Content: []byte("d"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "ray_historyserver/cluster2/logs/file5.txt",
			},
			Content: []byte("e"),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "ray_historyserver/cluster2/logs/subdir2/",
			},
			Content: []byte("e"),
		},
	}
	_, client, bucketName := setupFakeGCS(t, initialObjects...)
	handler := createRayLogsHandler(client, bucketName)

	tests := []struct {
		name      string
		clusterID string
		directory string
		expected  []string
	}{
		{
			name:      "list_files",
			clusterID: "cluster1",
			directory: "logs",
			expected:  []string{"file1.txt", "file2.log", "subdir/"},
		},
		{
			name:      "list_other",
			clusterID: "cluster1",
			directory: "other",
			expected:  []string{"file4.txt"},
		},
		{
			name:      "list_nonexistent",
			clusterID: "cluster1",
			directory: "nonexistent",
			expected:  nil,
		},
		{
			name:      "list_cluster2",
			clusterID: "cluster2",
			directory: "logs",
			expected:  []string{"file5.txt", "subdir2/"},
		},
		{
			name:      "list_empty_subdir",
			clusterID: "cluster2",
			directory: "logs/subdir2",
			expected:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			files := handler.ListFiles(tc.clusterID, tc.directory)
			sort.Strings(files)
			sort.Strings(tc.expected)
			if diff := cmp.Diff(tc.expected, files); diff != "" {
				t.Errorf("ListFiles(%q, %q) returned diff (-want +got):\n%s", tc.clusterID, tc.directory, diff)
			}
		})
	}
}

func TestList(t *testing.T) {
	// RootDir is "ray_historyserver"
	// Path format: {RootDir}/metadir/{ClusterName}_{Namespace}/{SessionName}
	ts := time.Now().UTC()
	sessionID := "session_" + ts.Format("2006-01-02_15-04-05_000000")

	initialObjects := []fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "ray_historyserver/metadir/mycluster1_default/" + sessionID,
			},
			Content: []byte(""),
		},
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       "ray_historyserver/metadir/mycluster2_testns/" + sessionID,
			},
			Content: []byte(""),
		},
	}
	_, client, bucketName := setupFakeGCS(t, initialObjects...)
	handler := createRayLogsHandler(client, bucketName)

	expected := []utils.ClusterInfo{
		{Name: "mycluster1", Namespace: "default", SessionName: sessionID, CreateTimeStamp: ts.Unix(), CreateTime: ts.UTC().Format("2006-01-02T15:04:05Z")},
		{Name: "mycluster2", Namespace: "testns", SessionName: sessionID, CreateTimeStamp: ts.Unix(), CreateTime: ts.UTC().Format("2006-01-02T15:04:05Z")},
	}

	result := handler.List()

	// Sort results for consistent comparison
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].Namespace < result[j].Namespace
	})

	if diff := cmp.Diff(expected, result); diff != "" {
		t.Errorf("List() returned diff (-want +got):\n%s", diff)
	}
}

func TestGetContent(t *testing.T) {
	clusterID := "clusterA"
	fileName := "important.log"
	objPath := "ray_historyserver/clusters/clusterA_ns/sessions/session123/logs/" + fileName
	fileContent := "Log content here"

	initialObjects := []fakestorage.Object{
		{
			ObjectAttrs: fakestorage.ObjectAttrs{
				BucketName: "test-bucket",
				Name:       objPath,
			},
			Content: []byte(fileContent),
		},
	}
	_, client, bucketName := setupFakeGCS(t, initialObjects...)
	handler := createRayLogsHandler(client, bucketName)

	reader := handler.GetContent(clusterID, fileName)
	if reader == nil {
		t.Fatalf("GetContent(%q, %q) returned nil reader, expected non-nil", clusterID, fileName)
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read content: %v", err)
	}

	if string(content) != fileContent {
		t.Errorf("GetContent(%q, %q) content mismatch: got %q, want %q", clusterID, fileName, string(content), fileContent)
	}
}
