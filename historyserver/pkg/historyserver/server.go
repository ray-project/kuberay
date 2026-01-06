package historyserver

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/sirupsen/logrus"
)

type ServerHandler struct {
	maxClusters  int
	rootDir      string
	dashboardDir string

	reader        storage.StorageReader
	clientManager *ClientManager
	eventHandler  *eventserver.EventHandler
}

func NewServerHandler(c *types.RayHistoryServerConfig, dashboardDir string, reader storage.StorageReader, clientManager *ClientManager, eventHandler *eventserver.EventHandler) *ServerHandler {
	return &ServerHandler{
		reader:        reader,
		clientManager: clientManager,
		eventHandler:  eventHandler,

		rootDir:      c.RootDir,
		dashboardDir: dashboardDir,
	}
}

func (s *ServerHandler) Run(stop chan struct{}) error {
	s.RegisterRouter()
	port := ":8080"
	server := &http.Server{
		Addr:         port,            // 监听地址
		ReadTimeout:  2 * time.Second, // 请求超时
		WriteTimeout: 5 * time.Second, // 写入响应超时
		IdleTimeout:  5 * time.Second, // 空闲超时
	}
	go func() {
		logrus.Infof("Starting server on %s", port)
		err := server.ListenAndServe()
		if err != nil {
			logrus.Fatalf("Error starting server: %v", err)
			os.Exit(1)
		}
		logrus.Errorf("Start server succssful, but end ...")
	}()

	<-stop
	logrus.Warnf("Receive stop single, so stop ray history server")
	// 创建一个上下文，超时10秒
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// 关闭服务器
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Ray HistoryServer forced to shutdown: %v", err)
	}
	return nil
}
