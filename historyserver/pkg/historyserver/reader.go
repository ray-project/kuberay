package historyserver

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"sort"

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

// TODO: implement this
func (h *ServerHandler) getGrafanaHealth(req *restful.Request, resp *restful.Response) {
	resp.WriteErrorString(http.StatusNotImplemented, "Grafana health not yet supported")
}
