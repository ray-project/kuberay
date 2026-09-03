package logcollector

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/storage/clusterlogs"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// rotationBaseName returns the active log name a Ray rotation backup was rotated
// out of ("raylet.out.2" -> "raylet.out") and whether name is such a backup.
// Ray numbers backups from 1 and reuses those names as the ring moves, so the
// index carries no identity and is dropped from the uploaded object name.
func rotationBaseName(name string) (string, bool) {
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 || dot == len(name)-1 {
		return "", false
	}
	index := name[dot+1:]
	if index[0] == '0' {
		return "", false
	}
	for _, c := range index {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	return name[:dot], true
}

// rotatedLogName builds the deterministic object name for one rotation
// generation: "worker-abc123-01000000-123.out.1" with inode 4390125 and size
// 1048576 becomes "worker-abc123-01000000-123.rotated.4390125-1048576.out".
func rotatedLogName(backupName string, inode uint64, size int64) (string, bool) {
	base, ok := rotationBaseName(backupName)
	if !ok {
		return "", false
	}
	identity := fmt.Sprintf("%s%d-%d", utils.RotatedLogMarker, inode, size)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		// Dotfile such as ".out": keep it whole rather than invent an extension.
		return base + identity, true
	}
	return stem + identity + ext, true
}

// scanRotatedLogs uploads completed rotation backups from the active session
// until stop is closed, so a generation is preserved before Ray's rotation ring
// overwrites it.
func (r *RayLogHandler) scanRotatedLogs(stop <-chan struct{}) {
	interval := r.RotatedLogScanInterval
	if interval <= 0 {
		interval = utils.DefaultRotatedLogScanInterval
	}
	logrus.Infof("Started scanning for rotated logs (interval=%v)", interval)
	r.collectActiveSessionRotatedLogs()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			logrus.Info("Shutdown signaled, stopping rotated log scan")
			return
		case <-ticker.C:
			r.collectActiveSessionRotatedLogs()
		}
	}
}

// collectActiveSessionRotatedLogs re-resolves session_latest on every pass so
// backups are attributed to the session that produced them.
func (r *RayLogHandler) collectActiveSessionRotatedLogs() {
	sessionDir, err := filepath.EvalSymlinks(utils.GetRaySessionLatestPath())
	if err != nil {
		logrus.Debugf("Rotated log scan: session_latest is not resolvable yet: %v", err)
		return
	}
	logsDir := filepath.Join(sessionDir, utils.RAY_SESSIONDIR_LOGDIR_NAME)
	r.collectRotatedLogsUnder(logsDir, filepath.Base(sessionDir), r.GetRayNodeName())
}

// collectRotatedLogsUnder collects every rotation backup below logsDir. Walk
// errors are left unreported: entries disappear as Ray advances the rotation
// ring, and the next scan covers whatever remains.
func (r *RayLogHandler) collectRotatedLogsUnder(logsDir, sessionID, nodeID string) {
	_ = filepath.WalkDir(logsDir, func(absPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && entry.Type().IsRegular() {
			r.collectRotatedLog(absPath, logsDir, sessionID, nodeID)
		}
		return nil
	})
}

// collectRotatedLog uploads absPath when it is a Ray rotation backup and reports
// whether it was one, so callers can skip their ordinary log handling. The
// periodic scan, shutdown and prev-logs paths all funnel through here, which is
// what keeps one generation to one deterministic object.
func (r *RayLogHandler) collectRotatedLog(absPath, logsDir, sessionID, nodeID string) bool {
	if _, ok := rotationBaseName(filepath.Base(absPath)); !ok {
		return false
	}
	if sessionID == "" || nodeID == "" {
		logrus.Warnf("Skipping rotated log %s: session or node ID is unknown", absPath)
		return true
	}
	relPath, err := filepath.Rel(logsDir, absPath)
	if err != nil {
		logrus.Errorf("Failed to get relative path for rotated log %s: %v", absPath, err)
		return true
	}

	r.rotatedMu.Lock()
	defer r.rotatedMu.Unlock()
	if r.rotatedUploaded == nil {
		r.rotatedUploaded = make(map[string]struct{})
	}
	if err := r.uploadRotatedLog(absPath, relPath, sessionID, nodeID); err != nil {
		logrus.Errorf("Failed to collect rotated log %s: %v", absPath, err)
	}
	return true
}

// uploadRotatedLog uploads one rotation backup. It must be called with rotatedMu
// held. The object is recorded only after the write succeeds, so the periodic
// active-session scan retries a failed generation while it remains in Ray's
// rotation ring. The prev-logs caller gets no such retry: that directory is
// removed after a single pass.
func (r *RayLogHandler) uploadRotatedLog(absPath, relPath, sessionID, nodeID string) error {
	file, err := os.Open(absPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Ray advanced the rotation ring between the walk and the open.
			return nil
		}
		return fmt.Errorf("failed to open %s: %w", absPath, err)
	}
	defer file.Close()

	// Stat the descriptor rather than the path so the identity and the uploaded
	// bytes describe the same inode even if Ray renames the file mid-upload.
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", absPath, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inode of %s is unavailable on this platform", absPath)
	}
	objectBaseName, ok := rotatedLogName(filepath.Base(relPath), stat.Ino, info.Size())
	if !ok {
		return nil
	}

	logDir := clusterlogs.LogsDir(r.RootDir, r.OwnerKind, r.OwnerName, r.RayClusterNamespace, r.RayClusterName, sessionID, nodeID)
	subDir := filepath.ToSlash(filepath.Dir(relPath))
	objectName := path.Join(logDir, subDir, objectBaseName)
	if _, uploaded := r.rotatedUploaded[objectName]; uploaded {
		return nil
	}

	if subDir != "." {
		if err := r.Writer.CreateDirectory(path.Join(logDir, subDir)); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", objectName, err)
		}
	}
	if err := r.Writer.WriteFile(objectName, file); err != nil {
		return fmt.Errorf("failed to write object %s: %w", objectName, err)
	}
	r.rotatedUploaded[objectName] = struct{}{}

	logrus.Infof("Uploaded rotated log %s (object: %s, size: %d bytes)", absPath, objectName, info.Size())
	return nil
}
