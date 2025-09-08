package backend

import (
	"github.com/ray-project/kuberay/historyserver/backend/collector/storage"
	"github.com/ray-project/kuberay/historyserver/backend/collector/storage/aliyunoss/ray"
	"github.com/ray-project/kuberay/historyserver/backend/types"
)

type WriterRegistry map[string]func(globalData *types.RayCollectorConfig, data map[string]interface{}) (storage.StorageWritter, error)

func GetWriterRegistry() WriterRegistry {
	return writerRegistry
}

var writerRegistry = WriterRegistry{
	"aliyunoss": ray.NewWritter,
}

type ReaderRegistry map[string]func(globalData *types.RayCollectorConfig, data map[string]interface{}) (storage.StorageReader, error)

func GetReaderRegistry() ReaderRegistry {
	return readerRegistry
}

var readerRegistry = ReaderRegistry{
	"aliyunoss": ray.NewReader,
}
