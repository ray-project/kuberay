// Package s3 is
/*
Copyright 2024 by the kuberay authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package s3

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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	awss3 "github.com/aws/aws-sdk-go/service/s3"
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
	// relativePath := strings.TrimPrefix(absoluteLogPathName, logdir)
	// Split relative path into subdir and filename
	subdir, filename := filepath.Split(relativePath)
	test_path_join := path.Join("aa./b/c/d", "e")
	t.Logf("file [%s] logdir [%s] subdir %s filename %s", absoluteLogPathName,
		logdir, subdir, filename)
	t.Logf("test_path_join [%s]", test_path_join)
}

// GetContent builds its object key from three pieces, and a deployment that sets
// a root dir only works if all three end up in the key. These tests pin that down
// by asserting on the key that actually reaches the server, using path style
// addressing (the same mode the MinIO support already relies on) so the full key
// stays in the request path.
const (
	testBucket  = "test-bucket"
	testRootDir = "ray-logs"
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

	sess, err := session.NewSession(&aws.Config{
		Credentials:      credentials.NewStaticCredentials("test-ak", "test-sk", ""),
		Endpoint:         aws.String(srv.URL),
		Region:           aws.String("us-east-1"),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
		MaxRetries:       aws.Int(0),
	})
	if err != nil {
		t.Fatalf("creating test session: %v", err)
	}

	return &RayLogsHandler{
		S3Client:  awss3.New(sess),
		S3Bucket:  testBucket,
		S3RootDir: testRootDir,
	}
}

// requestKey returns the object key a path style request addressed, and whether the
// request was a ListObjectsV2 call rather than a GetObject call.
func requestKey(r *http.Request) (key string, isList bool) {
	key = strings.TrimPrefix(r.URL.Path, "/"+testBucket)
	key = strings.TrimPrefix(key, "/")
	return key, r.URL.Query().Get("list-type") == "2"
}

func TestGetContentUsesRootDir(t *testing.T) {
	wantKey := path.Join(testRootDir, testClusterPrefix, testFileName)
	const wantContent = "core worker log line"

	var requested recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, isList := requestKey(r)
		if isList {
			writeEmptyListResult(w, r.URL.Query().Get("prefix"))
			return
		}
		requested.add(key)
		if key != wantKey {
			writeNoSuchKey(w)
			return
		}
		_, _ = io.WriteString(w, wantContent)
	}))
	defer srv.Close()

	reader := newTestHandler(t, srv).GetContent(testClusterPrefix, testFileName)
	gotKeys := requested.snapshot()
	if reader == nil {
		t.Fatalf("GetContent returned nil; keys requested: %v, want %q", gotKeys, wantKey)
	}

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading returned content: %v", err)
	}
	if string(got) != wantContent {
		t.Errorf("content = %q, want %q", got, wantContent)
	}
	if len(gotKeys) == 0 || gotKeys[0] != wantKey {
		t.Errorf("first requested key = %v, want %q", gotKeys, wantKey)
	}
}

// The recovery path lists the containing directory when the direct fetch misses,
// and that listing prefix has to be rooted too or it silently finds nothing.
func TestGetContentFallbackListsUnderRootDir(t *testing.T) {
	wantKey := path.Join(testRootDir, testClusterPrefix, testFileName)
	// The object sits one level deeper than asked for, so only the fallback can
	// reach it.
	nestedKey := path.Join(path.Dir(wantKey), "rotated", path.Base(wantKey))
	const wantContent = "recovered log line"

	var listed recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, isList := requestKey(r)
		if isList {
			prefix := r.URL.Query().Get("prefix")
			listed.add(prefix)
			if strings.HasPrefix(nestedKey, prefix) {
				writeListResult(w, prefix, nestedKey)
				return
			}
			writeEmptyListResult(w, prefix)
			return
		}
		if key != nestedKey {
			writeNoSuchKey(w)
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

	wantPrefix := path.Dir(wantKey) + "/"
	for _, prefix := range listPrefixes {
		if prefix == wantPrefix {
			return
		}
	}
	t.Errorf("list prefixes = %v, want one equal to %q", listPrefixes, wantPrefix)
}

func writeNoSuchKey(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`)
}

func writeEmptyListResult(w http.ResponseWriter, prefix string) {
	w.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult><Name>%s</Name><Prefix>%s</Prefix><KeyCount>0</KeyCount><MaxKeys>100</MaxKeys><IsTruncated>false</IsTruncated></ListBucketResult>`, testBucket, prefix))
}

func writeListResult(w http.ResponseWriter, prefix string, keys ...string) {
	var contents strings.Builder
	for _, key := range keys {
		contents.WriteString(fmt.Sprintf("<Contents><Key>%s</Key><Size>1</Size></Contents>", key))
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(w, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult><Name>%s</Name><Prefix>%s</Prefix><KeyCount>%d</KeyCount><MaxKeys>100</MaxKeys><IsTruncated>false</IsTruncated>%s</ListBucketResult>`, testBucket, prefix, len(keys), contents.String()))
}

func TestWalk(t *testing.T) {
	watchPath := fmt.Sprintf("%s/test/LLogs/", utils.GetTmpRayRoot())
	filepath.Walk(watchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logrus.Errorf("Walk path error %v", err)
			return err // Return error
		}
		// Check if it's a file
		if !info.IsDir() {
			logrus.Infof("Find new file %s", path) // Log file path
		}
		return nil
	})
}
