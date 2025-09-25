package logcollector

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hpcloud/tail"
	"github.com/ray-project/kuberay/historyserver/backend/collector/storage"
	"github.com/ray-project/kuberay/historyserver/utils"
	"github.com/sirupsen/logrus"
)

type RayLogHandler struct {
	EnableMeta bool

	RootDir    string
	LogDir     string
	SessionDir string
	LogFiles   chan string
	// fill after start
	MetaDir string

	RayClusterName string
	RayClusterID   string
	RayNodeName    string

	LogBatching  int
	PushInterval time.Duration

	HttpClient *http.Client
	Writter    storage.StorageWritter

	// key job id
	JobResourcesUrlInfo map[string]*JobUrlInfo
}

func (r *RayLogHandler) Start(stop chan struct{}) {
	go r.Run(stop)
}

func (r *RayLogHandler) Run(stop chan struct{}) error {
	watchPath := r.LogDir

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Fatalf("Create fsnotify NewWatcher error %v", err)
	}
	defer watcher.Close()
	go r.WatchLogsLoops(watcher, watchPath, stop)
	go r.PushLogsLoops(stop)

	if r.EnableMeta {
		// Persist meta data
		go r.PersistMetaLoop(stop)
	}

	<-stop
	logrus.Warnf("Receive stop single, so stop ray collector ")
	return nil
}

func (r *RayLogHandler) AddLogFile(absoluteLogPathName string) {
	r.LogFiles <- absoluteLogPathName
}

func (r *RayLogHandler) PushLog(absoluteLogPathName string) error {
	// 判断文件是否存在
	// 计算相对路径
	absoluteLogPathName = strings.TrimSpace(absoluteLogPathName)
	absoluteLogPathName = filepath.Clean(absoluteLogPathName)

	relativePath := strings.TrimPrefix(absoluteLogPathName, fmt.Sprintf("%s/", r.LogDir))
	// 分割相对路径为子目录和文件名
	// subdir events/aa/
	// filename a.txt
	subdir, filename := filepath.Split(relativePath)
	logDir := utils.GetLogDir(r.RootDir, r.RayClusterName, r.RayClusterID, r.RayNodeName)
	if len(subdir) != 0 {
		dirName := path.Join(logDir, subdir)
		if err := r.Writter.CreateDirectory(dirName); err != nil {
			logrus.Errorf("Failed to create directory '%s': %v", dirName, err)
			return err
		}
	}

	objectName := path.Join(logDir, subdir, filename)
	logrus.Infof("Begin to create and append oss object %s by absoluteLogPathName %s, oss subdir [%s] filename [%s] relativePath[%s]",
		objectName, absoluteLogPathName, subdir, filename, relativePath)

	// utils.DeleteObject(r.OssBucket, objectName)

	// 从文件开始读,
	// Go 1.20推荐使用 io.SeekEnd, 老版本可能需要改为os.SEEK_END
	seek := &tail.SeekInfo{Offset: 0, Whence: io.SeekStart}
	t, err := tail.TailFile(absoluteLogPathName, tail.Config{
		Follow:   true,
		Location: seek,
	})
	if err != nil {
		logrus.Errorf("Create TailFile %s error %v", absoluteLogPathName, err)
		return err
	}

	var nextPos int64 = 0

	logrus.Infof("Begin to first append oss object %s ...", objectName)
	nextPos, err = r.Writter.Append(objectName, strings.NewReader(""), nextPos)
	if err != nil {
		logrus.Errorf("First append object %s error %v",
			objectName,
			err)
		return err
	}

	lines := 0
	lastPush := time.Now()
	buf := bytes.NewBufferString("")
	for {
		select {
		case <-time.Tick(r.PushInterval):
			if lines > 0 {
				nextPos, err = r.Writter.Append(objectName, buf, nextPos)
				if err != nil {
					logrus.Errorf("Tail file %s to object %s error, append value: %v, nextPos %d error [%v]",
						absoluteLogPathName,
						objectName,
						buf.String(),
						nextPos,
						err)
					return err
				}

				now := time.Now()
				logrus.Infof("Tail file %s to object %s success, by interval, append value %s, lines %v, interval %v",
					absoluteLogPathName, objectName, buf.String(), lines, now.Sub(lastPush).Seconds())

				lastPush = now
				lines = 0
				buf = bytes.NewBufferString("")
			}
		case line, ok := <-t.Lines:
			if !ok {
				logrus.Infof("channel for path %v is closed", absoluteLogPathName)
				return nil
			}
			lines++
			buf.WriteString(line.Text + "\n")
			if lines >= r.LogBatching {
				nextPos, err = r.Writter.Append(objectName, buf, nextPos)
				if err != nil {
					logrus.Errorf("Tail file %s to object %s error, append value: %v, nextPos %d error [%v]",
						absoluteLogPathName,
						objectName,
						buf.String(),
						nextPos,
						err)
					return err
				}

				now := time.Now()
				logrus.Infof("Tail file %s to object %s success, by line count, append value %s, lines %v, interval %v",
					absoluteLogPathName, objectName, buf.String(), lines, now.Sub(lastPush).Seconds())

				lastPush = now
				lines = 0
				buf = bytes.NewBufferString("")
			}
		}
	}
}

func (r *RayLogHandler) PushLogsLoops(stop chan struct{}) {

	if err := r.Writter.CreateDirectory(r.RootDir); err != nil {
		logrus.Errorf("Failed to create oss root directory %s: %v", r.RootDir, err)
		return
	}

	for {
		select {
		case logfile := <-r.LogFiles:
			go r.PushLog(logfile)
		case <-stop:
			logrus.Warnf("Receive stop signal, so return PushLogsLoop")
			return
		}
	}
}

func (r *RayLogHandler) WatchLogsLoops(watcher *fsnotify.Watcher, walkPath string, stop chan struct{}) {
	// 监听当前目录
	if err := watcher.Add(walkPath); err != nil {
		logrus.Fatalf("Watcher rootpath %s error %v", r.LogDir, err)
	}

	err := filepath.Walk(walkPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logrus.Errorf("Walk path %s error %v", walkPath, err)
			return err // 返回错误
		}
		// 检查是否是文件
		if !info.IsDir() {
			logrus.Infof("Walk find new file %s", path) // 输出文件路径
			go r.AddLogFile(path)
		} else {
			logrus.Infof("Walk find new dir %s", path) // 输出dir路径
			if err := watcher.Add(path); err != nil {
				logrus.Fatalf("Watcher add %s error %v", r.LogDir, err)
			}
		}
		return nil
	})

	if err != nil {
		logrus.Errorf("Walk path %s error %++v", walkPath, err)
		return
	}
	logrus.Infof("Walk path %s success", walkPath)

	for {
		select {
		case <-stop:
			logrus.Warnf("Receive stop signal, so return watchFileLoops")
			return
		case event, ok := <-watcher.Events:
			if !ok {
				logrus.Warnf("Receive watcher events not ok")
				return
			}
			if event.Op == fsnotify.Create {
				name := event.Name
				info, _ := os.Stat(name)

				// 判断是文件还是目录
				if !info.IsDir() {
					logrus.Infof("Watch find: create a new file %s", name)
					r.AddLogFile(name)
				} else {
					if err := watcher.Add(name); err != nil {
						logrus.Fatalf("Watch add file %s error %v", name, err)
					}
				}
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				close(stop)
				logrus.Warnf("Watcher error, so return watchFileLoops")
				return
			}
		}
	}
}
