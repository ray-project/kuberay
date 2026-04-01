package eventcollector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/emicklei/go-restful/v3"
	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/storage"
)

type Event struct {
	Data      map[string]interface{}
	Timestamp time.Time

	SessionName string
	NodeID      string
}

type EventCollector struct {
	storageWriter      storage.StorageWriter
	stopped            chan struct{}
	clusterID          string
	sessionDir         string
	nodeID             string
	clusterName        string
	sessionName        string
	root               string
	currentSessionName string
	currentNodeID      string
	events             []Event
	flushInterval      time.Duration
	mutex              sync.Mutex
}

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

func NewEventCollector(writer storage.StorageWriter, rootDir, sessionDir, nodeID, clusterName, clusterID, sessionName string) *EventCollector {
	collector := &EventCollector{
		events:             make([]Event, 0),
		storageWriter:      writer,
		root:               rootDir,
		sessionDir:         sessionDir,
		nodeID:             nodeID,
		clusterName:        clusterName,
		clusterID:          clusterID,
		sessionName:        sessionName,
		mutex:              sync.Mutex{},
		flushInterval:      time.Hour, // Default flush interval: 1 hour
		stopped:            make(chan struct{}),
		currentSessionName: sessionName, // Initialize with configured sessionName
		currentNodeID:      nodeID,      // Initialize with configured nodeID
	}

	// Start goroutine to watch nodeID file changes
	go collector.watchNodeIDFile()

	return collector
}

func (ec *EventCollector) Run(stop <-chan struct{}, port int) {
	ws := new(restful.WebService)
	ws.Path("/v1")
	ws.Consumes(restful.MIME_JSON)
	ws.Produces(restful.MIME_JSON)

	ws.Route(ws.POST("/events").To(ec.persistEvents))

	restful.Add(ws)

	go func() {
		logrus.Infof("Starting event collector on port %d", port)
		logrus.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
	}()
	go func() {
		ec.periodicFlush()
	}()

	<-stop
	logrus.Info("Received stop signal, flushing events to storage")
	ec.flushEvents()
	close(ec.stopped)
}

// watchNodeIDFile watches /tmp/ray/raylet_node_id for content changes
func (ec *EventCollector) watchNodeIDFile() {
	nodeIDFilePath := "/tmp/ray/raylet_node_id"

	// Create new watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Errorf("Failed to create file watcher: %v", err)
		return
	}
	defer watcher.Close()

	// Add file to watch list
	err = watcher.Add(nodeIDFilePath)
	if err != nil {
		logrus.Infof("Failed to add %s to watcher, will watch for file creation: %v", nodeIDFilePath, err)
		// If file doesn't exist, watch parent directory
		err = watcher.Add("/tmp/ray")
		if err != nil {
			logrus.Errorf("Failed to watch directory /tmp/ray: %v", err)
			return
		}
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Check if this is the target file
			if event.Name == nodeIDFilePath && (event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create) {
				// Read file content
				content, err := os.ReadFile(nodeIDFilePath)
				if err != nil {
					logrus.Errorf("Failed to read node ID file %s: %v", nodeIDFilePath, err)
					continue
				}

				// Trim whitespace
				newNodeID := strings.TrimSpace(string(content))
				if newNodeID == "" {
					continue
				}

				// Check if nodeID changed
				ec.mutex.Lock()
				if ec.currentNodeID != newNodeID {
					oldNodeID := ec.currentNodeID
					logrus.Infof("Node ID changed from %s to %s, flushing events", oldNodeID, newNodeID)

					// Update current nodeID
					ec.currentNodeID = newNodeID

					// Collect events with same nodeID
					var eventsToFlush []Event
					var remainingEvents []Event
					for _, event := range ec.events {
						if event.NodeID == oldNodeID {
							eventsToFlush = append(eventsToFlush, event)
						} else if event.NodeID == newNodeID {
							remainingEvents = append(remainingEvents, event)
						} else {
							logrus.Errorf("Drop event with nodeId %v, event: %v", event.NodeID, event.Data)
						}
					}

					// Update event list, keep only events with different nodeID
					ec.events = remainingEvents
					ec.mutex.Unlock()

					// Flush events with same nodeID
					if len(eventsToFlush) > 0 {
						go ec.flushEventsInternal(eventsToFlush)
					}
					continue
				}
				ec.mutex.Unlock()
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

// persistEvents flushes the event collector's event buffer on session change to ensure cluster session isolation.
// It also parses batchified events pushed by the HTTP publisher of the aggregator agent and stores them in the
// event collector's event buffer.
func (ec *EventCollector) persistEvents(req *restful.Request, resp *restful.Response) {
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
	if len(eventDatas) == 0 {
		logrus.Errorf("No events to persist")
		resp.WriteError(http.StatusBadRequest, fmt.Errorf("no events to persist"))
		return
	}

	// Flush event collector's event buffer on session change.
	// Note that all batchified events pushed by the HTTP publisher of the aggregator agent
	// share exactly the same session name.
	// Ref: https://github.com/ray-project/ray/blob/e0dee730/python/ray/dashboard/utils.py#L70
	// In addition, each event must have a non-empty session name.
	// Ref: https://github.com/ray-project/ray/blob/e0dee730/src/ray/protobuf/public/events_base_event.proto#L100
	sessionNameStr, ok := eventDatas[0]["sessionName"].(string)
	if !ok {
		logrus.Errorf("Event sessionName not found or not a string")
		resp.WriteError(http.StatusBadRequest, fmt.Errorf("sessionName not found or not a string"))
		return
	}

	if ec.currentSessionName != sessionNameStr && len(ec.events) > 0 {
		logrus.Infof("Session name changed from %s to %s, flushing events", ec.currentSessionName, sessionNameStr)
		eventsToFlush := make([]Event, len(ec.events))
		copy(eventsToFlush, ec.events)
		ec.events = ec.events[:0]
		ec.flushEventsInternal(eventsToFlush)
		ec.currentSessionName = sessionNameStr
	}

	// Parse and store events to event collector's event buffer.
	for _, eventData := range eventDatas {
		timestampStr, ok := eventData["timestamp"].(string)
		if !ok {
			logrus.Errorf("Event timestamp not found or not a string")
			resp.WriteError(http.StatusBadRequest, fmt.Errorf("timestamp not found or not a string"))
			return
		}
		timestamp, err := time.Parse(time.RFC3339Nano, timestampStr)
		if err != nil {
			logrus.Errorf("Failed to parse timestamp: %v", err)
			resp.WriteError(http.StatusBadRequest, err)
			return
		}

		sessionNameStr, ok := eventData["sessionName"].(string)
		if !ok {
			logrus.Errorf("Event sessionName not found or not a string")
			resp.WriteError(http.StatusBadRequest, fmt.Errorf("sessionName not found or not a string"))
			return
		}

		ec.mutex.Lock()
		event := Event{
			Data:        eventData,
			Timestamp:   timestamp,
			SessionName: sessionNameStr,
			NodeID:      ec.currentNodeID,
		}
		ec.events = append(ec.events, event)
		ec.mutex.Unlock()

		logrus.Infof("Received event with ID: %v at %v, session: %s, node: %s", eventData["eventId"], timestamp, sessionNameStr, ec.currentNodeID)
	}

	resp.WriteHeader(http.StatusOK)
}

func (ec *EventCollector) periodicFlush() {
	ticker := time.NewTicker(ec.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			logrus.Info("Periodic flush triggered")
			ec.flushEvents()
		case <-ec.stopped:
			logrus.Info("Event collector stopped, exiting periodic flush")
			return
		}
	}
}

func (ec *EventCollector) flushEvents() {
	ec.mutex.Lock()
	if len(ec.events) == 0 {
		ec.mutex.Unlock()
		logrus.Info("No events to flush")
		return
	}

	// Copy current event list
	eventsToFlush := make([]Event, len(ec.events))
	copy(eventsToFlush, ec.events)

	// Clear event list
	ec.events = ec.events[:0]
	ec.mutex.Unlock()

	// Execute flush operation
	ec.flushEventsInternal(eventsToFlush)
}

// flushEventsInternal performs the actual event flush
func (ec *EventCollector) flushEventsInternal(eventsToFlush []Event) {
	// Group events by hour and type
	nodeEventsByHour := make(map[string][]Event) // Node-related events
	jobEventsByHour := make(map[string][]Event)  // Job-related events

	// Categorize events
	for _, event := range eventsToFlush {
		hourKey := event.Timestamp.Truncate(time.Hour).Format("2006-01-02-15")

		// Check event type
		if isNodeEvent(event.Data) {
			// Node-related events
			nodeEventsByHour[hourKey] = append(nodeEventsByHour[hourKey], event)
		} else if jobID := getJobID(event.Data); jobID != "" {
			// Job-related events, use jobID-hour as key
			jobKey := fmt.Sprintf("%s-%s", jobID, hourKey)
			jobEventsByHour[jobKey] = append(jobEventsByHour[jobKey], event)
		} else {
			// Default to node events
			nodeEventsByHour[hourKey] = append(nodeEventsByHour[hourKey], event)
		}
	}

	// Upload all events concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, len(nodeEventsByHour)+len(jobEventsByHour))

	// Upload node-related events
	for hour, events := range nodeEventsByHour {
		wg.Add(1)
		go func(hourKey string, hourEvents []Event) {
			defer wg.Done()
			if err := ec.flushNodeEventsForHour(hourKey, hourEvents); err != nil {
				errChan <- err
			}
		}(hour, events)
	}

	// Upload job-related events
	for jobHour, events := range jobEventsByHour {
		wg.Add(1)
		go func(jobHourKey string, hourEvents []Event) {
			defer wg.Done()
			// Split jobID and hourKey
			parts := strings.SplitN(jobHourKey, "-", 4) // Date format has 3 dashes, so use 4 parts
			if len(parts) < 4 {
				errChan <- fmt.Errorf("invalid job hour key: %s", jobHourKey)
				return
			}

			jobID := parts[0]
			hourKey := strings.Join(parts[1:], "-") // Rejoin time parts as hourKey

			if err := ec.flushJobEventsForHour(jobID, hourKey, hourEvents); err != nil {
				errChan <- err
			}
		}(jobHour, events)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		logrus.Errorf("Error flushing events: %v", err)
	}

	totalEvents := len(eventsToFlush)
	logrus.Infof("Successfully flushed %d events to storage (%d node events, %d job events)",
		totalEvents,
		countEventsInMap(nodeEventsByHour),
		countEventsInMap(jobEventsByHour))
}

// countEventsInMap counts total events in map
func countEventsInMap(eventsMap map[string][]Event) int {
	count := 0
	for _, events := range eventsMap {
		count += len(events)
	}
	return count
}

var nodeEventType = []string{"NODE_LIFECYCLE_EVENT", "NODE_DEFINITION_EVENT"}

// isNodeEvent checks if event is node-related
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

// getJobID gets jobID associated with event
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

// flushNodeEventsForHour flushes node events to storage
func (ec *EventCollector) flushNodeEventsForHour(hourKey string, events []Event) error {
	// Create event data
	eventsData := make([]map[string]interface{}, len(events))
	for i, event := range events {
		eventsData[i] = event.Data
	}

	data, err := json.Marshal(eventsData)
	if err != nil {
		return fmt.Errorf("failed to marshal node events: %w", err)
	}

	reader := bytes.NewReader(data)

	// Use sessionName from event, not config
	sessionNameToUse := ec.sessionName // Default to configured sessionName
	if len(events) > 0 && events[0].SessionName != "" {
		sessionNameToUse = events[0].SessionName
	}

	// Use NodeID from event, not current NodeID
	nodeIDToUse := ec.nodeID // Default to configured nodeID
	if len(events) > 0 && events[0].NodeID != "" {
		nodeIDToUse = events[0].NodeID
	}

	// Build node event storage path using event's nodeID
	basePath := path.Join(
		ec.root,
		fmt.Sprintf("%s_%s", ec.clusterName, ec.clusterID),
		sessionNameToUse,
		"node_events",
		fmt.Sprintf("%s-%s", nodeIDToUse, hourKey))

	// Ensure storage directory exists
	dir := path.Dir(basePath)
	if err := ec.storageWriter.CreateDirectory(dir); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write event file
	if err := ec.storageWriter.WriteFile(basePath, reader); err != nil {
		return fmt.Errorf("failed to write node events file %s: %w", basePath, err)
	}

	logrus.Infof("Successfully flushed %d node events for hour %s to %s, context: %s", len(events), hourKey, basePath, string(data))
	return nil
}

// flushJobEventsForHour flushes job events to storage
func (ec *EventCollector) flushJobEventsForHour(jobID, hourKey string, events []Event) error {
	// Create event data
	eventsData := make([]map[string]interface{}, len(events))
	for i, event := range events {
		eventsData[i] = event.Data
	}

	data, err := json.Marshal(eventsData)
	if err != nil {
		return fmt.Errorf("failed to marshal job events: %w", err)
	}

	reader := bytes.NewReader(data)

	// Use sessionName from event, not config
	sessionNameToUse := ec.sessionName // Default to configured sessionName
	if len(events) > 0 && events[0].SessionName != "" {
		sessionNameToUse = events[0].SessionName
	}

	// Use NodeID from event, not current NodeID
	nodeIDToUse := ec.nodeID // Default to configured nodeID
	if len(events) > 0 && events[0].NodeID != "" {
		nodeIDToUse = events[0].NodeID
	}

	// Build job event storage path using event's nodeID
	basePath := path.Join(
		ec.root,
		fmt.Sprintf("%s_%s", ec.clusterName, ec.clusterID),
		sessionNameToUse,
		"job_events",
		jobID,
		fmt.Sprintf("%s-%s", nodeIDToUse, hourKey))

	// Ensure storage directory exists
	dir := path.Dir(basePath)
	if err := ec.storageWriter.CreateDirectory(dir); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write event file
	if err := ec.storageWriter.WriteFile(basePath, reader); err != nil {
		return fmt.Errorf("failed to write job events file %s: %w", basePath, err)
	}

	logrus.Infof("Successfully flushed %d job events for job %s hour %s to %s", len(events), jobID, hourKey, basePath)
	return nil
}
