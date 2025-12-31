package collector

import (
	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/storage/aliyunoss/ray"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/storage/localtest"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/storage/s3"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
)

type WriterRegistry map[string]func(globalData *types.RayCollectorConfig, data map[string]interface{}) (storage.StorageWriter, error)

func GetWriterRegistry() WriterRegistry {
	return writerRegistry
}

var writerRegistry = WriterRegistry{
	"aliyunoss": ray.NewWritter,
	"s3":        s3.NewWritter,
}

type ReaderRegistry map[string]func(globalData *types.RayHistoryServerConfig, data map[string]interface{}) (storage.StorageReader, error)

func GetReaderRegistry() ReaderRegistry {
	return readerRegistry
}

var readerRegistry = ReaderRegistry{
	"aliyunoss": ray.NewReader,
	"localtest": localtest.NewReader,
	"s3":        s3.NewReader,
}
