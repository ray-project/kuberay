// Package server — Chrome-trace timeline generation from a SessionSnapshot.
//
// timeline.go ports v1 EventHandler.GetTasksTimeline (in
// pkg/eventserver/eventserver.go:1293) so v2 can emit Chrome Tracing-formatted
// timeline events purely from a snapshot, with no dependency on the live
// EventHandler. The generated JSON is consumed by the Ray Dashboard's
// "Timeline" tab via GET /api/v0/tasks/timeline.
//
// Design note: the algorithm is a verbatim port of v1's — walk filtered tasks,
// assign stable PID per Node IP and stable TID per (Node IP, componentType:ID),
// emit metadata "M" events for process_name/thread_name, then one "X" event
// per profile entry. Behavior parity matters because the frontend parses the
// response with a fixed schema; divergence would break the Timeline view.
package server

import (
	"encoding/json"
	"strings"

	"github.com/ray-project/kuberay/historyserver/beta/pkg/snapshot"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
)

// generateTimelineFromSnapshot builds the Chrome Tracing Format payload used
// by Ray Dashboard's timeline view. jobID, if non-empty, filters tasks so the
// response only contains events for one job; empty string yields all jobs.
//
// Returns an empty (but non-nil) slice when the snapshot holds no tasks with
// profile data so json.Marshal produces "[]" instead of "null", matching v1.
//
// The function is package-private on purpose — handlers.go is the only caller
// and tests live in the same package.
func generateTimelineFromSnapshot(snap *snapshot.SessionSnapshot, jobID string) []eventtypes.ChromeTraceEvent {
	events := []eventtypes.ChromeTraceEvent{}
	if snap == nil {
		return events
	}

	// Step 1: flatten snapshot.Tasks (taskID -> []attempt) into []Task and keep
	// only entries that (a) match jobID when provided, (b) carry profile data,
	// (c) have a supported componentType, and (d) have a known NodeIPAddress.
	// Mirrors v1's filter chain so the downstream PID/TID allocation produces
	// the same (nodeIP, componentID) universe.
	filteredTasks := make([]eventtypes.Task, 0)
	for _, attempts := range snap.Tasks {
		for _, task := range attempts {
			if jobID != "" && task.JobID != jobID {
				continue
			}
			if task.ProfileData == nil || len(task.ProfileData.Events) == 0 {
				continue
			}
			// Only "worker" and "driver" are valid components for the Ray
			// profiling payload — everything else is ignored (consistent with
			// ray-project/ray profiling.py).
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

	// Step 2: allocate a deterministic PID per node IP and a deterministic TID
	// per (nodeIP, componentType:componentID) pair. Tracked across the full
	// task set so every trace event can reference the same PID/TID as its
	// metadata entry.
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

	// Step 3: emit process_name + thread_name metadata events (Phase = "M").
	// The Chrome tracing viewer uses these to label rows.
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

	// Step 4: emit one complete event (Phase = "X") per ProfileEventRaw. The
	// loop mirrors v1 line-for-line; see eventserver.go:1373 for the reference
	// implementation.
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
			// Ray emits timestamps in nanoseconds; Chrome Tracing wants
			// microseconds. Divide as float so sub-microsecond fractions are
			// preserved (matches v1).
			startTimeUs := float64(profEvent.StartTime) / 1000.0
			durationUs := float64(profEvent.EndTime-profEvent.StartTime) / 1000.0

			// Best-effort extraData parse — if malformed, we fall through with
			// extraData == nil and lose the "name"/"task_id" overrides but
			// keep the structural event. v1 silently ignores the error too.
			var extraData map[string]interface{}
			if profEvent.ExtraData != "" {
				_ = json.Unmarshal([]byte(profEvent.ExtraData), &extraData)
			}

			// taskID + funcOrClassName fallback chain, mirroring v1:
			//   1) task.TaskID / task.FuncOrClassName
			//   2) task.GetFuncName() if FuncOrClassName is empty
			//   3) extraData["task_id"] override (typically hex-form)
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

			// Build args. actor_id is explicitly `nil` (not absent) to match
			// v1's JSON shape — Ray Dashboard frontend relies on the key
			// existing even when the task is not actor-owned.
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

			// Overall "task::<func>" events get a human-friendly display name
			// pulled from extraData (v1 behavior).
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
