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

// rotatedIdentity distinguishes one rotation generation of a log stream from
// every other generation of the same stream. Ray rotates at a byte threshold so
// sizes repeat, and Linux hands the inode of an evicted generation straight to
// the next one, so the last-modified time is what actually separates them. It is
// read from the opened descriptor and survives the .1 -> .2 renames Ray performs
// as the ring advances, which the inode change time would not.
type rotatedIdentity struct {
	inode     uint64
	size      int64
	modTimeNs int64
}

func (id rotatedIdentity) String() string {
	return fmt.Sprintf("%d-%d-%d", id.inode, id.size, id.modTimeNs)
}

// rotatedCandidate is a rotation backup pinned by an open descriptor so that a
// slow upload cannot let Ray evict a generation this scan already discovered.
type rotatedCandidate struct {
	file       *os.File
	path       string
	objectDir  string
	objectName string
	size       int64
}

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
// generation: "worker-abc123-01000000-123.out.1" with inode 4390125, size
// 1048576 and modification time 1788398123456789012 becomes
// "worker-abc123-01000000-123.rotated.4390125-1048576-1788398123456789012.out".
func rotatedLogName(backupName string, id rotatedIdentity) (string, bool) {
	base, ok := rotationBaseName(backupName)
	if !ok {
		return "", false
	}
	identity := utils.RotatedLogMarker + id.String()
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

// collectRotatedLogsUnder collects every rotation backup below logsDir. Each one
// is opened during discovery so that uploading an earlier generation cannot let
// Ray evict a later one before its own upload starts. Walk errors are left
// unreported: entries disappear as Ray advances the ring, and the next scan
// covers whatever remains.
func (r *RayLogHandler) collectRotatedLogsUnder(logsDir, sessionID, nodeID string) {
	if sessionID == "" || nodeID == "" {
		logrus.Warnf("Skipping rotated log scan of %s: session or node ID is unknown", logsDir)
		return
	}

	var candidates []*rotatedCandidate
	_ = filepath.WalkDir(logsDir, func(absPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && entry.Type().IsRegular() {
			if _, isBackup := rotationBaseName(entry.Name()); isBackup {
				if candidate := r.openRotatedCandidate(absPath, logsDir, sessionID, nodeID); candidate != nil {
					candidates = append(candidates, candidate)
				}
			}
		}
		return nil
	})
	r.uploadRotatedCandidates(candidates)
}

// collectRotatedLog uploads absPath when it is a Ray rotation backup and reports
// whether it was one, so callers can skip their ordinary log handling. The
// periodic scan, shutdown and prev-logs paths all funnel through the same open
// and upload steps, which is what keeps one generation to one deterministic
// object.
func (r *RayLogHandler) collectRotatedLog(absPath, logsDir, sessionID, nodeID string) bool {
	if _, ok := rotationBaseName(filepath.Base(absPath)); !ok {
		return false
	}
	if sessionID == "" || nodeID == "" {
		logrus.Warnf("Skipping rotated log %s: session or node ID is unknown", absPath)
		return true
	}
	if candidate := r.openRotatedCandidate(absPath, logsDir, sessionID, nodeID); candidate != nil {
		r.uploadRotatedCandidates([]*rotatedCandidate{candidate})
	}
	return true
}

// openRotatedCandidate pins absPath and derives its object key from the opened
// descriptor. It returns nil, having closed anything it opened, when the path
// lost the rotation race, cannot be identified, or is already uploaded.
func (r *RayLogHandler) openRotatedCandidate(absPath, logsDir, sessionID, nodeID string) *rotatedCandidate {
	relPath, err := filepath.Rel(logsDir, absPath)
	if err != nil {
		logrus.Errorf("Failed to get relative path for rotated log %s: %v", absPath, err)
		return nil
	}

	file, err := os.Open(absPath)
	if err != nil {
		// A missing path is Ray advancing the ring between the walk and the open.
		if !errors.Is(err, fs.ErrNotExist) {
			logrus.Errorf("Failed to open rotated log %s: %v", absPath, err)
		}
		return nil
	}

	// Stat the descriptor rather than the path so the identity and the uploaded
	// bytes describe the same inode even if Ray renames the file mid-upload.
	info, err := file.Stat()
	if err != nil {
		logrus.Errorf("Failed to stat rotated log %s: %v", absPath, err)
		file.Close()
		return nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		logrus.Errorf("Inode of rotated log %s is unavailable on this platform", absPath)
		file.Close()
		return nil
	}
	objectBaseName, ok := rotatedLogName(filepath.Base(relPath), rotatedIdentity{
		inode:     stat.Ino,
		size:      info.Size(),
		modTimeNs: info.ModTime().UnixNano(),
	})
	if !ok {
		file.Close()
		return nil
	}

	logDir := clusterlogs.LogsDir(r.RootDir, r.OwnerKind, r.OwnerName, r.RayClusterNamespace, r.RayClusterName, sessionID, nodeID)
	subDir := filepath.ToSlash(filepath.Dir(relPath))
	candidate := &rotatedCandidate{
		file:       file,
		path:       absPath,
		objectName: path.Join(logDir, subDir, objectBaseName),
		size:       info.Size(),
	}
	if subDir != "." {
		candidate.objectDir = path.Join(logDir, subDir)
	}

	// Release generations an earlier pass already uploaded so only new ones stay
	// pinned for the rest of the scan.
	if r.rotatedObjectUploaded(candidate.objectName) {
		file.Close()
		return nil
	}
	return candidate
}

func (r *RayLogHandler) rotatedObjectUploaded(objectName string) bool {
	r.rotatedMu.Lock()
	defer r.rotatedMu.Unlock()
	_, uploaded := r.rotatedUploaded[objectName]
	return uploaded
}

// uploadRotatedCandidates uploads pinned candidates in turn and closes every
// descriptor. A failed upload neither aborts the remaining candidates nor
// records the object, so the periodic active-session scan retries that
// generation while it remains in Ray's rotation ring. The prev-logs caller gets
// no such retry: that directory is removed after a single pass.
func (r *RayLogHandler) uploadRotatedCandidates(candidates []*rotatedCandidate) {
	if len(candidates) == 0 {
		return
	}
	r.rotatedMu.Lock()
	defer r.rotatedMu.Unlock()
	if r.rotatedUploaded == nil {
		r.rotatedUploaded = make(map[string]struct{})
	}
	for _, candidate := range candidates {
		if err := r.uploadRotatedCandidate(candidate); err != nil {
			logrus.Errorf("Failed to collect rotated log %s: %v", candidate.path, err)
		}
		candidate.file.Close()
	}
}

// uploadRotatedCandidate must be called with rotatedMu held.
func (r *RayLogHandler) uploadRotatedCandidate(candidate *rotatedCandidate) error {
	if _, uploaded := r.rotatedUploaded[candidate.objectName]; uploaded {
		return nil
	}
	if candidate.objectDir != "" {
		if err := r.Writer.CreateDirectory(candidate.objectDir); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", candidate.objectName, err)
		}
	}
	if err := r.Writer.WriteFile(candidate.objectName, candidate.file); err != nil {
		return fmt.Errorf("failed to write object %s: %w", candidate.objectName, err)
	}
	r.rotatedUploaded[candidate.objectName] = struct{}{}

	logrus.Infof("Uploaded rotated log %s (object: %s, size: %d bytes)", candidate.path, candidate.objectName, candidate.size)
	return nil
}
