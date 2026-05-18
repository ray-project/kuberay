package eventcollector

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
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
	defaultFlushInterval    = time.Hour

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
	// CompressionEnabled is a single switch that controls the entire
	// disk-first + gzip pipeline. When true, events are written to local
	// JSONL files and rotated/gzipped/uploaded asynchronously. When false,
	// events are buffered in-memory and periodically flushed to remote
	// storage (legacy semantics).
	CompressionEnabled bool
	// FlushInterval controls how often the in-memory buffer is flushed when
	// CompressionEnabled is false. Defaults to 1h (legacy behavior). Set
	// lower for more frequent writes.
	FlushInterval time.Duration
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

// memEvent is the in-memory record used when CompressionEnabled is false.
// Mirrors the legacy Event struct: captures the event payload along with the
// nodeID/session snapshot taken at arrival time, so that periodic flushes
// preserve the original sender attribution even if the collector's current
// nodeID later changes.
type memEvent struct {
	data        map[string]interface{}
	timestamp   time.Time
	sessionName string
	nodeID      string
}

// rotationTask describes a rotated JSONL file ready for compression + upload.
type rotationTask struct {
	jsonlPath   string
	category    string
	sessionName string
	nodeID      string
	createdAt   time.Time
	size        int64
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
	flushInterval      time.Duration

	writeMu       sync.Mutex
	activeFiles   map[string]*activeFileState
	rotationQueue chan rotationTask
	totalDiskUsed atomic.Int64

	// memEvents is the in-memory buffer used when compressionEnabled is
	// false. Protected by writeMu.
	memEvents []memEvent

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
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = defaultFlushInterval
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
		flushInterval:      opts.FlushInterval,
		activeFiles:        make(map[string]*activeFileState),
		rotationQueue:      make(chan rotationTask, rotationQueueSize),
	}

	// Start goroutine to watch nodeID file changes.
	go collector.watchNodeIDFile()

	return collector
}

// Run starts the HTTP server, rotation loop, compression/upload worker and
// disk reconciler. It blocks until stop is closed.
func (ec *EventCollector) Run(stop <-chan struct{}, port int) {
	if ec.compressionEnabled {
		if err := os.MkdirAll(ec.dataDir, 0o755); err != nil {
			logrus.Errorf("Failed to create event data dir %s: %v", ec.dataDir, err)
		}

		// Initialize disk usage counter + resume any files left from a prior run.
		ec.initDiskUsage()
		ec.resumePendingFiles()
	}

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

	if ec.compressionEnabled {
		ec.workersWG.Add(1)
		go ec.rotationLoop()

		ec.workersWG.Add(1)
		go ec.compressionUploadWorker()

		ec.workersWG.Add(1)
		go ec.diskReconcileLoop()
	} else {
		logrus.Infof("Compression disabled: using in-memory buffer with flush interval %s", ec.flushInterval)
		ec.workersWG.Add(1)
		go ec.periodicMemFlushLoop()
	}

	<-stop
	logrus.Info("Received stop signal, draining events to storage")

	// Signal workers to shut down. When the disk pipeline is active, rotate
	// remaining active files so the upload worker can drain them. When the
	// in-memory buffer is active, flush whatever is still buffered before
	// stopping so no events are lost.
	if ec.compressionEnabled {
		close(ec.stopped)
		ec.rotateAllFiles()
	} else {
		ec.flushAllMemEvents()
		close(ec.stopped)
	}

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
			if ec.compressionEnabled {
				ec.rotateAllFilesLocked()
				ec.writeMu.Unlock()
			} else {
				// Mirror legacy behavior: split buffered events by nodeID,
				// flush those owned by the old nodeID, keep the ones already
				// tagged with the new nodeID, drop anything else.
				toFlush, dropped := ec.partitionMemEventsForNodeChangeLocked(oldNodeID, newNodeID)
				ec.writeMu.Unlock()
				if dropped > 0 {
					logrus.Warnf("Dropped %d buffered events with stale nodeID during node change", dropped)
				}
				if len(toFlush) > 0 {
					go ec.flushMemEventsInternal(toFlush)
				}
			}

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

// PersistEvents is the HTTP handler for POST /v1/events. When compression
// is enabled it appends events to local JSONL files (rotated and uploaded
// asynchronously). When compression is disabled it bypasses local disk and
// uploads each request's events directly to remote storage.
func (ec *EventCollector) PersistEvents(req *restful.Request, resp *restful.Response) {
	// Disk pressure only applies when the local disk pipeline is in use.
	if ec.compressionEnabled && ec.underDiskPressure() {
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

	if !ec.compressionEnabled {
		ec.persistEventsBuffered(eventDatas, resp)
		return
	}

	ec.writeMu.Lock()
	defer ec.writeMu.Unlock()

	touchedWriters := make(map[*activeFileState]struct{})

	for _, eventData := range eventDatas {
		timestampStr, ok := eventData["timestamp"].(string)
		if !ok {
			logrus.Errorf("Event timestamp not found or not a string")
			resp.WriteError(http.StatusBadRequest, fmt.Errorf("timestamp not found"))
			return
		}
		sessionNameStr, ok := eventData["sessionName"].(string)
		if !ok {
			logrus.Errorf("Event sessionName not found or not a string")
			resp.WriteError(http.StatusBadRequest, fmt.Errorf("sessionName not found"))
			return
		}
		if _, err := time.Parse(time.RFC3339Nano, timestampStr); err != nil {
			logrus.Errorf("Failed to parse timestamp: %v", err)
			resp.WriteError(http.StatusBadRequest, err)
			return
		}

		// Session change: rotate all active files, then continue with the new session.
		if ec.currentSessionName != sessionNameStr {
			logrus.Infof("Session name changed from %s to %s, rotating active files", ec.currentSessionName, sessionNameStr)
			ec.rotateAllFilesLocked()
			ec.currentSessionName = sessionNameStr
			touchedWriters = make(map[*activeFileState]struct{})
		}

		// Enrich event with the current node ID (matches prior behavior).
		eventData["_nodeId"] = ec.currentNodeID

		category := ec.categorize(eventData)

		line, err := json.Marshal(eventData)
		if err != nil {
			logrus.Errorf("Failed to marshal event: %v", err)
			continue
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
		}
	}

	resp.WriteHeader(http.StatusOK)
}

// persistEventsBuffered validates events and appends them to the in-memory
// buffer. Used when CompressionEnabled is false. The buffer is flushed to
// remote storage by periodicMemFlushLoop on the configured FlushInterval,
// on node ID change, or on shutdown. This mirrors the legacy semantics:
// events are grouped per (category, hour, nodeID, sessionName) at flush time
// and uploaded as a JSON array to {root}/{cluster}_{ns}/{session}/{category}/
// {nodeID}-{hour}.
func (ec *EventCollector) persistEventsBuffered(eventDatas []map[string]interface{}, resp *restful.Response) {
	for _, eventData := range eventDatas {
		timestampStr, ok := eventData["timestamp"].(string)
		if !ok {
			logrus.Errorf("Event timestamp not found or not a string")
			resp.WriteError(http.StatusBadRequest, fmt.Errorf("timestamp not found"))
			return
		}
		sessionNameStr, ok := eventData["sessionName"].(string)
		if !ok {
			logrus.Errorf("Event sessionName not found or not a string")
			resp.WriteError(http.StatusBadRequest, fmt.Errorf("sessionName not found"))
			return
		}
		ts, err := time.Parse(time.RFC3339Nano, timestampStr)
		if err != nil {
			logrus.Errorf("Failed to parse timestamp: %v", err)
			resp.WriteError(http.StatusBadRequest, err)
			return
		}

		// Snapshot currentNodeID inside the lock per-event to match the
		// legacy behavior, so that a nodeID change mid-batch correctly
		// attributes earlier events to the old node.
		ec.writeMu.Lock()
		ec.memEvents = append(ec.memEvents, memEvent{
			data:        eventData,
			timestamp:   ts,
			sessionName: sessionNameStr,
			nodeID:      ec.currentNodeID,
		})
		ec.writeMu.Unlock()
	}

	resp.WriteHeader(http.StatusOK)
}

// periodicMemFlushLoop drains the in-memory buffer on a fixed interval.
// Only started when CompressionEnabled is false.
func (ec *EventCollector) periodicMemFlushLoop() {
	defer ec.workersWG.Done()
	ticker := time.NewTicker(ec.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ec.flushAllMemEvents()
		case <-ec.stopped:
			return
		}
	}
}

// flushAllMemEvents snapshots and uploads everything currently buffered.
func (ec *EventCollector) flushAllMemEvents() {
	ec.writeMu.Lock()
	if len(ec.memEvents) == 0 {
		ec.writeMu.Unlock()
		return
	}
	toFlush := make([]memEvent, len(ec.memEvents))
	copy(toFlush, ec.memEvents)
	ec.memEvents = ec.memEvents[:0]
	ec.writeMu.Unlock()

	ec.flushMemEventsInternal(toFlush)
}

// partitionMemEventsForNodeChangeLocked splits the buffer on node-id change:
// events tagged with oldNodeID are returned for immediate flush, events
// tagged with newNodeID are retained for the next flush cycle, anything
// else is dropped. Must be called with writeMu held.
func (ec *EventCollector) partitionMemEventsForNodeChangeLocked(oldNodeID, newNodeID string) (toFlush []memEvent, dropped int) {
	if len(ec.memEvents) == 0 {
		return nil, 0
	}
	remaining := ec.memEvents[:0]
	for _, evt := range ec.memEvents {
		switch evt.nodeID {
		case oldNodeID:
			toFlush = append(toFlush, evt)
		case newNodeID:
			remaining = append(remaining, evt)
		default:
			dropped++
		}
	}
	ec.memEvents = remaining
	return toFlush, dropped
}

// flushMemEventsInternal groups events by (category, hour, nodeID,
// sessionName) and uploads each group as a JSON array. Mirrors the legacy
// flushEventsInternal grouping rules.
func (ec *EventCollector) flushMemEventsInternal(events []memEvent) {
	if len(events) == 0 {
		return
	}

	type bucket struct {
		category    string
		hourKey     string
		nodeID      string
		sessionName string
		events      []memEvent
	}
	buckets := make(map[string]*bucket)
	for _, evt := range events {
		hourKey := evt.timestamp.Truncate(time.Hour).Format("2006-01-02-15")
		category := ec.categorize(evt.data)
		key := category + "|" + hourKey + "|" + evt.nodeID + "|" + evt.sessionName
		b, ok := buckets[key]
		if !ok {
			b = &bucket{
				category:    category,
				hourKey:     hourKey,
				nodeID:      evt.nodeID,
				sessionName: evt.sessionName,
			}
			buckets[key] = b
		}
		b.events = append(b.events, evt)
	}

	var wg sync.WaitGroup
	for _, b := range buckets {
		wg.Add(1)
		go func(b *bucket) {
			defer wg.Done()
			if err := ec.uploadMemBucket(b.category, b.hourKey, b.nodeID, b.sessionName, b.events); err != nil {
				logrus.Errorf("Failed to upload bucket %s/%s: %v", b.category, b.hourKey, err)
			}
		}(b)
	}
	wg.Wait()

	logrus.Infof("Flushed %d buffered events across %d buckets", len(events), len(buckets))
}

// uploadMemBucket serializes a single bucket to a JSON array and uploads it
// to the legacy storage key format (no extension, no nano suffix).
func (ec *EventCollector) uploadMemBucket(category, hourKey, nodeID, sessionName string, events []memEvent) error {
	if sessionName == "" {
		sessionName = ec.sessionName
	}
	if nodeID == "" {
		nodeID = ec.nodeID
	}

	payload := make([]map[string]interface{}, len(events))
	for i, e := range events {
		payload[i] = e.data
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal bucket: %w", err)
	}

	key := ec.buildLegacyEventStorageKey(category, hourKey, nodeID, sessionName)
	if err := ec.storageWriter.CreateDirectory(path.Dir(key)); err != nil {
		return fmt.Errorf("create remote dir %s: %w", path.Dir(key), err)
	}
	if err := ec.storageWriter.WriteFile(key, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	logrus.Infof("Flushed %d events to %s", len(events), key)
	return nil
}

// buildLegacyEventStorageKey constructs the remote storage key in the
// legacy format (no extension, no nano suffix), used by the in-memory
// buffered path:
//
//	Node events: {root}/{clusterName}_{namespace}/{sessionName}/node_events/{nodeID}-{hour}
//	Job events:  {root}/{clusterName}_{namespace}/{sessionName}/job_events/{jobID}/{nodeID}-{hour}
func (ec *EventCollector) buildLegacyEventStorageKey(category, hourKey, nodeID, sessionName string) string {
	return path.Join(
		ec.root,
		ec.clusterKey(),
		sessionName,
		category,
		fmt.Sprintf("%s-%s", nodeID, hourKey),
	)
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
	dir := filepath.Join(ec.dataDir, ec.clusterKey(), category)
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

// compressionUploadWorker drains rotationQueue.
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

// processRotatedFile gzips the file (unless disabled), uploads it, then
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

	ext := ".jsonl"
	if ec.compressionEnabled {
		ext = ".jsonl.gz"
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
func (ec *EventCollector) resumePendingFiles() {
	clusterRoot := filepath.Join(ec.dataDir, ec.clusterKey())
	entries, err := os.Stat(clusterRoot)
	if err != nil || !entries.IsDir() {
		return
	}

	_ = filepath.Walk(clusterRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(clusterRoot, p)
		if err != nil {
			return nil
		}
		// rel is like "node_events/<file>" or "job_events/<jobID>/<file>".
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 2 {
			return nil
		}
		var category string
		switch parts[0] {
		case categoryNodeEvents:
			category = categoryNodeEvents
		case categoryJobPrefix:
			if len(parts) < 3 {
				return nil
			}
			category = path.Join(categoryJobPrefix, parts[1])
		default:
			return nil
		}

		name := info.Name()
		task := rotationTask{
			jsonlPath:   p,
			category:    category,
			sessionName: ec.currentSessionName,
			nodeID:      ec.currentNodeID,
			createdAt:   info.ModTime(),
			size:        info.Size(),
		}

		switch {
		case strings.HasSuffix(name, ".jsonl.gz"):
			// Already compressed: upload as-is by pretending the jsonlPath is the .gz
			// and disabling further compression for this task.
			go ec.uploadOnly(task, p)
		case strings.HasSuffix(name, ".jsonl"):
			select {
			case ec.rotationQueue <- task:
			default:
				logrus.Warnf("rotation queue full during resume, deferring %s", p)
			}
		}
		return nil
	})
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
