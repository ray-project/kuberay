package eventcollector

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emicklei/go-restful/v3"
	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// Defaults for disk-first event storage. Kept aligned with the
// collector-memory-control-proposal.md design document.
const (
	defaultRotationInterval = 5 * time.Minute
	defaultMaxFileSizeBytes = int64(100) * 1024 * 1024
	defaultMaxDiskBytes     = int64(200) * 1024 * 1024

	rotationCheckInterval = 30 * time.Second
	diskReconcileInterval = 1 * time.Minute
	bufWriterSize         = 64 * 1024
	diskPressureWatermark = 0.8
	rotationQueueSize     = 256

	categoryNodeEvents = "node_events"
	categoryJobPrefix  = "job_events"
)

// Options groups tunables for disk-first event storage.
type Options struct {
	DataDir          string
	RotationInterval time.Duration
	MaxFileSizeBytes int64
	MaxDiskBytes     int64
	// CompressionEnabled controls whether rotated JSONL files are gzipped
	// before being uploaded to remote storage. When false, plain JSONL files
	// are uploaded as-is. Events are always written to local disk first
	// regardless of this setting.
	CompressionEnabled bool
}

// eventTypesWithJobID enumerates event types whose payload carries a jobId.
// Mirrors the previous in-memory implementation.
var eventTypesWithJobID = []string{
	// Job Events (Driver Job)
	"driverJobDefinitionEvent",
	"driverJobLifecycleEvent",

	// Task Events (Normal Task)
	"taskDefinitionEvent",
	"taskLifecycleEvent",
	"taskProfileEvents",

	// Actor Events (Actor Task + Actor Definition)
	"actorTaskDefinitionEvent",
	"actorDefinitionEvent",
}

var nodeEventType = []string{"NODE_LIFECYCLE_EVENT", "NODE_DEFINITION_EVENT"}

// activeFileState wraps the per-category append target.
type activeFileState struct {
	file        *os.File
	writer      *bufio.Writer
	path        string
	category    string
	sessionName string
	nodeID      string
	size        int64
	createdAt   time.Time
}

// rotationTask describes a rotated JSONL file ready for upload.
type rotationTask struct {
	jsonlPath   string
	category    string
	sessionName string
	nodeID      string
	createdAt   time.Time
	size        int64
	// extOverride, when non-empty, forces the remote key extension instead of
	// deriving it from ec.compressionEnabled. Used during resume to preserve the
	// original file type.
	extOverride string
}

// EventCollector persists events to local disk as JSONL and asynchronously
// compresses + uploads rotated files to remote storage.
type EventCollector struct {
	storageWriter storage.StorageWriter

	stopped chan struct{} // closed after graceful shutdown completes

	clusterNamespace   string
	sessionDir         string
	nodeID             string
	clusterName        string
	sessionName        string
	root               string
	currentSessionName string
	currentNodeID      string

	// Disk-first storage configuration.
	dataDir            string
	rotationInterval   time.Duration
	maxFileSizeBytes   int64
	maxDiskBytes       int64
	compressionEnabled bool

	draining atomic.Bool // set during shutdown to reject new events

	writeMu       sync.Mutex
	activeFiles   map[string]*activeFileState
	rotationQueue chan rotationTask
	totalDiskUsed atomic.Int64

	workersWG sync.WaitGroup
}

// NewEventCollector constructs an EventCollector with disk-first storage.
func NewEventCollector(
	writer storage.StorageWriter,
	rootDir, sessionDir, nodeID, clusterName, clusterNamespace, sessionName string,
	opts Options,
) *EventCollector {
	if opts.DataDir == "" {
		opts.DataDir = "/tmp/ray/event-data"
	}
	if opts.RotationInterval <= 0 {
		opts.RotationInterval = defaultRotationInterval
	}
	if opts.MaxFileSizeBytes <= 0 {
		opts.MaxFileSizeBytes = defaultMaxFileSizeBytes
	}
	if opts.MaxDiskBytes <= 0 {
		opts.MaxDiskBytes = defaultMaxDiskBytes
	}

	collector := &EventCollector{
		storageWriter:      writer,
		root:               rootDir,
		sessionDir:         sessionDir,
		nodeID:             nodeID,
		clusterName:        clusterName,
		clusterNamespace:   clusterNamespace,
		sessionName:        sessionName,
		currentSessionName: sessionName,
		currentNodeID:      nodeID,
		stopped:            make(chan struct{}),

		dataDir:            opts.DataDir,
		rotationInterval:   opts.RotationInterval,
		maxFileSizeBytes:   opts.MaxFileSizeBytes,
		maxDiskBytes:       opts.MaxDiskBytes,
		compressionEnabled: opts.CompressionEnabled,
		activeFiles:        make(map[string]*activeFileState),
		rotationQueue:      make(chan rotationTask, rotationQueueSize),
	}

	// Start goroutine to watch nodeID file changes.
	go collector.watchNodeIDFile()

	return collector
}

// Run starts the HTTP server, rotation loop, upload worker and disk
// reconciler. It blocks until stop is closed.
func (ec *EventCollector) Run(stop <-chan struct{}, port int) {
	if err := os.MkdirAll(ec.dataDir, 0o755); err != nil {
		logrus.Errorf("Failed to create event data dir %s: %v", ec.dataDir, err)
	}

	// Initialize disk usage counter + resume any files left from a prior run.
	ec.initDiskUsage()
	ec.resumePendingFiles()

	ws := new(restful.WebService)
	ws.Path("/v1")
	ws.Consumes(restful.MIME_JSON)
	ws.Produces(restful.MIME_JSON)
	ws.Route(ws.POST("/events").To(ec.PersistEvents))
	restful.Add(ws)

	go func() {
		logrus.Infof("Starting event collector on port %d", port)
		logrus.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
	}()

	ec.workersWG.Add(1)
	go ec.rotationLoop()

	ec.workersWG.Add(1)
	go ec.compressionUploadWorker()

	ec.workersWG.Add(1)
	go ec.diskReconcileLoop()

	<-stop
	logrus.Info("Received stop signal, draining events to storage")

	// Reject new events before rotating so no writes slip in after the final rotation.
	ec.draining.Store(true)

	// Rotate remaining active files so the upload worker can drain them.
	ec.rotateAllFiles()
	close(ec.stopped)

	ec.workersWG.Wait()
}

// watchNodeIDFile watches the configured raylet_node_id file for content
// changes; on change we rotate all active files so rotated content keeps the
// old nodeID while subsequent writes use the new one.
func (ec *EventCollector) watchNodeIDFile() {
	nodeIDFilePath := utils.GetRayNodeIDPath()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Errorf("Failed to create file watcher: %v", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(nodeIDFilePath); err != nil {
		logrus.Infof("Failed to add %s to watcher, will watch parent directory: %v", nodeIDFilePath, err)
		tmpRayRoot := utils.GetTmpRayRoot()
		if err := watcher.Add(tmpRayRoot); err != nil {
			logrus.Errorf("Failed to watch directory %s: %v", tmpRayRoot, err)
			return
		}
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Name != nodeIDFilePath {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			content, err := os.ReadFile(nodeIDFilePath)
			if err != nil {
				logrus.Errorf("Failed to read node ID file %s: %v", nodeIDFilePath, err)
				continue
			}
			newNodeID := strings.TrimSpace(string(content))
			if newNodeID == "" {
				continue
			}

			ec.writeMu.Lock()
			if ec.currentNodeID == newNodeID {
				ec.writeMu.Unlock()
				continue
			}
			oldNodeID := ec.currentNodeID
			logrus.Infof("Node ID changed from %s to %s, flushing/rotating events", oldNodeID, newNodeID)
			ec.currentNodeID = newNodeID
			ec.rotateAllFilesLocked()
			ec.writeMu.Unlock()

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logrus.Errorf("File watcher error: %v", err)
		case <-ec.stopped:
			logrus.Info("Event collector stopped, exiting node ID watcher")
			return
		}
	}
}

// PersistEvents is the HTTP handler for POST /v1/events. Events are always
// appended to local JSONL files which are rotated and uploaded to remote
// storage asynchronously.
func (ec *EventCollector) PersistEvents(req *restful.Request, resp *restful.Response) {
	if ec.draining.Load() {
		resp.AddHeader("Retry-After", "5")
		resp.WriteErrorString(http.StatusServiceUnavailable, "event collector is shutting down")
		return
	}

	// Disk pressure only applies when the local disk pipeline is in use.
	if ec.underDiskPressure() {
		resp.AddHeader("Retry-After", "10")
		resp.WriteErrorString(http.StatusServiceUnavailable, "event collector under disk pressure")
		return
	}

	body, err := io.ReadAll(req.Request.Body)
	if err != nil {
		logrus.Errorf("Failed to read request body: %v", err)
		resp.WriteError(http.StatusBadRequest, err)
		return
	}

	var eventDatas []map[string]interface{}
	if err := json.Unmarshal(body, &eventDatas); err != nil {
		logrus.Errorf("Failed to unmarshal event: %v", err)
		resp.WriteError(http.StatusBadRequest, err)
		return
	}

	// Phase 1: validate all events before acquiring the lock, so a
	// mid-batch validation error cannot leave partial data on disk.
	for _, eventData := range eventDatas {
		if _, ok := eventData["timestamp"].(string); !ok {
			logrus.Errorf("Event timestamp not found or not a string")
			resp.WriteError(http.StatusBadRequest, fmt.Errorf("timestamp not found"))
			return
		}
		if _, ok := eventData["sessionName"].(string); !ok {
			logrus.Errorf("Event sessionName not found or not a string")
			resp.WriteError(http.StatusBadRequest, fmt.Errorf("sessionName not found"))
			return
		}
		if _, err := time.Parse(time.RFC3339Nano, eventData["timestamp"].(string)); err != nil {
			logrus.Errorf("Failed to parse timestamp: %v", err)
			resp.WriteError(http.StatusBadRequest, err)
			return
		}
	}

	// Phase 2: enrich, marshal, and write all validated events under the lock.
	ec.writeMu.Lock()
	defer ec.writeMu.Unlock()

	touchedWriters := make(map[*activeFileState]struct{})

	for _, eventData := range eventDatas {
		sessionNameStr := eventData["sessionName"].(string)

		if ec.currentSessionName != sessionNameStr {
			logrus.Infof("Session name changed from %s to %s, rotating active files", ec.currentSessionName, sessionNameStr)
			ec.rotateAllFilesLocked()
			ec.currentSessionName = sessionNameStr
			touchedWriters = make(map[*activeFileState]struct{})
		}

		eventData["_nodeId"] = ec.currentNodeID
		category := ec.categorize(eventData)

		line, err := json.Marshal(eventData)
		if err != nil {
			logrus.Errorf("Failed to marshal event: %v", err)
			resp.WriteError(http.StatusInternalServerError, fmt.Errorf("marshal event: %w", err))
			return
		}

		state, err := ec.getOrCreateActiveFileLocked(category, sessionNameStr)
		if err != nil {
			logrus.Errorf("Failed to open active file for %s: %v", category, err)
			resp.WriteError(http.StatusInternalServerError, err)
			return
		}

		n, err := state.writer.Write(line)
		if err != nil {
			logrus.Errorf("Failed to write event to %s: %v", state.path, err)
			resp.WriteError(http.StatusInternalServerError, err)
			return
		}
		m, err := state.writer.WriteString("\n")
		if err != nil {
			logrus.Errorf("Failed to write newline to %s: %v", state.path, err)
			resp.WriteError(http.StatusInternalServerError, err)
			return
		}
		written := int64(n + m)
		state.size += written
		ec.totalDiskUsed.Add(written)
		touchedWriters[state] = struct{}{}
	}

	for state := range touchedWriters {
		if err := state.writer.Flush(); err != nil {
			logrus.Errorf("Failed to flush %s: %v", state.path, err)
			resp.WriteError(http.StatusInternalServerError, fmt.Errorf("flush %s: %w", state.path, err))
			return
		}
	}

	resp.WriteHeader(http.StatusOK)
}

// categorize determines the storage category path for an event.
// - NODE_* events → "node_events"
// - others with a jobID → "job_events/{jobID}"
// - fallback → "node_events" (matches previous behavior)
func (ec *EventCollector) categorize(eventData map[string]interface{}) string {
	if isNodeEvent(eventData) {
		return categoryNodeEvents
	}
	if jobID := getJobID(eventData); jobID != "" {
		return path.Join(categoryJobPrefix, jobID)
	}
	return categoryNodeEvents
}

// getOrCreateActiveFileLocked returns the active file for a category,
// opening one lazily if necessary. Must be called with writeMu held.
func (ec *EventCollector) getOrCreateActiveFileLocked(category, sessionName string) (*activeFileState, error) {
	if state, ok := ec.activeFiles[category]; ok {
		return state, nil
	}
	return ec.openNewActiveFileLocked(category, sessionName)
}

// openNewActiveFileLocked opens a fresh JSONL file for the given category.
// Must be called with writeMu held.
func (ec *EventCollector) openNewActiveFileLocked(category, sessionName string) (*activeFileState, error) {
	dir := filepath.Join(ec.dataDir, ec.clusterKey(), sessionName, category)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}

	createdAt := time.Now()
	nodeID := ec.currentNodeID
	fileName := fmt.Sprintf("%s_%d.jsonl", sanitizeFileComponent(nodeID), createdAt.UnixNano())
	fullPath := filepath.Join(dir, fileName)

	f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", fullPath, err)
	}

	state := &activeFileState{
		file:        f,
		writer:      bufio.NewWriterSize(f, bufWriterSize),
		path:        fullPath,
		category:    category,
		sessionName: sessionName,
		nodeID:      nodeID,
		createdAt:   createdAt,
	}
	ec.activeFiles[category] = state
	return state, nil
}

// clusterKey returns "{clusterName}_{namespace}" used as dataDir subdirectory.
func (ec *EventCollector) clusterKey() string {
	return fmt.Sprintf("%s_%s", ec.clusterName, ec.clusterNamespace)
}

// rotationLoop periodically checks rotation conditions for each active file.
func (ec *EventCollector) rotationLoop() {
	defer ec.workersWG.Done()
	ticker := time.NewTicker(rotationCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ec.checkRotation()
		case <-ec.stopped:
			return
		}
	}
}

func (ec *EventCollector) checkRotation() {
	ec.writeMu.Lock()
	defer ec.writeMu.Unlock()

	now := time.Now()
	for category, state := range ec.activeFiles {
		if now.Sub(state.createdAt) >= ec.rotationInterval || state.size >= ec.maxFileSizeBytes {
			if err := ec.rotateFileLocked(category); err != nil {
				logrus.Errorf("Failed to rotate %s: %v", category, err)
			}
		}
	}
}

// rotateAllFiles rotates every active file. Safe to call from any goroutine.
func (ec *EventCollector) rotateAllFiles() {
	ec.writeMu.Lock()
	defer ec.writeMu.Unlock()
	ec.rotateAllFilesLocked()
}

// rotateAllFilesLocked rotates every active file. Must be called with writeMu held.
func (ec *EventCollector) rotateAllFilesLocked() {
	for category := range ec.activeFiles {
		if err := ec.rotateFileLocked(category); err != nil {
			logrus.Errorf("Failed to rotate %s: %v", category, err)
		}
	}
}

// rotateFileLocked closes the active file for a category and queues it for
// compression + upload. Must be called with writeMu held.
func (ec *EventCollector) rotateFileLocked(category string) error {
	state, ok := ec.activeFiles[category]
	if !ok {
		return nil
	}
	delete(ec.activeFiles, category)

	if err := state.writer.Flush(); err != nil {
		logrus.Errorf("Failed to flush %s before rotation: %v", state.path, err)
	}
	if err := state.file.Sync(); err != nil {
		logrus.Warnf("Failed to sync %s before rotation: %v", state.path, err)
	}
	if err := state.file.Close(); err != nil {
		logrus.Errorf("Failed to close %s before rotation: %v", state.path, err)
	}

	// Empty file: drop it without uploading to avoid useless uploads.
	if state.size == 0 {
		if err := os.Remove(state.path); err != nil && !os.IsNotExist(err) {
			logrus.Warnf("Failed to remove empty rotated file %s: %v", state.path, err)
		}
		return nil
	}

	task := rotationTask{
		jsonlPath:   state.path,
		category:    state.category,
		sessionName: state.sessionName,
		nodeID:      state.nodeID,
		createdAt:   state.createdAt,
		size:        state.size,
	}

	select {
	case ec.rotationQueue <- task:
	default:
		logrus.Warnf("rotation queue full, scheduling retry for %s", state.path)
		go func(t rotationTask) {
			time.Sleep(5 * time.Second)
			select {
			case ec.rotationQueue <- t:
			case <-ec.stopped:
			}
		}(task)
	}
	return nil
}

// compressionUploadWorker drains rotationQueue, optionally compressing
// rotated files before uploading them to remote storage.
func (ec *EventCollector) compressionUploadWorker() {
	defer ec.workersWG.Done()

	drain := func() {
		for {
			select {
			case task := <-ec.rotationQueue:
				ec.processRotatedFile(task)
			default:
				return
			}
		}
	}

	for {
		select {
		case task := <-ec.rotationQueue:
			ec.processRotatedFile(task)
		case <-ec.stopped:
			drain()
			return
		}
	}
}

// processRotatedFile optionally gzips the file, uploads it, then
// cleans up the local copies.
func (ec *EventCollector) processRotatedFile(task rotationTask) {
	uploadPath := task.jsonlPath
	var compressedPath string

	if ec.compressionEnabled {
		gzPath := task.jsonlPath + ".gz"
		if err := gzipFile(task.jsonlPath, gzPath); err != nil {
			logrus.Errorf("Failed to compress %s: %v", task.jsonlPath, err)
			// Leave the .jsonl file for retry on next restart.
			return
		}
		uploadPath = gzPath
		compressedPath = gzPath
		if info, err := os.Stat(gzPath); err == nil {
			ec.totalDiskUsed.Add(info.Size())
		}
	}

	key := ec.buildEventStorageKey(task)

	f, err := os.Open(uploadPath)
	if err != nil {
		logrus.Errorf("Failed to open %s for upload: %v", uploadPath, err)
		return
	}

	// Ensure parent directory exists in remote storage (best-effort).
	if err := ec.storageWriter.CreateDirectory(path.Dir(key)); err != nil {
		logrus.Warnf("Failed to create remote directory %s: %v", path.Dir(key), err)
	}

	if err := ec.storageWriter.WriteFile(key, f); err != nil {
		f.Close()
		logrus.Errorf("Failed to upload %s to %s: %v", uploadPath, key, err)
		// Keep both local files for retry on next restart.
		return
	}
	f.Close()

	// Clean up local files (decrement accounting best-effort).
	if info, err := os.Stat(task.jsonlPath); err == nil {
		ec.totalDiskUsed.Add(-info.Size())
	}
	if err := os.Remove(task.jsonlPath); err != nil && !os.IsNotExist(err) {
		logrus.Warnf("Failed to remove %s after upload: %v", task.jsonlPath, err)
	}

	if compressedPath != "" {
		if info, err := os.Stat(compressedPath); err == nil {
			ec.totalDiskUsed.Add(-info.Size())
		}
		if err := os.Remove(compressedPath); err != nil && !os.IsNotExist(err) {
			logrus.Warnf("Failed to remove %s after upload: %v", compressedPath, err)
		}
	}

	logrus.Infof("Uploaded %d bytes to %s", task.size, key)
}

// buildEventStorageKey constructs the remote storage key for a rotated file.
// Format includes a UnixNano suffix to avoid collisions when multiple files
// are rotated within the same hour for the same nodeID+category:
//
//	Node events: {root}/{clusterName}_{namespace}/{sessionName}/node_events/{nodeID}-{hour}-{nanoTs}.jsonl.gz
//	Job events:  {root}/{clusterName}_{namespace}/{sessionName}/job_events/{jobID}/{nodeID}-{hour}-{nanoTs}.jsonl.gz
func (ec *EventCollector) buildEventStorageKey(task rotationTask) string {
	hour := task.createdAt.Truncate(time.Hour).Format("2006-01-02-15")
	sessionName := task.sessionName
	if sessionName == "" {
		sessionName = ec.sessionName
	}
	nodeID := task.nodeID
	if nodeID == "" {
		nodeID = ec.nodeID
	}

	ext := task.extOverride
	if ext == "" {
		ext = ".jsonl"
		if ec.compressionEnabled {
			ext = ".jsonl.gz"
		}
	}

	fileName := fmt.Sprintf("%s-%s-%d%s", nodeID, hour, task.createdAt.UnixNano(), ext)
	return path.Join(
		ec.root,
		ec.clusterKey(),
		sessionName,
		task.category,
		fileName,
	)
}

// diskReconcileLoop periodically reconciles totalDiskUsed with a real
// directory walk to correct any drift.
func (ec *EventCollector) diskReconcileLoop() {
	defer ec.workersWG.Done()
	ticker := time.NewTicker(diskReconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if actual, err := dirSize(ec.dataDir); err == nil {
				ec.totalDiskUsed.Store(actual)
			} else {
				logrus.Warnf("Failed to reconcile disk usage: %v", err)
			}
		case <-ec.stopped:
			return
		}
	}
}

// initDiskUsage initializes the disk usage counter by walking dataDir once.
func (ec *EventCollector) initDiskUsage() {
	if size, err := dirSize(ec.dataDir); err == nil {
		ec.totalDiskUsed.Store(size)
	} else {
		logrus.Warnf("Failed to compute initial disk usage for %s: %v", ec.dataDir, err)
	}
}

// resumePendingFiles enqueues any files left behind by a prior run for upload.
// Pre-existing .jsonl files are treated as rotated (already-closed) and
// compressed+uploaded. Pre-existing .jsonl.gz files are re-uploaded directly.
// When both a .jsonl and its .jsonl.gz exist (failed cleanup), only the .gz is
// uploaded to avoid duplicates.
//
// Directory layout: {clusterKey}/{sessionName}/{category}/{file}
func (ec *EventCollector) resumePendingFiles() {
	clusterRoot := filepath.Join(ec.dataDir, ec.clusterKey())
	entries, err := os.Stat(clusterRoot)
	if err != nil || !entries.IsDir() {
		return
	}

	type pendingFile struct {
		path     string
		info     os.FileInfo
		category string
		session  string
	}

	// First pass: collect all event files.
	gzFiles := make(map[string]pendingFile)   // keyed by base path without .gz
	jsonlFiles := make(map[string]pendingFile) // keyed by full path

	_ = filepath.Walk(clusterRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(clusterRoot, p)
		if err != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 3 {
			return nil
		}

		sessionName := parts[0]
		var category string
		switch parts[1] {
		case categoryNodeEvents:
			category = categoryNodeEvents
		case categoryJobPrefix:
			if len(parts) < 4 {
				return nil
			}
			category = path.Join(categoryJobPrefix, parts[2])
		default:
			return nil
		}

		pf := pendingFile{path: p, info: info, category: category, session: sessionName}

		name := info.Name()
		switch {
		case strings.HasSuffix(name, ".jsonl.gz"):
			basePath := strings.TrimSuffix(p, ".gz")
			gzFiles[basePath] = pf
		case strings.HasSuffix(name, ".jsonl"):
			jsonlFiles[p] = pf
		}
		return nil
	})

	// Second pass: process files, skipping .jsonl when a .jsonl.gz already exists.
	for basePath, pf := range gzFiles {
		delete(jsonlFiles, basePath) // skip the .jsonl counterpart

		nodeID := nodeIDFromFileName(pf.info.Name())
		task := rotationTask{
			jsonlPath:   pf.path,
			category:    pf.category,
			sessionName: pf.session,
			nodeID:      nodeID,
			createdAt:   createdAtFromFileName(pf.info.Name(), pf.info.ModTime()),
			size:        pf.info.Size(),
			extOverride: ".jsonl.gz",
		}
		go ec.uploadOnly(task, pf.path)
	}

	for _, pf := range jsonlFiles {
		nodeID := nodeIDFromFileName(pf.info.Name())
		task := rotationTask{
			jsonlPath:   pf.path,
			category:    pf.category,
			sessionName: pf.session,
			nodeID:      nodeID,
			createdAt:   createdAtFromFileName(pf.info.Name(), pf.info.ModTime()),
			size:        pf.info.Size(),
		}
		select {
		case ec.rotationQueue <- task:
		default:
			logrus.Warnf("rotation queue full during resume, scheduling retry for %s", pf.path)
			go func(t rotationTask) {
				time.Sleep(5 * time.Second)
				select {
				case ec.rotationQueue <- t:
				case <-ec.stopped:
				}
			}(task)
		}
	}
}

// uploadOnly uploads a pre-existing compressed file to remote storage and
// removes it locally. Used during startup resume for .jsonl.gz leftovers.
func (ec *EventCollector) uploadOnly(task rotationTask, gzPath string) {
	key := ec.buildEventStorageKey(task)
	f, err := os.Open(gzPath)
	if err != nil {
		logrus.Errorf("Failed to open %s for resume upload: %v", gzPath, err)
		return
	}
	if err := ec.storageWriter.CreateDirectory(path.Dir(key)); err != nil {
		logrus.Warnf("Failed to create remote directory %s: %v", path.Dir(key), err)
	}
	if err := ec.storageWriter.WriteFile(key, f); err != nil {
		f.Close()
		logrus.Errorf("Failed to resume upload %s to %s: %v", gzPath, key, err)
		return
	}
	f.Close()
	if info, err := os.Stat(gzPath); err == nil {
		ec.totalDiskUsed.Add(-info.Size())
	}
	if err := os.Remove(gzPath); err != nil && !os.IsNotExist(err) {
		logrus.Warnf("Failed to remove %s after resume upload: %v", gzPath, err)
	}
	logrus.Infof("Resumed upload to %s", key)
}

// underDiskPressure reports whether new events should be rejected.
func (ec *EventCollector) underDiskPressure() bool {
	if ec.maxDiskBytes <= 0 {
		return false
	}
	threshold := int64(float64(ec.maxDiskBytes) * diskPressureWatermark)
	return ec.totalDiskUsed.Load() >= threshold
}

// isNodeEvent checks if event is node-related.
func isNodeEvent(eventData map[string]interface{}) bool {
	eventType, ok := eventData["eventType"].(string)
	if !ok {
		return false
	}
	for _, nodeEvent := range nodeEventType {
		if eventType == nodeEvent {
			return true
		}
	}
	return false
}

// getJobID extracts a jobId from known nested event payloads.
func getJobID(eventData map[string]interface{}) string {
	for _, eventType := range eventTypesWithJobID {
		if nestedEvent, ok := eventData[eventType].(map[string]interface{}); ok {
			if jobID, hasJob := nestedEvent["jobId"]; hasJob && jobID != "" {
				return fmt.Sprintf("%v", jobID)
			}
		}
	}
	return ""
}

// gzipFile compresses src into dst using gzip.
func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, in); err != nil {
		gz.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return out.Sync()
}

// dirSize returns the total byte size of files under root.
func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// sanitizeFileComponent replaces path separators in filename components.
func sanitizeFileComponent(s string) string {
	if s == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "_", string(os.PathSeparator), "_", " ", "_")
	return replacer.Replace(s)
}

// nodeIDFromFileName extracts the nodeID from a file named
// "{sanitizedNodeID}_{unixNano}.jsonl" (or .jsonl.gz). Returns empty string
// if the format is unrecognized.
func nodeIDFromFileName(name string) string {
	// Strip known suffixes.
	base := strings.TrimSuffix(name, ".gz")
	base = strings.TrimSuffix(base, ".jsonl")

	// The last "_" separates nodeID from the Unix-nano timestamp.
	idx := strings.LastIndex(base, "_")
	if idx <= 0 {
		return ""
	}
	return base[:idx]
}

// createdAtFromFileName parses the UnixNano timestamp embedded in filenames
// like "{nodeId}_{unixNano}.jsonl[.gz]". Falls back to fallback if parsing fails.
func createdAtFromFileName(name string, fallback time.Time) time.Time {
	base := strings.TrimSuffix(name, ".gz")
	base = strings.TrimSuffix(base, ".jsonl")

	idx := strings.LastIndex(base, "_")
	if idx < 0 || idx >= len(base)-1 {
		return fallback
	}
	nanos, err := strconv.ParseInt(base[idx+1:], 10, 64)
	if err != nil {
		return fallback
	}
	return time.Unix(0, nanos)
}
