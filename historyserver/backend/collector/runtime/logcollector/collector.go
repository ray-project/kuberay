package logcollector

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hpcloud/tail"
	"github.com/ray-project/kuberay/historyserver/backend/collector/storage"
	"github.com/sirupsen/logrus"
)

type RayLogHandler struct {
	EnableMeta bool

	RootDir  string
	LogDir   string
	LogFiles chan string

	LogBatching  int
	PushInterval time.Duration

	Writter storage.StorageWritter
}

func (r *RayLogHandler) Start(stop <-chan int) {

}

func (h *RayLogHandler) Run(stop chan struct{}) error {
	watchPath := h.LogDir

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Fatalf("Create fsnotify NewWatcher error %v", err)
	}
	defer watcher.Close()
	go h.WatchLogsLoops(watcher, watchPath, stop)
	go h.PushLogsLoops(stop)

	if h.EnableMeta {
		// Persist meta data
		// go h.PersistMetaLoop(stop)
	}

	select {
	case <-stop:
		logrus.Warnf("Receive stop single, so stop ray collector ")
		return nil
	}
	logrus.Errorf("Run exist ...")
	return nil
}

func (h *RayLogHandler) AddLogFile(absoluteLogPathName string) {
	h.LogFiles <- absoluteLogPathName
}

func (h *RayLogHandler) PushLog(absoluteLogPathName string) error {
	// 判断文件是否存在
	// 计算相对路径
	absoluteLogPathName = strings.TrimSpace(absoluteLogPathName)
	absoluteLogPathName = filepath.Clean(absoluteLogPathName)

	relativePath := strings.TrimPrefix(absoluteLogPathName, fmt.Sprintf("%s/", h.LogDir))
	// 分割相对路径为子目录和文件名
	// subdir events/aa/
	// filename a.txt
	subdir, filename := filepath.Split(relativePath)

	if len(subdir) != 0 {
		dirName := path.Join(h.RootDir, subdir)
		if err := h.Writter.CreateDirectory(dirName); err != nil {
			logrus.Errorf("Failed to create directory '%s': %v", dirName, err)
			return err
		}
	}

	objectName := path.Join(h.RootDir, subdir, filename)
	logrus.Infof("Begin to create and append oss object %s by absoluteLogPathName %s, oss subdir [%s] filename [%s] relativePath[%s]",
		objectName, absoluteLogPathName, subdir, filename, relativePath)

	// utils.DeleteObject(h.OssBucket, objectName)

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
	nextPos, err = h.Writter.Append(objectName, strings.NewReader(""), nextPos)
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
		case <-time.Tick(h.PushInterval):
			if lines > 0 {
				nextPos, err = h.Writter.Append(objectName, buf, nextPos)
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
			if lines >= h.LogBatching {
				nextPos, err = h.Writter.Append(objectName, buf, nextPos)
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

func (h *RayLogHandler) PushLogsLoops(stop chan struct{}) {

	if err := h.Writter.CreateDirectory(h.RootDir); err != nil {
		logrus.Errorf("Failed to create oss root directory %s: %v", h.RootDir, err)
		return
	}

	for {
		select {
		case logfile := <-h.LogFiles:
			go h.PushLog(logfile)
		case <-stop:
			logrus.Warnf("Receive stop signal, so return PushLogsLoop")
			return
		}
	}
}

func (h *RayLogHandler) WatchLogsLoops(watcher *fsnotify.Watcher, walkPath string, stop chan struct{}) {
	// 监听当前目录
	if err := watcher.Add(walkPath); err != nil {
		logrus.Fatalf("Watcher rootpath %s error %v", h.LogDir, err)
	}

	err := filepath.Walk(walkPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logrus.Errorf("Walk path %s error %v", walkPath, err)
			return err // 返回错误
		}
		// 检查是否是文件
		if !info.IsDir() {
			logrus.Infof("Walk find new file %s", path) // 输出文件路径
			go h.AddLogFile(path)
		} else {
			logrus.Infof("Walk find new dir %s", path) // 输出dir路径
			if err := watcher.Add(path); err != nil {
				logrus.Fatalf("Watcher add %s error %v", h.LogDir, err)
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
					h.AddLogFile(name)
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
