// Package eventserver provides the log event reader for the History Server.
//
// This file implements reading Log Events from logs/{nodeId}/events/event_*.log files
// stored in object storage, matching Ray Dashboard's event monitoring behavior.
//
// Reference:
//   - Ray Dashboard event_utils.py: python/ray/dashboard/datacenter.py (monitor_events)
//   - Ray Dashboard event_head.py: python/ray/dashboard/modules/event/event_head.py
package eventserver

import (
	"bufio"
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
	logsBaseDir := path.Join(clusterInfo.SessionName, "logs")

	// List all items under logs/ to find node directories
	// Note: ListFiles returns base names only (e.g., "node1/", "node2/")
	nodeEntries := r.reader.ListFiles(clusterID, logsBaseDir)

	// Filter to get node IDs (directories end with "/")
	var nodeIDs []string
	for _, entry := range nodeEntries {
		// Remove trailing "/" to get node ID
		nodeID := strings.TrimSuffix(entry, "/")
		if nodeID != "" && nodeID != "events" {
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
func (r *LogEventReader) readEventFile(clusterID, filePath string, jobEventMap *types.JobEventMap) error {
	reader := r.reader.GetContent(clusterID, filePath)
	if reader == nil {
		return fmt.Errorf("failed to get content for %s", filePath)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", filePath, err)
	}

	// Parse JSON Lines (one JSON object per line)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))

	// Increase buffer size for potentially long lines
	const maxScanTokenSize = 1024 * 1024 // 1MB
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)

	lineNum := 0
	eventCount := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event types.LogEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			logrus.Warnf("Failed to parse event at %s line %d: %v", filePath, lineNum, err)
			continue
		}

		// Skip events without event_id
		if event.EventID == "" {
			continue
		}

		// Restore escaped newlines in message (matching Ray Dashboard behavior)
		event.RestoreNewline()

		jobEventMap.AddEvent(&event)
		eventCount++
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error scanning %s: %w", filePath, err)
	}

	logrus.Debugf("Read %d events from %s (%d lines)", eventCount, filePath, lineNum)
	return nil
}
