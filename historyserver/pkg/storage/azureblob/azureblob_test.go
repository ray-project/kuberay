package azureblob

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

func TestTrim(t *testing.T) {
	tmpRayRoot := utils.GetTmpRayRoot()
	absoluteLogPathName := fmt.Sprintf(" %s/test/LLogs/events/aa/a.txt  ", tmpRayRoot)
	logdir := fmt.Sprintf("%s/test/lLogs/", tmpRayRoot)

	absoluteLogPathName = strings.TrimSpace(absoluteLogPathName)
	absoluteLogPathName = filepath.Clean(absoluteLogPathName)

	logdir = strings.TrimSpace(logdir)
	logdir = filepath.Clean(logdir)

	relativePath := strings.TrimPrefix(absoluteLogPathName, logdir+"/")
	// Split relative path into subdir and filename
	subdir, filename := filepath.Split(relativePath)
	test_path_join := path.Join("aa./b/c/d", "e")
	t.Logf("file [%s] logdir [%s] subdir %s filename %s", absoluteLogPathName,
		logdir, subdir, filename)
	t.Logf("test_path_join [%s]", test_path_join)
}

// GetContent builds its blob path from three pieces, and a deployment that sets a
// root dir only works if all three end up in the path. These tests assert on the
// path that actually reaches the server rather than on a helper's return value.
const (
	testContainer = "test-container"
	testRootDir   = "ray-logs"
	// Callers pass a root-dir-relative path prefix here, not a bare cluster id.
	// See clusterlogs.Prefix("", ...) in pkg/historyserver/router.go.
	testClusterPrefix = "ray_cluster_history/raycluster/default/my-cluster"
	testFileName      = "session_2026-05-08_18-35-06_774618_1/logs/node123/events/event_CORE_WORKER_256.log"
)

// recorder collects what a test server was asked for. Requests are served on the
// server's own goroutines, so access is mutex-guarded to stay clean under -race.
type recorder struct {
	mu     sync.Mutex
	values []string
}

func (rec *recorder) add(value string) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.values = append(rec.values, value)
}

func (rec *recorder) snapshot() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]string(nil), rec.values...)
}

func newTestHandler(t *testing.T, srv *httptest.Server) *RayLogsHandler {
	t.Helper()

	client, err := container.NewClientWithNoCredential(srv.URL+"/"+testContainer, nil)
	if err != nil {
		t.Fatalf("creating test container client: %v", err)
	}

	return &RayLogsHandler{
		ContainerClient: client,
		ContainerName:   testContainer,
		RootDir:         testRootDir,
	}
}

// blobPath returns the blob the request addressed, and whether the request was a
// container listing rather than a blob download.
func blobPath(r *http.Request) (name string, isList bool) {
	name = strings.TrimPrefix(r.URL.Path, "/"+testContainer)
	name = strings.TrimPrefix(name, "/")
	return name, r.URL.Query().Get("comp") == "list"
}

func TestGetContentUsesRootDir(t *testing.T) {
	wantPath := path.Join(testRootDir, testClusterPrefix, testFileName)
	const wantContent = "core worker log line"

	var requested recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, isList := blobPath(r)
		if isList {
			writeListResult(w, r.URL.Query().Get("prefix"))
			return
		}
		requested.add(name)
		if name != wantPath {
			writeBlobNotFound(w)
			return
		}
		_, _ = io.WriteString(w, wantContent)
	}))
	defer srv.Close()

	reader := newTestHandler(t, srv).GetContent(testClusterPrefix, testFileName)
	gotPaths := requested.snapshot()
	if reader == nil {
		t.Fatalf("GetContent returned nil; blobs requested: %v, want %q", gotPaths, wantPath)
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading returned content: %v", err)
	}
	if string(got) != wantContent {
		t.Errorf("content = %q, want %q", got, wantContent)
	}
	if len(gotPaths) == 0 || gotPaths[0] != wantPath {
		t.Errorf("first requested blob = %v, want %q", gotPaths, wantPath)
	}
}

// When the direct download fails, GetContent lists the containing directory and
// retries any blob whose full path matches. That listing prefix has to be rooted
// too, or the retry has nothing to find. The first download here fails with a
// server error so the fallback is the only way to reach the content.
func TestGetContentFallbackListsUnderRootDir(t *testing.T) {
	wantPath := path.Join(testRootDir, testClusterPrefix, testFileName)
	const wantContent = "recovered log line"

	var listed recorder
	var downloads recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, isList := blobPath(r)
		if isList {
			prefix := r.URL.Query().Get("prefix")
			listed.add(prefix)
			if strings.HasPrefix(wantPath, prefix) {
				writeListResult(w, prefix, wantPath)
				return
			}
			writeListResult(w, prefix)
			return
		}
		downloads.add(name)
		// Miss the first attempt so the fallback has to do the work. BlobNotFound
		// is used rather than a server error because the SDK retries the latter,
		// which would satisfy the download before the fallback ever runs.
		if name != wantPath || len(downloads.snapshot()) == 1 {
			writeBlobNotFound(w)
			return
		}
		_, _ = io.WriteString(w, wantContent)
	}))
	defer srv.Close()

	reader := newTestHandler(t, srv).GetContent(testClusterPrefix, testFileName)
	listPrefixes := listed.snapshot()
	if reader == nil {
		t.Fatalf("GetContent returned nil; list prefixes tried: %v", listPrefixes)
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading returned content: %v", err)
	}
	if string(got) != wantContent {
		t.Errorf("content = %q, want %q", got, wantContent)
	}

	wantPrefix := path.Dir(wantPath) + "/"
	for _, prefix := range listPrefixes {
		if prefix == wantPrefix {
			return
		}
	}
	t.Errorf("list prefixes = %v, want one equal to %q", listPrefixes, wantPrefix)
}

func writeBlobNotFound(w http.ResponseWriter) {
	w.Header().Set("x-ms-error-code", "BlobNotFound")
	w.WriteHeader(http.StatusNotFound)
}

func writeListResult(w http.ResponseWriter, prefix string, names ...string) {
	var blobs strings.Builder
	for _, name := range names {
		blobs.WriteString(fmt.Sprintf("<Blob><Name>%s</Name><Properties></Properties></Blob>", name))
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<EnumerationResults ContainerName="%s"><Prefix>%s</Prefix><Delimiter>/</Delimiter><Blobs>%s</Blobs><NextMarker /></EnumerationResults>`, testContainer, prefix, blobs.String()))
}

func TestWalk(t *testing.T) {
	watchPath := fmt.Sprintf("%s/test/LLogs/", utils.GetTmpRayRoot())
	filepath.Walk(watchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logrus.Errorf("Walk path error %v", err)
			return err
		}

		if !info.IsDir() {
			logrus.Infof("Find new file %s", path)
		}
		return nil
	})
}
