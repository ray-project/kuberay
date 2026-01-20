package historyserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/emicklei/go-restful/v3"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
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

// getGrafanaHealth checks if Grafana is running and returns Grafana configuration
func (s *ServerHandler) getGrafanaHealth(req *restful.Request, resp *restful.Response) {
	grafanaHost := utils.GetEnvWithDefault(utils.GrafanaHost, utils.DefaultGrafanaHost)

	if grafanaHost == utils.GrafanaDisabledValue {
		result := map[string]interface{}{
			"result": true,
			"msg":    "Grafana disabled",
			"data": map[string]interface{}{
				"grafanaHost": utils.GrafanaDisabledValue,
			},
		}
		resp.WriteAsJson(result)
		return
	}

	healthURL := grafanaHost
	if !strings.HasSuffix(healthURL, "/") {
		healthURL += "/"
	}
	healthURL += utils.GrafanaHealthcheckPath

	httpReq, err := http.NewRequestWithContext(req.Request.Context(), http.MethodGet, healthURL, nil)
	if err != nil {
		result := map[string]interface{}{
			"result": false,
			"msg":    "Grafana healthcheck failed",
			"data": map[string]interface{}{
				"exception": err.Error(),
			},
		}
		logrus.Infof("Error on creating grafana health request: %v", err)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, result)
		return
	}

	httpResp, err := s.httpClient.Do(httpReq)
	if err != nil {
		result := map[string]interface{}{
			"result": false,
			"msg":    "Grafana healthcheck failed",
			"data": map[string]interface{}{
				"exception": err.Error(),
			},
		}
		logrus.Infof("Error on fetching grafana endpoint. Is grafana running? %v", err)
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, result)
		return
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, errRead := io.ReadAll(httpResp.Body)
		if errRead != nil {
			logrus.Warnf("Error on reading grafana health response body: %v", errRead)
		}
		result := map[string]interface{}{
			"result": false,
			"msg":    "Grafana healthcheck failed",
			"data": map[string]interface{}{
				"status": httpResp.StatusCode,
				"body":   string(body),
			},
		}
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, result)
		return
	}

	var healthData map[string]interface{}
	if err := json.NewDecoder(httpResp.Body).Decode(&healthData); err != nil {
		result := map[string]interface{}{
			"result": false,
			"msg":    "Grafana healthcheck failed",
			"data": map[string]interface{}{
				"exception": "Failed to parse grafana health response: " + err.Error(),
			},
		}
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, result)
		return
	}

	if database, ok := healthData["database"].(string); !ok || database != "ok" {
		result := map[string]interface{}{
			"result": false,
			"msg":    "Grafana healthcheck failed. Database not ok.",
			"data": map[string]interface{}{
				"status": httpResp.StatusCode,
				"json":   healthData,
			},
		}
		resp.WriteHeaderAndEntity(http.StatusInternalServerError, result)
		return
	}

	grafanaIframeHost := utils.GetEnvWithDefault(utils.GrafanaIframeHost, grafanaHost)
	prometheusName := utils.GetEnvWithDefault(utils.PrometheusName, utils.DefaultPrometheusName)
	grafanaOrgID := utils.GetEnvWithDefault(utils.GrafanaOrgID, utils.DefaultGrafanaOrgID)
	grafanaClusterFilter := os.Getenv(utils.GrafanaClusterFilterEnv)

	result := map[string]interface{}{
		"result": true,
		"msg":    "Grafana running",
		"data": map[string]interface{}{
			"grafanaHost":          grafanaIframeHost,
			"grafanaOrgId":         grafanaOrgID,
			"sessionName":          req.Attribute(COOKIE_SESSION_NAME_KEY),
			"dashboardUids":        getDashboardUIDs(),
			"dashboardDatasource":  prometheusName,
			"grafanaClusterFilter": grafanaClusterFilter,
		},
	}
	resp.WriteAsJson(result)
}

func getDashboardUIDs() map[string]string {
	return map[string]string{
		"default":         "rayDefaultDashboard",
		"serve":           "rayServeDashboard",
		"serveDeployment": "rayServeDeploymentDashboard",
		"serveLlm":        "rayServeLlmDashboard",
		"data":            "rayDataDashboard",
		"train":           "rayTrainDashboard",
	}
}
