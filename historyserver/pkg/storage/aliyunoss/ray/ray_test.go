package ray

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

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
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

func newTestOSSClient(t *testing.T, handler http.Handler) *oss.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := oss.LoadDefaultConfig().
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test-ak", "test-sk")).
		WithRegion("cn-hangzhou").
		WithEndpoint(server.URL).
		WithUsePathStyle(true).
		WithRetryMaxAttempts(1)
	return oss.NewClient(cfg)
}

func TestGetContentUsesRootDirInObjectKey(t *testing.T) {
	const (
		bucket    = "test-bucket"
		rootDir   = "tmp"
		clusterID = "dlc1hphloqj7jax0_quotaf5jxu1uzuel"
		fileName  = "session_2026-05-08_18-35-06_774618_1/logs/events/event_CORE_WORKER_256.log"
		content   = "direct object content"
	)
	expectedPath := "/" + path.Join(bucket, rootDir, clusterID, fileName)

	var (
		mu       sync.Mutex
		requests []string
	)
	client := newTestOSSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()

		if r.URL.Path != expectedPath {
			http.NotFound(w, r)
			return
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	handler := &RayLogsHandler{OssClient: client, OssBucket: bucket, OssRootDir: rootDir}

	reader := handler.GetContent(clusterID, fileName)
	if reader == nil {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("GetContent() returned nil after requests %v; want GET %q", requests, expectedPath)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read GetContent() result: %v", err)
	}
	if string(got) != content {
		t.Fatalf("GetContent() = %q, want %q", got, content)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 || requests[0] != expectedPath {
		t.Fatalf("GetContent() requested %v, want only [%q]", requests, expectedPath)
	}
}

func TestGetContentFallbackUsesRootDirInListPrefix(t *testing.T) {
	const (
		bucket    = "test-bucket"
		rootDir   = "tmp"
		clusterID = "dlc1hphloqj7jax0_quotaf5jxu1uzuel"
		fileName  = "session_2026-05-08_18-35-06_774618_1/logs/events/event_CORE_WORKER_256.log"
		content   = "fallback object content"
	)
	expectedKey := path.Join(rootDir, clusterID, fileName)
	expectedDir := path.Dir(expectedKey)
	fallbackKey := path.Join(expectedDir, "worker-123", path.Base(fileName))
	expectedPaths := []string{
		"/" + path.Join(bucket, expectedKey),
		"/" + bucket + "/",
		"/" + path.Join(bucket, fallbackKey),
	}

	var (
		mu         sync.Mutex
		requests   []string
		listPrefix string
	)
	client := newTestOSSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPrefix := r.URL.Query().Get("prefix")
		mu.Lock()
		requests = append(requests, r.URL.Path)
		if r.URL.Path == expectedPaths[1] && r.URL.Query().Get("list-type") == "2" {
			listPrefix = requestPrefix
		}
		mu.Unlock()

		switch {
		case r.URL.Path == expectedPaths[0]:
			http.NotFound(w, r)
		case r.URL.Path == expectedPaths[1] && r.URL.Query().Get("list-type") == "2":
			if requestPrefix != expectedDir+"/" {
				http.Error(w, "unexpected list prefix: "+requestPrefix, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			body := fmt.Sprintf("<ListBucketResult><Name>%s</Name><Prefix>%s/</Prefix><MaxKeys>100</MaxKeys><IsTruncated>false</IsTruncated><Contents><Key>%s</Key></Contents><KeyCount>1</KeyCount></ListBucketResult>", bucket, expectedDir, fallbackKey)
			if _, err := io.WriteString(w, body); err != nil {
				t.Errorf("write list response: %v", err)
			}
		case r.URL.Path == expectedPaths[2]:
			if _, err := io.WriteString(w, content); err != nil {
				t.Errorf("write object response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	handler := &RayLogsHandler{OssClient: client, OssBucket: bucket, OssRootDir: rootDir}

	reader := handler.GetContent(clusterID, fileName)
	if reader == nil {
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("GetContent() returned nil after requests %v with list prefix %q", requests, listPrefix)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read GetContent() result: %v", err)
	}
	if string(got) != content {
		t.Fatalf("GetContent() = %q, want %q", got, content)
	}

	mu.Lock()
	defer mu.Unlock()
	if strings.Join(requests, "\n") != strings.Join(expectedPaths, "\n") {
		t.Fatalf("GetContent() requested %v, want %v", requests, expectedPaths)
	}
	if listPrefix != expectedDir+"/" {
		t.Fatalf("list prefix = %q, want %q", listPrefix, expectedDir+"/")
	}
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
