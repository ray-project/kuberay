// Package eventserver provides the log event reader for the History Server.
//
// This file implements reading Log Events from logs/{nodeId}/events/event_*.log files
// stored in object storage, matching Ray Dashboard's event monitoring behavior.
//
// Reference:
//   - Ray Dashboard event_utils.py (monitor_events, _read_file, parse_event_strings)
//     python/ray/dashboard/modules/event/event_utils.py
//   - Ray Dashboard event_head.py
//     python/ray/dashboard/modules/event/event_head.py
package eventserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	"github.com/sirupsen/logrus"
)

// maxLineLengthLimit is the maximum line length for event files.
// Ray Dashboard uses EVENT_READ_LINE_LENGTH_LIMIT = 2MB (configurable via env var).
// Reference: python/ray/dashboard/modules/event/event_consts.py
// TODO: Make this configurable via environment variable (e.g., EVENT_READ_LINE_LENGTH_LIMIT)
// to match Ray Dashboard's behavior.
const maxLineLengthLimit = 2 * 1024 * 1024 // 2MB, matching Ray Dashboard default

// LogEventReader reads Log Events from object storage.
// It scans logs/{nodeId}/events/event_*.log files matching Ray Dashboard's behavior.
type LogEventReader struct {
	reader storage.StorageReader
}

// NewLogEventReader creates a new LogEventReader.
func NewLogEventReader(reader storage.StorageReader) *LogEventReader {
	return &LogEventReader{
		reader: reader,
	}
}

// ReadLogEvents reads all Log Events from logs/{nodeId}/events/event_*.log files
// and stores them in the provided ClusterLogEventMap.
//
// Path structure in storage: {clusterName}_{namespace}/{sessionName}/logs/{nodeId}/events/event_*.log
// This is called from eventserver.go Run() to populate events for a cluster session.
func (r *LogEventReader) ReadLogEvents(clusterInfo utils.ClusterInfo, clusterSessionKey string, eventStore *types.ClusterLogEventMap) error {
	// Build cluster ID used by StorageReader
	clusterID := clusterInfo.Name + "_" + clusterInfo.Namespace

	// Get or create the JobEventMap for this cluster session
	jobEventMap := eventStore.GetOrCreateJobEventMap(clusterSessionKey)

	// Path: {sessionName}/logs/
	logsBaseDir := path.Join(clusterInfo.SessionName, utils.RAY_SESSIONDIR_LOGDIR_NAME)

	// List all items under logs/ to find node directories
	// Note: ListFiles returns base names only (e.g., "node1/", "node2/")
	nodeEntries := r.reader.ListFiles(clusterID, logsBaseDir)

	// Filter to get node IDs (only directories end with "/")
	// This matches the pattern used in eventserver.go getAllJobEventFiles()
	var nodeIDs []string
	for _, entry := range nodeEntries {
		// Skip non-directory entries (files don't end with "/")
		if !strings.HasSuffix(entry, "/") {
			continue
		}
		// Remove trailing "/" to get node ID
		nodeID := strings.TrimSuffix(entry, "/")
		if nodeID != "" {
			nodeIDs = append(nodeIDs, nodeID)
		}
	}
	logrus.Debugf("Found %d node directories for cluster %s: %v", len(nodeIDs), clusterSessionKey, nodeIDs)

	for _, nodeID := range nodeIDs {
		// Path: {sessionName}/logs/{nodeId}/events/
		eventsDir := path.Join(clusterInfo.SessionName, "logs", nodeID, "events")
		// Note: ListFiles returns base names only (e.g., "event_GCS.log")
		eventFileNames := r.reader.ListFiles(clusterID, eventsDir)

		for _, fileName := range eventFileNames {
			// Only process event_*.log files
			if !strings.HasPrefix(fileName, "event_") || !strings.HasSuffix(fileName, ".log") {
				continue
			}

			// Build full path relative to clusterID for GetContent
			eventFilePath := path.Join(clusterInfo.SessionName, "logs", nodeID, "events", fileName)

			// Read and parse the event file
			// Note: Duplicate events are handled by JobEventMap's deduplication using event_id as key.
			// This matches the design of existing RayEvents reading in eventserver.go.
			if err := r.readEventFile(clusterID, eventFilePath, jobEventMap); err != nil {
				logrus.Warnf("Failed to read event file %s: %v", eventFilePath, err)
				// Continue with other files - failed files will be retried in the next cycle
			}
		}
	}

	return nil
}

// readEventFile reads and parses a single event_*.log file (JSON Lines format).
// Lines exceeding maxLineLengthLimit are drained and skipped without accumulating
// in memory, matching Ray Dashboard's _read_file() behavior in event_utils.py.
func (r *LogEventReader) readEventFile(clusterID, filePath string, jobEventMap *types.JobEventMap) error {
	ioReader := r.reader.GetContent(clusterID, filePath)
	if ioReader == nil {
		return fmt.Errorf("failed to get content for %s", filePath)
	}

	// Use a moderate initial buffer (64KB); readLineWithLimit handles long-line
	// draining so we never accumulate more than maxLineLengthLimit in memory.
	br := bufio.NewReaderSize(ioReader, 64*1024)

	lineNum := 0
	eventCount := 0
	for {
		line, n, tooLong, err := readLineWithLimit(br, maxLineLengthLimit)

		// No remaining data — clean EOF with nothing left to process
		if err == io.EOF && n == 0 {
			break
		}
		if err != nil && err != io.EOF {
			return fmt.Errorf("error reading %s at line %d: %w", filePath, lineNum+1, err)
		}

		lineNum++

		if tooLong {
			// Matching Ray Dashboard behavior: skip long lines and continue.
			// Reference: event_utils.py _read_file() — "Ignored long string: %s...(%s chars)"
			logrus.Warnf("Ignored long line at %s line %d: %d bytes (limit: %d)",
				filePath, lineNum, n, maxLineLengthLimit)
		} else if event := r.parseLine(line, filePath, lineNum); event != nil {
			jobEventMap.AddEvent(event)
			eventCount++
		}

		// EOF after processing the last partial line (file didn't end with '\n')
		if err == io.EOF {
			break
		}
	}

	logrus.Debugf("Read %d events from %s (%d lines)", eventCount, filePath, lineNum)
	return nil
}

// readLineWithLimit reads one logical line (up to the next '\n') from br.
// It bounds memory usage: if the line exceeds limit bytes, the remainder is
// drained without being accumulated, and tooLong is set to true.
//
// Returns:
//   - line:    the full line bytes (nil when tooLong)
//   - n:       total bytes consumed for this logical line (including '\n')
//   - tooLong: true if the line exceeded limit
//   - err:     io.EOF when the stream ends (line/n may still hold the last partial data)
func readLineWithLimit(br *bufio.Reader, limit int) (line []byte, n int, tooLong bool, err error) {
	var buf []byte

	for {
		frag, e := br.ReadSlice('\n')
		n += len(frag)

		if !tooLong {
			if n > limit {
				// Line exceeded the limit — drop accumulated data and mark as too long.
				tooLong = true
				buf = nil
			} else {
				buf = append(buf, frag...)
			}
		}
		// If tooLong, we keep looping to drain remaining bytes until '\n' or EOF,
		// but do not accumulate them.

		switch e {
		case nil:
			// Found '\n' — full line complete.
			return buf, n, tooLong, nil
		case bufio.ErrBufferFull:
			// Fragment filled the internal buffer but no '\n' yet — keep reading.
			continue
		case io.EOF:
			// Stream ended. frag may contain the last partial line without '\n'.
			return buf, n, tooLong, io.EOF
		default:
			return nil, n, false, e
		}
	}
}

// parseLine parses a single JSON line into a LogEvent.
// Returns nil if the line is empty, invalid, or missing event_id.
func (r *LogEventReader) parseLine(line []byte, filePath string, lineNum int) *types.LogEvent {
	// Trim whitespace and newline
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}

	var event types.LogEvent
	if err := json.Unmarshal(line, &event); err != nil {
		logrus.Warnf("Failed to parse event at %s line %d: %v", filePath, lineNum, err)
		return nil
	}

	// Skip events without event_id
	if event.EventID == "" {
		return nil
	}

	// Restore escaped newlines in message (matching Ray Dashboard behavior)
	event.RestoreNewline()

	return &event
}
