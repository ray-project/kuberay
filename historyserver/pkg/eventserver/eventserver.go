package eventserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
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
						curr["clusterName"] = clusterInfo.Name + "_" + clusterInfo.Namespace
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
			// Merge definition fields (preserve existing Events, ProfileData, and identifiers if any)
			existingEvents := t.Events
			existingProfileData := t.ProfileData
			existingNodeID := t.NodeID
			existingWorkerID := t.WorkerID
			*t = currTask
			if len(existingEvents) > 0 {
				t.Events = existingEvents
				t.State = existingEvents[len(existingEvents)-1].State
			}
			if existingProfileData != nil {
				t.ProfileData = existingProfileData
			}
			if existingNodeID != "" {
				t.NodeID = existingNodeID
			}
			if existingWorkerID != "" {
				t.WorkerID = existingWorkerID
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
	case types.TASK_PROFILE_EVENT:
		taskProfileEvent, ok := eventMap["taskProfileEvents"]
		if !ok {
			return fmt.Errorf("event does not have 'taskProfileEvents'")
		}
		jsonBytes, err := json.Marshal(taskProfileEvent)
		if err != nil {
			return err
		}

		var profileData types.TaskProfileEventDTO
		if err := json.Unmarshal(jsonBytes, &profileData); err != nil {
			logrus.Errorf("Failed to unmarshal TASK_PROFILE_EVENT: %v", err)
			return err
		}

		if profileData.TaskID == "" || len(profileData.ProfileEvents.Events) == 0 {
			logrus.Debugf("TASK_PROFILE_EVENT has no taskId or events, skipping")
			return nil
		}

		// Convert events to ProfileEventRaw format
		var rawEvents = make([]types.ProfileEventRaw, 0, len(profileData.ProfileEvents.Events))
		for _, e := range profileData.ProfileEvents.Events {
			startTime, err := strconv.ParseInt(e.StartTime, 10, 64)
			if err != nil {
				logrus.Warnf("Failed to parse StartTime '%s': %v", e.StartTime, err)
				continue
			}
			endTime, err := strconv.ParseInt(e.EndTime, 10, 64)
			if err != nil {
				logrus.Warnf("Failed to parse EndTime '%s': %v", e.EndTime, err)
				continue
			}

			rawEvents = append(rawEvents, types.ProfileEventRaw{
				EventName: e.EventName,
				StartTime: startTime,
				EndTime:   endTime,
				ExtraData: e.ExtraData,
			})
		}

		taskMap := h.ClusterTaskMap.GetOrCreateTaskMap(currentClusterName)
		taskMap.CreateOrMergeAttempt(profileData.TaskID, profileData.AttemptNumber, func(t *types.Task) {
			// Ensure core identifiers are set
			if t.TaskID == "" {
				t.TaskID = profileData.TaskID
			}
			if t.JobID == "" {
				t.JobID = profileData.JobID
			}

			// Initialize ProfileData if not exists
			if t.ProfileData == nil {
				t.ProfileData = &types.ProfileData{
					ComponentID:   profileData.ProfileEvents.ComponentID,
					ComponentType: profileData.ProfileEvents.ComponentType,
					NodeIPAddress: profileData.ProfileEvents.NodeIPAddress,
				}
			}

			// Merge events with deduplication based on (eventName, startTime, endTime)
			type eventKey struct {
				EventName string
				StartTime int64
				EndTime   int64
			}
			existingKeys := make(map[eventKey]struct{})
			for _, e := range t.ProfileData.Events {
				existingKeys[eventKey{e.EventName, e.StartTime, e.EndTime}] = struct{}{}
			}
			for _, e := range rawEvents {
				key := eventKey{e.EventName, e.StartTime, e.EndTime}
				if _, ok := existingKeys[key]; !ok {
					t.ProfileData.Events = append(t.ProfileData.Events, e)
					existingKeys[key] = struct{}{}
				}
			}

			// Extract func_or_class_name from extraData if available
			for _, e := range rawEvents {
				if strings.HasPrefix(e.EventName, "task::") && e.ExtraData != "" {
					var extra map[string]interface{}
					if err := json.Unmarshal([]byte(e.ExtraData), &extra); err == nil {
						if name, ok := extra["name"].(string); ok && name != "" {
							// For actor methods, name might be just "increment" or "get_count"
							// But eventName has the full form like "task::Counter.increment"
							// Use eventName to get the full func_or_class_name
							t.FuncOrClassName = strings.TrimPrefix(e.EventName, "task::")
							t.Name = name
						}
					}
				}
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

// GetTasksTimeline returns timeline data in Chrome Tracing Format
// Output format matches Ray Dashboard's /api/v0/tasks/timeline endpoint
func (h *EventHandler) GetTasksTimeline(clusterName string, jobID string) []types.ChromeTraceEvent {
	var tasks []types.Task
	if jobID != "" {
		tasks = h.GetTasksByJobID(clusterName, jobID)
	} else {
		tasks = h.GetTasks(clusterName)
	}

	if len(tasks) == 0 {
		return []types.ChromeTraceEvent{}
	}

	events := []types.ChromeTraceEvent{}

	// Build PID/TID mappings
	// PID: Node IP -> numeric ID
	// TID: Worker ID -> numeric ID per node
	nodeIPToPID := make(map[string]int)
	workerToTID := make(map[string]map[string]int) // nodeIP -> workerID -> tid
	pidCounter := 0
	tidCounters := make(map[string]int) // per-node tid counter

	// First pass: collect all unique nodes and workers
	for _, task := range tasks {
		if task.ProfileData == nil {
			continue
		}
		nodeIP := task.ProfileData.NodeIPAddress
		workerID := task.ProfileData.ComponentID

		if nodeIP == "" {
			continue
		}

		if _, exists := nodeIPToPID[nodeIP]; !exists {
			nodeIPToPID[nodeIP] = pidCounter
			pidCounter++
			workerToTID[nodeIP] = make(map[string]int)
			tidCounters[nodeIP] = 0
		}

		if workerID != "" {
			if _, exists := workerToTID[nodeIP][workerID]; !exists {
				workerToTID[nodeIP][workerID] = tidCounters[nodeIP]
				tidCounters[nodeIP]++
			}
		}
	}

	// Generate process_name and thread_name metadata events
	for nodeIP, pid := range nodeIPToPID {
		events = append(events, types.ChromeTraceEvent{
			Name:  "process_name",
			PID:   pid,
			TID:   nil,
			Phase: "M",
			Args: map[string]interface{}{
				"name": "Node " + nodeIP,
			},
		})

		for workerID, tid := range workerToTID[nodeIP] {
			tidVal := tid
			events = append(events, types.ChromeTraceEvent{
				Name:  "thread_name",
				PID:   pid,
				TID:   &tidVal,
				Phase: "M",
				Args: map[string]interface{}{
					"name": "worker:" + workerID,
				},
			})
		}
	}

	// Generate trace events from ProfileData
	for _, task := range tasks {
		if task.ProfileData == nil || len(task.ProfileData.Events) == 0 {
			continue
		}

		nodeIP := task.ProfileData.NodeIPAddress
		workerID := task.ProfileData.ComponentID

		pid, ok := nodeIPToPID[nodeIP]
		if !ok {
			continue
		}

		var tidPtr *int
		if tid, ok := workerToTID[nodeIP][workerID]; ok {
			tidPtr = &tid
		}

		for _, profEvent := range task.ProfileData.Events {
			// Convert nanoseconds to microseconds
			startTimeUs := float64(profEvent.StartTime) / 1000.0
			durationUs := float64(profEvent.EndTime-profEvent.StartTime) / 1000.0

			// Parse extraData for additional fields
			var extraData map[string]interface{}
			if profEvent.ExtraData != "" {
				json.Unmarshal([]byte(profEvent.ExtraData), &extraData)
			}

			// Determine task_id and func_or_class_name
			taskIDForArgs := task.TaskID
			funcOrClassName := task.FuncOrClassName

			// Try to get from extraData if available (for hex format task_id)
			if extraData != nil {
				if tid, ok := extraData["task_id"].(string); ok && tid != "" {
					taskIDForArgs = tid
				}
			}

			// Build args
			actorID := extractActorIDFromTaskID(taskIDForArgs)
			args := map[string]interface{}{
				"task_id":            taskIDForArgs,
				"job_id":             task.JobID,
				"attempt_number":     task.AttemptNumber,
				"func_or_class_name": funcOrClassName,
				"actor_id":           nil,
			}

			if actorID != "" {
				args["actor_id"] = actorID
			}

			// Add "name" field for overall task events (task::xxx)
			if strings.HasPrefix(profEvent.EventName, "task::") {
				if extraData != nil {
					if name, ok := extraData["name"].(string); ok {
						args["name"] = name
					}
				}
			}

			// Determine event name for display
			eventName := profEvent.EventName
			displayName := profEvent.EventName

			// For overall task events like "task::slow_task", use the full name from extraData
			if strings.HasPrefix(profEvent.EventName, "task::") && extraData != nil {
				if name, ok := extraData["name"].(string); ok {
					displayName = name
				}
			}

			traceEvent := types.ChromeTraceEvent{
				Category:  eventName,
				Name:      displayName,
				PID:       pid,
				TID:       tidPtr,
				Timestamp: &startTimeUs,
				Duration:  &durationUs,
				Color:     getChromeTraceColor(eventName),
				Args:      args,
				Phase:     "X",
			}

			events = append(events, traceEvent)
		}
	}

	return events
}

// getChromeTraceColor maps event names to Chrome trace colors
// Based on Ray's _default_color_mapping in profiling.py
func getChromeTraceColor(eventName string) string {
	// Handle task::xxx pattern (overall task event)
	if strings.HasPrefix(eventName, "task::") {
		return "generic_work"
	}

	// Direct mapping for known event names
	// This logic follows Ray's profiling implementation:
	// https://github.com/ray-project/ray/blob/68d01c4c48a59c7768ec9c2359a1859966c446b6/python/ray/_private/profiling.py#L25
	switch eventName {
	case "task:deserialize_arguments":
		return "rail_load"
	case "task:execute":
		return "rail_animation"
	case "task:store_outputs":
		return "rail_idle"
	case "task:submit_task", "task":
		return "rail_response"
	case "worker_idle":
		return "cq_build_abandoned"
	case "ray.get":
		return "good"
	case "ray.put":
		return "terrible"
	case "ray.wait":
		return "vsync_highlight_color"
	case "submit_task":
		return "background_memory_dump"
	case "wait_for_function", "fetch_and_run_function", "register_remote_function":
		return "detailed_memory_dump"
	default:
		return "generic_work"
	}
}

func extractActorIDFromTaskID(taskIDHex string) string {
	if len(taskIDHex) != 48 {
		return "" // can't process if encoded in base64
	}

	actorPortion := taskIDHex[16:40] // 24 chars for actor id (12 bytes)
	jobPortion := taskIDHex[40:48]   // 8 chars for job id (4 bytes)

	// Check if all Fs (no actor)
	if actorPortion == "ffffffffffffffffffffffff" {
		return ""
	}

	return actorPortion + jobPortion
}
