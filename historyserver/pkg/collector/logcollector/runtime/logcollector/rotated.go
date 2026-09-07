package logcollector

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/storage/clusterlogs"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// rotatedIdentity distinguishes one rotation generation of a log stream from
// every other generation of the same stream. Linux hands the inode of an evicted
// generation straight to the next one, so the last-modified time is what
// actually separates them. It is read from the opened descriptor and survives
// the .1 -> .2 renames Ray performs as the ring advances, which the inode change
// time would not.
type rotatedIdentity struct {
	modTimeNs int64
	inode     uint64
}

func (id rotatedIdentity) String() string {
	return fmt.Sprintf("%d-%d", id.modTimeNs, id.inode)
}

// rotatedCandidate is a rotation backup pinned by an open descriptor so that Ray
// cannot evict the generation while it uploads.
type rotatedCandidate struct {
	file       *os.File
	path       string
	objectName string
	size       int64
}

// rotationBaseName reports whether name is a Ray rotation backup ("raylet.out.2")
// and, if so, returns the active log it was rotated out of ("raylet.out").
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

// rotationIndex returns the backup index of a Ray rotation backup
// ("raylet.out.2" -> 2), or 0 when name is not one.
func rotationIndex(name string) int {
	base, ok := rotationBaseName(name)
	if !ok {
		return 0
	}
	index, err := strconv.Atoi(name[len(base)+1:])
	if err != nil {
		return 0
	}
	return index
}

// rotatedLogName builds the deterministic object name for one rotation
// generation: "worker-abc123-01000000-123.out.1" with modification time
// 1788398123456789012 and inode 4390125 becomes
// "worker-abc123-01000000-123.rotated.1788398123456789012-4390125.out". Leading
// with the time keeps a plain listing of one stream in rotation order.
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
	r.collectActiveSessionRotatedLogs(stop)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			logrus.Info("Shutdown signaled, stopping rotated log scan")
			return
		case <-ticker.C:
			r.collectActiveSessionRotatedLogs(stop)
		}
	}
}

// collectActiveSessionRotatedLogs re-resolves session_latest on every pass so
// backups are attributed to the session that produced them.
func (r *RayLogHandler) collectActiveSessionRotatedLogs(stop <-chan struct{}) {
	sessionDir, err := filepath.EvalSymlinks(utils.GetRaySessionLatestPath())
	if err != nil {
		logrus.Debugf("Rotated log scan: session_latest is not resolvable yet: %v", err)
		return
	}
	logsDir := filepath.Join(sessionDir, utils.RAY_SESSIONDIR_LOGDIR_NAME)
	r.collectRotatedLogsUnder(logsDir, filepath.Base(sessionDir), r.GetRayNodeName(), stop)
}

// collectRotatedLogsUnder collects every rotation backup below logsDir, highest
// rotation index first because that is the generation Ray evicts next. Each
// backup is opened only while it uploads, so a node with many streams does not
// pin a descriptor per backup. Walk errors are left unreported: entries
// disappear as Ray advances the ring, and the next scan covers what remains.
func (r *RayLogHandler) collectRotatedLogsUnder(logsDir, sessionID, nodeID string, stop <-chan struct{}) {
	objectPrefix := r.rotatedObjectPrefix(sessionID, nodeID)
	if objectPrefix == "" {
		logrus.Warnf("Skipping rotated log scan of %s: session or node ID is unknown", logsDir)
		return
	}

	var backups []string
	walkComplete := true
	_ = filepath.WalkDir(logsDir, func(absPath string, entry fs.DirEntry, walkErr error) error {
		walkComplete = walkComplete && walkErr == nil
		if walkErr == nil && entry.Type().IsRegular() {
			if _, isBackup := rotationBaseName(entry.Name()); isBackup {
				backups = append(backups, absPath)
			}
		}
		return nil
	})
	slices.SortStableFunc(backups, func(a, b string) int {
		return cmp.Compare(rotationIndex(filepath.Base(b)), rotationIndex(filepath.Base(a)))
	})

	seen := make(map[string]struct{}, len(backups))
	for _, absPath := range backups {
		// Shutdown collection picks up whatever this pass leaves behind.
		if stopRequested(stop) {
			logrus.Debug("Shutdown signaled, ending rotated log scan early")
			return
		}
		if objectName, ok := r.collectRotatedLogAt(absPath, logsDir, objectPrefix); ok {
			seen[objectName] = struct{}{}
		}
	}
	// Only a pass that saw the whole directory can tell which generations Ray has
	// dropped, so a partial walk leaves the uploaded set alone.
	if walkComplete {
		r.pruneRotatedUploaded(seen)
	}
}

// collectRotatedLog uploads absPath when it is a Ray rotation backup and reports
// whether it was one, so callers can skip their ordinary log handling. The
// periodic scan, shutdown and prev-logs paths all funnel through the same open
// and upload steps, which is what keeps one generation to one deterministic
// object.
func (r *RayLogHandler) collectRotatedLog(absPath, logsDir, objectPrefix string) bool {
	if _, ok := rotationBaseName(filepath.Base(absPath)); !ok {
		return false
	}
	if objectPrefix == "" {
		logrus.Warnf("Skipping rotated log %s: session or node ID is unknown", absPath)
		return true
	}
	r.collectRotatedLogAt(absPath, logsDir, objectPrefix)
	return true
}

// collectRotatedLogAt opens absPath, uploads it unless this run already did, and
// returns the object name of the generation it found. It reports false when the
// path lost the rotation race or could not be identified.
func (r *RayLogHandler) collectRotatedLogAt(absPath, logsDir, objectPrefix string) (string, bool) {
	candidate := r.openRotatedCandidate(absPath, logsDir, objectPrefix)
	if candidate == nil {
		return "", false
	}
	defer candidate.file.Close()

	if err := r.uploadRotatedCandidate(candidate); err != nil {
		logrus.Errorf("Failed to collect rotated log %s: %v", candidate.path, err)
	}
	return candidate.objectName, true
}

// rotatedObjectPrefix is the object directory a node's rotation backups are
// uploaded under, or "" when the session or node is not known yet.
func (r *RayLogHandler) rotatedObjectPrefix(sessionID, nodeID string) string {
	if sessionID == "" || nodeID == "" {
		return ""
	}
	return clusterlogs.LogsDir(r.RootDir, r.OwnerKind, r.OwnerName, r.RayClusterNamespace, r.RayClusterName, sessionID, nodeID)
}

// openRotatedCandidate pins absPath and derives its object key from the opened
// descriptor. It returns nil, having closed anything it opened, when the path
// lost the rotation race or cannot be identified.
func (r *RayLogHandler) openRotatedCandidate(absPath, logsDir, objectPrefix string) *rotatedCandidate {
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
		modTimeNs: info.ModTime().UnixNano(),
		inode:     stat.Ino,
	})
	if !ok {
		file.Close()
		return nil
	}

	return &rotatedCandidate{
		file:       file,
		path:       absPath,
		objectName: path.Join(objectPrefix, filepath.ToSlash(filepath.Dir(relPath)), objectBaseName),
		size:       info.Size(),
	}
}

// uploadRotatedCandidate uploads a generation this run has not written yet. A
// failed upload is not recorded, so the periodic active-session scan retries it
// while the generation remains in Ray's rotation ring. The prev-logs caller gets
// no such retry: that directory is removed after a single pass.
func (r *RayLogHandler) uploadRotatedCandidate(candidate *rotatedCandidate) error {
	r.rotatedMu.Lock()
	defer r.rotatedMu.Unlock()

	if _, uploaded := r.rotatedUploaded[candidate.objectName]; uploaded {
		return nil
	}
	if err := r.Writer.CreateDirectory(path.Dir(candidate.objectName)); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", candidate.objectName, err)
	}
	if err := r.Writer.WriteFile(candidate.objectName, candidate.file); err != nil {
		return fmt.Errorf("failed to write object %s: %w", candidate.objectName, err)
	}
	if r.rotatedUploaded == nil {
		r.rotatedUploaded = make(map[string]struct{})
	}
	r.rotatedUploaded[candidate.objectName] = struct{}{}

	logrus.Infof("Uploaded rotated log %s (object: %s, size: %d bytes)", candidate.path, candidate.objectName, candidate.size)
	return nil
}

// pruneRotatedUploaded drops generations Ray has removed from the rotation ring,
// keeping only the object names seen on disk during a complete scan.
func (r *RayLogHandler) pruneRotatedUploaded(seen map[string]struct{}) {
	r.rotatedMu.Lock()
	defer r.rotatedMu.Unlock()

	for objectName := range r.rotatedUploaded {
		if _, ok := seen[objectName]; !ok {
			delete(r.rotatedUploaded, objectName)
		}
	}
}

func stopRequested(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}
