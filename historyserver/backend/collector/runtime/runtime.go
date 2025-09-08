package runtime

import (
	"fmt"
	"net/http"
	"path"
	"time"

	"github.com/ray-project/kuberay/historyserver/backend/collector/runtime/logcollector"
	"github.com/ray-project/kuberay/historyserver/backend/collector/storage"
	"github.com/ray-project/kuberay/historyserver/backend/types"
)

func NewCollector(config *types.RayCollectorConfig, writter storage.StorageWritter) RayLogCollector {
	handler := logcollector.RayLogHandler{
		LogFiles: make(chan string),

		RootDir:    config.RootDir,
		SessionDir: config.SessionDir,

		RayClusterName: config.RayNodeName,
		RayClusterID:   config.RayClusterID,

		LogBatching:  config.LogBatching,
		PushInterval: config.PushInterval,

		HttpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,              // 最大空闲连接数
				MaxIdleConnsPerHost: 20,               // 每个主机的最大空闲连接数
				IdleConnTimeout:     90 * time.Second, // 空闲连接的超时时间
			},
		},
		Writter: writter,
	}
	rootMetaDir := fmt.Sprintf("%s/", path.Clean(path.Join(handler.RootDir, handler.RayClusterName+"_"+handler.RayClusterID, "_meta")))
	handler.MetaDir = rootMetaDir

	return &handler
}
