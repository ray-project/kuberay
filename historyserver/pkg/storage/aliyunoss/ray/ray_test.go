package ray

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

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

func TestGetContentPathComparison(t *testing.T) {
	handler := &RayLogsHandler{OssRootDir: "tmp"}
	clusterID := "dlc1hphloqj7jax0_quotaf5jxu1uzuel"
	fileName := "session_2026-04-09_06-41-42_754637_1/logs/abc123/events/event_RAYLET.log"
	fullPath := handler.objectKey(clusterID, fileName)
	// f is a full root-dir-inclusive key returned by _listFiles (onlyBase=false).
	f := "tmp/dlc1hphloqj7jax0_quotaf5jxu1uzuel/session_2026-04-09_06-41-42_754637_1/logs/abc123/events/event_RAYLET.log"

	if path.Base(f) != path.Base(fullPath) {
		t.Errorf("expected path.Base(%q) == path.Base(%q)", f, fullPath)
	}
}

func TestObjectKeyIncludesRootDir(t *testing.T) {
	handler := &RayLogsHandler{OssRootDir: "tmp"}
	clusterID := "dlc1hphloqj7jax0_quotaf5jxu1uzuel"
	fileName := "session_2026-05-08_18-35-06_774618_1/logs/events/event_CORE_WORKER_256.log"

	got := handler.objectKey(clusterID, fileName)
	want := "tmp/dlc1hphloqj7jax0_quotaf5jxu1uzuel/session_2026-05-08_18-35-06_774618_1/logs/events/event_CORE_WORKER_256.log"

	if got != want {
		t.Fatalf("objectKey() = %q, want %q", got, want)
	}
}

func TestObjectDirIncludesRootDir(t *testing.T) {
	handler := &RayLogsHandler{OssRootDir: "tmp"}
	clusterID := "dlc1hphloqj7jax0_quotaf5jxu1uzuel"
	fileName := "session_2026-05-08_18-35-06_774618_1/logs/events/event_CORE_WORKER_256.log"

	got := handler.objectDir(clusterID, fileName)
	want := "tmp/dlc1hphloqj7jax0_quotaf5jxu1uzuel/session_2026-05-08_18-35-06_774618_1/logs/events"

	if got != want {
		t.Fatalf("objectDir() = %q, want %q", got, want)
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
