package eventserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/emicklei/go-restful/v3"
	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/storage"
)

type Event struct {
	Data      map[string]interface{}
	Timestamp time.Time

	SessionName string
	NodeID      string
}

type EventServer struct {
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

func NewEventServer(writer storage.StorageWriter, rootDir, sessionDir, nodeID, clusterName, clusterID, sessionName string) *EventServer {
	server := &EventServer{
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
	go server.watchNodeIDFile()

	return server
}

func (es *EventServer) InitServer(port int) {
	ws := new(restful.WebService)
	ws.Path("/v1")
	ws.Consumes(restful.MIME_JSON)
	ws.Produces(restful.MIME_JSON)

	ws.Route(ws.POST("/events").To(es.PersistEvents))

	restful.Add(ws)

	go func() {
		logrus.Infof("Starting event server on port %d", port)
		logrus.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
	}()
	go func() {
		es.periodicFlush()
	}()

	// Handle SIGTERM signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigChan
		logrus.Info("Received SIGTERM, flushing events to storage")
		es.flushEvents()
		close(es.stopped)
	}()
}

// watchNodeIDFile watches /tmp/ray/raylet_node_id for content changes
func (es *EventServer) watchNodeIDFile() {
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
				es.mutex.Lock()
				if es.currentNodeID != newNodeID {
					oldNodeID := es.currentNodeID
					logrus.Infof("Node ID changed from %s to %s, flushing events", oldNodeID, newNodeID)

					// Update current nodeID
					es.currentNodeID = newNodeID

					// Collect events with same nodeID
					var eventsToFlush []Event
					var remainingEvents []Event
					for _, event := range es.events {
						if event.NodeID == oldNodeID {
							eventsToFlush = append(eventsToFlush, event)
						} else if event.NodeID == newNodeID {
							remainingEvents = append(remainingEvents, event)
						} else {
							logrus.Errorf("Drop event with nodeId %v, event: %v", event.NodeID, event.Data)
						}
					}

					// Update event list, keep only events with different nodeID
					es.events = remainingEvents
					es.mutex.Unlock()

					// Flush events with same nodeID
					if len(eventsToFlush) > 0 {
						go es.flushEventsInternal(eventsToFlush)
					}
					continue
				}
				es.mutex.Unlock()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logrus.Errorf("File watcher error: %v", err)
		case <-es.stopped:
			logrus.Info("Event server stopped, exiting node ID watcher")
			return
		}
	}
}

func (es *EventServer) PersistEvents(req *restful.Request, resp *restful.Response) {
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

	for _, eventData := range eventDatas {
		// Parse timestamp
		timestampStr, ok := eventData["timestamp"].(string)
		if !ok {
			logrus.Errorf("Event timestamp not found or not a string")
			resp.WriteError(http.StatusBadRequest, fmt.Errorf("timestamp not found"))
			return
		}

		// Get sessionName from event
		sessionNameStr, ok := eventData["sessionName"].(string)
		if !ok {
			logrus.Errorf("Event sessionName not found or not a string")
			resp.WriteError(http.StatusBadRequest, fmt.Errorf("sessionName not found"))
			return
		}

		timestamp, err := time.Parse(time.RFC3339Nano, timestampStr)
		if err != nil {
			logrus.Errorf("Failed to parse timestamp: %v", err)
			resp.WriteError(http.StatusBadRequest, err)
			return
		}

		es.mutex.Lock()
		event := Event{
			Data:        eventData,
			Timestamp:   timestamp,
			SessionName: sessionNameStr,
			NodeID:      es.currentNodeID, // Store currentNodeID when event arrived
		}
		es.events = append(es.events, event)

		// Check if sessionName changed
		if es.currentSessionName != sessionNameStr {
			logrus.Infof("Session name changed from %s to %s, flushing events", es.currentSessionName, sessionNameStr)
			// Save current events before flushing
			eventsToFlush := make([]Event, len(es.events))
			copy(eventsToFlush, es.events)

			// Clear event list
			es.events = es.events[:0]

			// Update current sessionName
			es.currentSessionName = sessionNameStr

			// Unlock before flushing
			es.mutex.Unlock()

			// Flush previous events
			es.flushEventsInternal(eventsToFlush)
			return
		}
		es.mutex.Unlock()

		logrus.Infof("Received event with ID: %v at %v, session: %s, node: %s", eventData["eventId"], timestamp, sessionNameStr, es.currentNodeID)
	}

	resp.WriteHeader(http.StatusOK)
}

func (es *EventServer) periodicFlush() {
	ticker := time.NewTicker(es.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			logrus.Info("Periodic flush triggered")
			es.flushEvents()
		case <-es.stopped:
			logrus.Info("Event server stopped, exiting periodic flush")
			return
		}
	}
}

func (es *EventServer) WaitForStop() <-chan struct{} {
	return es.stopped
}

func (es *EventServer) flushEvents() {
	es.mutex.Lock()
	if len(es.events) == 0 {
		es.mutex.Unlock()
		logrus.Info("No events to flush")
		return
	}

	// Copy current event list
	eventsToFlush := make([]Event, len(es.events))
	copy(eventsToFlush, es.events)

	// Clear event list
	es.events = es.events[:0]
	es.mutex.Unlock()

	// Execute flush operation
	es.flushEventsInternal(eventsToFlush)
}

// flushEventsInternal performs the actual event flush
func (es *EventServer) flushEventsInternal(eventsToFlush []Event) {
	// Group events by hour and type
	nodeEventsByHour := make(map[string][]Event) // Node-related events
	jobEventsByHour := make(map[string][]Event)  // Job-related events

	// Categorize events
	for _, event := range eventsToFlush {
		hourKey := event.Timestamp.Truncate(time.Hour).Format("2006-01-02-15")

		// Check event type
		if es.isNodeEvent(event.Data) {
			// Node-related events
			nodeEventsByHour[hourKey] = append(nodeEventsByHour[hourKey], event)
		} else if jobID := es.getJobID(event.Data); jobID != "" {
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
			if err := es.flushNodeEventsForHour(hourKey, hourEvents); err != nil {
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

			if err := es.flushJobEventsForHour(jobID, hourKey, hourEvents); err != nil {
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
func (es *EventServer) isNodeEvent(eventData map[string]interface{}) bool {
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
func (es *EventServer) getJobID(eventData map[string]interface{}) string {
	if jobID, hasJob := eventData["jobId"]; hasJob && jobID != "" {
		return fmt.Sprintf("%v", jobID)
	}

	eventTypesWithJobID := []string{
		"driverJobDefinitionEvent",
		"driverJobLifecycleEvent",
		"taskDefinitionEvent",
		"taskLifecycleEvent",
		"actorTaskDefinitionEvent",
		"actorDefinitionEvent",
	}

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
func (es *EventServer) flushNodeEventsForHour(hourKey string, events []Event) error {
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
	sessionNameToUse := es.sessionName // Default to configured sessionName
	if len(events) > 0 && events[0].SessionName != "" {
		sessionNameToUse = events[0].SessionName
	}

	// Use NodeID from event, not current NodeID
	nodeIDToUse := es.nodeID // Default to configured nodeID
	if len(events) > 0 && events[0].NodeID != "" {
		nodeIDToUse = events[0].NodeID
	}

	// Build node event storage path using event's nodeID
	basePath := path.Join(
		es.root,
		fmt.Sprintf("%s_%s", es.clusterName, es.clusterID),
		sessionNameToUse,
		"node_events",
		fmt.Sprintf("%s-%s", nodeIDToUse, hourKey))

	// Ensure storage directory exists
	dir := path.Dir(basePath)
	if err := es.storageWriter.CreateDirectory(dir); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write event file
	if err := es.storageWriter.WriteFile(basePath, reader); err != nil {
		return fmt.Errorf("failed to write node events file %s: %w", basePath, err)
	}

	logrus.Infof("Successfully flushed %d node events for hour %s to %s, context: %s", len(events), hourKey, basePath, string(data))
	return nil
}

// flushJobEventsForHour flushes job events to storage
func (es *EventServer) flushJobEventsForHour(jobID, hourKey string, events []Event) error {
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
	sessionNameToUse := es.sessionName // Default to configured sessionName
	if len(events) > 0 && events[0].SessionName != "" {
		sessionNameToUse = events[0].SessionName
	}

	// Use NodeID from event, not current NodeID
	nodeIDToUse := es.nodeID // Default to configured nodeID
	if len(events) > 0 && events[0].NodeID != "" {
		nodeIDToUse = events[0].NodeID
	}

	// Build job event storage path using event's nodeID
	basePath := path.Join(
		es.root,
		fmt.Sprintf("%s_%s", es.clusterName, es.clusterID),
		sessionNameToUse,
		"job_events",
		jobID,
		fmt.Sprintf("%s-%s", nodeIDToUse, hourKey))

	// Ensure storage directory exists
	dir := path.Dir(basePath)
	if err := es.storageWriter.CreateDirectory(dir); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write event file
	if err := es.storageWriter.WriteFile(basePath, reader); err != nil {
		return fmt.Errorf("failed to write job events file %s: %w", basePath, err)
	}

	logrus.Infof("Successfully flushed %d job events for job %s hour %s to %s", len(events), jobID, hourKey, basePath)
	return nil
}
