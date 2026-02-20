package localtest

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// MockReader is a mock implementation of the StorageReader interface
type MockReader struct {
	data     map[string]map[string]string
	clusters []utils.ClusterInfo
}

// NewMockReader creates a new mock reader
func NewMockReader() *MockReader {
	clusters := []utils.ClusterInfo{
		{
			Name:            "cluster-1",
			SessionName:     "session-1",
			CreateTime:      "2023-01-01T00:00:00Z",
			CreateTimeStamp: 1672531200000,
		},
		{
			Name:            "cluster-2",
			SessionName:     "session-2",
			CreateTime:      "2023-01-02T00:00:00Z",
			CreateTimeStamp: 1672617600000,
		},
	}

	data := map[string]map[string]string{
		"cluster-1": {
			"log.txt":       "This is log content for cluster-1\nMultiple lines\nof log content",
			"metadata.json": "{\n  \"name\": \"cluster-1\",\n  \"sessionName\": \"session-1\",\n  \"createTime\": \"2023-01-01T00:00:00Z\"\n}",
		},
		"cluster-2": {
			"log.txt":       "This is log content for cluster-2\nMultiple lines\nof log content",
			"metadata.json": "{\n  \"name\": \"cluster-2\",\n  \"sessionName\": \"session-2\",\n  \"createTime\": \"2023-01-02T00:00:00Z\"\n}",
		},
	}

	return &MockReader{
		clusters: clusters,
		data:     data,
	}
}

// List returns all available files from backend
func (r *MockReader) List(ctx context.Context) []utils.ClusterInfo {
	return r.clusters
}

// GetContent returns content for a specific file
func (r *MockReader) GetContent(ctx context.Context, clusterId string, fileName string) io.Reader {
	if clusterData, ok := r.data[clusterId]; ok {
		if content, ok := clusterData[fileName]; ok {
			return strings.NewReader(content)
		}
	}
	return strings.NewReader("")
}

func (r *MockReader) ListFiles(ctx context.Context, clusterId string, dir string) []string {
	if clusterData, ok := r.data[clusterId]; ok {
		files := make([]string, 0, len(clusterData))
		for fileName := range clusterData {
			files = append(files, fileName)
		}
		return files
	}
	return []string{}
}

// NewReader creates a new StorageReader
func NewReader(c *types.RayHistoryServerConfig, jd map[string]interface{}) (storage.StorageReader, error) {
	return NewMockReader(), nil
}

// DelayedMockReader is a mock implementation of the StorageReader interface with configurable delay.
// This is useful for testing timeout behavior.
type DelayedMockReader struct {
	MockReader
	delay time.Duration
}

// NewDelayedMockReader creates a mock reader that delays responses by the specified duration.
// The delay is applied in GetContent to simulate slow storage operations (e.g., network latency, slow disk I/O).
func NewDelayedMockReader(delay time.Duration) *DelayedMockReader {
	return &DelayedMockReader{
		MockReader: *NewMockReader(),
		delay:      delay,
	}
}

// List returns all available cluster info from backend with delay and context cancellation support.
// If the context is cancelled before the delay completes, it returns an empty slice.
func (r *DelayedMockReader) List(ctx context.Context) []utils.ClusterInfo {
	select {
	case <-time.After(r.delay):
		return r.MockReader.List(ctx)
	case <-ctx.Done():
		return []utils.ClusterInfo{}
	}
}

// GetContent simulates slow file read but respects context cancellation.
// If the context is cancelled before the delay completes, it returns nil immediately.
func (r *DelayedMockReader) GetContent(ctx context.Context, clusterId string, fileName string) io.Reader {
	select {
	case <-time.After(r.delay):
		return r.MockReader.GetContent(ctx, clusterId, fileName)
	case <-ctx.Done():
		return nil
	}
}

// ListFiles returns a list of files for a given cluster and directory with delay and context cancellation support.
// If the context is cancelled before the delay completes, it returns an empty slice.
func (r *DelayedMockReader) ListFiles(ctx context.Context, clusterId string, dir string) []string {
	select {
	case <-time.After(r.delay):
		return r.MockReader.ListFiles(ctx, clusterId, dir)
	case <-ctx.Done():
		return []string{}
	}
}
