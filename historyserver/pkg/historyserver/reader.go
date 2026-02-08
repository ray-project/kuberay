package historyserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"

	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const (
	// DEFAULT_LOG_LIMIT is the default number of lines to return when lines parameter is not specified or is 0.
	// This matches Ray Dashboard API default behavior.
	DEFAULT_LOG_LIMIT = 1000

	// DEFAULT_DOWNLOAD_FILENAME is the default filename when download_filename is not specified
	DEFAULT_DOWNLOAD_FILENAME = "file.txt"

	// MAX_LOG_LIMIT is the maximum number of lines that can be requested.
	// Requests exceeding this limit will be capped to this value.
	MAX_LOG_LIMIT = 10000
)

// ANSI escape code pattern for filtering colored output from logs
// Matches patterns like: \x1b[31m (red color), \x1b[0m (reset), etc.
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]+m`)

// filterAnsiEscapeCodes removes ANSI escape sequences from log content
func filterAnsiEscapeCodes(content []byte) []byte {
	return ansiEscapePattern.ReplaceAll(content, []byte(""))
}

func (s *ServerHandler) listClusters(limit int) []utils.ClusterInfo {
	// Initial continuation marker
	logrus.Debugf("Prepare to get list clusters info ...")
	ctx := context.Background()
	liveClusters, _ := s.clientManager.ListRayClusters(ctx)
	liveClusterNames := []string{}
	liveClusterInfos := []utils.ClusterInfo{}
	for _, liveCluster := range liveClusters {
		liveClusterInfo := utils.ClusterInfo{
			Name:            liveCluster.Name,
			Namespace:       liveCluster.Namespace,
			CreateTime:      liveCluster.CreationTimestamp.String(),
			CreateTimeStamp: liveCluster.CreationTimestamp.Unix(),
			SessionName:     "live",
		}
		liveClusterInfos = append(liveClusterInfos, liveClusterInfo)
		liveClusterNames = append(liveClusterNames, liveCluster.Name)
	}
	logrus.Infof("live clusters: %v", liveClusterNames)
	clusters := s.reader.List()
	sort.Sort(utils.ClusterInfoList(clusters))
	if limit > 0 && limit < len(clusters) {
		clusters = clusters[:limit]
	}
	clusters = append(liveClusterInfos, clusters...)
	return clusters
}

func (s *ServerHandler) _getNodeLogs(rayClusterNameID, sessionId, nodeId, dir string) ([]byte, error) {
	logPath := path.Join(sessionId, "logs", nodeId)
	if dir != "" {
		logPath = path.Join(logPath, dir)
	}
	files := s.reader.ListFiles(rayClusterNameID, logPath)

	// Categorize log files to match Ray Dashboard API format.
	// Ref: Ray Dashboard's LogsManager._categorize_log_files in log_manager.py
	categorized := categorizeLogFiles(files)

	ret := map[string]interface{}{
		"result": true,
		"msg":    "",
		"data": map[string]interface{}{
			"result": categorized,
		},
	}
	return json.Marshal(ret)
}

// categorizeLogFiles categorizes log files by component type.
// This mirrors Ray Dashboard's LogsManager._categorize_log_files logic.
// Ref: https://github.com/ray-project/ray/blob/master/python/ray/dashboard/modules/log/log_manager.py
//
// NOTE: The order of checks matters. "log_monitor" must be checked before "monitor"
// because "log_monitor" contains "monitor" as a substring.
func categorizeLogFiles(files []string) map[string][]string {
	result := make(map[string][]string)
	for _, f := range files {
		switch {
		case strings.Contains(f, "worker") && strings.HasSuffix(f, ".out"):
			result["worker_out"] = append(result["worker_out"], f)
		case strings.Contains(f, "worker") && strings.HasSuffix(f, ".err"):
			result["worker_err"] = append(result["worker_err"], f)
		case strings.Contains(f, "core-worker") && strings.HasSuffix(f, ".log"):
			result["core_worker"] = append(result["core_worker"], f)
		case strings.Contains(f, "core-driver") && strings.HasSuffix(f, ".log"):
			result["driver"] = append(result["driver"], f)
		case strings.Contains(f, "raylet."):
			result["raylet"] = append(result["raylet"], f)
		case strings.Contains(f, "gcs_server."):
			result["gcs_server"] = append(result["gcs_server"], f)
		case strings.Contains(f, "log_monitor"):
			result["internal"] = append(result["internal"], f)
		case strings.Contains(f, "monitor"):
			result["autoscaler"] = append(result["autoscaler"], f)
		case strings.Contains(f, "agent."):
			result["agent"] = append(result["agent"], f)
		case strings.Contains(f, "dashboard."):
			result["dashboard"] = append(result["dashboard"], f)
		default:
			result["internal"] = append(result["internal"], f)
		}
	}
	return result
}

func (s *ServerHandler) _getNodeLogFile(rayClusterNameID, sessionID string, options GetLogFileOptions) ([]byte, error) {
	// Resolve node_id and filename based on options
	nodeID, filename, err := s.resolveLogFilename(rayClusterNameID, sessionID, options)
	if err != nil {
		// Preserve HTTPError status code if already set, otherwise use BadRequest
		var httpErr *utils.HTTPError
		if errors.As(err, &httpErr) {
			return nil, err
		}
		return nil, utils.NewHTTPError(err, http.StatusBadRequest)
	}

	// Build log path
	logPath := path.Join(sessionID, "logs", nodeID, filename)

	reader := s.reader.GetContent(rayClusterNameID, logPath)

	if reader == nil {
		return nil, utils.NewHTTPError(fmt.Errorf("log file not found: %s", logPath), http.StatusNotFound)
	}

	maxLines := options.Lines
	if maxLines < 0 {
		// -1 means read all lines
		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		if options.FilterAnsiCode {
			content = filterAnsiEscapeCodes(content)
		}
		return content, nil
	}

	if maxLines == 0 {
		maxLines = DEFAULT_LOG_LIMIT
	}

	if maxLines > MAX_LOG_LIMIT {
		logrus.Warnf("Requested lines (%d) exceeds max limit (%d), capping to max", maxLines, MAX_LOG_LIMIT)
		maxLines = MAX_LOG_LIMIT
	}

	scanner := bufio.NewScanner(reader)
	buffer := make([]string, maxLines)
	index := 0
	totalLines := 0

	// TODO(nary): Optimize this to prevent scanning through the whole log file to get last n lines
	// Get the last N lines following Ray Dashboard API behavior with circular buffer
	// Example with maxLines=3, file has 5 lines:
	// 	Line 1: buffer[0], Line 2: buffer[1], Line 3: buffer[2]
	// 	Line 4: buffer[0] (overwrites Line 1), Line 5: buffer[1] (overwrites Line 2)
	// 	Final buffer: ["Line 4", "Line 5", "Line 3"]
	for scanner.Scan() {
		buffer[index%maxLines] = scanner.Text()
		index++
		totalLines++
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Reconstruct lines in correct order
	var lines []string
	if totalLines <= maxLines {
		// File has fewer lines than requested, return all
		lines = buffer[:totalLines]
	} else {
		// Construct response with correct log line order from the circular buffer
		// Example: buffer=["Line 4", "Line 5", "Line 3"], start=2
		//   buffer[2:] = ["Line 3"] (oldest)
		//   buffer[:2] = ["Line 4", "Line 5"] (newest)
		//   Result: ["Line 3", "Line 4", "Line 5"]
		start := index % maxLines
		lines = append(buffer[start:], buffer[:start]...)
	}

	result := []byte(strings.Join(lines, "\n"))
	if options.FilterAnsiCode {
		result = filterAnsiEscapeCodes(result)
	}

	return result, nil
}

// resolveLogFilename resolves the log file node_id and filename based on the provided options.
// This mirrors Ray Dashboard's resolve_filename logic.
// The sessionID parameter is required for task_id resolution to search worker log files.
func (s *ServerHandler) resolveLogFilename(clusterNameID, sessionID string, options GetLogFileOptions) (nodeID, filename string, err error) {
	// If filename is explicitly provided, use it and ignore suffix
	if options.Filename != "" {
		if options.NodeID == "" {
			return "", "", fmt.Errorf("node_id is required when filename is provided")
		}
		filename := options.Filename
		// Append attempt_number if specified for explicit filenames
		if options.AttemptNumber > 0 {
			filename = fmt.Sprintf("%s.%d", filename, options.AttemptNumber)
		}
		return options.NodeID, filename, nil
	}

	// If task_id is provided, resolve from task events
	if options.TaskID != "" {
		return s.resolveTaskLogFilename(clusterNameID, sessionID, options.TaskID, options.AttemptNumber, options.Suffix)
	}

	// If actor_id is provided, resolve from actor events
	if options.ActorID != "" {
		return s.resolveActorLogFilename(clusterNameID, sessionID, options.ActorID, options.Suffix)
	}

	// If pid is provided, resolve worker log file
	if options.PID > 0 {
		return s.resolvePidLogFilename(clusterNameID, sessionID, options.NodeID, options.PID, options.Suffix)
	}

	return "", "", fmt.Errorf("must provide one of: filename, task_id, actor_id, or pid")
}

// resolvePidLogFilename resolves a log file by PID.
// It requires a nodeID and searches for a log file with a name ending in "-{pid}.{suffix}".
func (s *ServerHandler) resolvePidLogFilename(clusterNameID, sessionID, nodeID string, pid int, suffix string) (string, string, error) {
	if nodeID == "" {
		return "", "", fmt.Errorf("node_id is required for pid resolution")
	}

	// Convert to hex if not already is
	nodeIDHex, err := utils.ConvertBase64ToHex(nodeID)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode node_id: %w", err)
	}

	logPath := path.Join(sessionID, "logs", nodeIDHex)
	files := s.reader.ListFiles(clusterNameID, logPath)

	pidSuffix := fmt.Sprintf("-%d.%s", pid, suffix)

	for _, file := range files {
		if strings.HasSuffix(file, pidSuffix) {
			return nodeIDHex, file, nil
		}
	}

	return "", "", utils.NewHTTPError(fmt.Errorf("log file not found for pid %d in path %s", pid, logPath), http.StatusNotFound)
}

// resolveTaskLogFilename resolves log file for a task by querying task events.
// This mirrors Ray Dashboard's _resolve_task_filename logic.
// The sessionID parameter is required for searching worker log files when task_log_info is not available.
func (s *ServerHandler) resolveTaskLogFilename(clusterNameID, sessionID, taskID string, attemptNumber int, suffix string) (nodeID, filename string, err error) {
	// Construct full cluster session key for event lookup
	// We append the sessionID to the clusterNameID (which is "name_namespace")
	// to match the key format used by utils.BuildClusterSessionKey.
	fullKey := fmt.Sprintf("%s_%s", clusterNameID, sessionID)

	// Get task attempts by task ID
	taskAttempts, found := s.eventHandler.GetTaskByID(fullKey, taskID)
	if !found {
		return "", "", fmt.Errorf("task not found: task_id=%s", taskID)
	}

	// Find the specific attempt
	var foundTask *eventtypes.Task
	for i, task := range taskAttempts {
		if task.AttemptNumber == attemptNumber {
			foundTask = &taskAttempts[i]
			break
		}
	}

	if foundTask == nil {
		return "", "", fmt.Errorf("task attempt not found: task_id=%s, attempt_number=%d", taskID, attemptNumber)
	}

	// Check if task has node_id
	if foundTask.NodeID == "" {
		return "", "", fmt.Errorf("task %s (attempt %d) has no node_id (task not scheduled yet)", taskID, attemptNumber)
	}

	// Check if this is an actor task
	if foundTask.ActorID != "" {
		return "", "", fmt.Errorf(
			"for actor task, please query actor log for actor(%s) by providing actor_id query parameter",
			foundTask.ActorID,
		)
	}

	// Check if task has worker_id
	if foundTask.WorkerID == "" {
		return "", "", fmt.Errorf(
			"task %s (attempt %d) has no worker_id",
			taskID, attemptNumber,
		)
	}

	// Try to use task_log_info if available
	// NOTE: task_log_info is currently not supported in ray export event, so we will always
	// fallback to following logic.
	if foundTask.TaskLogInfo != nil && len(foundTask.TaskLogInfo) > 0 {
		filenameKey := "stdout_file"
		if suffix == "err" {
			filenameKey = "stderr_file"
		}

		if logFilename, ok := foundTask.TaskLogInfo[filenameKey]; ok && logFilename != "" {
			return foundTask.NodeID, logFilename, nil
		}
	}

	// Fallback: Find worker log file by worker_id
	if sessionID == "" {
		return "", "", fmt.Errorf(
			"task %s (attempt %d) has no task_log_info and sessionID is required to search for worker log files",
			taskID, attemptNumber,
		)
	}
	nodeIDHex, logFilename, err := s.findWorkerLogFile(clusterNameID, sessionID, foundTask.NodeID, foundTask.WorkerID, suffix)
	if err != nil {
		return "", "", fmt.Errorf(
			"failed to find worker log file for task %s (attempt %d, worker_id=%s, node_id=%s): %w",
			taskID, attemptNumber, foundTask.WorkerID, foundTask.NodeID, err,
		)
	}

	return nodeIDHex, logFilename, nil
}

// resolveActorLogFilename resolves log file for an actor by querying actor events.
// This mirrors Ray Dashboard's _resolve_actor_filename logic.
func (s *ServerHandler) resolveActorLogFilename(clusterNameID, sessionID, actorID, suffix string) (nodeID, filename string, err error) {
	// Construct full cluster session key for event lookup
	// We append the sessionID to the clusterNameID (which is "name_namespace")
	// to match the key format used by utils.BuildClusterSessionKey.
	fullKey := fmt.Sprintf("%s_%s", clusterNameID, sessionID)

	// Get actor by actor ID
	actor, found := s.eventHandler.GetActorByID(fullKey, actorID)
	if !found {
		return "", "", fmt.Errorf("actor not found: actor_id=%s", actorID)
	}

	// Check if actor has node_id (means it was scheduled)
	if actor.Address.NodeID == "" {
		return "", "", fmt.Errorf(
			"actor %s has no node_id (actor not scheduled yet)",
			actorID,
		)
	}

	// Check if actor has worker_id
	if actor.Address.WorkerID == "" {
		return "", "", fmt.Errorf(
			"actor %s has no worker_id (actor not scheduled yet)",
			actorID,
		)
	}

	if sessionID == "" {
		return "", "", fmt.Errorf(
			"sessionID is required to search for worker log files for actor %s",
			actorID,
		)
	}

	// Find worker log file by worker_id
	nodeIDHex, logFilename, err := s.findWorkerLogFile(
		clusterNameID,
		sessionID,
		actor.Address.NodeID,
		actor.Address.WorkerID,
		suffix,
	)
	if err != nil {
		return "", "", fmt.Errorf(
			"failed to find worker log file for actor %s (worker_id=%s, node_id=%s): %w",
			actorID, actor.Address.WorkerID, actor.Address.NodeID, err,
		)
	}

	return nodeIDHex, logFilename, nil
}

// findWorkerLogFile searches for a worker log file by worker_id.
// Worker log files follow the pattern: worker-{worker_id_hex}-{job_id_hex}-{pid}.{suffix}
// Ref: https://github.com/ray-project/ray/blob/219ee7037bbdc02f66b58a814c9ad2618309c19e/src/ray/core_worker/core_worker_process.cc#L80-L80
// Returns (nodeIDHex, filename, error).
func (s *ServerHandler) findWorkerLogFile(clusterNameID, sessionID, nodeID, workerID, suffix string) (string, string, error) {
	// Convert to hex if not already is
	nodeIDHex, err := utils.ConvertBase64ToHex(nodeID)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode node_id: %w", err)
	}

	// Convert Base64 worker_id to hex
	workerIDHex, err := utils.ConvertBase64ToHex(workerID)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode worker_id: %w", err)
	}

	// List all files in the node's log directory
	logPath := path.Join(sessionID, "logs", nodeIDHex)
	files := s.reader.ListFiles(clusterNameID, logPath)

	// Search for files matching pattern: worker-{worker_id_hex}-*.{suffix}
	workerPrefix := fmt.Sprintf("worker-%s-", workerIDHex)
	workerSuffix := fmt.Sprintf(".%s", suffix)

	for _, file := range files {
		if strings.HasPrefix(file, workerPrefix) && strings.HasSuffix(file, workerSuffix) {
			return nodeIDHex, file, nil
		}
	}

	return "", "", fmt.Errorf("worker log file not found: worker_id=%s (hex=%s), suffix=%s, searched in %s", workerID, workerIDHex, suffix, logPath)
}

func (s *ServerHandler) GetNodes(rayClusterNameID, sessionId string) ([]byte, error) {
	logPath := path.Join(sessionId, "logs")
	nodes := s.reader.ListFiles(rayClusterNameID, logPath)
	templ := map[string]interface{}{
		"result": true,
		"msg":    "Node summary fetched.",
		"data": map[string]interface{}{
			"summary": []map[string]interface{}{},
		},
	}
	nodeSummary := []map[string]interface{}{}
	for _, node := range nodes {
		nodeSummary = append(nodeSummary, map[string]interface{}{
			"raylet": map[string]interface{}{
				"nodeId": path.Clean(node),
				"state":  "ALIVE",
			},
			"ip": "UNKNOWN",
		})
	}
	templ["data"].(map[string]interface{})["summary"] = nodeSummary
	return json.Marshal(templ)
}

// ipToNodeId resolves node_id from node_ip by querying node_events from storage.
// This mirrors Ray Dashboard's ip_to_node_id logic.
// Returns node_id in hex format if found, error otherwise.
func (s *ServerHandler) ipToNodeId(rayClusterNameID, sessionID, nodeIP string) (string, error) {
	if nodeIP == "" {
		return "", fmt.Errorf("node_ip is empty")
	}

	// List all node_events files
	nodeEventsPath := path.Join(sessionID, "node_events")
	files := s.reader.ListFiles(rayClusterNameID, nodeEventsPath)

	// Parse each node event file to find matching node_ip
	for _, file := range files {
		filePath := path.Join(nodeEventsPath, file)
		reader := s.reader.GetContent(rayClusterNameID, filePath)
		if reader == nil {
			continue
		}

		if closer, ok := reader.(io.Closer); ok {
			defer closer.Close()
		}

		data, err := io.ReadAll(reader)
		if err != nil {
			logrus.Warnf("Failed to read node event file %s: %v", filePath, err)
			continue
		}

		var events []map[string]interface{}
		if err := json.Unmarshal(data, &events); err != nil {
			logrus.Warnf("Failed to unmarshal node events from %s: %v", filePath, err)
			continue
		}

		// Search for NODE_DEFINITION_EVENT with matching node_ip
		for _, event := range events {
			eventType, ok := event["eventType"].(string)
			if !ok || eventType != string(eventtypes.NODE_DEFINITION_EVENT) {
				continue
			}

			nodeDefEvent, ok := event["nodeDefinitionEvent"].(map[string]interface{})
			if !ok {
				continue
			}

			ipAddr, ok := nodeDefEvent["nodeIpAddress"].(string)
			if !ok || ipAddr != nodeIP {
				continue
			}

			// Found matching node, extract node_id
			nodeIDBytes, ok := nodeDefEvent["nodeId"].(string)
			if !ok {
				continue
			}

			// Convert to hex if not already is
			nodeIDHex, err := utils.ConvertBase64ToHex(nodeIDBytes)
			if err != nil {
				logrus.Warnf("Failed to decode node_id %s: %v", nodeIDBytes, err)
				continue
			}
			logrus.Infof("Resolved node_ip %s to node_id %s", nodeIP, nodeIDHex)
			return nodeIDHex, nil
		}
	}

	return "", fmt.Errorf("node_id not found for node_ip=%s", nodeIP)
}
