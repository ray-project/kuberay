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
		IsHead: config.Role == "Head",
		LogFiles:   make(chan string),

		RootDir:    config.RootDir,
		SessionDir: config.SessionDir,

		RayClusterName: config.RayClusterName,
		RayClusterID:   config.RayClusterID,
		RayNodeName:    config.RayNodeName,

		LogBatching:      config.LogBatching,
		PushInterval:     config.PushInterval,
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
	// clusterRootDir uses flat key format (name_id) for S3/OSS performance optimization.
	// See utils.connector for the design rationale.
	clusterRootDir := fmt.Sprintf("%s/", path.Clean(path.Join(handler.RootDir, handler.RayClusterName+"_"+handler.RayClusterID)))
	handler.ClusterDir = clusterRootDir

	return &handler
}
