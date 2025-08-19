package storage

import "io"

// StorageWriter is the interface for storage writer.
type StorageWritter interface {
	CreateDirectory(path string) error
	Append(file string, reader io.Reader, appendPosition int64) (nextPod int64, err error)
}

// HistoryServer create readers for each storage runtime
// Depends on the implementation of front end.
type StorageReader interface {
}
