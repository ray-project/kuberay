package logcollector

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
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

	// Store file paths to be processed on shutdown
	logFilePaths map[string]bool
	filePathMu   sync.Mutex

	// Channel for signaling shutdown
	shutdownChan chan struct{}
}

func (r *RayLogHandler) Start(stop <-chan struct{}) error {
	go r.Run(stop)
	return nil
}

func (r *RayLogHandler) Run(stop <-chan struct{}) error {
	watchPath := r.LogDir

	// Initialize log file paths storage
	r.logFilePaths = make(map[string]bool)
	r.shutdownChan = make(chan struct{})

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Fatalf("Create fsnotify NewWatcher error %v", err)
	}
	defer watcher.Close()

	// Setup signal handling for SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM)
	go r.WatchLogsLoops(watcher, watchPath)
	go r.PushLogsLoops()

	select {
	case <-sigChan:
		logrus.Info("Received SIGTERM, processing all logs...")
		r.processAllLogs()
		close(r.shutdownChan)
	}
	logrus.Warnf("Receive stop single, so stop ray collector ")
	return nil
}

func (r *RayLogHandler) WaitForStop() <-chan struct{} {
	return r.shutdownChan
}

func (r *RayLogHandler) AddLogFile(absoluteLogPathName string) {
	r.LogFiles <- absoluteLogPathName
}

func (r *RayLogHandler) PushLog(absoluteLogPathName string) error {
	// Simply store the file path for later processing
	absoluteLogPathName = strings.TrimSpace(absoluteLogPathName)
	absoluteLogPathName = filepath.Clean(absoluteLogPathName)

	r.filePathMu.Lock()
	r.logFilePaths[absoluteLogPathName] = true
	r.filePathMu.Unlock()

	logrus.Infof("Registered log file for later processing: %s", absoluteLogPathName)
	return nil
}

func (r *RayLogHandler) processAllLogs() {
	logrus.Info("Processing all log files...")
	r.filePathMu.Lock()
	defer r.filePathMu.Unlock()

	if err := r.Writter.CreateDirectory(r.RootDir); err != nil {
		logrus.Errorf("Failed to create root directory %s: %v", r.RootDir, err)
		return
	}

	for filePath := range r.logFilePaths {
		// Process each file now
		if err := r.processLogFile(filePath); err != nil {
			logrus.Errorf("Failed to process log file %s: %v", filePath, err)
		}
	}

	logrus.Info("Finished processing all log files")
}

func (r *RayLogHandler) processLogFile(absoluteLogPathName string) error {
	// 计算相对路径
	relativePath := strings.TrimPrefix(absoluteLogPathName, fmt.Sprintf("%s/", r.LogDir))
	// 分割相对路径为子目录和文件名
	subdir, filename := filepath.Split(relativePath)
	sessionName := path.Base(r.SessionDir)
	logDir := utils.GetLogDir(r.RootDir, r.RayClusterName, r.RayClusterID, sessionName, r.RayNodeName)

	if len(subdir) != 0 {
		dirName := path.Join(logDir, subdir)
		if err := r.Writter.CreateDirectory(dirName); err != nil {
			logrus.Errorf("Failed to create directory '%s': %v", dirName, err)
			return err
		}
	}

	objectName := path.Join(logDir, subdir, filename)
	logrus.Infof("Processing log file %s (object: %s)", absoluteLogPathName, objectName)

	// Read the entire file content only when processing
	content, err := os.ReadFile(absoluteLogPathName)
	if err != nil {
		logrus.Errorf("Failed to read file %s: %v", absoluteLogPathName, err)
		return err
	}

	// Write to storage
	err = r.Writter.WriteFile(objectName, bytes.NewReader(content))
	if err != nil {
		logrus.Errorf("Failed to write object %s: %v", objectName, err)
		return err
	}

	logrus.Infof("Successfully wrote object %s, size: %d bytes", objectName, len(content))
	return nil
}

func (r *RayLogHandler) PushLogsLoops() {
	if r.EnableMeta {
		metadir := path.Clean(r.RootDir + "/" + "metadir")
		metafile := path.Clean(metadir + "/" + fmt.Sprintf("%s/%v",
			utils.AppendRayClusterNameID(r.RayClusterName, r.RayClusterID),
			path.Base(r.SessionDir),
		))
		if err := r.Writter.CreateDirectory(path.Dir(metafile)); err != nil {
			logrus.Errorf("CreateObjectIfNotExist %s error %v", metadir, err)
			return
		}
		if err := r.Writter.WriteFile(metafile, strings.NewReader("")); err != nil {
			logrus.Errorf("CreateObjectIfNotExist %s error %v", metafile, err)
			return
		}
	}
	for {
		select {
		case logfile := <-r.LogFiles:
			r.PushLog(logfile)
		case <-r.shutdownChan:
			logrus.Warnf("Receive shutdown signal, so return PushLogsLoop")
			return
		}
	}
}

func (r *RayLogHandler) WatchLogsLoops(watcher *fsnotify.Watcher, walkPath string) {
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
		case <-r.shutdownChan:
			logrus.Warnf("Receive shutdown signal, so return watchFileLoops")
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
				logrus.Warnf("Watcher error, so return watchFileLoops")
				return
			}
		}
	}
}
