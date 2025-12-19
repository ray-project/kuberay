package eventserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
				clusterList := h.reader.ListClusters()
				for _, clusterInfo := range clusterList {
					eventFileList := append(h.getAllJobEventFiles(clusterInfo), h.getAllNodeEventFiles(clusterInfo)...)

					logrus.Infof("current eventFileList for cluster %s is: %v", clusterInfo.Name, eventFileList)
					for i := range eventFileList {
						// TODO: Filter out ones that have already been read
						logrus.Infof("Reading event file: %s", eventFileList[i])

						eventioReader := h.reader.GetContent(clusterInfo.Name, eventFileList[i])
						eventbytes, err := io.ReadAll(eventioReader)
						if err != nil {
							logrus.Fatal(err)
							return
						}

						// Unmarshal the list of events
						var eventList []map[string]any
						if err := json.Unmarshal(eventbytes, &eventList); err != nil {
							logrus.Fatalf("Failed to unmarshal event: %v", err)
							return
						}

						// Evenly distribute event to each channel
						for i, curr := range eventList {
							// current index % number of event processors dictates which channel it goes to
							curr["clusterName"] = clusterInfo.Name
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
	eventType := types.EventType(eventMap["eventType"].(string))
	currentClusterName := eventMap["clusterName"].(string)
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

		clusterTaskMapObject, ok := h.ClusterTaskMap.ClusterTaskMap[currentClusterName]
		if !ok {
			// Does not exist, create a new list
			clusterTaskMapObject = types.NewTaskMap()
			h.ClusterTaskMap.ClusterTaskMap[currentClusterName] = clusterTaskMapObject
		}

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

	case types.ACTOR_DEFINITION_EVENT:

	case types.ACTOR_LIFECYCLE_EVENT:

	case types.ACTOR_TASK_DEFINITION_EVENT:
	default:
		logrus.Infof("Event not supported, skipping: %v", eventMap)
	}

	return nil
}

// getAllJobEventFiles get all the job event files for the given cluster.
// Assuming that the events file object follow the format root/clustername/sessionid/job_events/{job-*}/*
func (h *EventHandler) getAllJobEventFiles(clusterInfo utils.ClusterInfo) []string {
	var allJobFiles []string
	jobEventDirPrefix := clusterInfo.Name + "/" + clusterInfo.SessionName + "/job_events/"
	jobDirList := h.reader.ListFiles(clusterInfo.Name, jobEventDirPrefix)
	for _, currJobDir := range jobDirList {
		allJobFiles = append(allJobFiles, h.reader.ListFiles(clusterInfo.Name, currJobDir)...)
	}

	return allJobFiles
}

// getAllNodeEventFiles get all the node event files for the given cluster.
// Assuming that the events file object follow the format root/clustername/sessionid/node_events/*
func (h *EventHandler) getAllNodeEventFiles(clusterInfo utils.ClusterInfo) []string {
	nodeEventDirPrefix := clusterInfo.Name + "/" + clusterInfo.SessionName + "/node_events/"
	nodeEventFiles := h.reader.ListFiles(clusterInfo.Name, nodeEventDirPrefix)
	return nodeEventFiles
}
