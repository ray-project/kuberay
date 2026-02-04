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

	// clusterNameVal is actually the cluster session key.
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
		return h.handleTaskDefinitionEvent(eventMap, currentClusterName, false)
	case types.TASK_LIFECYCLE_EVENT:
		return h.handleTaskLifecycleEvent(eventMap, currentClusterName)
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
		return h.handleTaskDefinitionEvent(eventMap, currentClusterName, true)
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

// GetTasks returns a slice of thread-safe deep copies of all task attempts for a given cluster session.
// Deep copy ensures the returned data is safe to use after the lock is released.
func (h *EventHandler) GetTasks(clusterSessionKey string) []types.Task {
	h.ClusterTaskMap.RLock()
	defer h.ClusterTaskMap.RUnlock()

	taskMap, ok := h.ClusterTaskMap.ClusterTaskMap[clusterSessionKey]
	if !ok {
		// TODO(jwj): Add error handling.
		logrus.Errorf("Task map not found for cluster session: %s", clusterSessionKey)
		return []types.Task{}
	}

	taskMap.Lock()
	defer taskMap.Unlock()

	// Flatten all task attempts into a single slice with deep copy.
	allTasks := make([]types.Task, 0)
	for _, taskAttempts := range taskMap.TaskMap {
		for _, taskAttempt := range taskAttempts {
			allTasks = append(allTasks, taskAttempt.DeepCopy())
		}
	}

	return allTasks
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

// handleTaskDefinitionEvent processes TASK_DEFINITION_EVENT or ACTOR_TASK_DEFINITION_EVENT and preserves the task attempt ordering.
func (h *EventHandler) handleTaskDefinitionEvent(eventMap map[string]any, clusterSessionKey string, isActorTask bool) error {
	var taskDefField string
	if isActorTask {
		taskDefField = "actorTaskDefinitionEvent"
	} else {
		taskDefField = "taskDefinitionEvent"
	}

	taskDef, ok := eventMap[taskDefField]
	if !ok {
		return fmt.Errorf("event does not have '%s' field", taskDefField)
	}

	jsonTaskDefinition, err := json.Marshal(taskDef)
	if err != nil {
		return fmt.Errorf("failed to marshal %s event: %w", taskDefField, err)
	}

	var currTask types.Task
	if err := json.Unmarshal(jsonTaskDefinition, &currTask); err != nil {
		return fmt.Errorf("failed to unmarshal %s event: %w", taskDefField, err)
	}
	if currTask.TaskID == "" {
		return fmt.Errorf("task ID is empty")
	}
	normalizeTaskIDsToHex(&currTask)

	// Manually set the task type for an actor task.
	if isActorTask {
		currTask.TaskType = types.ACTOR_TASK
	}

	taskMap := h.ClusterTaskMap.GetOrCreateTaskMap(clusterSessionKey)
	taskMap.CreateOrMergeAttempt(currTask.TaskID, currTask.TaskAttempt, func(task *types.Task) {
		// Preserve existing lifecycle-derived fields to avoid overwriting.
		existingStateTransitions := task.StateTransitions
		existingRayErrorInfo := task.RayErrorInfo
		existingNodeID := task.NodeID
		existingWorkerID := task.WorkerID
		existingWorkerPID := task.WorkerPID
		existingIsDebuggerPaused := task.IsDebuggerPaused
		existingActorReprName := task.ActorReprName
		existingTaskLogInfo := task.TaskLogInfo

		*task = currTask

		if len(existingStateTransitions) > 0 {
			task.StateTransitions = existingStateTransitions
			task.RayErrorInfo = existingRayErrorInfo
			task.NodeID = existingNodeID
			task.WorkerID = existingWorkerID
			task.WorkerPID = existingWorkerPID
			task.IsDebuggerPaused = existingIsDebuggerPaused
			task.ActorReprName = existingActorReprName
			task.TaskLogInfo = existingTaskLogInfo
			task.State = task.GetLastState()
		}
	})

	return nil
}

// handleTaskLifecycleEvent processes TASK_LIFECYCLE_EVENT and merges state transitions for a given task attempt.
func (h *EventHandler) handleTaskLifecycleEvent(eventMap map[string]any, clusterSessionKey string) error {
	taskLifecycle, ok := eventMap["taskLifecycleEvent"]
	if !ok {
		return fmt.Errorf("event does not have 'taskLifecycleEvent' field")
	}

	jsonTaskLifecycle, err := json.Marshal(taskLifecycle)
	if err != nil {
		return fmt.Errorf("failed to marshal task lifecycle event: %w", err)
	}

	var currTask types.Task
	if err := json.Unmarshal(jsonTaskLifecycle, &currTask); err != nil {
		return fmt.Errorf("failed to unmarshal task lifecycle event: %w", err)
	}
	if currTask.TaskID == "" {
		return fmt.Errorf("task ID is empty")
	}
	normalizeTaskIDsToHex(&currTask)

	// TODO(jwj): Clarify if there must be at least one state transition. Can one task have more than one state transition?
	if len(currTask.StateTransitions) == 0 {
		return fmt.Errorf("TASK_LIFECYCLE_EVENT must have at least one state transition")
	}

	taskMap := h.ClusterTaskMap.GetOrCreateTaskMap(clusterSessionKey)
	taskMap.CreateOrMergeAttempt(currTask.TaskID, currTask.TaskAttempt, func(task *types.Task) {
		// --- DEDUPLICATION using (State + Timestamp) as unique key ---
		// Build a set of existing event keys to detect duplicates
		type eventKey struct {
			State     string
			Timestamp int64
		}
		existingKeys := make(map[eventKey]bool)
		for _, e := range task.StateTransitions {
			existingKeys[eventKey{string(e.State), e.Timestamp.UnixNano()}] = true
		}

		// Only append events that haven't been seen before
		for _, e := range currTask.StateTransitions {
			key := eventKey{string(e.State), e.Timestamp.UnixNano()}
			if !existingKeys[key] {
				task.StateTransitions = append(task.StateTransitions, e)
				existingKeys[key] = true
			}
		}

		// Sort events by timestamp to ensure correct order
		sort.Slice(task.StateTransitions, func(i, j int) bool {
			return task.StateTransitions[i].Timestamp.Before(task.StateTransitions[j].Timestamp)
		})

		if len(task.StateTransitions) == 0 {
			return
		}

		// TODO(jwj): Before beta, the lifecycle-related fields are overwritten.
		// In beta, the complete historical replay will be supported.
		task.RayErrorInfo = currTask.RayErrorInfo
		task.JobID = currTask.JobID
		if currTask.NodeID != "" {
			task.NodeID = currTask.NodeID
		}
		if currTask.WorkerID != "" {
			task.WorkerID = currTask.WorkerID
		}
		if currTask.WorkerPID != 0 {
			task.WorkerPID = currTask.WorkerPID
		}
		task.IsDebuggerPaused = currTask.IsDebuggerPaused
		task.ActorReprName = currTask.ActorReprName
		task.TaskLogInfo = currTask.TaskLogInfo
		task.State = task.GetLastState()

		// Derive creation time, start time and end time from state transitions.
		// Ref: https://github.com/ray-project/ray/blob/d0b1d151d8ea964a711e451d0ae736f8bf95b629/python/ray/util/state/common.py#L1660-L1685
		for _, tr := range task.StateTransitions {
			switch tr.State {
			case types.PENDING_ARGS_AVAIL:
				if task.CreationTime.IsZero() {
					task.CreationTime = tr.Timestamp
				}
			case types.RUNNING:
				if task.StartTime.IsZero() {
					task.StartTime = tr.Timestamp
				}
			case types.FINISHED, types.FAILED:
				// Take the latest timestamp as the end time.
				task.EndTime = tr.Timestamp
			}
		}
	})

	return nil
}

// normalizeTaskIDsToHex converts base64-encoded Ray IDs in task-related events:
//   - TASK_DEFINITION_EVENT
//   - ACTOR_TASK_DEFINITION_EVENT
//   - TASK_LIFECYCLE_EVENT
//
// to hex so stored tasks match the live cluster API schema.
func normalizeTaskIDsToHex(task *types.Task) {
	normalize := func(base64ID string) string {
		if base64ID == "" {
			return ""
		}

		// Check if the Ray ID is nil.
		isNil, err := utils.IsBase64Nil(base64ID)
		if err != nil {
			logrus.Errorf("Failed to check if Ray ID is nil: %v", err)
			return base64ID
		}
		if isNil {
			logrus.Infof("Ray ID is nil, keeping original base64 ID: %s", base64ID)
			return base64ID
		}

		hexID, err := utils.ConvertBase64ToHex(base64ID)
		if err != nil {
			logrus.Errorf("Failed to convert ID from base64 to hex, keeping original: %v", err)
			return base64ID
		}
		return hexID
	}

	task.TaskID = normalize(task.TaskID)
	task.ActorID = normalize(task.ActorID)
	task.JobID = normalize(task.JobID)
	task.ParentTaskID = normalize(task.ParentTaskID)
	task.PlacementGroupID = normalize(task.PlacementGroupID)
	task.NodeID = normalize(task.NodeID)
	task.WorkerID = normalize(task.WorkerID)
}
