package historyserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/emicklei/go-restful/v3"
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
	// Build log path with attempt_number if specified
	logPath := path.Join(sessionID, "logs", options.NodeID, options.Filename)

	reader := s.reader.GetContent(rayClusterNameID, logPath)

	if reader == nil {
		return nil, utils.NewHTTPError(fmt.Errorf("log file not found: %s", logPath), http.StatusNotFound)
	}

	maxLines := options.Lines
	if maxLines < 0 {
		// -1 means read all lines
		return io.ReadAll(reader)
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

	return []byte(strings.Join(lines, "\n")), nil
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
