package utils

import (
	"context"
	"io"
)

// ContextReader wraps an io.Reader and checks context cancellation on each Read.
// This is useful for ensuring that long-running read operations (like reading large files
// or slow network streams) can be cancelled when the context times out or is cancelled.
type ContextReader struct {
	ctx    context.Context
	reader io.Reader
}

// NewContextReader creates a new ContextReader that wraps the given reader with context cancellation support.
func NewContextReader(ctx context.Context, reader io.Reader) *ContextReader {
	return &ContextReader{
		ctx:    ctx,
		reader: reader,
	}
}

// Read implements io.Reader. It checks if the context is cancelled before delegating to the underlying reader.
// If the context is cancelled, it returns the context error immediately without reading.
func (r *ContextReader) Read(p []byte) (n int, err error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
	}
	return r.reader.Read(p)
}
