package runtime

import (
	"github.com/ray-project/kuberay/historyserver/backend/collector/runtime/logcollector"
	"github.com/ray-project/kuberay/historyserver/backend/collector/storage"
	"github.com/ray-project/kuberay/historyserver/backend/types"
)

func NewCollector(config *types.RayCollectorConfig, writter storage.StorageWritter) RayLogCollector {
	handler := logcollector.RayLogHandler{
		LogFiles: make(chan string),

		LogBatching:  config.LogBatching,
		PushInterval: config.PushInterval,

		Writter: writter,
	}

	return &handler
}
