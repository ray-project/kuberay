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
				return err
			}
		}
	}
}

// Run will start numOfEventProcessors (default to 2) processing functions and the event reader. The event reader will run once an hr,
// which is currently how often the collector flushes.
func (h *EventHandler) Run(stop chan struct{}, numOfEventProcessors int) error {
	var wg sync.WaitGroup

	if numOfEventProcessors == 0 {
		numOfEventProcessors = 2
	}
	eventProcessorChannels := make([]chan map[string]any, numOfEventProcessors)
	cctx := make([]context.CancelFunc, numOfEventProcessors)

	for i := range numOfEventProcessors {
		eventProcessorChannels[i] = make(chan map[string]any, 20)
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
		fmt.Printf("Starting this loop of event processing\n")
		for {
			select {
			case <-stop:
				for i, currChan := range eventProcessorChannels {
					close(currChan)
					cctx[i]()
				}
				logrus.Infof("Event processor received stop signal, exiting.")
				return
			default:
				// S3, minio, and GCS are flat structures, object names are whole paths
				clusterList := h.reader.List()
				for _, clusterInfo := range clusterList {
					clusterNameNamespace := clusterInfo.Name + "_" + clusterInfo.Namespace
					eventFileList := append(h.getAllJobEventFiles(clusterInfo), h.getAllNodeEventFiles(clusterInfo)...)

					logrus.Infof("current eventFileList for cluster %s is: %v", clusterInfo.Name, eventFileList)
					for i := range eventFileList {
						// TODO: Filter out ones that have already been read
						logrus.Infof("Reading event file: %s", eventFileList[i])

						eventioReader := h.reader.GetContent(clusterNameNamespace, eventFileList[i])
						if eventioReader == nil {
							logrus.Errorf("Failed to get content for event file: %s, skipping", eventFileList[i])
							continue
						}
						eventbytes, err := io.ReadAll(eventioReader)
						if err != nil {
							logrus.Errorf("Failed to read event file: %v", err)
							continue
						}

						// Unmarshal the list of events
						var eventList []map[string]any
						if err := json.Unmarshal(eventbytes, &eventList); err != nil {
							logrus.Errorf("Failed to unmarshal event: %v", err)
							continue
						}

						// Evenly distribute event to each channel
						for i, curr := range eventList {
							// current index % number of event processors dictates which channel it goes to
							curr["clusterName"] = clusterInfo.Name + "_" + clusterInfo.Namespace
							eventProcessorChannels[i%numOfEventProcessors] <- curr
						}
					}
				}
			}

			// Sleep 1 hr since thats how often it flushes
			logrus.Infof("Finished reading files, going to sleep...")
			time.Sleep(1 * time.Hour)
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
		// We take out the taskDefinitionEvent from the event, marshal it into json so we can
		// unmarshal it into a Task object.
		var currTask types.Task
		taskDef, ok := eventMap["taskDefinitionEvent"]
		if !ok {
			return fmt.Errorf("event does not have 'taskDefinitionEvent'")
		}
		jsonTaskDefinition, err := json.Marshal(taskDef)
		if err != nil {
			return err
		}

		err = json.Unmarshal(jsonTaskDefinition, &currTask)
		if err != nil {
			return err
		}

		h.ClusterTaskMap.Lock()
		clusterTaskMapObject, ok := h.ClusterTaskMap.ClusterTaskMap[currentClusterName]
		if !ok {
			// Does not exist, create a new list
			clusterTaskMapObject = types.NewTaskMap()
			h.ClusterTaskMap.ClusterTaskMap[currentClusterName] = clusterTaskMapObject
		}
		h.ClusterTaskMap.Unlock()

		taskId := currTask.TaskID
		clusterTaskMapObject.Lock()
		storedTask, ok := clusterTaskMapObject.TaskMap[taskId]
		if !ok {
			clusterTaskMapObject.TaskMap[taskId] = currTask
		} else {
			// TODO: see if there are any fields that needs to be added. Or updated i.e. taskAttempt
			if storedTask.AttemptNumber < currTask.AttemptNumber {
				storedTask.AttemptNumber = currTask.AttemptNumber
			}
			clusterTaskMapObject.TaskMap[taskId] = storedTask
		}
		clusterTaskMapObject.Unlock()
	case types.TASK_LIFECYCLE_EVENT:
		// TODO: Update task state based on lifecycle event
		logrus.Debugf("TASK_LIFECYCLE_EVENT received, not yet implemented")

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
		_, ok = clusterActorMapObject.ActorMap[actorId]
		if !ok {
			clusterActorMapObject.ActorMap[actorId] = currActor
		}
		clusterActorMapObject.Unlock()

	case types.ACTOR_LIFECYCLE_EVENT:
		// TODO: Update actor state based on lifecycle event
		logrus.Debugf("ACTOR_LIFECYCLE_EVENT received, not yet implemented")

	case types.ACTOR_TASK_DEFINITION_EVENT:
		// TODO: Handle actor task definition event
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

// GetTasks returns a thread-safe copy of all tasks for a given cluster
func (h *EventHandler) GetTasks(clusterName string) []types.Task {
	h.ClusterTaskMap.RLock()
	defer h.ClusterTaskMap.RUnlock()

	taskMap, ok := h.ClusterTaskMap.ClusterTaskMap[clusterName]
	if !ok {
		return []types.Task{}
	}

	taskMap.Lock()
	defer taskMap.Unlock()

	tasks := make([]types.Task, 0, len(taskMap.TaskMap))
	for _, task := range taskMap.TaskMap {
		tasks = append(tasks, task)
	}
	return tasks
}

// GetTaskByID returns a specific task by ID for a given cluster
func (h *EventHandler) GetTaskByID(clusterName, taskID string) (types.Task, bool) {
	h.ClusterTaskMap.RLock()
	defer h.ClusterTaskMap.RUnlock()

	taskMap, ok := h.ClusterTaskMap.ClusterTaskMap[clusterName]
	if !ok {
		return types.Task{}, false
	}

	taskMap.Lock()
	defer taskMap.Unlock()

	task, ok := taskMap.TaskMap[taskID]
	return task, ok
}

// GetTasksByJobID returns all tasks for a given job ID in a cluster
func (h *EventHandler) GetTasksByJobID(clusterName, jobID string) []types.Task {
	h.ClusterTaskMap.RLock()
	defer h.ClusterTaskMap.RUnlock()

	taskMap, ok := h.ClusterTaskMap.ClusterTaskMap[clusterName]
	if !ok {
		return []types.Task{}
	}

	taskMap.Lock()
	defer taskMap.Unlock()

	tasks := make([]types.Task, 0)
	for _, task := range taskMap.TaskMap {
		if task.JobID == jobID {
			tasks = append(tasks, task)
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
