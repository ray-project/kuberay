package storage

import (
	"io"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// StroageWriter is the interface for writing to storage
type StorageWriter interface {
	// CreateDirectory should create directory using given path
	CreateDirectory(path string) error
	// WriteFile should write everything from the reader
	WriteFile(file string, reader io.ReadSeeker) error
}

// StroageReader is the interface fr reading from storage
type StorageReader interface {
	// List returns a list of all available cluster and information
	ListClusters() []utils.ClusterInfo
	// GetContent will return a reader given the clusterID and the file to read
	GetContent(clusterId string, file string) io.Reader
	// ListFiles will return a list of files of current directory given the cluster and the directory
	// S3, minio, and GCS are all flat object storages, this assumes that file names are also "paths"
	ListFiles(clusterId, directory string) []string
}
