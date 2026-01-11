package historyserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"sort"
	"time"

	"github.com/emicklei/go-restful/v3"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	"github.com/sirupsen/logrus"
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

func (s *ServerHandler) ClusterInfo(rayClusterNameID string) []byte {
	templ := `{
    "result": true,
    "msg": "Got formatted cluster status.",
    "data": {
        "clusterStatus": "======== Autoscaler status: %s ========\nNode status\n---------------------------------------------------------------\nActive:\n (no active nodes)\nIdle:\n 0 headgroup\nPending:\n (no pending nodes)\nRecent failures:\n (no failures)\n\nResources\n---------------------------------------------------------------\nTotal Usage:\n 0B/0B memory\n 0B/0B object_store_memory\n\nFrom request_resources:\n (none)\nPending Demands:\n (no resource demands)"
    }
}`
	afterRender := fmt.Sprintf(templ, time.Now().Format("2006-01-02 15:04:05.000000"))
	return []byte(afterRender)
}

func isDir(path string) bool {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fileInfo.IsDir()
}

type grafanaHealthReturnMsg struct {
	Result bool        `json:"result"`
	Msg    string      `json:"msg"`
	Data   grafanaData `json:"data"`
}

type grafanaData struct {
	GrafanaHost         string            `json:"grafanaHost"`
	SessionName         string            `json:"sessionName"`
	DashboardDatasource string            `json:"dashboardDatasource"`
	DashboardUids       map[string]string `json:"dashboardUids"`
}

// TODO: implement this
func (h *ServerHandler) getGrafanaHealth(req *restful.Request, resp *restful.Response) {
	resp.WriteErrorString(http.StatusNotImplemented, "Grafana health not yet supported")
}
