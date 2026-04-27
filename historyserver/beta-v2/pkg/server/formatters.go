package server

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// taskPrefix is the prefix Ray uses for overall task events ("task::<func>").
const taskPrefix = "task::"

// formatJobForResponse converts eventtypes.Job to the JSON shape expected by Ray Dashboard.
func formatJobForResponse(job eventtypes.Job) map[string]interface{} {
	// If SubmissionID is empty, try to get it from metadata
	submissionID := job.SubmissionID
	if submissionID == "" && job.Config.Metadata != nil {
		if metaSubmissionID, ok := job.Config.Metadata["job_submission_id"]; ok {
			submissionID = metaSubmissionID
		}
	}

	// Determine job status: prefer the latest StatusTransition, fall back to
	// job.Status, then infer from State.
	// Ref: https://github.com/ray-project/ray/blob/beae3b3f94/python/ray/dashboard/client/src/pages/job/hook/useJobList.ts#L12
	status := string(job.Status)
	if len(job.StatusTransitions) > 0 {
		if latest := string(job.StatusTransitions[len(job.StatusTransitions)-1].Status); latest != "" {
			status = latest
		}
	}
	if status == "" {
		// Only infer RUNNING from CREATED state. JOBFINISHED does not imply SUCCEEDED
		// (the driver may have crashed or been stopped), so we leave it empty rather
		// than showing a misleading status.
		if job.State == eventtypes.CREATED {
			status = string(eventtypes.JOBRUNNING)
		}
	}

	// Infer job type if not set. Ray Dashboard expects "SUBMISSION" or "DRIVER".
	jobType := job.JobType
	if jobType == "" {
		if submissionID != "" {
			jobType = "SUBMISSION"
		} else {
			jobType = "DRIVER"
		}
	}

	result := map[string]interface{}{
		"driver_exit_code":          job.DriverExitCode,
		"driver_node_id":            job.DriverNodeID,
		"driver_agent_http_address": job.DriverAgentHttpAddress,
		"runtime_env":               job.RuntimeEnv,
		"metadata":                  job.Config.Metadata,
		"error_type":                job.ErrorType,
		"message":                   job.Message,
		"entrypoint":                job.EntryPoint,
		"status":                    status,
		"driver_info": map[string]interface{}{
			"id":              job.JobID,
			"node_ip_address": job.DriverNodeIPAddress,
			"pid":             job.DriverPID,
		},
		"job_id":        job.JobID,
		"submission_id": submissionID,
		"type":          jobType,
	}

	if !job.StartTime.IsZero() {
		result["start_time"] = job.StartTime.UnixMilli()
	}

	if !job.EndTime.IsZero() {
		result["end_time"] = job.EndTime.UnixMilli()
	}
	return result
}

// formatActorForResponse converts an eventtypes.Actor to the Ray Dashboard
// API format (camelCase keys, hex-encoded IDs).
// Ref: https://github.com/ray-project/ray/blob/a8fdb50e72/python/ray/dashboard/client/src/type/actor.ts#L18-L69
func formatActorForResponse(actor eventtypes.Actor) map[string]interface{} {
	// Convert Base64 IDs to Hex format for Dashboard API compatibility
	actorIDHex, _ := utils.ConvertBase64ToHex(actor.ActorID)
	jobIDHex, _ := utils.ConvertBase64ToHex(actor.JobID)
	nodeIDHex, _ := utils.ConvertBase64ToHex(actor.Address.NodeID)
	workerIDHex, _ := utils.ConvertBase64ToHex(actor.Address.WorkerID)
	placementGroupIDHex := actor.PlacementGroupID
	if placementGroupIDHex != "" {
		placementGroupIDHex, _ = utils.ConvertBase64ToHex(placementGroupIDHex)
	}

	result := map[string]interface{}{
		"actorId":          actorIDHex,
		"jobId":            jobIDHex,
		"placementGroupId": placementGroupIDHex,
		"state":            string(actor.State),
		"pid":              actor.PID,
		"address": map[string]interface{}{
			"nodeId":    nodeIDHex,
			"ipAddress": actor.Address.IPAddress,
			"port":      actor.Address.Port,
			"workerId":  workerIDHex,
		},
		"name":              actor.Name,
		"numRestarts":       actor.NumRestarts,
		"actorClass":        actor.ActorClass,
		"requiredResources": actor.RequiredResources,
		// Note: The key is "exitDetail" (singular), not "exitDetails" (plural). This matches
		// the Ray Dashboard frontend TypeScript type and the live Ray Dashboard API.
		// Ref: https://github.com/ray-project/ray/blob/a8fdb50e72/python/ray/dashboard/client/src/type/actor.ts#L33
		"exitDetail":    actor.ExitDetails,
		"reprName":      actor.ReprName,
		"callSite":      actor.CallSite,
		"isDetached":    actor.IsDetached,
		"rayNamespace": actor.RayNamespace,
		"labelSelector": actor.LabelSelector,
	}

	if !actor.StartTime.IsZero() {
		result["startTime"] = actor.StartTime.UnixMilli()
	}

	if !actor.EndTime.IsZero() {
		result["endTime"] = actor.EndTime.UnixMilli()
	}

	return result
}

// summarizeTasksByFuncName groups tasks by function name and counts by state.
// The Dashboard always calls this with a job_id filter, so internal Ray
// tasks belonging to a different job (e.g. _StatsActor) never reach here.
// Ref: https://github.com/ray-project/ray/blob/777f37f002c14bd4c587f4d095b85c62690647de/python/ray/util/state/common.py#L1035-L1064
func summarizeTasksByFuncName(tasks []eventtypes.Task) *utils.TaskSummariesByFuncName {
	summary := make(map[string]*utils.TaskSummaryPerFuncOrClassName)
	totalTasks := 0
	totalActorTasks := 0
	totalActorScheduled := 0

	for _, task := range tasks {
		funcName := task.GetFuncName()
		if funcName == "" {
			// Skip tasks without a function name. The definition and lifecycle events
			// arrive separately, so a task may have lifecycle but no definition (and
			// thus no FunctionDescriptor or TaskType yet). Using a fallback like
			// "unknown" would pollute the summary with a synthetic key the Dashboard
			// does not expect.
			// Ref: https://github.com/ray-project/ray/blob/777f37f002c14bd4c587f4d095b85c62690647de/python/ray/util/state/common.py#L1049
			continue
		}

		if _, ok := summary[funcName]; !ok {
			summary[funcName] = &utils.TaskSummaryPerFuncOrClassName{
				FuncOrClassName: funcName,
				Type:            string(task.TaskType),
				StateCounts:     make(map[string]int),
			}
		}

		// "NIL" matches Ray's default for tasks with no lifecycle events
		// (protobuf TaskStatus enum value 0).
		// Ref: https://github.com/ray-project/ray/blob/777f37f002c14bd4c587f4d095b85c62690647de/python/ray/util/state/common.py#L1688-L1691
		state := string(task.State)
		if state == "" {
			state = "NIL"
		}
		summary[funcName].StateCounts[state]++

		// Count by task type
		switch task.TaskType {
		case eventtypes.NORMAL_TASK:
			totalTasks++
		case eventtypes.ACTOR_CREATION_TASK:
			totalActorScheduled++
		case eventtypes.ACTOR_TASK:
			totalActorTasks++
		}
	}

	return &utils.TaskSummariesByFuncName{
		Summary:             summary,
		TotalTasks:          totalTasks,
		TotalActorTasks:     totalActorTasks,
		TotalActorScheduled: totalActorScheduled,
		SummaryBy:           "func_name",
	}
}

// formatTaskForResponse formats a single task attempt for the Ray Dashboard
// task API response.
// Ref: https://github.com/ray-project/ray/blob/d0b1d151d8ea964a711e451d0ae736f8bf95b629/python/ray/util/state/common.py#L730-L819
func formatTaskForResponse(task eventtypes.Task, detail bool) map[string]interface{} {
	// TODO(jwj): Maybe define result schema in types.go.
	result := map[string]interface{}{
		"task_id":            task.TaskID,
		"attempt_number":     task.TaskAttempt,
		"name":               task.GetTaskName(),
		"state":              string(task.State),
		"job_id":             task.JobID,
		"actor_id":           task.ActorID,
		"type":               string(task.TaskType),
		"func_or_class_name": task.GetFuncName(),
		"parent_task_id":     task.ParentTaskID,
		"node_id":            task.NodeID,
		"worker_id":          task.WorkerID,
		"worker_pid":         task.WorkerPID,
	}
	if task.RayErrorInfo != nil {
		result["error_type"] = string(task.RayErrorInfo.ErrorType)
	} else {
		result["error_type"] = nil
	}

	if detail {
		result["language"] = string(task.Language)
		result["required_resources"] = task.RequiredResources
		// Frontend expects runtime_env_info as a JSON string, not an object.
		// Serialize to match the Ray State API format.
		// Ref: https://github.com/ray-project/ray/blob/2f93603ad1/python/ray/dashboard/client/src/type/task.ts#L39
		runtimeEnvInfoObj := map[string]interface{}{
			"serialized_runtime_env": task.SerializedRuntimeEnv,
			// RuntimeEnvUris and RuntimeEnvConfig are never populated on the Ray side.
			// Ref: https://github.com/ray-project/ray/blob/50c715e79c5ca93118e1280f3842a1946b2cddac/src/ray/core_worker/task_event_buffer.cc#L189-L237
			"runtime_env_config": map[string]interface{}{
				"setup_timeout_seconds": 600,
				"eager_install":         true,
				"log_files":             []string{},
			},
		}
		runtimeEnvInfoBytes, _ := json.Marshal(runtimeEnvInfoObj)
		result["runtime_env_info"] = string(runtimeEnvInfoBytes)
		isNil, err := utils.IsHexNil(task.PlacementGroupID)
		if isNil || task.PlacementGroupID == "" || err != nil {
			result["placement_group_id"] = nil
		} else {
			result["placement_group_id"] = task.PlacementGroupID
		}

		events := make([]map[string]interface{}, 0, len(task.StateTransitions))
		for _, event := range task.StateTransitions {
			events = append(events, map[string]interface{}{
				"state":      string(event.State),
				"created_ms": event.Timestamp.UnixMilli(),
			})
		}
		result["events"] = events
		// TODO(jwj): Support profiling_data after TASK_PROFILE_EVENT is supported.
		// Ref: https://github.com/ray-project/ray/blob/d0b1d151d8ea964a711e451d0ae736f8bf95b629/python/ray/util/state/common.py#L1616-L1622
		// result["profiling_data"] = task.ProfilingData
		result["task_log_info"] = task.TaskLogInfo
		if task.RayErrorInfo != nil {
			result["error_message"] = task.RayErrorInfo.ErrorMessage
		} else {
			result["error_message"] = nil
		}
		if task.IsDebuggerPaused != nil {
			result["is_debugger_paused"] = *task.IsDebuggerPaused
		} else {
			result["is_debugger_paused"] = nil
		}
		if task.CallSite != nil {
			result["call_site"] = *task.CallSite
		} else {
			result["call_site"] = nil
		}
		if task.LabelSelector != nil {
			result["label_selector"] = task.LabelSelector
		} else {
			result["label_selector"] = map[string]string{}
		}

		if !task.CreationTime.IsZero() {
			result["creation_time_ms"] = task.CreationTime.UnixMilli()
		} else {
			result["creation_time_ms"] = nil
		}
		if !task.StartTime.IsZero() {
			result["start_time_ms"] = task.StartTime.UnixMilli()
		} else {
			result["start_time_ms"] = nil
		}
		if !task.EndTime.IsZero() {
			result["end_time_ms"] = task.EndTime.UnixMilli()
		} else {
			result["end_time_ms"] = nil
		}
	}

	return result
}

// formatNodeSummaryReplayForResp formats one node's summary replay for the
// response. Fields must match the Ray Dashboard NodeDetail type to avoid
// TypeError crashes on the frontend.
// Ref: https://github.com/ray-project/ray/blob/8a7b47bc5c/python/ray/dashboard/client/src/pages/node/NodeDetail.tsx#L65-L233
func formatNodeSummaryReplayForResp(node eventtypes.Node, sessionName string) []map[string]interface{} {
	nodeId := node.NodeID
	nodeIpAddress := node.NodeIPAddress
	labels := node.Labels
	var nodeTypeName string
	if nodeGroup, exists := labels["ray.io/node-group"]; exists {
		nodeTypeName = nodeGroup
	}
	isHeadNode := nodeTypeName == "headgroup"
	rayletSocketName := fmt.Sprintf("/tmp/ray/%s/sockets/raylet", sessionName)
	objectStoreSocketName := fmt.Sprintf("/tmp/ray/%s/sockets/plasma_store", sessionName)

	// Start timestamp from Ray NodeInfo protobuf.
	// Ref: https://github.com/ray-project/ray/blob/f953f199b5d68d47c07c865c5ebcd2333d49f365/src/ray/protobuf/gcs.proto#L345-L346
	var startTimestamp int64
	if !node.StartTimestamp.IsZero() {
		startTimestamp = node.StartTimestamp.UnixMilli()
	}

	// Wait for Ray to export the following fields.
	// Ref: https://github.com/ray-project/ray/issues/60129
	hostname := node.Hostname
	nodeName := node.NodeName
	instanceID := node.InstanceID
	instanceTypeName := node.InstanceTypeName

	nodeSummaryReplay := make([]map[string]interface{}, 0)
	for _, tr := range node.StateTransitions {
		transitionTimestamp := tr.Timestamp.UnixMilli()
		resourcesTotal := convertResourcesToAPISchema(tr.Resources)

		// Handle DEAD state-specific fields.
		var endTimestamp int64
		var stateMessage string
		if tr.State == eventtypes.NODE_DEAD {
			endTimestamp = tr.Timestamp.UnixMilli()
			if tr.DeathInfo != nil {
				stateMessage = composeStateMessage(string(tr.DeathInfo.Reason), tr.DeathInfo.ReasonMessage)
			}
		}

		// Host-level metrics (cpus, mem, shm, bootTime, disk, gpus, tpus) are not
		// available from Ray Base Events; they require Prometheus/Grafana with
		// Ray metrics enabled. For historical replay we use placeholder values
		// matching the Dashboard's expected schema.
		nodeSummarySnapshot := map[string]interface{}{
			"t":            transitionTimestamp,
			"now":          transitionTimestamp,
			"hostname":     hostname,
			"ip":           nodeIpAddress,
			"cpu":          0,
			"cmdline":      []string{},
			"cpus":         []int{0, 0},
			"mem":          []int{0, 0, 0, 0},
			"shm":          0,
			"bootTime":     0,
			"loadAvg":      [][]float64{{0, 0, 0}, {0, 0, 0}},
			"networkSpeed": []float64{0, 0},
			// Frontend expects disk as {mountPoint: {total, used, free, percent}}, not an array.
			"disk": map[string]map[string]interface{}{
				"/":    {"total": 0, "used": 0, "free": 0, "percent": 0.0},
				"/tmp": {"total": 0, "used": 0, "free": 0, "percent": 0.0},
			},
			// Frontend expects gpus as GPUStats[] ({uuid, name, utilizationGpu, ...}), not int array.
			"gpus": []interface{}{},
			"tpus": []interface{}{},
			// Frontend expects workers as Worker[] and actors as {[actorId]: ActorDetail}.
			// actors is set as empty map here; getNode() fills it with actual data.
			"workers": []interface{}{},
			"actors":  map[string]interface{}{},
			"raylet": map[string]interface{}{
				"storeStats": map[string]interface{}{
					"objectStoreBytesAvail": resourcesTotal["objectStoreMemory"],
				},
				"nodeId":                nodeId,
				"nodeManagerAddress":    nodeIpAddress,
				"nodeManagerHostname":   hostname,
				"rayletSocketName":      rayletSocketName,
				"objectStoreSocketName": objectStoreSocketName,
				"metricsExportPort":     "8080",
				"resourcesTotal":        resourcesTotal,
				"nodeName":              nodeName,
				"instanceId":            instanceID,
				"nodeTypeName":          nodeTypeName,
				"instanceTypeName":      instanceTypeName,
				"startTimeMs":           startTimestamp,
				"isHeadNode":            isHeadNode,
				"labels":                labels,
				"state":                 string(tr.State),
				"endTimeMs":             endTimestamp,
				"stateMessage":          stateMessage,
			},
		}
		nodeSummaryReplay = append(nodeSummaryReplay, nodeSummarySnapshot)
	}

	return nodeSummaryReplay
}

// formatNodeResourceReplayForResp formats one node's resource replay for the response.
func formatNodeResourceReplayForResp(node eventtypes.Node) []map[string]interface{} {
	nodeResourceReplay := make([]map[string]interface{}, 0)
	for _, tr := range node.StateTransitions {
		transitionTimestamp := tr.Timestamp.UnixMilli()

		// Create a resource snapshot.
		var resourceString string
		if tr.State == eventtypes.NODE_ALIVE {
			resourceString = constructResourceString(tr.Resources)
		}
		nodeResourceSnapshot := map[string]interface{}{
			"t":              transitionTimestamp,
			"resourceString": resourceString,
		}
		nodeResourceReplay = append(nodeResourceReplay, nodeResourceSnapshot)
	}

	return nodeResourceReplay
}

// convertResourcesToAPISchema converts Ray's resource format to Dashboard API schema.
// Conversion rules:
//   - "object_store_memory" is converted to "objectStoreMemory"
//   - "node:__internal_head__" is converted to "node:InternalHead"
//   - Other fields remain unchanged (e.g., "memory", "CPU", "node:<node-ip>")
func convertResourcesToAPISchema(resources map[string]float64) map[string]float64 {
	if len(resources) == 0 {
		return map[string]float64{}
	}

	convertedResources := make(map[string]float64, len(resources))
	for k, v := range resources {
		convertedKey := k
		if k == "object_store_memory" {
			convertedKey = "objectStoreMemory"
		} else if k == "node:__internal_head__" {
			convertedKey = "node:InternalHead"
		}
		convertedResources[convertedKey] = v
	}

	return convertedResources
}

// composeStateMessage builds the state message for a node DEAD transition
// from its death reason and message.
// Ref: https://github.com/ray-project/ray/blob/f953f199b5d68d47c07c865c5ebcd2333d49f365/python/ray/dashboard/utils.py#L738-L765
func composeStateMessage(deathReason string, deathReasonMessage string) string {
	var stateMessage string
	if deathReason == string(eventtypes.EXPECTED_TERMINATION) {
		stateMessage = "Expected termination"
	} else if deathReason == string(eventtypes.UNEXPECTED_TERMINATION) {
		stateMessage = "Unexpected termination"
	} else if deathReason == string(eventtypes.AUTOSCALER_DRAIN_PREEMPTED) {
		stateMessage = "Terminated due to preemption"
	} else if deathReason == string(eventtypes.AUTOSCALER_DRAIN_IDLE) {
		stateMessage = "Terminated due to idle (no Ray activity)"
	} else {
		stateMessage = ""
	}

	if deathReasonMessage != "" {
		if stateMessage != "" {
			stateMessage = fmt.Sprintf("%s: %s", stateMessage, deathReasonMessage)
		} else {
			stateMessage = deathReasonMessage
		}
	}
	return stateMessage
}

// constructResourceString builds the human-readable resource string from a
// state transition. Placement group entries are skipped.
// Ref: https://github.com/ray-project/ray/blob/f953f199b5d68d47c07c865c5ebcd2333d49f365/python/ray/autoscaler/_private/util.py#L643-L665
func constructResourceString(resources map[string]float64) string {
	resourceKeys := make([]string, 0, len(resources))
	for k := range resources {
		resourceKeys = append(resourceKeys, k)
	}
	sort.Strings(resourceKeys)

	resourceString := ""
	for _, k := range resourceKeys {
		v := resources[k]

		if k == "memory" || k == "object_store_memory" {
			formattedUsed := "0B"
			formattedTotal := formatMemory(v)
			resourceString += fmt.Sprintf("%s/%s %s", formattedUsed, formattedTotal, k)
		} else if strings.HasPrefix(k, "node:") {
			// Skip per-node resources
			continue
		} else if strings.HasPrefix(k, "accelerator_type:") {
			// Skip accelerator_type
			// Ref: https://github.com/ray-project/ray/issues/33272
			continue
		} else {
			// Handle CPU, GPU, TPU, and other resources
			resourceString += fmt.Sprintf("%.1f/%.1f %s", 0.0, v, k)
		}

		resourceString += "\n"
	}
	resourceString = strings.TrimSuffix(resourceString, "\n")

	return resourceString
}

// formatMemory formats a memory value to a human-readable string.
func formatMemory(memBytes float64) string {
	type unit struct {
		suffix       string
		bytesPerUnit float64
	}
	units := []unit{
		{suffix: "TiB", bytesPerUnit: math.Pow(2, 40)},
		{suffix: "GiB", bytesPerUnit: math.Pow(2, 30)},
		{suffix: "MiB", bytesPerUnit: math.Pow(2, 20)},
		{suffix: "KiB", bytesPerUnit: math.Pow(2, 10)},
	}
	for _, unit := range units {
		if memBytes >= unit.bytesPerUnit {
			memInUnit := memBytes / unit.bytesPerUnit
			return fmt.Sprintf("%.2f%s", memInUnit, unit.suffix)
		}
	}
	return fmt.Sprintf("%dB", int(memBytes))
}

// getChromeTraceColor maps event names to Chrome trace colors based on
// Ray's _default_color_mapping in profiling.py.
func getChromeTraceColor(eventName string) string {
	// Handle task::xxx pattern (overall task event)
	if strings.HasPrefix(eventName, taskPrefix) {
		return "generic_work"
	}

	// Direct mapping; follows Ray's profiling implementation:
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

// extractActorIDFromTaskID extracts the ActorID from a TaskID following
// Ray's ID specification (src/ray/design_docs/id_specification.md):
//   - TaskID: 8B unique + 16B ActorID (24 bytes / 48 hex chars).
//   - ActorID: 12B unique + 4B JobID (16 bytes / 32 hex chars).
//
// The last 32 hex chars of a 48-char TaskID are the ActorID. Returns an
// empty string when the ActorID's "unique" portion is all Fs, which marks
// normal/driver tasks with no associated actor.
func extractActorIDFromTaskID(taskIDHex string) string {
	if len(taskIDHex) != 48 {
		return "" // can't process if encoded in base64
	}

	actorPortion := taskIDHex[16:40] // 24 chars for actor id (12 bytes)
	jobPortion := taskIDHex[40:48]   // 8 chars for job id (4 bytes)

	// Check if all Fs (no actor)
	if strings.ToLower(actorPortion) == "ffffffffffffffffffffffff" {
		return ""
	}

	return actorPortion + jobPortion
}
