package runtime

import (
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/runtime/logcollector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

func NewCollector(config *types.RayCollectorConfig, writer storage.StorageWriter) RayLogCollector {
	handler := logcollector.RayLogHandler{
		EnableMeta: config.Role == "Head",
		LogFiles:   make(chan string),

		RootDir:    config.RootDir,
		SessionDir: config.SessionDir,

		RayClusterName: config.RayClusterName,
		RayClusterID:   config.RayClusterID,
		RayNodeName:    config.RayNodeName,

		LogBatching:  config.LogBatching,
		PushInterval: config.PushInterval,

		HttpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,              // 最大空闲连接数
				MaxIdleConnsPerHost: 20,               // 每个主机的最大空闲连接数
				IdleConnTimeout:     90 * time.Second, // 空闲连接的超时时间
			},
		},
		Writer:       writer,
		ShutdownChan: make(chan struct{}),
	}
	logDir := strings.TrimSpace(path.Join(config.SessionDir, utils.RAY_SESSIONDIR_LOGDIR_NAME))
	handler.LogDir = logDir
	rootMetaDir := fmt.Sprintf("%s/", path.Clean(path.Join(handler.RootDir, handler.RayClusterName+"_"+handler.RayClusterID, "meta")))
	handler.MetaDir = rootMetaDir

	return &handler
}
