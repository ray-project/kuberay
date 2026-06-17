package localtest

import (
	"fmt"
	"io"
	"strings"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// MockReader is a mock implementation of the StorageReader interface
type MockReader struct {
	data     map[string]map[string]string
	clusters []utils.ClusterInfo
	metas    map[string]*utils.MetaJson
}

// NewMockReader creates a new mock reader
func NewMockReader() *MockReader {
	clusters := []utils.ClusterInfo{
		{
			Name:            "cluster-1",
			Namespace:       "default",
			SessionName:     "session_2023-01-01_00-00-00_000000",
			CreateTime:      "2023-01-01T00:00:00Z",
			CreateTimeStamp: 1672531200000,
		},
		{
			Name:            "cluster-2",
			Namespace:       "default",
			SessionName:     "session_2023-01-02_00-00-00_000000",
			CreateTime:      "2023-01-02T00:00:00Z",
			CreateTimeStamp: 1672617600000,
		},
	}

	data := map[string]map[string]string{
		"cluster-1": {
			"log.txt":       "This is log content for cluster-1\nMultiple lines\nof log content",
			"metadata.json": "{\n  \"name\": \"cluster-1\",\n  \"sessionName\": \"session_2023-01-01_00-00-00_000000\",\n  \"createTime\": \"2023-01-01T00:00:00Z\"\n}",
		},
		"cluster-2": {
			"log.txt":       "This is log content for cluster-2\nMultiple lines\nof log content",
			"metadata.json": "{\n  \"name\": \"cluster-2\",\n  \"sessionName\": \"session_2023-01-02_00-00-00_000000\",\n  \"createTime\": \"2023-01-02T00:00:00Z\"\n}",
		},
	}

	metas := map[string]*utils.MetaJson{
		"metadir/cluster-1_default/session_2023-01-01_00-00-00_000000.meta.json": {
			SessionName:      "session_2023-01-01_00-00-00_000000",
			ClusterID:        "cluster-1",
			ClusterNamespace: "default",
			Status:           utils.SessionStatusCompleted,
			EndTime:          1672534800,
		},
		"metadir/cluster-2_default/session_2023-01-02_00-00-00_000000.meta.json": {
			SessionName:      "session_2023-01-02_00-00-00_000000",
			ClusterID:        "cluster-2",
			ClusterNamespace: "default",
			Status:           utils.SessionStatusInProgress,
		},
	}

	return &MockReader{
		clusters: clusters,
		data:     data,
		metas:    metas,
	}
}

// List returns all available files from backend, enriched with meta.json data
func (r *MockReader) List() []utils.ClusterInfo {
	result := make([]utils.ClusterInfo, len(r.clusters))
	copy(result, r.clusters)
	for i := range result {
		metaPath := utils.MetadirMetaJsonPath("", result[i].Name, result[i].Namespace, result[i].SessionName)
		if meta, err := r.ReadMeta(metaPath); err == nil {
			result[i].Status = meta.Status
			result[i].EndTime = meta.EndTime
		}
	}
	return result
}

// GetContent returns content for a specific file
func (r *MockReader) GetContent(clusterId string, fileName string) io.Reader {
	if clusterData, ok := r.data[clusterId]; ok {
		if content, ok := clusterData[fileName]; ok {
			return strings.NewReader(content)
		}
	}
	return strings.NewReader("")
}

func (r *MockReader) ListFiles(clusterId string, dir string) []string {
	if clusterData, ok := r.data[clusterId]; ok {
		files := make([]string, 0, len(clusterData))
		for fileName := range clusterData {
			files = append(files, fileName)
		}
		return files
	}
	return []string{}
}

func (r *MockReader) ReadMeta(path string) (*utils.MetaJson, error) {
	if meta, ok := r.metas[path]; ok {
		return meta, nil
	}
	return nil, fmt.Errorf("meta not found: %s", path)
}

// NewReader creates a new StorageReader
func NewReader(c *types.RayHistoryServerConfig, jd map[string]interface{}) (storage.StorageReader, error) {
	return NewMockReader(), nil
}
