package historyserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
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

func (s *ServerHandler) MetaKeyInfo(rayClusterNameID, key string) []byte {
	baseObject := path.Join(utils.GetMetaDirByNameID(s.rootDir, rayClusterNameID), key)
	logrus.Infof("Prepare to get object %s info ...", baseObject)
	body := s.reader.GetContent(rayClusterNameID, baseObject)
	if body == nil {
		logrus.Errorf("Failed to get content for object %s", baseObject)
		return nil
	}
	data, err := io.ReadAll(body)
	if err != nil {
		logrus.Errorf("Failed to read all data from object %s : %v", baseObject, err)
		return nil
	}

	return data
}

func (s *ServerHandler) LogKeyInfo(rayClusterNameID, nodeID, sessionId, key string, lines int64) []byte {
	baseObject := path.Join(utils.GetLogDirByNameID(s.rootDir, rayClusterNameID, nodeID, sessionId), key)
	logrus.Infof("Prepare to get object %s info ...", baseObject)
	body := s.reader.GetContent(rayClusterNameID, baseObject)
	if body == nil {
		logrus.Errorf("Failed to get content for object %s", baseObject)
		return nil
	}
	data, err := io.ReadAll(body)
	if err != nil {
		logrus.Errorf("Failed to read all data from object %s : %v", baseObject, err)
		return nil
	}
	return data
}

func (s *ServerHandler) staticFileHandler(req *restful.Request, resp *restful.Response) {
	logrus.Infof("static parameters %++v", req.PathParameters())
	logrus.Infof("static request %++v", *req.Request)
	//	logrus.Infof("static query %++v", req.)
	// Get the path parameter
	path := req.PathParameter("path")

	isHomePage := true
	_, err := req.Request.Cookie(COOKIE_CLUSTER_NAME_KEY)
	isHomePage = err != nil
	prefix := ""
	if isHomePage {
		prefix = "homepage"
	} else {
		version := "v2.51.0"
		if versionCookie, err := req.Request.Cookie(COOKIE_DASHBOARD_VERSION_KEY); err == nil {
			version = versionCookie.Value
		}
		prefix = version + "/client/build"
	}

	// Construct the full path to the static directory
	fullPath := filepath.Join(s.dashboardDir, prefix, "static", path)
	logrus.Infof("staticFileHandler fullpath %s", fullPath)

	// Check if the full path exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		resp.WriteErrorString(http.StatusNotFound, "File or directory not found")
		logrus.Errorf("File or directory %s not found", fullPath)
		return
	}

	// Serve the file or directory
	if isDir(fullPath) {
		// List files in the directory
		files, err := os.ReadDir(fullPath)
		if err != nil {
			resp.WriteErrorString(http.StatusInternalServerError, "Error reading directory")
			logrus.Errorf("Error reading directory %s %s", fullPath, err)
			return
		}
		resp.WriteAsJson(files)
	} else {
		// Serve the file
		http.ServeFile(resp.ResponseWriter, req.Request, fullPath)
		logrus.Infof("ServerFile %s", fullPath)
	}
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
