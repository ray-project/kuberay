package historyserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/emicklei/go-restful/v3"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	"github.com/sirupsen/logrus"
)

const (
	// DEFAULT_LOG_LIMIT is the default number of lines to return when lines parameter is not specified or is 0.
	// This matches Ray Dashboard API default behavior.
	DEFAULT_LOG_LIMIT = 1000

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
	// TODO(nary): make logs/ response the same for live and dead cluster
	// Live cluster: {"result": true, "msg": "", "data": {"result": {"agent": ["file1"], ...}}}
	// Dead cluster: {"data": {"result": {"padding": ["file1", "file2", ...]}}}
	logPath := path.Join(sessionId, "logs", nodeId)
	if dir != "" {
		logPath = path.Join(logPath, dir)
	}
	files := s.reader.ListFiles(rayClusterNameID, logPath)
	ret := map[string]interface{}{
		"data": map[string]interface{}{
			"result": map[string]interface{}{
				"padding": files,
			},
		},
	}
	return json.Marshal(ret)
}

func (s *ServerHandler) _getNodeLogFile(rayClusterNameID, sessionID string, options GetLogFileOptions) ([]byte, error) {
	// Resolve node_id and filename based on options
	nodeID, filename, err := s.resolveLogFilename(rayClusterNameID, options)
	if err != nil {
		return nil, utils.NewHTTPError(err, http.StatusBadRequest)
	}

	// Build log path
	logPath := path.Join(sessionID, "logs", nodeID, filename)

	// Append attempt_number if specified and not using task_id
	// (task_id already includes attempt_number in resolution)
	if options.AttemptNumber > 0 && options.TaskID == "" {
		logPath = fmt.Sprintf("%s.%d", logPath, options.AttemptNumber)
	}

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
func (s *ServerHandler) resolveLogFilename(clusterNameID string, options GetLogFileOptions) (nodeID, filename string, err error) {
	// Validate suffix
	if options.Suffix != "out" && options.Suffix != "err" {
		return "", "", fmt.Errorf("invalid suffix: %s (must be 'out' or 'err')", options.Suffix)
	}

	// If filename is explicitly provided, use it and ignore suffix
	if options.Filename != "" {
		if options.NodeID == "" {
			return "", "", fmt.Errorf("node_id is required when filename is provided")
		}
		return options.NodeID, options.Filename, nil
	}

	// If task_id is provided, resolve from task events
	if options.TaskID != "" {
		return s.resolveTaskLogFilename(clusterNameID, options.TaskID, options.AttemptNumber, options.Suffix)
	}

	// If actor_id is provided, resolve from actor events
	// TODO: not implemented
	if options.ActorID != "" {
		return "", "", fmt.Errorf("actor_id resolution not yet implemented")
	}

	// If pid is provided, resolve worker log file
	// TODO: not implemented
	if options.PID > 0 {
		return "", "", fmt.Errorf("pid resolution not yet implemented")
	}

	return "", "", fmt.Errorf("must provide one of: filename, task_id, actor_id, or pid")
}

// resolveTaskLogFilename resolves log file for a task by querying task events.
// This mirrors Ray Dashboard's _resolve_task_filename logic.
func (s *ServerHandler) resolveTaskLogFilename(clusterNameID, taskID string, attemptNumber int, suffix string) (nodeID, filename string, err error) {
	// Get task attempts by task ID
	taskAttempts, found := s.eventHandler.GetTaskByID(clusterNameID, taskID)
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

	// Check if task has TaskLogInfo
	if foundTask.TaskLogInfo == nil {
		// Check if this is an actor task
		if foundTask.ActorID != "" {
			return "", "", fmt.Errorf(
				"for actor task, please query actor log for actor(%s) by providing actor_id query parameter",
				foundTask.ActorID,
			)
		}
		return "", "", fmt.Errorf(
			"task %s (attempt %d) has no task_log_info (worker_id=%s, node_id=%s)",
			taskID, attemptNumber, foundTask.WorkerID, foundTask.NodeID,
		)
	}

	// Get the log filename from task_log_info based on suffix
	filenameKey := "stdout_file"
	if suffix == "err" {
		filenameKey = "stderr_file"
	}

	logFilename, ok := foundTask.TaskLogInfo[filenameKey]
	if !ok || logFilename == "" {
		return "", "", fmt.Errorf(
			"missing log filename info (%s) in task_log_info for task %s (attempt %d)",
			filenameKey, taskID, attemptNumber,
		)
	}

	return foundTask.NodeID, logFilename, nil
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

// TODO: implement this
func (h *ServerHandler) getGrafanaHealth(req *restful.Request, resp *restful.Response) {
	resp.WriteErrorString(http.StatusNotImplemented, "Grafana health not yet supported")
}
