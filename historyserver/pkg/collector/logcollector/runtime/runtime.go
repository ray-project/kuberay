package runtime

import (
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/runtime/logcollector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

func NewCollector(config *types.RayCollectorConfig, writer storage.StorageWriter) RayLogCollector {
	handler := logcollector.RayLogHandler{
		IsHead:   config.Role == "Head",
		LogFiles: make(chan string),

		RootDir:    config.RootDir,
		SessionDir: config.SessionDir,

		RayClusterName:      config.RayClusterName,
		RayClusterNamespace: config.RayClusterNamespace,
		RayNodeName:         config.RayNodeName,

		LogBatching:          config.LogBatching,
		PushInterval:         config.PushInterval,
		DashboardAddress:     config.DashboardAddress,
		AdditionalEndpoints:  config.AdditionalEndpoints,
		EndpointPollInterval: config.EndpointPollInterval,

		HttpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,              // Max idle connections
				MaxIdleConnsPerHost: 20,               // Max idle connections per host
				IdleConnTimeout:     90 * time.Second, // Idle connection timeout
			},
		},
		Writer:       writer,
		ShutdownChan: make(chan struct{}),
	}
	logDir := strings.TrimSpace(filepath.Join(config.SessionDir, utils.RAY_SESSIONDIR_LOGDIR_NAME))
	handler.LogDir = logDir
	cluster := utils.ClusterRef{Namespace: handler.RayClusterNamespace, Name: handler.RayClusterName}
	clusterRootDir := fmt.Sprintf("%s/", path.Clean(path.Join(handler.RootDir, cluster.StoragePrefix())))
	handler.ClusterDir = clusterRootDir

	return &handler
}
