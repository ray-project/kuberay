package localtest

import (
	"encoding/json"
	"io"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// MockWriter is a mock implementation of the StorageWriter interface
type MockWriter struct {
	directories map[string]bool
	files       map[string]string // filePath -> content
	metaJsons   map[string]string // metaPath -> JSON string
}

// NewMockWriter creates a new mock writer
func NewMockWriter() *MockWriter {
	return &MockWriter{
		directories: make(map[string]bool),
		files:       make(map[string]string),
		metaJsons:   make(map[string]string),
	}
}

// CreateDirectory creates a directory (mock implementation)
func (w *MockWriter) CreateDirectory(path string) error {
	w.directories[path] = true
	return nil
}

// WriteFile writes a file (mock implementation)
func (w *MockWriter) WriteFile(file string, reader io.ReadSeeker) error {
	content, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	w.files[file] = string(content)
	return nil
}

// WriteMeta writes a meta.json file (mock implementation)
func (w *MockWriter) WriteMeta(path string, meta utils.MetaJson) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	w.metaJsons[path] = string(data)
	return nil
}

// GetFileContent returns the content of a written file for testing purposes
func (w *MockWriter) GetFileContent(file string) (string, bool) {
	content, exists := w.files[file]
	return content, exists
}

// HasDirectory checks if a directory was created
func (w *MockWriter) HasDirectory(path string) bool {
	return w.directories[path]
}

// NewWriter creates a new StorageWriter
func NewWriter(c *types.RayHistoryServerConfig, jd map[string]interface{}) (storage.StorageWriter, error) {
	return NewMockWriter(), nil
}
