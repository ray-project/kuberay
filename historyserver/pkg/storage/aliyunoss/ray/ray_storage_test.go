package ray

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/retry"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// This backend had no test that constructed a handler at all. These cover the
// same four methods gcs_handler_test.go covers for the gcs backend: List,
// ListFiles, CreateDirectory and WriteFile. The assertions are on the requests
// that reach the server, so a change that stops rooting a key or drops a bucket
// operation shows up.

const (
	testBucket  = "test-bucket"
	testRootDir = "ray-logs"
	// Callers pass a root-dir-relative path prefix here, not a bare cluster id.
	// See clusterlogs.Prefix("", ...) in pkg/historyserver/router.go.
	testClusterPrefix = "ray_cluster_history/raycluster/default/my-cluster"
	metadataPrefix    = testRootDir + "/cluster-metadata/"
)

// recorder collects what a test server was asked for. Requests are served on
// the server's own goroutines, so access is mutex-guarded to stay clean under
// -race.
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

func newTestHandler(_ *testing.T, srv *httptest.Server) *RayLogsHandler {
	cfg := oss.LoadDefaultConfig().
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test-ak", "test-sk")).
		WithRegion("cn-hangzhou").
		WithEndpoint(srv.URL).
		// Path style keeps the bucket in the request path, so the test server
		// sees the full key instead of a bucket-qualified host name.
		WithUsePathStyle(true).
		WithRetryer(retry.NopRetryer{})

	return &RayLogsHandler{
		OssClient:  oss.NewClient(cfg),
		OssBucket:  testBucket,
		OssRootDir: testRootDir,
	}
}

// objectKey returns the key a path style request addressed.
func objectKey(r *http.Request) string {
	key := strings.TrimPrefix(r.URL.Path, "/"+testBucket)
	return strings.TrimPrefix(key, "/")
}

func writeListResult(w http.ResponseWriter, prefix string, keys []string, commonPrefixes []string) {
	var body strings.Builder
	for _, key := range keys {
		body.WriteString(fmt.Sprintf("<Contents><Key>%s</Key><Size>1</Size></Contents>", key))
	}
	for _, cp := range commonPrefixes {
		body.WriteString(fmt.Sprintf("<CommonPrefixes><Prefix>%s</Prefix></CommonPrefixes>", cp))
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult><Name>%s</Name><Prefix>%s</Prefix><KeyCount>%d</KeyCount><MaxKeys>100</MaxKeys><IsTruncated>false</IsTruncated>%s</ListBucketResult>`,
		testBucket, prefix, len(keys)+len(commonPrefixes), body.String()))
}

// ListFiles has to hand back files without a trailing slash and subdirectories
// with one: callers such as ServerHandler.listFilesRecursive tell the two apart
// that way. The directory's own placeholder object, which CreateDirectory
// writes and a listing rooted at that directory returns as a key, is neither.
func TestListFilesSeparatesFilesFromDirectories(t *testing.T) {
	const dir = "session_2026-05-08_18-35-06_774618_1/logs/node123/events"
	wantPrefix := path.Join(testRootDir, testClusterPrefix, dir) + "/"

	var listed recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := r.URL.Query().Get("prefix")
		listed.add(prefix)
		if prefix != wantPrefix {
			writeListResult(w, prefix, nil, nil)
			return
		}
		if r.URL.Query().Get("delimiter") != "/" {
			// Without the delimiter the listing reports no CommonPrefixes and
			// returns everything below the prefix instead, so a request that
			// forgets it gets the nested file rather than the subdirectory it
			// stands for.
			writeListResult(w, prefix,
				[]string{prefix, prefix + "event_RAYLET.log", prefix + "event_GCS.log", prefix + "old/rotated.log"},
				nil)
			return
		}
		writeListResult(w, prefix,
			[]string{prefix, prefix + "event_RAYLET.log", prefix + "event_GCS.log"},
			[]string{prefix + "old/"})
	}))
	defer srv.Close()

	got := newTestHandler(t, srv).ListFiles(testClusterPrefix, dir)

	// Order is up to the backend; the contract is which entries come back and
	// whether each carries the trailing slash.
	want := []string{"event_GCS.log", "event_RAYLET.log", "old/"}
	if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
		t.Errorf("ListFiles() diff (-want +got):\n%s\nprefixes listed: %v", diff, listed.snapshot())
	}
}

// The keys are written out in full here rather than built with
// clustermetadata.EncodePath, so that a change in the layout has to be made in
// two places before this test agrees with it.
func TestListReadsClusterMetadataUnderRootDir(t *testing.T) {
	newerSession := "session_2026-05-08_18-35-06_774618"
	olderSession := "session_2026-05-07_09-00-00_000001"
	newer := time.Date(2026, 5, 8, 18, 35, 6, 774618000, time.UTC)
	older := time.Date(2026, 5, 7, 9, 0, 0, 1000, time.UTC)

	var listed recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := r.URL.Query().Get("prefix")
		listed.add(prefix)
		if prefix != metadataPrefix {
			writeListResult(w, prefix, nil, nil)
			return
		}
		writeListResult(w, prefix, []string{
			metadataPrefix + "raycluster/defaultns_mycluster1/" + olderSession,
			metadataPrefix + "rayjob/defaultns_myrayjob_mycluster2/" + newerSession,
			// A directory placeholder and a malformed entry: neither can be
			// decoded, and neither may take the whole listing down with it.
			metadataPrefix + "raycluster/",
			metadataPrefix + "raycluster/not-a-cluster-dir",
		}, nil)
	}))
	defer srv.Close()

	got := newTestHandler(t, srv).List()

	// ClusterInfoList sorts newest first, so the rayjob entry leads.
	want := []utils.ClusterInfo{
		{
			Name: "mycluster2", Namespace: "defaultns",
			OwnerKind: "rayjob", OwnerName: "myrayjob",
			SessionName:     newerSession,
			CreateTimeStamp: newer.Unix(),
			CreateTime:      "2026-05-08T18:35:06Z",
		},
		{
			Name: "mycluster1", Namespace: "defaultns",
			SessionName:     olderSession,
			CreateTimeStamp: older.Unix(),
			CreateTime:      "2026-05-07T09:00:00Z",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("List() diff (-want +got):\n%s\nprefixes listed: %v", diff, listed.snapshot())
	}
}

func TestCreateDirectoryWritesPlaceholderWhenMissing(t *testing.T) {
	const dir = "ray-logs/ray_cluster_history/raycluster/default/my-cluster/session_1/logs/node123/events"
	wantKey := dir + "/"

	var puts recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := objectKey(r)
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			puts.add(fmt.Sprintf("%s|%d", key, len(body)))
		default:
			t.Errorf("unexpected %s request for %q", r.Method, key)
		}
	}))
	defer srv.Close()

	if err := newTestHandler(t, srv).CreateDirectory(dir); err != nil {
		t.Fatalf("CreateDirectory: %v", err)
	}

	// The placeholder is what makes the directory visible to tools that list by
	// prefix; it carries no content.
	want := []string{wantKey + "|0"}
	if diff := cmp.Diff(want, puts.snapshot()); diff != "" {
		t.Errorf("objects written diff (-want +got):\n%s", diff)
	}
}

func TestCreateDirectoryLeavesExistingDirectoryAlone(t *testing.T) {
	const dir = "ray-logs/ray_cluster_history/raycluster/default/my-cluster/session_1/logs"

	var puts recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := objectKey(r)
		switch r.Method {
		case http.MethodHead:
			if key != dir+"/" {
				t.Errorf("existence check key = %q, want %q", key, dir+"/")
			}
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
		case http.MethodPut:
			puts.add(key)
		default:
			t.Errorf("unexpected %s request for %q", r.Method, key)
		}
	}))
	defer srv.Close()

	if err := newTestHandler(t, srv).CreateDirectory(dir); err != nil {
		t.Fatalf("CreateDirectory: %v", err)
	}

	if written := puts.snapshot(); len(written) != 0 {
		t.Errorf("CreateDirectory rewrote an existing directory: %v", written)
	}
}

func TestWriteFileUploadsBodyToGivenKey(t *testing.T) {
	const key = "ray-logs/ray_cluster_history/raycluster/default/my-cluster/session_1/logs/node123/raylet.out"
	const content = "raylet line one\nraylet line two\n"

	var uploads recorder
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotKey := objectKey(r)
		if r.Method != http.MethodPut {
			t.Errorf("unexpected %s request for %q", r.Method, gotKey)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading uploaded body: %v", err)
			return
		}
		uploads.add(gotKey + "|" + string(body))
	}))
	defer srv.Close()

	if err := newTestHandler(t, srv).WriteFile(key, bytes.NewReader([]byte(content))); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	want := []string{key + "|" + content}
	if diff := cmp.Diff(want, uploads.snapshot()); diff != "" {
		t.Errorf("uploads diff (-want +got):\n%s", diff)
	}
}
