package historyserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"strconv"

	"github.com/emicklei/go-restful/v3"
	"github.com/ray-project/kuberay/historyserver/utils"
	"github.com/sirupsen/logrus"
)

const (
	COOKIE_CLUSTER_NAME_KEY = "cluster_name"
	COOKIE_SESSION_NAME_KEY = "session_name"
)

func routerClusters(s *ServerHandler) {
	ws := new(restful.WebService)
	defer restful.Add(ws)

	ws.Path("/clusters").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON) //.Filter(s.loginWrapper)
	ws.Route(ws.GET("/").To(s.getClusters).
		Doc("get all clusters").
		Writes([]string{}))
}

func routerNodes(s *ServerHandler) {
	ws := new(restful.WebService)
	defer restful.Add(ws)
	ws.Path("/nodes").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON) //.Filter(s.loginWrapper)
	ws.Route(ws.GET("/").To(s.getNodes).Filter(s.CookieHandle).
		Doc("get nodes for a given clusters").Param(ws.QueryParameter("view", "such as summary")).
		Writes(""))
	ws.Route(ws.GET("/{node_id}").To(s.getNode).Filter(s.CookieHandle).
		Doc("get specifical nodes  ").
		Param(ws.PathParameter("node_id", "node_id")).
		Writes(""))
}

func routerEvents(s *ServerHandler) {
	ws := new(restful.WebService)
	defer restful.Add(ws)
	ws.Path("/events").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON) //.Filter(s.loginWrapper)
	ws.Route(ws.GET("/").To(s.getEvents).Filter(s.CookieHandle).
		Doc("get events").
		Writes(""))
}

func routerAPI(s *ServerHandler) {
	ws := new(restful.WebService)
	defer restful.Add(ws)
	ws.Path("/api").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON) //.Filter(s.loginWrapper)
	ws.Route(ws.GET("/cluster_status").To(s.getClusterStatus).Filter(s.CookieHandle).
		Doc("get clusters status").Param(ws.QueryParameter("format", "such as 1")).
		Writes("")) // 这里你可以替换为具体的返回类型
	ws.Route(ws.GET("/grafana_health").To(s.getGrafanaHealth).Filter(s.CookieHandle).
		Doc("get grafana_health").
		Writes("")) // 这里你可以替换为具体的返回类型
	ws.Route(ws.GET("/prometheus_health").To(s.getPrometheusHealth).Filter(s.CookieHandle).
		Doc("get prometheus_health").
		Writes("")) // 这里你可以替换为具体的返回类型

	ws.Route(ws.GET("/jobs").To(s.getJobs).Filter(s.CookieHandle).
		Doc("get jobs").
		Writes("")) // 这里你可以替换为具体的返回类型

	ws.Route(ws.GET("/jobs/{job_id}").To(s.getJob).Filter(s.CookieHandle).
		Doc("get single job").
		Param(ws.PathParameter("job_id", "job_id")).
		Writes("")) // 这里你可以替换为具体的返回类型

	ws.Route(ws.GET("/data/datasets/{job_id}").To(s.getDatasets).Filter(s.CookieHandle).
		Doc("get datasets").
		Param(ws.PathParameter("job_id", "job_id")).
		Writes("")) // 这里你可以替换为具体的返回类型

	ws.Route(ws.GET("/serve/applications/").To(s.getServeApplications).Filter(s.CookieHandle).
		Doc("get appliations").
		Writes("")) // 这里你可以替换为具体的返回类型

	ws.Route(ws.GET("/v0/placement_groups/").To(s.getPlacementGroups).Filter(s.CookieHandle).
		Doc("get placement_groups").
		Writes("")) // 这里你可以替换为具体的返回类型

	ws.Route(ws.GET("/v0/logs").To(s.getNodeLogs).Filter(s.CookieHandle).
		Doc("get appliations").Param(ws.QueryParameter("node_id", "node_id")).
		Writes("")) // 这里你可以替换为具体的返回类型
	ws.Route(ws.GET("/v0/logs/file").To(s.getNodeLogFile).Filter(s.CookieHandle).
		Doc("get logfile").Param(ws.QueryParameter("node_id", "node_id")).
		Param(ws.QueryParameter("filename", "filename")).
		Param(ws.QueryParameter("lines", "lines")).
		Param(ws.QueryParameter("format", "format")).
		Writes("")) // 这里你可以替换为具体的返回类型

	ws.Route(ws.GET("/v0/tasks").To(s.getTaskDetail).Filter(s.CookieHandle).
		Doc("get task detail ").Param(ws.QueryParameter("limit", "limit")).
		Param(ws.QueryParameter("filter_keys", "filter_keys")).
		Param(ws.QueryParameter("filter_predicates", "filter_predicates")).
		Param(ws.QueryParameter("filter_values", "filter_values")).
		Writes("")) // 这里你可以替换为具体的返回类型

	ws.Route(ws.GET("/v0/tasks/summarize").To(s.getTaskSummarize).Filter(s.CookieHandle).
		Doc("get summarize").
		Param(ws.QueryParameter("filter_keys", "filter_keys")).
		Param(ws.QueryParameter("filter_predicates", "filter_predicates")).
		Param(ws.QueryParameter("filter_values", "filter_values")).
		Param(ws.QueryParameter("summary_by", "summary_by")).
		Writes("")) // 这里你可以替换为具体的返回类型
}

func routerRoot(s *ServerHandler) {
	ws := new(restful.WebService)
	defer restful.Add(ws)
	ws.Route(ws.GET("/").To(func(_ *restful.Request, w *restful.Response) {
		data, err := os.ReadFile(path.Join(s.dashboardDir, "index.html")) // 确保 index.html 文件存在
		if err != nil {
			http.Error(w, "could not read HTML file", http.StatusInternalServerError)
			logrus.Errorf("could not read HTML file")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
	}).Writes(""))
}

func routerHealthz(s *ServerHandler) {

	http.HandleFunc("/readz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
		logrus.Debugf("request /readz")
	})
	http.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
		logrus.Debugf("request /livez")
	})

}

func routerStatic(s *ServerHandler) {
	ws := new(restful.WebService)
	defer restful.Add(ws)
	ws.Path("/static").Consumes("*/*").Produces("*/*")
	ws.Route(ws.GET("/{path:*}").To(s.staticFileHandler).
		Doc("Get static file or directory").
		Param(ws.PathParameter("path", "path of the static file").DataType("string")))

}

func routerLogical(s *ServerHandler) {
	ws := new(restful.WebService)
	defer restful.Add(ws)
	ws.Path("/logical").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON) //.Filter(s.loginWrapper)
	ws.Route(ws.GET("/actors").To(s.getLogicalActors).Filter(s.CookieHandle).
		Doc("get logical actors").
		Writes("")) // 这里你可以替换为具体的返回类型
	ws.Route(ws.GET("/actors/{single_actor}").To(s.getLogicalActor).Filter(s.CookieHandle).
		Doc("get logical single actor").
		Param(ws.PathParameter("single_actor", "single_actor")).
		Writes("")) // 这里你可以替换为具体的返回类型

}

func (s *ServerHandler) RegisterRouter() {
	routerClusters(s)
	routerNodes(s)
	routerEvents(s)
	routerAPI(s)
	routerRoot(s)
	routerHealthz(s)
	routerStatic(s)
	routerLogical(s)
}

func (s *ServerHandler) getClusters(req *restful.Request, resp *restful.Response) {
	clusters := s.listClusters(s.maxClusters)
	resp.WriteAsJson(clusters)
}

// getNodes 返回指定集群的节点
func (s *ServerHandler) getNodes(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	view := req.QueryParameter("view")
	logrus.Warnf("view is %s, but not do anything", view)
	data := s.OssMetaKeyInfo(clusterNameID, utils.OssMetaFile_NodeSummaryKey)

	resp.Write(data)
}

func (s *ServerHandler) getEvents(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	data := s.OssMetaKeyInfo(clusterNameID, utils.OssMetaFile_Events)
	resp.Write(data)
}
func (s *ServerHandler) getPrometheusHealth(req *restful.Request, resp *restful.Response) {
	data := `{"result": true, "msg": "prometheus running", "data": {}}`
	resp.Write([]byte(data))
}
func (s *ServerHandler) getJobs(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	data := s.OssMetaKeyInfo(clusterNameID, utils.OssMetaFile_Jobs)
	resp.Write(data)
}
func (s *ServerHandler) getNode(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	node_id := req.PathParameter("node_id")
	data := s.OssMetaKeyInfo(clusterNameID, fmt.Sprintf("%s%s", utils.OssMetaFile_Node_Prefix, node_id))
	resp.Write(data)
}

func (s *ServerHandler) getJob(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	job_id := req.PathParameter("job_id")
	logrus.Debugf("job_id is %s", job_id)

	data := s.OssMetaKeyInfo(clusterNameID, utils.OssMetaFile_Jobs)
	allData := []map[string]interface{}{}
	if err := json.Unmarshal(data, &allData); err != nil {
		logrus.Errorf("Ummarshal alljobs error%v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	var job map[string]interface{}
	var find bool
	for _, singleData := range allData {
		id, ok := singleData["job_id"].(string)
		if ok && id == job_id {
			job = singleData
			find = true
			break
		}
	}
	if !find {
		logrus.Warnf("Can not find jobid %s from alljobs", job_id)
	} else {
		logrus.Infof("Find jobid %s from alljobs", job_id)
	}
	jobData, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	resp.Write(jobData)
}

func (s *ServerHandler) getDatasets(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	job_id := req.PathParameter("job_id")
	data := s.OssMetaKeyInfo(clusterNameID, fmt.Sprintf("%s%s", utils.OssMetaFile_JOBDATASETS_Prefix, job_id))
	resp.Write(data)
}

func (s *ServerHandler) getServeApplications(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	data := s.OssMetaKeyInfo(clusterNameID, utils.OssMetaFile_Applications)
	resp.Write(data)
}

func (s *ServerHandler) getPlacementGroups(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	data := s.OssMetaKeyInfo(clusterNameID, utils.OssMetaFile_PlacementGroups)
	resp.Write(data)
}

func (s *ServerHandler) getClusterStatus(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	data := s.OssMetaKeyInfo(clusterNameID, utils.OssMetaFile_ClusterStatus)

	resp.Write(data)
}

func (s *ServerHandler) getNodeLogs(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	nodeId := req.QueryParameter("node_id")
	data := s.OssMetaKeyInfo(clusterNameID, fmt.Sprintf("%s%s", utils.OssMetaFile_NodeLogs_Prefix, nodeId))
	// 根据 clustername 返回节点信息，以下是示例
	//resp.WriteEntity(data)
	resp.Write(data)
}

func (s *ServerHandler) getLogicalActors(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	data := s.OssMetaKeyInfo(clusterNameID, utils.OssMetaFile_LOGICAL_ACTORS)
	resp.Write(data)
}

func (s *ServerHandler) getLogicalActor(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	nodeId := req.PathParameter("single_actor")
	data := s.OssMetaKeyInfo(clusterNameID, utils.OssMetaFile_LOGICAL_ACTORS)
	var allActors = map[string]interface{}{}
	replyActorInfo := ReplyActorInfo{
		Result: true,
		Msg:    "All actors fetched.",
		Data:   ActorInfoData{},
	}
	if err := json.Unmarshal(data, &allActors); err != nil {
		logrus.Errorf("Ummarshal allTask error %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	allActorsData := allActors["data"].(map[string]interface{})
	actors := allActorsData["actors"].(map[string]interface{})
	for k, actor := range actors {
		a := actor.(map[string]interface{})
		if k == nodeId {
			replyActorInfo.Data.Detail = a
			break
		}
	}
	actData, err := json.MarshalIndent(&replyActorInfo, "", "  ")
	if err != nil {
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	resp.Write(actData)
}

func (s *ServerHandler) getNodeLogFile(req *restful.Request, resp *restful.Response) {
	resp.Header().Set("Content-Type", "text/plain")
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	nodeId := req.QueryParameter("node_id")
	filename := req.QueryParameter("filename")
	lines := req.QueryParameter("lines")
	logrus.Infof("get logfile lines %s", lines)
	format := req.QueryParameter("format")
	logrus.Infof("format is %s", format)
	limit, err := strconv.ParseInt(lines, 10, 64)
	if err != nil {
		logrus.Errorf("ParseInt error ")
		limit = 0
	}
	data := make([]byte, 0, 1000)
	if format == "leading_1" {
		rawData := s.OssLogKeyInfo(clusterNameID, nodeId, filename, limit)
		if len(rawData) > 0 {
			data = append(data, byte('1'))
			data = append(data, rawData...)
		}
	} else {
		data = append(data, s.OssLogKeyInfo(clusterNameID, nodeId, filename, limit)...)
	}

	resp.Write(data)
}

func getTaskInfo(allTaskData []byte, findTaskID string) ([]byte, error) {
	var allTasks = map[string]interface{}{}
	var findTaskInfo = &ReplyTaskInfo{
		Msg:    "",
		Result: false,
	}

	if err := json.Unmarshal(allTaskData, &allTasks); err != nil {
		logrus.Errorf("Ummarshal allTask error %v", err)
		return nil, err
	}
	data := allTasks["data"].(map[string]interface{})
	result := data["result"].(map[string]interface{})
	secondResults := result["result"].([]interface{})
	for _, single := range secondResults {
		r := single.(map[string]interface{})
		taskid := r["task_id"].(string)
		if taskid == findTaskID {
			findTaskInfo.Result = true
			findTaskInfo.Data.Result.Result = make([]interface{}, 0)
			findTaskInfo.Data.Result.Result = append(findTaskInfo.Data.Result.Result, r)
			findTaskInfo.Data.Result.NumFiltered = 1
			findTaskInfo.Data.Result.NumAfterTruncation = 1
			findTaskInfo.Data.Result.Total = 1
			break
		}
	}
	return json.MarshalIndent(findTaskInfo, "", "  ")
}

func (s *ServerHandler) getTaskSummarize(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	// limit := req.QueryParameter("limit")
	filter_keys := req.QueryParameter("filter_keys")
	summary_by := req.QueryParameter("summary_by")
	//filter_predicates := req.QueryParameter("filter_predicates")
	filter_values := req.QueryParameter("filter_values")

	switch filter_keys {
	case "job_id":
		var data []byte
		if summary_by == "" || summary_by == "func_name" {
			data = s.OssMetaKeyInfo(clusterNameID, fmt.Sprintf("%s%s", utils.OssMetaFile_JOBTASK_SUMMARIZE_BY_FUNC_NAME_Prefix, filter_values))
		} else if summary_by == "lineage" {
			data = s.OssMetaKeyInfo(clusterNameID, fmt.Sprintf("%s%s", utils.OssMetaFile_JOBTASK_SUMMARIZE_BY_LINEAGE_Prefix, filter_values))
		}
		//OssMetaFile_JOBTASK_SUMMARIZE_BY_LINEAGE_Prefix
		resp.Write(data)
	default:
		logrus.Errorf("Wrong filter keys %s", filter_keys)
		resp.WriteErrorString(http.StatusInternalServerError, "Wrong filter keys")
	}

}

func (s *ServerHandler) getTaskDetail(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	// limit := req.QueryParameter("limit")
	filter_keys := req.QueryParameter("filter_keys")
	//filter_predicates := req.QueryParameter("filter_predicates")
	filter_values := req.QueryParameter("filter_values")

	switch filter_keys {
	case "job_id":
		data := s.OssMetaKeyInfo(clusterNameID, fmt.Sprintf("%s%s", utils.OssMetaFile_JOBTASK_DETAIL_Prefix, filter_values))
		resp.Write(data)
	case "task_id":
		data := s.OssMetaKeyInfo(clusterNameID, utils.OssMetaFile_ALLTASKS_DETAIL)
		taskData, err := getTaskInfo(data, filter_values)
		if err != nil {
			logrus.Errorf("get task info error %v", err)
			resp.WriteErrorString(http.StatusInternalServerError, "Wrong task info")
			return
		}
		resp.Write(taskData)

	default:
		logrus.Errorf("Wrong filter keys %s", filter_keys)
		resp.WriteErrorString(http.StatusInternalServerError, "Wrong filter keys")
	}

}

// CookieHandle 是一个示例的预处理函数
func (s *ServerHandler) CookieHandle(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	// 从请求中获取 Cookie
	clusterName, err := req.Request.Cookie(COOKIE_CLUSTER_NAME_KEY)
	if err != nil {
		resp.WriteHeaderAndEntity(http.StatusBadRequest, "Cluster Cookie not found")
		return
	}
	sessionName, err := req.Request.Cookie(COOKIE_SESSION_NAME_KEY)
	if err != nil {
		resp.WriteHeaderAndEntity(http.StatusBadRequest, "RayCluster Session Name Cookie not found")
		return
	}
	req.SetAttribute(COOKIE_CLUSTER_NAME_KEY, clusterName.Value)
	req.SetAttribute(COOKIE_SESSION_NAME_KEY, sessionName.Value)
	logrus.Infof("Request URL %s", req.Request.URL.String())
	chain.ProcessFilter(req, resp)
}
