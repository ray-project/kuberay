package eventserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
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

// Run will start numOfEventProcessors (default to 2) processing functions and the event reader. The event reader will run once an hr,
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
		taskMap.UpsertAttempt(currTask.TaskID, currTask.AttemptNumber, func(t *types.Task) {
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
		taskMap.UpsertAttempt(taskId, int(taskAttempt), func(t *types.Task) {
			t.Events = append(t.Events, stateEvents...)
			t.State = stateEvents[len(stateEvents)-1].State
		})

	case types.ACTOR_DEFINITION_EVENT:
		var currActor types.Actor
		actorDef, ok := eventMap["actorDefinitionEvent"]
		if !ok {
			return fmt.Errorf("event does not have 'actorDefinitionEvent'")
		}
		jsonActorDefinition, err := json.Marshal(actorDef)
		if err != nil {
			return err
		}

		err = json.Unmarshal(jsonActorDefinition, &currActor)
		if err != nil {
			return err
		}

		h.ClusterActorMap.Lock()
		clusterActorMapObject, ok := h.ClusterActorMap.ClusterActorMap[currentClusterName]
		if !ok {
			// Does not exist, create a new map
			clusterActorMapObject = types.NewActorMap()
			h.ClusterActorMap.ClusterActorMap[currentClusterName] = clusterActorMapObject
		}
		h.ClusterActorMap.Unlock()

		actorId := currActor.ActorID
		clusterActorMapObject.Lock()
		existingActor, exists := clusterActorMapObject.ActorMap[actorId]
		if !exists {
			clusterActorMapObject.ActorMap[actorId] = currActor
		} else {
			// Compare NumRestarts to keep the latest state
			if currActor.NumRestarts > existingActor.NumRestarts {
				clusterActorMapObject.ActorMap[actorId] = currActor
			}
		}
		clusterActorMapObject.Unlock()

	case types.ACTOR_LIFECYCLE_EVENT:
		// TODO: Update actor state based on lifecycle event
		logrus.Debugf("ACTOR_LIFECYCLE_EVENT received, not yet implemented")

	case types.ACTOR_TASK_DEFINITION_EVENT:
		// TODO: Handle actor task definition event
		// This is related to GET /api/v0/tasks (type=ACTOR_TASK)
		logrus.Debugf("ACTOR_TASK_DEFINITION_EVENT received, not yet implemented")
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

// GetTasks returns a thread-safe copy of all tasks (including all attempts) for a given cluster.
// Each task attempt is returned as a separate element in the slice.
func (h *EventHandler) GetTasks(clusterName string) []types.Task {
	h.ClusterTaskMap.RLock()
	defer h.ClusterTaskMap.RUnlock()

	taskMap, ok := h.ClusterTaskMap.ClusterTaskMap[clusterName]
	if !ok {
		return []types.Task{}
	}

	taskMap.Lock()
	defer taskMap.Unlock()

	// Flatten all attempts into a single slice
	var tasks []types.Task
	for _, attempts := range taskMap.TaskMap {
		tasks = append(tasks, attempts...)
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
	// Return a copy to avoid data race
	result := make([]types.Task, len(attempts))
	copy(result, attempts)
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
				tasks = append(tasks, task)
			}
		}
	}
	return tasks
}

// GetActors returns a thread-safe copy of all actors for a given cluster
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
		actors = append(actors, actor)
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
	return actor, ok
}

// GetActorsMap returns a thread-safe copy of all actors as a map for a given cluster
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
		actors[id] = actor
	}
	return actors
}
