package historyserver

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/emicklei/go-restful/v3"
	"github.com/ray-project/kuberay/historyserver/utils"
	"github.com/sirupsen/logrus"
)

func (s *ServerHandler) listClusters(limit int) []utils.ClusterInfo {
	// 初始的继续标记
	logrus.Debugf("Prepare to get list clusters info ...")
	clusters := s.reader.List()
	sort.Sort(utils.ClusterInfoList(clusters))
	if limit > 0 {
		clusters = clusters[:limit]
	}
	return clusters
}

func (s *ServerHandler) OssMetaKeyInfo(rayClusterNameID, key string) []byte {
	baseObject := path.Join(utils.GetOssMetaDirByNameID(s.rootDir, rayClusterNameID), key)
	logrus.Infof("Prepare to get object %s info ...", baseObject)
	body := s.reader.GetContent(rayClusterNameID, baseObject)
	data, err := io.ReadAll(body)
	if err != nil {
		logrus.Errorf("Failed to read all data from object %s : %v", baseObject, err)
		return nil
	}
	return data
}

func (s *ServerHandler) OssLogKeyInfo(rayClusterNameID, nodeID, key string, lines int64) []byte {
	baseObject := path.Join(utils.GetOssLogDirByNameID(s.rootDir, rayClusterNameID, nodeID), key)
	logrus.Infof("Prepare to get object %s info ...", baseObject)
	body := s.reader.GetContent(rayClusterNameID, baseObject)
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

	// Construct the full path to the static directory
	fullPath := filepath.Join(s.dashboardDir, "static", path)
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

func (h *ServerHandler) getGrafanaHealth(req *restful.Request, resp *restful.Response) {
	data := grafanaData{
		GrafanaHost:         "https://g.console.aliyun.com",
		SessionName:         req.Attribute(COOKIE_SESSION_NAME_KEY).(string),
		DashboardDatasource: "Prometheus",
		DashboardUids: map[string]string{
			"default":                     "ray_cluster",
			"default_params":              "orgId=1",
			"serve":                       "ray_serve",
			"serve_params":                "orgId=1",
			"data":                        "ray_data",
			"data_params":                 "orgId=1",
			"ray_serve_deployment":        "ray_serve_deployment",
			"ray_serve_deployment_params": "orgId=1",
		},
	}
	ret := grafanaHealthReturnMsg{
		Result: true,
		Msg:    "Grafana is Running",
		Data:   data,
	}
	retStr, err := json.Marshal(ret)
	if err != nil {
		logrus.Errorf("Error: %v, Value: %v", err, ret)
		resp.WriteErrorString(400, err.Error())
		return
	}
	logrus.Info(string(retStr))
	resp.Write([]byte(retStr))
}
