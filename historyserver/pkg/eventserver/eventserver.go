package eventserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	"github.com/sirupsen/logrus"
)

type EventHandler struct {
	reader storage.StorageReader

	ClusterTaskMap  *types.ClusterTaskMap
	ClusterActorMap *types.ClusterActorMap
	ClusterJobMap   *types.ClusterJobMap
	ClusterEventMap *types.ClusterEventMap // For /events API
}

var eventFilePattern = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}-\d{2}$`)

func isValidEventFile(fileName string) bool {
	// Skip directories
	if strings.HasSuffix(fileName, "/") {
		return false
	}
	// Only files matching {nodeId}-{YYYY-MM-DD-HH} format are valid event files
	return eventFilePattern.MatchString(fileName)
}

func NewEventHandler(reader storage.StorageReader) *EventHandler {
	return &EventHandler{
		reader: reader,
		ClusterTaskMap: &types.ClusterTaskMap{
			ClusterTaskMap: make(map[string]*types.TaskMap),
		},
		ClusterActorMap: &types.ClusterActorMap{
			ClusterActorMap: make(map[string]*types.ActorMap),
		},
		ClusterJobMap: &types.ClusterJobMap{
			ClusterJobMap: make(map[string]*types.JobMap),
		},
		ClusterEventMap: types.NewClusterEventMap(),
	}
}

// ProcessEvents func reads the channel and then processes the event received
func (h *EventHandler) ProcessEvents(ctx context.Context, ch <-chan map[string]any) error {
	logrus.Infof("Starting a event processor channel")
	for {
		select {
		case <-ctx.Done():
			// TODO: The context was cancelled, either stop here or process the rest of the events and return
			// Currently, it will just stop.
			logrus.Warnf("Event processor context was cancelled")
			return ctx.Err()
		case currEventData, ok := <-ch:
			if !ok {
				logrus.Warnf("Channel was closed")
				return nil
			}
			if err := h.storeEvent(currEventData); err != nil {
				logrus.Errorf("Failed to store event: %v", err)
				continue
			}
		}
	}
}

// Run will start numOfEventProcessors (default to 5) processing functions and the event reader. The event reader will run once an hr,
// which is currently how often the collector flushes.
func (h *EventHandler) Run(stop chan struct{}, numOfEventProcessors int) error {
	var wg sync.WaitGroup

	if numOfEventProcessors == 0 {
		numOfEventProcessors = 5
	}
	eventProcessorChannels := make([]chan map[string]any, numOfEventProcessors)
	cctx := make([]context.CancelFunc, numOfEventProcessors)

	for i := range numOfEventProcessors {
		eventProcessorChannels[i] = make(chan map[string]any, 100)
	}

	for i, currEventChannel := range eventProcessorChannels {
		wg.Add(1)
		ctx, cancel := context.WithCancel(context.Background())
		cctx[i] = cancel
		go func() {
			defer wg.Done()
			var processor EventProcessor[map[string]any] = h
			err := processor.ProcessEvents(ctx, currEventChannel)
			if err == ctx.Err() {
				logrus.Warnf("Event processor go routine %d is now closed", i)
				return
			}
			if err != nil {
				logrus.Errorf("event processor %d go routine failed %v", i, err)
				return
			}
		}()
	}

	// Start reading files and sending events for processing
	wg.Add(1)
	go func() {
		defer wg.Done()
		logrus.Info("Starting event file reader loop")

		// Helper function to process all events
		processAllEvents := func() {
			clusterList := h.reader.List()
			for _, clusterInfo := range clusterList {
				clusterNameNamespace := clusterInfo.Name + "_" + clusterInfo.Namespace
				clusterSessionKey := utils.BuildClusterSessionKey(clusterInfo.Name, clusterInfo.Namespace, clusterInfo.SessionName)
				eventFileList := append(h.getAllJobEventFiles(clusterInfo), h.getAllNodeEventFiles(clusterInfo)...)

				logrus.Infof("current eventFileList for cluster %s is: %v", clusterInfo.Name, eventFileList)
				for _, eventFile := range eventFileList {
					// TODO: Filter out ones that have already been read
					logrus.Infof("Reading event file: %s", eventFile)

					eventioReader := h.reader.GetContent(clusterNameNamespace, eventFile)
					if eventioReader == nil {
						logrus.Errorf("Failed to get content for event file: %s, skipping", eventFile)
						continue
					}
					eventbytes, err := io.ReadAll(eventioReader)
					if err != nil {
						logrus.Errorf("Failed to read event file: %v", err)
						continue
					}

					var eventList []map[string]any
					if err := json.Unmarshal(eventbytes, &eventList); err != nil {
						logrus.Errorf("Failed to unmarshal event: %v", err)
						continue
					}

					// Evenly distribute events to each channel
					for i, curr := range eventList {
						// Skip nil events (can occur with corrupted event files containing null elements)
						if curr == nil {
							continue
						}
						curr["clusterName"] = clusterSessionKey
						eventProcessorChannels[i%numOfEventProcessors] <- curr
					}
				}
			}
		}

		// Process events immediately on startup
		processAllEvents()

		// Create a ticker for hourly processing
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for {
			logrus.Info("Finished reading files, waiting for next cycle...")
			select {
			case <-stop:
				// Received stop signal, clean up and exit
				for i, currChan := range eventProcessorChannels {
					close(currChan)
					cctx[i]()
				}
				logrus.Info("Event processor received stop signal, exiting.")
				return
			case <-ticker.C:
				// Process events every hour
				processAllEvents()
			}
		}
	}()

	wg.Wait()
	return nil
}

// transformToEvent converts a RayEvent map to the API response Event format.
// This extracts common fields and puts event-type-specific data into customFields.
// It also extracts sourceHostname and sourcePid from nested events where available.
func transformToEvent(eventMap map[string]any) *types.Event {
	event := &types.Event{
		Severity:     "INFO",
		CustomFields: make(map[string]any),
	}

	// Extract common fields (already camelCase)
	if v, ok := eventMap["eventId"].(string); ok {
		event.EventID = v
	}
	if v, ok := eventMap["sourceType"].(string); ok {
		event.SourceType = v
	}
	// Convert ISO 8601 timestamp to Unix milliseconds to match Ray Dashboard format
	if v, ok := eventMap["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			event.Timestamp = fmt.Sprintf("%d", t.UnixMilli())
		} else {
			event.Timestamp = v // Keep original if parsing fails
		}
	}
	if v, ok := eventMap["severity"].(string); ok {
		event.Severity = v
	}
	if v, ok := eventMap["message"].(string); ok {
		event.Message = v
	}
	if v, ok := eventMap["sessionName"].(string); ok {
		event.CustomFields["sessionName"] = v
	}
	// Extract nodeId from base RayEvent (field #18)
	if v, ok := eventMap["nodeId"].(string); ok {
		event.NodeID = v
	}

	// Extract eventType and set as Label for filtering
	eventType, _ := eventMap["eventType"].(string)
	event.EventType = eventType
	event.Label = eventType // Use eventType as label for Dashboard compatibility

	// Map eventType to its corresponding nested data field
	eventDataFields := map[string]string{
		string(types.TASK_DEFINITION_EVENT):       "taskDefinitionEvent",
		string(types.TASK_LIFECYCLE_EVENT):        "taskLifecycleEvent",
		string(types.TASK_PROFILE_EVENT):          "taskProfileEvents",
		string(types.ACTOR_DEFINITION_EVENT):      "actorDefinitionEvent",
		string(types.ACTOR_LIFECYCLE_EVENT):       "actorLifecycleEvent",
		string(types.ACTOR_TASK_DEFINITION_EVENT): "actorTaskDefinitionEvent",
		string(types.NODE_DEFINITION_EVENT):       "nodeDefinitionEvent",
		string(types.NODE_LIFECYCLE_EVENT):        "nodeLifecycleEvent",
		string(types.DRIVER_JOB_DEFINITION_EVENT): "driverJobDefinitionEvent",
		string(types.DRIVER_JOB_LIFECYCLE_EVENT):  "driverJobLifecycleEvent",
	}

	if dataField, ok := eventDataFields[eventType]; ok {
		if data, ok := eventMap[dataField].(map[string]any); ok {
			event.CustomFields[dataField] = data

			// Extract sourceHostname and sourcePid from nested events where available
			extractHostnameAndPid(event, eventType, data)
		}
	}

	return event
}

// extractHostnameAndPid extracts sourceHostname and sourcePid from nested event data.
// These fields are available in specific event types as discovered from Ray protos.
func extractHostnameAndPid(event *types.Event, eventType string, data map[string]any) {
	switch eventType {
	case string(types.NODE_DEFINITION_EVENT):
		// NodeDefinitionEvent has: hostname, node_name, node_ip_address
		if hostname, ok := data["hostname"].(string); ok && hostname != "" {
			event.SourceHostname = hostname
		}
	case string(types.TASK_LIFECYCLE_EVENT):
		// TaskLifecycleEvent has: worker_pid, worker_id, node_id
		if pid, ok := data["workerPid"].(float64); ok {
			event.SourcePid = int(pid)
		}
	case string(types.ACTOR_LIFECYCLE_EVENT):
		// ActorLifecycleEvent has pid in StateTransition when ALIVE
		if transitions, ok := data["stateTransitions"].([]any); ok && len(transitions) > 0 {
			// Get the latest transition
			if lastTransition, ok := transitions[len(transitions)-1].(map[string]any); ok {
				if pid, ok := lastTransition["pid"].(float64); ok {
					event.SourcePid = int(pid)
				}
			}
		}
	case string(types.DRIVER_JOB_DEFINITION_EVENT):
		// DriverJobDefinitionEvent has: driver_pid, driver_node_id
		if pid, ok := data["driverPid"].(float64); ok {
			event.SourcePid = int(pid)
		} else if pidStr, ok := data["driverPid"].(string); ok && pidStr != "" {
			// Sometimes stored as string
			var pidInt int
			if _, err := fmt.Sscanf(pidStr, "%d", &pidInt); err == nil {
				event.SourcePid = pidInt
			}
		}
	}
}

// extractJobIDFromEvent extracts jobId from various event type payloads
func extractJobIDFromEvent(eventMap map[string]any) string {
	// Check common nested structures for jobId
	nestedFields := []string{
		"taskDefinitionEvent", "taskLifecycleEvent",
		"actorDefinitionEvent", "actorLifecycleEvent",
		"driverJobDefinitionEvent", "driverJobLifecycleEvent",
		"taskProfileEvents", "actorTaskDefinitionEvent",
	}

	for _, field := range nestedFields {
		if nested, ok := eventMap[field].(map[string]any); ok {
			if jobID, ok := nested["jobId"].(string); ok && jobID != "" {
				return jobID
			}
		}
	}
	return "" // Will be grouped under "global"
}

// storeEvent unmarshals the event map into the correct actor/task struct and then stores it into the corresonding list
func (h *EventHandler) storeEvent(eventMap map[string]any) error {
	eventTypeVal, ok := eventMap["eventType"]
	if !ok {
		return fmt.Errorf("event missing 'eventType' field")
	}
	eventTypeStr, ok := eventTypeVal.(string)
	if !ok {
		return fmt.Errorf("eventType is not a string, got %T", eventTypeVal)
	}
	eventType := types.EventType(eventTypeStr)

	clusterNameVal, ok := eventMap["clusterName"]
	if !ok {
		return fmt.Errorf("event missing 'clusterName' field")
	}
	currentClusterName, ok := clusterNameVal.(string)
	if !ok {
		return fmt.Errorf("clusterName is not a string, got %T", clusterNameVal)
	}

	// ========== Store RayEvent for /events API ==========
	if event := transformToEvent(eventMap); event != nil && event.EventID != "" {
		jobID := extractJobIDFromEvent(eventMap)
		clusterEventMap := h.ClusterEventMap.GetOrCreateEventMap(currentClusterName)
		clusterEventMap.AddEvent(jobID, *event)
	}
	// ====================================================

	logrus.Infof("current eventType: %v", eventType)
	switch eventType {
	case types.TASK_DEFINITION_EVENT:
		taskDef, ok := eventMap["taskDefinitionEvent"]
		if !ok {
			return fmt.Errorf("event does not have 'taskDefinitionEvent'")
		}
		jsonTaskDefinition, err := json.Marshal(taskDef)
		if err != nil {
			return err
		}

		var currTask types.Task
		if err := json.Unmarshal(jsonTaskDefinition, &currTask); err != nil {
			return err
		}

		taskMap := h.ClusterTaskMap.GetOrCreateTaskMap(currentClusterName)
		taskMap.CreateOrMergeAttempt(currTask.TaskID, currTask.AttemptNumber, func(t *types.Task) {
			// Merge definition fields (preserve existing Events if any)
			existingEvents := t.Events
			*t = currTask
			if len(existingEvents) > 0 {
				t.Events = existingEvents
				t.State = existingEvents[len(existingEvents)-1].State
			}
		})

	case types.TASK_LIFECYCLE_EVENT:
		lifecycleEvent, ok := eventMap["taskLifecycleEvent"].(map[string]any)
		if !ok {
			return fmt.Errorf("invalid taskLifecycleEvent format")
		}

		taskId, _ := lifecycleEvent["taskId"].(string)
		taskAttempt, _ := lifecycleEvent["taskAttempt"].(float64)
		transitions, _ := lifecycleEvent["stateTransitions"].([]any)

		nodeId, _ := lifecycleEvent["nodeId"].(string)
		workerId, _ := lifecycleEvent["workerId"].(string)

		if len(transitions) == 0 || taskId == "" {
			return nil
		}

		// Parse state transitions
		var stateEvents []types.StateEvent
		for _, transition := range transitions {
			tr, ok := transition.(map[string]any)
			if !ok {
				continue
			}
			state, _ := tr["state"].(string)
			timestampStr, _ := tr["timestamp"].(string)

			var timestamp time.Time
			if timestampStr != "" {
				timestamp, _ = time.Parse(time.RFC3339Nano, timestampStr)
			}

			stateEvents = append(stateEvents, types.StateEvent{
				State:     types.TaskStatus(state),
				Timestamp: timestamp,
			})
		}

		if len(stateEvents) == 0 {
			return nil
		}

		taskMap := h.ClusterTaskMap.GetOrCreateTaskMap(currentClusterName)
		taskMap.CreateOrMergeAttempt(taskId, int(taskAttempt), func(t *types.Task) {
			// --- DEDUPLICATION using (State + Timestamp) as unique key ---
			// Build a set of existing event keys to detect duplicates
			type eventKey struct {
				State     string
				Timestamp int64
			}
			existingKeys := make(map[eventKey]bool)
			for _, e := range t.Events {
				existingKeys[eventKey{string(e.State), e.Timestamp.UnixNano()}] = true
			}

			// Only append events that haven't been seen before
			for _, e := range stateEvents {
				key := eventKey{string(e.State), e.Timestamp.UnixNano()}
				if !existingKeys[key] {
					t.Events = append(t.Events, e)
					existingKeys[key] = true
				}
			}

			// Sort events by timestamp to ensure correct order
			sort.Slice(t.Events, func(i, j int) bool {
				return t.Events[i].Timestamp.Before(t.Events[j].Timestamp)
			})

			if len(t.Events) == 0 {
				return
			}

			t.State = t.Events[len(t.Events)-1].State

			if nodeId != "" {
				t.NodeID = nodeId
			}
			if workerId != "" {
				t.WorkerID = workerId
			}
			if t.StartTime.IsZero() {
				for _, e := range t.Events {
					if e.State == types.RUNNING {
						t.StartTime = e.Timestamp
						break
					}
				}
			}
			lastEvent := t.Events[len(t.Events)-1]
			if lastEvent.State == types.FINISHED || lastEvent.State == types.FAILED {
				t.EndTime = lastEvent.Timestamp
			}
		})

	case types.ACTOR_DEFINITION_EVENT:
		actorDef, ok := eventMap["actorDefinitionEvent"]
		if !ok {
			return fmt.Errorf("event does not have 'actorDefinitionEvent'")
		}
		jsonActorDefinition, err := json.Marshal(actorDef)
		if err != nil {
			return err
		}

		var currActor types.Actor
		if err := json.Unmarshal(jsonActorDefinition, &currActor); err != nil {
			return err
		}

		// Use CreateOrMergeActor pattern (same as Task)
		actorMap := h.ClusterActorMap.GetOrCreateActorMap(currentClusterName)
		actorMap.CreateOrMergeActor(currActor.ActorID, func(a *types.Actor) {
			// Preserve lifecycle-derived fields that may have arrived first
			existingEvents := a.Events
			existingState := a.State
			existingStartTime := a.StartTime
			existingEndTime := a.EndTime
			existingNumRestarts := a.NumRestarts
			existingPID := a.PID
			existingExitDetails := a.ExitDetails
			existingAddress := a.Address

			// Overwrite with definition fields
			*a = currActor

			// Restore lifecycle-derived fields if they existed
			if len(existingEvents) > 0 {
				a.Events = existingEvents
				a.State = existingState
				a.StartTime = existingStartTime
				a.EndTime = existingEndTime
				a.NumRestarts = existingNumRestarts
				a.PID = existingPID
				a.ExitDetails = existingExitDetails
				a.Address = existingAddress
			}
		})
	case types.ACTOR_LIFECYCLE_EVENT:
		lifecycleEvent, ok := eventMap["actorLifecycleEvent"].(map[string]any)
		if !ok {
			return fmt.Errorf("invalid actorLifecycleEvent format")
		}

		actorId, _ := lifecycleEvent["actorId"].(string)
		transitions, _ := lifecycleEvent["stateTransitions"].([]any)

		if len(transitions) == 0 || actorId == "" {
			return nil
		}

		// Parse state transitions into ActorStateEvent slice
		var stateEvents []types.ActorStateEvent
		for _, transition := range transitions {
			tr, ok := transition.(map[string]any)
			if !ok {
				continue
			}
			state, _ := tr["state"].(string)
			timestampStr, _ := tr["timestamp"].(string)
			nodeId, _ := tr["nodeId"].(string)
			workerId, _ := tr["workerId"].(string)
			reprName, _ := tr["reprName"].(string)

			var timestamp time.Time
			if timestampStr != "" {
				timestamp, _ = time.Parse(time.RFC3339Nano, timestampStr)
			}

			// DeathCause is a complex object, store as JSON string
			var deathCause string
			if dc, ok := tr["deathCause"]; ok {
				if dcBytes, err := json.Marshal(dc); err == nil {
					deathCause = string(dcBytes)
				}
			}

			stateEvents = append(stateEvents, types.ActorStateEvent{
				State:      types.StateType(state),
				Timestamp:  timestamp,
				NodeID:     nodeId,
				WorkerID:   workerId,
				ReprName:   reprName,
				DeathCause: deathCause,
			})
		}

		if len(stateEvents) == 0 {
			return nil
		}

		actorMap := h.ClusterActorMap.GetOrCreateActorMap(currentClusterName)
		actorMap.CreateOrMergeActor(actorId, func(a *types.Actor) {
			// Ensure ActorID is set (in case LIFECYCLE arrives before DEFINITION)
			a.ActorID = actorId

			// --- DEDUPLICATION using (State + Timestamp) as unique key ---
			// Build a set of existing event keys to detect duplicates
			type eventKey struct {
				State     string
				Timestamp int64
			}
			existingKeys := make(map[eventKey]bool)
			for _, e := range a.Events {
				existingKeys[eventKey{string(e.State), e.Timestamp.UnixNano()}] = true
			}

			// Only append events that haven't been seen before
			for _, e := range stateEvents {
				key := eventKey{string(e.State), e.Timestamp.UnixNano()}
				if !existingKeys[key] {
					a.Events = append(a.Events, e)
					existingKeys[key] = true
				}
			}

			// Sort events by timestamp to ensure correct order
			sort.Slice(a.Events, func(i, j int) bool {
				return a.Events[i].Timestamp.Before(a.Events[j].Timestamp)
			})

			if len(a.Events) == 0 {
				return
			}

			lastEvent := a.Events[len(a.Events)-1]

			// --- UPDATE STATE ---
			a.State = lastEvent.State

			// --- UPDATE ADDRESS from ALIVE state ---
			// NodeID and WorkerID are only populated in ALIVE state
			for i := len(a.Events) - 1; i >= 0; i-- {
				if a.Events[i].State == types.ALIVE && a.Events[i].NodeID != "" {
					a.Address.NodeID = a.Events[i].NodeID
					a.Address.WorkerID = a.Events[i].WorkerID
					break
				}
			}

			// --- UPDATE ReprName from latest ---
			if lastEvent.ReprName != "" {
				a.ReprName = lastEvent.ReprName
			}

			// --- CALCULATE StartTime (first ALIVE timestamp) ---
			if a.StartTime.IsZero() {
				for _, e := range a.Events {
					if e.State == types.ALIVE {
						a.StartTime = e.Timestamp
						break
					}
				}
			}

			// --- HANDLE DEAD state ---
			if lastEvent.State == types.DEAD {
				a.EndTime = lastEvent.Timestamp

				// Parse deathCause to extract PID, IP, errorMessage
				if lastEvent.DeathCause != "" {
					var deathCauseMap map[string]any
					if err := json.Unmarshal([]byte(lastEvent.DeathCause), &deathCauseMap); err == nil {
						if ctx, ok := deathCauseMap["actorDiedErrorContext"].(map[string]any); ok {
							// Extract PID
							if pid, ok := ctx["pid"].(float64); ok {
								a.PID = int(pid)
							}
							// Extract IP address
							if ip, ok := ctx["nodeIpAddress"].(string); ok {
								a.Address.IPAddress = ip
							}
							// Extract error message as ExitDetails
							if errMsg, ok := ctx["errorMessage"].(string); ok {
								a.ExitDetails = errMsg
							}
						}
					}
				}
			}

			// --- COUNT RESTARTS ---
			restartCount := 0
			for _, e := range a.Events {
				if e.State == types.RESTARTING {
					restartCount++
				}
			}
			a.NumRestarts = restartCount
		})

	case types.ACTOR_TASK_DEFINITION_EVENT:
		// TODO: Handle actor task definition event
		// This is related to GET /api/v0/tasks (type=ACTOR_TASK)
		logrus.Debugf("ACTOR_TASK_DEFINITION_EVENT received, not yet implemented")

	case types.DRIVER_JOB_DEFINITION_EVENT:
		// NOTE: When event comes in, JobID will be in base64, processing will convert it to Hex
		jobDef, ok := eventMap["driverJobDefinitionEvent"]
		if !ok {
			return fmt.Errorf("event does not have 'driverJobDefinitionEvent'")
		}

		jsonDriverJobDefinition, err := json.Marshal(jobDef)
		if err != nil {
			return err
		}

		var currJob types.Job
		if err := json.Unmarshal(jsonDriverJobDefinition, &currJob); err != nil {
			return err
		}

		// Convert JobID from base64 to hex
		currJob.JobID, err = utils.ConvertBase64ToHex(currJob.JobID)
		if err != nil {
			logrus.Errorf("Failed to convert JobID from base64 to Hex, will keep JobID in base64: %v", err)
		}

		// Convert DriverNodeID from base64 to hex
		currJob.DriverNodeID, err = utils.ConvertBase64ToHex(currJob.DriverNodeID)
		if err != nil {
			logrus.Errorf("Failed to convert DriverNodeID from base64 to hex, will keep DriverNodeID in base64: %v", err)
		}

		jobMap := h.ClusterJobMap.GetOrCreateJobMap(currentClusterName)
		jobMap.CreateOrMergeJob(currJob.JobID, func(j *types.Job) {
			// If for some reason jobID is empty, we will keep whatever is in 'j'
			var existingJobID string
			if currJob.JobID == "" {
				existingJobID = j.JobID
			}

			// ========== Lifecycle-derived fields ==========
			// These fields are set by DRIVER_JOB_LIFECYCLE_EVENT
			// We need to preserve them if lifecycle event arrived before definition event
			existingStateTransitions := j.StateTransitions
			existingState := j.State
			existingStartTime := j.StartTime
			existingEndTime := j.EndTime

			// Overwrite with definition fields
			*j = currJob

			// Restore lifecycle-derived fields if they existed
			if len(existingStateTransitions) > 0 {
				if existingJobID != "" {
					// This means that jobID was somehow empty.
					j.JobID = existingJobID
				}
				j.StateTransitions = existingStateTransitions
				j.State = existingState
				j.StartTime = existingStartTime
				j.EndTime = existingEndTime
			}

			// ========== Definition-only fields ==========
			// Status, StatusTransitions, Message, DriverExitCode
			// are ONLY set by DRIVER_JOB_DEFINITION_EVENT
			// They are already in currJob, no need to restore
		})
	case types.DRIVER_JOB_LIFECYCLE_EVENT:
		// NOTE: When event comes in, JobID will be in base64, processing will convert it to Hex
		jobLifecycleEvent, ok := eventMap["driverJobLifecycleEvent"].(map[string]any)
		if !ok {
			return fmt.Errorf("invalid driverJobLifecycleEvent format")
		}

		// Get JobID and also convert JobID to hex from base64
		// Will leave it as it if it's empty or somehow not a string
		jobId, ok := jobLifecycleEvent["jobId"].(string)
		if !ok {
			logrus.Errorf("jobID is missing or is not a string, leaving it as is")
		} else {
			jobId, _ = utils.ConvertBase64ToHex(jobId)
		}
		stateTransitionUnstructed, _ := jobLifecycleEvent["stateTransitions"].([]any)

		if len(stateTransitionUnstructed) == 0 || jobId == "" {
			return nil
		}

		// TODO(chiayi): Will need to convert status timeline once it is added as well
		// Following fields are related to status transition:
		//  - status
		//  - message
		//  - errorType
		//  - driverExitCode
		var stateTransitions []types.JobStateTransition
		for _, transition := range stateTransitionUnstructed {
			tr, ok := transition.(map[string]any)
			if !ok {
				continue
			}
			state, _ := tr["state"].(string)
			timestampStr, _ := tr["timestamp"].(string)

			var timestamp time.Time
			if timestampStr != "" {
				timestamp, _ = time.Parse(time.RFC3339Nano, timestampStr)
			}

			stateTransitions = append(stateTransitions, types.JobStateTransition{
				State:     types.JobState(state),
				Timestamp: timestamp,
			})
		}

		if len(stateTransitions) == 0 {
			return nil
		}

		jobMap := h.ClusterJobMap.GetOrCreateJobMap(currentClusterName)
		jobMap.CreateOrMergeJob(jobId, func(j *types.Job) {
			// TODO(chiayi): take care of status (job progress) state if part of DriverJobLifecycleEvent
			j.JobID = jobId

			type stateTransitionKey struct {
				State     string
				Timestamp int64
			}

			existingStateKeys := make(map[stateTransitionKey]bool)
			for _, t := range j.StateTransitions {
				existingStateKeys[stateTransitionKey{string(t.State), t.Timestamp.UnixNano()}] = true
			}

			for _, t := range stateTransitions {
				key := stateTransitionKey{string(t.State), t.Timestamp.UnixNano()}
				if !existingStateKeys[key] {
					j.StateTransitions = append(j.StateTransitions, t)
					existingStateKeys[key] = true
				}
			}

			sort.Slice(j.StateTransitions, func(i, k int) bool {
				return j.StateTransitions[i].Timestamp.Before(j.StateTransitions[k].Timestamp)
			})

			if len(j.StateTransitions) == 0 {
				return
			}

			lastStateTransition := j.StateTransitions[len(j.StateTransitions)-1]
			j.State = lastStateTransition.State

			if j.StartTime.IsZero() {
				for _, t := range j.StateTransitions {
					if t.State == types.CREATED {
						j.StartTime = t.Timestamp
						break
					}
				}
			}

			if lastStateTransition.State == types.JOBFINISHED {
				j.EndTime = lastStateTransition.Timestamp
			}
		})
	default:
		logrus.Infof("Event not supported, skipping: %v", eventMap)
	}

	return nil
}

// getAllJobEventFiles get all the job event files for the given cluster.
// Assuming that the events file object follow the format root/clustername/sessionid/job_events/{job-*}/*
func (h *EventHandler) getAllJobEventFiles(clusterInfo utils.ClusterInfo) []string {
	var allJobFiles []string
	clusterNameID := clusterInfo.Name + "_" + clusterInfo.Namespace
	jobEventDirPrefix := clusterInfo.SessionName + "/job_events/"
	jobDirList := h.reader.ListFiles(clusterNameID, jobEventDirPrefix)

	for _, jobDir := range jobDirList {
		// Skip non-directory entries
		if !strings.HasSuffix(jobDir, "/") {
			continue
		}
		jobDirPath := jobEventDirPrefix + jobDir
		jobFiles := h.reader.ListFiles(clusterNameID, jobDirPath)
		for _, jobFile := range jobFiles {
			if isValidEventFile(jobFile) {
				allJobFiles = append(allJobFiles, jobDirPath+jobFile)
			}
		}
	}
	return allJobFiles
}

// getAllNodeEventFiles retrieves all node event files for the given cluster
func (h *EventHandler) getAllNodeEventFiles(clusterInfo utils.ClusterInfo) []string {
	clusterNameID := clusterInfo.Name + "_" + clusterInfo.Namespace
	nodeEventDirPrefix := clusterInfo.SessionName + "/node_events/"
	nodeEventFileNames := h.reader.ListFiles(clusterNameID, nodeEventDirPrefix)

	// Filter out directories (items ending with /) and build full paths
	var nodeEventFiles []string
	for _, fileName := range nodeEventFileNames {
		// Skip directories
		if isValidEventFile(fileName) {
			fullPath := nodeEventDirPrefix + fileName
			nodeEventFiles = append(nodeEventFiles, fullPath)
		}
	}
	return nodeEventFiles
}

// GetTasks returns a thread-safe deep copy of all tasks (including all attempts) for a given cluster.
// Each task attempt is returned as a separate element in the slice.
// Deep copy ensures the returned data is safe to use after the lock is released.
func (h *EventHandler) GetTasks(clusterName string) []types.Task {
	h.ClusterTaskMap.RLock()
	defer h.ClusterTaskMap.RUnlock()

	taskMap, ok := h.ClusterTaskMap.ClusterTaskMap[clusterName]
	if !ok {
		return []types.Task{}
	}

	taskMap.Lock()
	defer taskMap.Unlock()

	// Flatten all attempts into a single slice with deep copy
	var tasks []types.Task
	for _, attempts := range taskMap.TaskMap {
		for _, task := range attempts {
			tasks = append(tasks, task.DeepCopy())
		}
	}
	return tasks
}

// GetTaskByID returns all attempts for a specific task ID in a given cluster.
// Returns a slice of tasks representing all attempts, sorted by attempt number is not guaranteed.
func (h *EventHandler) GetTaskByID(clusterName, taskID string) ([]types.Task, bool) {
	h.ClusterTaskMap.RLock()
	defer h.ClusterTaskMap.RUnlock()

	taskMap, ok := h.ClusterTaskMap.ClusterTaskMap[clusterName]
	if !ok {
		return nil, false
	}

	taskMap.Lock()
	defer taskMap.Unlock()

	attempts, ok := taskMap.TaskMap[taskID]
	if !ok || len(attempts) == 0 {
		return nil, false
	}
	// Return a deep copy to avoid data race
	result := make([]types.Task, len(attempts))
	for i, task := range attempts {
		result[i] = task.DeepCopy()
	}
	return result, true
}

// GetTasksByJobID returns all tasks (including all attempts) for a given job ID in a cluster.
func (h *EventHandler) GetTasksByJobID(clusterName, jobID string) []types.Task {
	h.ClusterTaskMap.RLock()
	defer h.ClusterTaskMap.RUnlock()

	taskMap, ok := h.ClusterTaskMap.ClusterTaskMap[clusterName]
	if !ok {
		return []types.Task{}
	}

	taskMap.Lock()
	defer taskMap.Unlock()

	var tasks []types.Task
	for _, attempts := range taskMap.TaskMap {
		for _, task := range attempts {
			if task.JobID == jobID {
				tasks = append(tasks, task.DeepCopy())
			}
		}
	}
	return tasks
}

// GetActors returns a thread-safe deep copy of all actors for a given cluster
func (h *EventHandler) GetActors(clusterName string) []types.Actor {
	h.ClusterActorMap.RLock()
	defer h.ClusterActorMap.RUnlock()

	actorMap, ok := h.ClusterActorMap.ClusterActorMap[clusterName]
	if !ok {
		return []types.Actor{}
	}

	actorMap.Lock()
	defer actorMap.Unlock()

	actors := make([]types.Actor, 0, len(actorMap.ActorMap))
	for _, actor := range actorMap.ActorMap {
		actors = append(actors, actor.DeepCopy())
	}
	return actors
}

// GetActorByID returns a specific actor by ID for a given cluster
func (h *EventHandler) GetActorByID(clusterName, actorID string) (types.Actor, bool) {
	h.ClusterActorMap.RLock()
	defer h.ClusterActorMap.RUnlock()

	actorMap, ok := h.ClusterActorMap.ClusterActorMap[clusterName]
	if !ok {
		return types.Actor{}, false
	}

	actorMap.Lock()
	defer actorMap.Unlock()

	actor, ok := actorMap.ActorMap[actorID]
	if !ok {
		return types.Actor{}, false
	}
	return actor.DeepCopy(), true
}

// GetActorsMap returns a thread-safe deep copy of all actors as a map for a given cluster
func (h *EventHandler) GetActorsMap(clusterName string) map[string]types.Actor {
	h.ClusterActorMap.RLock()
	defer h.ClusterActorMap.RUnlock()

	actorMap, ok := h.ClusterActorMap.ClusterActorMap[clusterName]
	if !ok {
		return map[string]types.Actor{}
	}

	actorMap.Lock()
	defer actorMap.Unlock()

	actors := make(map[string]types.Actor, len(actorMap.ActorMap))
	for id, actor := range actorMap.ActorMap {
		actors[id] = actor.DeepCopy()
	}
	return actors
}

func (h *EventHandler) GetJobsMap(clusterName string) map[string]types.Job {
	h.ClusterJobMap.RLock()
	defer h.ClusterJobMap.RUnlock()

	jobMap, ok := h.ClusterJobMap.ClusterJobMap[clusterName]
	if !ok {
		return map[string]types.Job{}
	}

	jobMap.Lock()
	defer jobMap.Unlock()

	jobs := make(map[string]types.Job, len(jobMap.JobMap))
	for id, job := range jobMap.JobMap {
		jobs[id] = job.DeepCopy()
	}
	return jobs
}

func (h *EventHandler) GetJobByJobID(clusterName, jobID string) (types.Job, bool) {
	h.ClusterJobMap.RLock()
	defer h.ClusterJobMap.RUnlock()

	jobMap, ok := h.ClusterJobMap.ClusterJobMap[clusterName]
	if !ok {
		return types.Job{}, false
	}

	jobMap.Lock()
	defer jobMap.Unlock()

	job, ok := jobMap.JobMap[jobID]
	if !ok {
		return types.Job{}, false
	}
	return job.DeepCopy(), true
}
