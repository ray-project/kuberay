package historyserver

import (
	"encoding/json"
	"strings"

	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/snapshot"
)

// taskPrefix is extracted to avoid hard-coded "task::" usage when matching
// Ray's task event names.
const taskPrefix = "task::"

// generateTimelineFromSnapshot builds the Chrome Tracing Format payload used
// by Ray Dashboard's timeline view. jobID, if non-empty, filters tasks to a
// single job; an empty string yields all jobs.
//
// Returns an empty (but non-nil) slice when no tasks carry profile data so
// json.Marshal produces "[]" instead of "null".
func generateTimelineFromSnapshot(snap *snapshot.SessionSnapshot, jobID string) []eventtypes.ChromeTraceEvent {
	events := []eventtypes.ChromeTraceEvent{}
	if snap == nil {
		return events
	}

	// Step 1: flatten attempts into []Task and keep only those with profile
	// data, a supported componentType, a node IP, and (when jobID is set) a
	// matching job.
	filteredTasks := make([]eventtypes.Task, 0)
	for _, attempts := range snap.Tasks {
		for _, task := range attempts {
			if jobID != "" && task.JobID != jobID {
				continue
			}
			if task.ProfileData == nil || len(task.ProfileData.Events) == 0 {
				continue
			}
			// Ray's profiling payload only uses "worker" and "driver" (ray-project/ray profiling.py).
			componentType := task.ProfileData.ComponentType
			if componentType != "worker" && componentType != "driver" {
				continue
			}
			if task.ProfileData.NodeIPAddress == "" {
				continue
			}
			filteredTasks = append(filteredTasks, task)
		}
	}

	if len(filteredTasks) == 0 {
		return events
	}

	// Step 2: assign a deterministic PID per node IP and TID per
	// (nodeIP, componentType:componentID), reused across all events.
	nodeIPToPID := make(map[string]int)
	nodeIPToClusterIDToTID := make(map[string]map[string]int)
	pidCounter := 0
	tidCounter := 0

	for _, task := range filteredTasks {
		nodeIP := task.ProfileData.NodeIPAddress
		clusterID := task.ProfileData.ComponentType + ":" + task.ProfileData.ComponentID
		if _, exists := nodeIPToPID[nodeIP]; !exists {
			nodeIPToPID[nodeIP] = pidCounter
			pidCounter++
			nodeIPToClusterIDToTID[nodeIP] = make(map[string]int)
		}
		if _, exists := nodeIPToClusterIDToTID[nodeIP][clusterID]; !exists {
			nodeIPToClusterIDToTID[nodeIP][clusterID] = tidCounter
			tidCounter++
		}
	}

	// Step 3: emit process_name/thread_name metadata events ("M") that the
	// Chrome tracing viewer uses to label rows.
	for nodeIP, pid := range nodeIPToPID {
		events = append(events, eventtypes.ChromeTraceEvent{
			Name:  "process_name",
			PID:   pid,
			TID:   nil,
			Phase: "M",
			Args: map[string]interface{}{
				"name": "Node " + nodeIP,
			},
		})

		for clusterID, tid := range nodeIPToClusterIDToTID[nodeIP] {
			tidVal := tid
			events = append(events, eventtypes.ChromeTraceEvent{
				Name:  "thread_name",
				PID:   pid,
				TID:   &tidVal,
				Phase: "M",
				Args: map[string]interface{}{
					"name": clusterID,
				},
			})
		}
	}

	// Step 4: emit one complete event ("X") per ProfileEventRaw.
	for _, task := range filteredTasks {
		nodeIP := task.ProfileData.NodeIPAddress
		clusterID := task.ProfileData.ComponentType + ":" + task.ProfileData.ComponentID

		pid, ok := nodeIPToPID[nodeIP]
		if !ok {
			continue
		}
		var tidPtr *int
		if tid, ok := nodeIPToClusterIDToTID[nodeIP][clusterID]; ok {
			tidVal := tid
			tidPtr = &tidVal
		} else {
			// Defensive: first pass should have populated this; skip rather
			// than emit a null-TID event that the viewer would drop.
			continue
		}

		for _, profEvent := range task.ProfileData.Events {
			// Ray emits ns; Chrome Tracing wants us. Float division preserves
			// sub-microsecond fractions.
			startTimeUs := float64(profEvent.StartTime) / 1000.0
			durationUs := float64(profEvent.EndTime-profEvent.StartTime) / 1000.0

			// Best-effort parse: a malformed extraData drops the name/task_id
			// overrides but the structural event is still emitted.
			var extraData map[string]interface{}
			if profEvent.ExtraData != "" {
				_ = json.Unmarshal([]byte(profEvent.ExtraData), &extraData)
			}

			// taskID/funcOrClassName fallback: TaskID + FuncOrClassName, then
			// GetFuncName(), then extraData["task_id"] override.
			taskIDForArgs := task.TaskID
			funcOrClassName := task.FuncOrClassName
			if funcOrClassName == "" {
				funcOrClassName = task.GetFuncName()
			}
			if extraData != nil {
				if tid, ok := extraData["task_id"].(string); ok && tid != "" {
					taskIDForArgs = tid
				}
			}

			// actor_id is explicitly nil (not absent) — Ray Dashboard relies on
			// the key being present.
			actorID := extractActorIDFromTaskID(taskIDForArgs)
			args := map[string]interface{}{
				"task_id":            taskIDForArgs,
				"job_id":             task.JobID,
				"attempt_number":     task.TaskAttempt,
				"func_or_class_name": funcOrClassName,
				"actor_id":           nil,
			}
			if actorID != "" {
				args["actor_id"] = actorID
			}

			// task::<func> events get a human-friendly display name from extraData.
			eventName := profEvent.EventName
			displayName := profEvent.EventName
			if strings.HasPrefix(profEvent.EventName, taskPrefix) && extraData != nil {
				if name, ok := extraData["name"].(string); ok && name != "" {
					displayName = name
					args["name"] = name
				}
			}

			events = append(events, eventtypes.ChromeTraceEvent{
				Category:  eventName,
				Name:      displayName,
				PID:       pid,
				TID:       tidPtr,
				Timestamp: &startTimeUs,
				Duration:  &durationUs,
				Color:     getChromeTraceColor(eventName),
				Args:      args,
				Phase:     "X",
			})
		}
	}

	return events
}

// getChromeTraceColor maps event names to Chrome trace colors.
// Based on Ray's _default_color_mapping in profiling.py.
//
// Duplicated from pkg/eventserver/eventserver.go (private there) so the
// snapshot-based timeline path stays in this package; the two copies must
// be kept in sync until eventserver's private copy is retired.
func getChromeTraceColor(eventName string) string {
	if strings.HasPrefix(eventName, taskPrefix) {
		return "generic_work"
	}
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

// extractActorIDFromTaskID extracts the ActorID from a TaskID following Ray's
// ID specification.
//
//   - TaskID: 8B unique + 16B ActorID (total 24 bytes = 48 hex chars)
//   - ActorID: 12B unique + 4B JobID (total 16 bytes = 32 hex chars)
//
// For a 48-character hex TaskID, the last 32 hex characters (bytes 16–48)
// correspond to the ActorID. The "unique" portion (first 24 hex chars) is
// all-Fs for normal/driver tasks with no associated actor; in that case an
// empty string is returned.
//
// Duplicated from pkg/eventserver/eventserver.go.
func extractActorIDFromTaskID(taskIDHex string) string {
	if len(taskIDHex) != 48 {
		return ""
	}

	actorPortion := taskIDHex[16:40]
	jobPortion := taskIDHex[40:48]

	if strings.ToLower(actorPortion) == "ffffffffffffffffffffffff" {
		return ""
	}

	return actorPortion + jobPortion
}
