package backend

import (
	"github.com/ray-project/kuberay/historyserver/backend/collector/storage"
	"github.com/ray-project/kuberay/historyserver/backend/types"
)

type Registry map[string]func(globalData *types.RayCollectorConfig, data map[string]interface{}) (storage.StorageWritter, error)

func GetRegistry() Registry {
	return registry
}

var registry = Registry{}
