package logcollector

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
)

// activeLogKind is how an active (not yet rotated) session log is uploaded
// while the cluster runs.
type activeLogKind int

const (
	// activeLogSkip leaves the file to the shutdown upload.
	activeLogSkip activeLogKind = iota
	// activeLogChunk uploads only the bytes appended since the last pass, as
	// one object per pass, because the file only ever grows.
	activeLogChunk
	// activeLogOverwrite re-uploads the whole file because Ray rewrites it in
	// place, so there is no stable prefix to build on.
	activeLogOverwrite
)

// chunkDirSuffix names the object directory that holds a file's chunks, next
// to where the whole file lands on shutdown: "<file>.chunks/<offset>".
const chunkDirSuffix = ".chunks"

// tailState is where the previous pass stopped reading one active log. The
// inode detects Ray swapping the file underneath the path on rotation.
//
// Progress is in memory only and a collector restart will re-upload the whole file from chunk 0.
type tailState struct {
	inode  uint64
	offset int64
}

// classifyActiveLog picks the upload strategy for a path relative to the
// session logs directory. Only the files the dashboard widgets read for a
// running cluster are covered; everything else waits for shutdown.
//
// Ray's log directory layout: https://docs.ray.io/en/latest/ray-observability/user-guides/configure-logging.html#logging-directory-structure
func classifyActiveLog(relPath string) activeLogKind {
	relPath = filepath.ToSlash(relPath)
	dir, base := path.Split(relPath)
	switch {
	case relPath == "debug_state.txt":
		// file is overwritten
		// https://github.com/ray-project/ray/blob/c8466ab8fd2b14691633b163c56b5ef036d7d146/src/ray/raylet/node_manager.cc#L2613-L2619
		return activeLogOverwrite
	case dir == "" && strings.HasPrefix(base, "job-driver-") && strings.HasSuffix(base, ".log"):
		// file is appended
		// https://github.com/ray-project/ray/blob/c8466ab8fd2b14691633b163c56b5ef036d7d146/python/ray/dashboard/modules/job/job_supervisor.py#L178-L188
		return activeLogChunk
	case dir == "" && strings.HasPrefix(base, "worker-") && (strings.HasSuffix(base, ".out") || strings.HasSuffix(base, ".err")):
		// file is appended
		// https://github.com/ray-project/ray/blob/c8466ab8fd2b14691633b163c56b5ef036d7d146/src/ray/util/pipe_logger.cc#L198-L203
		return activeLogChunk
	case dir == "events/" && strings.HasPrefix(base, "event_") && strings.HasSuffix(base, ".log"):
		// file is appended
		// https://github.com/ray-project/ray/blob/c8466ab8fd2b14691633b163c56b5ef036d7d146/python/ray/_private/event/event_logger.py#L119
		return activeLogChunk
	default:
		return activeLogSkip
	}
}

// chunkObjectName is the object one chunk uploads to. Offsets are zero-padded
// so a lexicographic listing is also byte order.
//
// For example, the chunk holding bytes [4096, 8192) will return:
//
//	job-driver-raysubmit_abc.log.chunks/00000000000000004096
func chunkObjectName(objectName string, offset int64) string {
	return fmt.Sprintf("%s%s/%020d", objectName, chunkDirSuffix, offset)
}

// uploadNewBytes uploads whatever absPath gained since the last pass as one
// chunk object.
//
// TODO: log rotation is not supported yet. A rotated file restarts from offset
// zero, overwriting the old generation's chunks.
func (r *RayLogHandler) uploadNewBytes(absPath, objectName string) error {
	file, err := os.Open(absPath)
	if err != nil {
		return fmt.Errorf("failed to open active log %s: %w", absPath, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat active log %s: %w", absPath, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inode of active log %s is unavailable on this platform", absPath)
	}

	if r.activeLogs == nil {
		r.activeLogs = make(map[string]tailState)
	}
	state := r.activeLogs[absPath]
	// Inode change or shrink means rotation or truncation. We will start over from offset 0.
	if state.inode != stat.Ino || info.Size() < state.offset {
		state = tailState{inode: stat.Ino}
	}
	if info.Size() == state.offset {
		return nil
	}

	chunkName := chunkObjectName(objectName, state.offset)
	if err := r.Writer.CreateDirectory(path.Dir(chunkName)); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", chunkName, err)
	}
	// Fix the range at the stat size so bytes appended mid-upload go to the next chunk.
	chunk := io.NewSectionReader(file, state.offset, info.Size()-state.offset)
	if err := r.Writer.WriteFile(chunkName, chunk); err != nil {
		return fmt.Errorf("failed to write object %s: %w", chunkName, err)
	}

	state.offset = info.Size()
	r.activeLogs[absPath] = state
	logrus.Debugf("Uploaded active log chunk %s (object: %s, size: %d bytes)", absPath, chunkName, chunk.Size())
	return nil
}

// collectActiveLog uploads one active log according to classifyActiveLog.
func (r *RayLogHandler) collectActiveLog(absPath, logsDir, objectPrefix, sessionID, nodeID string) error {
	relPath, err := filepath.Rel(logsDir, absPath)
	if err != nil {
		return err
	}
	switch classifyActiveLog(relPath) {
	case activeLogChunk:
		return r.uploadNewBytes(absPath, path.Join(objectPrefix, filepath.ToSlash(relPath)))
	case activeLogOverwrite:
		return r.processSessionLatestLogFile(absPath, logsDir, sessionID, nodeID)
	default:
		return nil
	}
}
