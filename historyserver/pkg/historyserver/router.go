package historyserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/emicklei/go-restful/v3"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	COOKIE_CLUSTER_NAME_KEY      = "cluster_name"
	COOKIE_CLUSTER_NAMESPACE_KEY = "cluster_namespace"
	COOKIE_SESSION_NAME_KEY      = "session_name"
	COOKIE_DASHBOARD_VERSION_KEY = "dashboard_version"

	ATTRIBUTE_SERVICE_NAME = "cluster_service_name"
)

type ServiceInfo struct {
	ServiceName string
	Namespace   string
	Port        int
}

func RequestLogFilter(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	logrus.Infof("Received request: %s %s", req.Request.Method, req.Request.URL.String())
	chain.ProcessFilter(req, resp)
}

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
	ws.Path("/api").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON).Filter(RequestLogFilter) //.Filter(s.loginWrapper)
	ws.Route(ws.GET("/cluster_status").To(s.getClusterStatus).Filter(s.CookieHandle).
		Doc("get clusters status").Param(ws.QueryParameter("format", "such as 1")).
		Writes("")) // Placeholder for specific return type
	ws.Route(ws.GET("/grafana_health").To(s.getGrafanaHealth).Filter(s.CookieHandle).
		Doc("get grafana_health").
		Writes("")) // Placeholder for specific return type
	ws.Route(ws.GET("/prometheus_health").To(s.getPrometheusHealth).Filter(s.CookieHandle).
		Doc("get prometheus_health").
		Writes("")) // Placeholder for specific return type

	ws.Route(ws.GET("/jobs/").To(s.getJobs).Filter(s.CookieHandle).
		Doc("get driver jobs").
		Writes("")) // Placeholder for specific return type

	ws.Route(ws.GET("/jobs/{job_id}").To(s.getJob).Filter(s.CookieHandle).
		Doc("get single driver job").
		Param(ws.PathParameter("job_id", "job_id")).
		Writes("")) // Placeholder for specific return type

	ws.Route(ws.GET("/data/datasets/{job_id}").To(s.getDatasets).Filter(s.CookieHandle).
		Doc("get datasets").
		Param(ws.PathParameter("job_id", "job_id")).
		Writes("")) // Placeholder for specific return type

	ws.Route(ws.GET("/serve/applications/").To(s.getServeApplications).Filter(s.CookieHandle).
		Doc("get appliations").
		Writes("")) // Placeholder for specific return type

	ws.Route(ws.GET("/v0/placement_groups/").To(s.getPlacementGroups).Filter(s.CookieHandle).
		Doc("get placement_groups").
		Writes("")) // Placeholder for specific return type

	ws.Route(ws.GET("/v0/logs").To(s.getNodeLogs).Filter(s.CookieHandle).
		Doc("get appliations").Param(ws.QueryParameter("node_id", "node_id")).
		Writes("")) // Placeholder for specific return type
	ws.Route(ws.GET("/v0/logs/file").To(s.getNodeLogFile).Filter(s.CookieHandle).
		Doc("get logfile").Param(ws.QueryParameter("node_id", "node_id")).
		Param(ws.QueryParameter("filename", "filename")).
		Param(ws.QueryParameter("lines", "lines")).
		Produces("text/plain").
		Writes("")) // Placeholder for specific return type

	ws.Route(ws.GET("/v0/tasks").To(s.getTaskDetail).Filter(s.CookieHandle).
		Doc("get task detail ").
		// TODO: support limit
		// Param(ws.QueryParameter("limit", "limit")).
		Param(ws.QueryParameter("filter_keys", "filter_keys")).
		Param(ws.QueryParameter("filter_predicates", "filter_predicates")).
		Param(ws.QueryParameter("filter_values", "filter_values")).
		Writes("")) // Placeholder for specific return type

	ws.Route(ws.GET("/v0/tasks/summarize").To(s.getTaskSummarize).Filter(s.CookieHandle).
		Doc("get summarize").
		Param(ws.QueryParameter("filter_keys", "filter_keys")).
		Param(ws.QueryParameter("filter_predicates", "filter_predicates")).
		Param(ws.QueryParameter("filter_values", "filter_values")).
		Param(ws.QueryParameter("summary_by", "summary_by")).
		Writes("")) // Placeholder for specific return type
}

// func routerRoot(s *ServerHandler) {
// 	ws := new(restful.WebService)
// 	defer restful.Add(ws)
// 	ws.Filter(RequestLogFilter)
// 	ws.Route(ws.GET("/").To(func(req *restful.Request, w *restful.Response) {
// 		isHomePage := true
// 		_, err := req.Request.Cookie(COOKIE_CLUSTER_NAME_KEY)
// 		isHomePage = err != nil
// 		prefix := ""
// 		if isHomePage {
// 			prefix = "homepage"
// 		} else {
// 			version := "v2.51.0"
// 			if versionCookie, err := req.Request.Cookie(COOKIE_DASHBOARD_VERSION_KEY); err == nil {
// 				version = versionCookie.Value
// 			}
// 			prefix = version + "/client/build"
// 		}
// 		// Check if homepage file exists; if so use it, otherwise use default index.html
// 		homepagePath := path.Join(s.dashboardDir, prefix, "index.html")

// 		var data []byte

// 		if _, statErr := os.Stat(homepagePath); !os.IsNotExist(statErr) {
// 			data, err = os.ReadFile(homepagePath)
// 		} else {
// 			http.Error(w, "could not read HTML file", http.StatusInternalServerError)
// 			logrus.Errorf("could not read HTML file: %v", statErr)
// 			return
// 		}

// 		if err != nil {
// 			http.Error(w, "could not read HTML file", http.StatusInternalServerError)
// 			logrus.Errorf("could not read HTML file: %v", err)
// 			return
// 		}
// 		w.Header().Set("Content-Type", "text/html")
// 		w.Write(data)
// 	}).Writes(""))
// }

// TODO: this is the frontend's entry.
// func routerHomepage(s *ServerHandler) {
// 	ws := new(restful.WebService)
// 	defer restful.Add(ws)
// 	ws.Path("/homepage").Consumes("*/*").Produces("*/*").Filter(RequestLogFilter)
// 	ws.Route(ws.GET("/").To(func(_ *restful.Request, w *restful.Response) {
// 		data, err := os.ReadFile(path.Join(s.dashboardDir, "homepage/index.html"))
// 		if err != nil {
// 			// Fallback to root path
// 			routerRoot(s)
// 			return
// 		}
// 		w.Header().Set("Content-Type", "text/html")
// 		w.Write(data)
// 	}).Writes(""))
// }

func routerHealthz(s *ServerHandler) {

	http.HandleFunc("/readz", func(w http.ResponseWriter, r *http.Request) {
		logrus.Infof("Received request: %s %s", r.Method, r.URL.String())
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
		logrus.Debugf("request /readz")
	})
	http.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) {
		logrus.Infof("Received request: %s %s", r.Method, r.URL.String())
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
		logrus.Debugf("request /livez")
	})

}

func routerLogical(s *ServerHandler) {
	ws := new(restful.WebService)
	defer restful.Add(ws)
	ws.Path("/logical").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON).Filter(RequestLogFilter) //.Filter(s.loginWrapper)
	ws.Route(ws.GET("/actors").To(s.getLogicalActors).Filter(s.CookieHandle).
		Doc("get logical actors").
		Param(ws.QueryParameter("filter_keys", "filter_keys")).
		Param(ws.QueryParameter("filter_predicates", "filter_predicates")).
		Param(ws.QueryParameter("filter_values", "filter_values")).
		Writes("")) // Placeholder for specific return type

	// TODO: discuss with Ray Core team about this
	// I noticed that IDs (`actor_id`, `job_id`, `node_id`, etc.) in Ray Base Events
	// are encoded as Base64, while the Dashboard/State APIs use Hex.
	// Problem: Base64 can contain `/` characters, which breaks URL routing:
	ws.Route(ws.GET("/actors/{single_actor:*}").To(s.getLogicalActor).Filter(s.CookieHandle).
		Doc("get logical single actor").
		Param(ws.PathParameter("single_actor", "single_actor")).
		Writes("")) // Placeholder for specific return type

}

func routerRayClusterSet(s *ServerHandler) {
	ws := new(restful.WebService)
	defer restful.Add(ws)

	ws.Path("/enter_cluster").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON).Filter(RequestLogFilter)
	ws.Route(ws.GET("/{namespace}/{name}/{session}").To(func(r1 *restful.Request, r2 *restful.Response) {
		name := r1.PathParameter("name")
		namespace := r1.PathParameter("namespace")
		session := r1.PathParameter("session")
		http.SetCookie(r2, &http.Cookie{MaxAge: 600, Path: "/", Name: COOKIE_CLUSTER_NAME_KEY, Value: name})
		http.SetCookie(r2, &http.Cookie{MaxAge: 600, Path: "/", Name: COOKIE_CLUSTER_NAMESPACE_KEY, Value: namespace})
		http.SetCookie(r2, &http.Cookie{MaxAge: 600, Path: "/", Name: COOKIE_SESSION_NAME_KEY, Value: session})
		r2.WriteJson(map[string]interface{}{
			"result":    "success",
			"name":      name,
			"namespace": namespace,
			"session":   session,
		}, "application/json")
	}).
		Doc("set cookie for cluster").
		Param(ws.PathParameter("namespace", "namespace")).
		Param(ws.PathParameter("name", "name")).
		Param(ws.PathParameter("session", "session")).
		Writes("")) // Placeholder for specific return type
}

func (s *ServerHandler) RegisterRouter() {
	routerRayClusterSet(s)
	routerClusters(s)
	routerNodes(s)
	routerEvents(s)
	routerAPI(s)
	// routerRoot(s)
	// routerHomepage(s)
	routerHealthz(s)
	routerLogical(s)
}

func (s *ServerHandler) redirectRequest(req *restful.Request, resp *restful.Response) {
	svcInfo := req.Attribute(ATTRIBUTE_SERVICE_NAME).(ServiceInfo)

	var targetURL string
	if s.useKubernetesProxy {
		// Use Kubernetes API server proxy to access the in-cluster RayDashboard services.
		targetURL = fmt.Sprintf("%s/api/v1/namespaces/%s/services/%s:dashboard/proxy%s",
			s.clientManager.configs[0].Host,
			svcInfo.Namespace,
			svcInfo.ServiceName,
			req.Request.URL.String())
		logrus.Infof("Using Kubernetes API server proxy to access service %s/%s: %s",
			svcInfo.Namespace, svcInfo.ServiceName, req.Request.URL.String())
	} else {
		// Connect through in-cluster service discovery.
		targetURL = fmt.Sprintf("http://%s:%d%s", svcInfo.ServiceName, svcInfo.Port, req.Request.URL.String())
		logrus.Infof("Using in-cluster service discovery to access service %s/%s: %s",
			svcInfo.Namespace, svcInfo.ServiceName, req.Request.URL.String())
	}

	// Create a new request to the target URL.
	proxyReq, err := http.NewRequest(req.Request.Method, targetURL, req.Request.Body)
	if err != nil {
		logrus.Errorf("Failed to create proxy request: %v", err)
		resp.WriteError(http.StatusInternalServerError, err)
		return
	}

	// Copy headers from original request to proxy request.
	for key, values := range req.Request.Header {
		if strings.ToLower(key) != "host" {
			for _, value := range values {
				// Use Add() to preserve multiple values for the same header key.
				proxyReq.Header.Add(key, value)
			}
		}
	}

	// Send the proxy request to the target URL.
	remoteResp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		logrus.Errorf("Failed to proxy request to %s: %v", targetURL, err)
		resp.WriteError(http.StatusBadGateway, err)
		return
	}
	defer remoteResp.Body.Close()

	// Copy headers from remote response.
	for key, values := range remoteResp.Header {
		for _, value := range values {
			resp.Header().Add(key, value)
		}
	}

	// Set status code.
	resp.WriteHeader(remoteResp.StatusCode)

	// Copy response body.
	_, err = io.Copy(resp, remoteResp.Body)
	if err != nil {
		logrus.Errorf("Failed to copy response body: %v", err)
	}
}

func (s *ServerHandler) getClusters(req *restful.Request, resp *restful.Response) {
	clusters := s.listClusters(s.maxClusters)
	resp.WriteAsJson(clusters)
}

// getNodes returns nodes for the specified cluster
func (s *ServerHandler) getNodes(req *restful.Request, resp *restful.Response) {
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)
	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	clusterNamespace := req.Attribute(COOKIE_CLUSTER_NAMESPACE_KEY).(string)
	data, err := s.GetNodes(clusterNameID+"_"+clusterNamespace, sessionName)
	if data == nil {
		logrus.Errorf("Failed to get nodes for cluster %s", clusterNameID+"_"+clusterNamespace)
		resp.WriteError(http.StatusInternalServerError, errors.New("failed to get nodes"))
		return
	}
	if err != nil {
		logrus.Errorf("Error: %v", err)
		resp.WriteError(400, err)
		return
	}
	resp.Write(data)
}

func (s *ServerHandler) getEvents(req *restful.Request, resp *restful.Response) {
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)
	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}
	// Return "not yet supported" for historical data
	resp.WriteErrorString(http.StatusNotImplemented, "Historical events not yet supported")
}

func (s *ServerHandler) getPrometheusHealth(req *restful.Request, resp *restful.Response) {
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)
	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}
	// Return "not yet supported" for prometheus health
	resp.WriteErrorString(http.StatusNotImplemented, "Prometheus health not yet supported")
}

func (s *ServerHandler) getJobs(req *restful.Request, resp *restful.Response) {
	clusterName := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	clusterNamespace := req.Attribute(COOKIE_CLUSTER_NAMESPACE_KEY).(string)
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)

	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}

	clusterSessionKey := utils.BuildClusterSessionKey(clusterName, clusterNamespace, sessionName)
	jobsMap := s.eventHandler.GetJobsMap(clusterSessionKey)

	jobs := make([]eventtypes.Job, 0, len(jobsMap))
	for _, job := range jobsMap {
		jobs = append(jobs, job)
	}

	// Formate response to match Ray Dashboard API format
	formattedJobs := make([]interface{}, 0)
	for _, job := range jobs {
		formattedJobs = append(formattedJobs, formatJobForResponse(job))
	}

	response := formattedJobs

	respData, err := json.Marshal(response)
	if err != nil {
		logrus.Errorf("Failed to marshal jobs response: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	resp.Write(respData)
}

// formatJobForResponse will convert eventtypes.Job to the format expected by Ray Dashboard
func formatJobForResponse(job eventtypes.Job) map[string]interface{} {
	// If SubmissionID is empty, try to get it from metadata
	submissionID := job.SubmissionID
	if submissionID == "" && job.Config.Metadata != nil {
		if metaSubmissionID, ok := job.Config.Metadata["job_submission_id"]; ok {
			submissionID = metaSubmissionID
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
		"status":                    string(job.Status),
		"driver_info": map[string]interface{}{
			"id":              job.JobID,
			"node_ip_address": job.DriverNodeIPAddress,
			"pid":             job.DriverPID,
		},
		"job_id":        job.JobID,
		"submission_id": submissionID,
		"type":          string(job.JobType),
	}

	if !job.StartTime.IsZero() {
		result["start_time"] = job.StartTime.UnixMilli()
	}

	if !job.EndTime.IsZero() {
		result["end_time"] = job.EndTime.UnixMilli()
	}
	return result
}

func (s *ServerHandler) getNode(req *restful.Request, resp *restful.Response) {
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)
	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}
	// Return "not yet supported" for node
	resp.WriteErrorString(http.StatusNotImplemented, "Node not yet supported")
}

func (s *ServerHandler) getJob(req *restful.Request, resp *restful.Response) {
	clusterName := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	clusterNamespace := req.Attribute(COOKIE_CLUSTER_NAMESPACE_KEY).(string)
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)
	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}

	jobID := req.PathParameter("job_id")

	clusterSessionKey := utils.BuildClusterSessionKey(clusterName, clusterNamespace, sessionName)
	job, found := s.eventHandler.GetJobByJobID(clusterSessionKey, jobID)

	if !found {
		responseString := fmt.Sprintf("Job %s does not exist", jobID)
		resp.Write([]byte(responseString))
		return
	}

	respData, err := json.Marshal(formatJobForResponse(job))
	if err != nil {
		logrus.Errorf("Failed to marshal jobs response: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	resp.Write(respData)

}

func (s *ServerHandler) getDatasets(req *restful.Request, resp *restful.Response) {
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)
	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}

	// Return "not yet supported" for datasets
	resp.WriteErrorString(http.StatusNotImplemented, "Datasets not yet supported")
}

func (s *ServerHandler) getServeApplications(req *restful.Request, resp *restful.Response) {
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)
	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}

	// Return "not yet supported" for serve applications
	resp.WriteErrorString(http.StatusNotImplemented, "Serve applications not yet supported")
}

func (s *ServerHandler) getPlacementGroups(req *restful.Request, resp *restful.Response) {
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)
	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}

	// Return "not yet supported" for placement groups
	resp.WriteErrorString(http.StatusNotImplemented, "Placement groups not yet supported")
}

func (s *ServerHandler) getClusterStatus(req *restful.Request, resp *restful.Response) {
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)
	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}

	// Return "not yet supported" for cluster status
	resp.WriteErrorString(http.StatusNotImplemented, "Cluster status not yet supported")
}

func (s *ServerHandler) getNodeLogs(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	clusterNamespace := req.Attribute(COOKIE_CLUSTER_NAMESPACE_KEY).(string)
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)
	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}
	folder := ""
	if req.QueryParameter("folder") != "" {
		folder = req.QueryParameter("folder")
	}
	if req.QueryParameter("glob") != "" {
		folder = req.QueryParameter("glob")
		folder = strings.TrimSuffix(folder, "*")
	}
	data, err := s._getNodeLogs(clusterNameID+"_"+clusterNamespace, sessionName, req.QueryParameter("node_id"), folder)
	if err != nil {
		logrus.Errorf("Error: %v", err)
		resp.WriteError(400, err)
		return
	}
	resp.Write(data)
}

func (s *ServerHandler) getLogicalActors(req *restful.Request, resp *restful.Response) {
	clusterName := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	clusterNamespace := req.Attribute(COOKIE_CLUSTER_NAMESPACE_KEY).(string)
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)

	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}

	filterKey := req.QueryParameter("filter_keys")
	filterValue := req.QueryParameter("filter_values")
	filterPredicate := req.QueryParameter("filter_predicates")

	// Get actors from EventHandler's in-memory map
	clusterSessionKey := utils.BuildClusterSessionKey(clusterName, clusterNamespace, sessionName)
	actorsMap := s.eventHandler.GetActorsMap(clusterSessionKey)

	// Convert map to slice for filtering
	actors := make([]eventtypes.Actor, 0, len(actorsMap))
	for _, actor := range actorsMap {
		actors = append(actors, actor)
	}

	// Apply generic filtering
	actors = utils.ApplyFilter(actors, filterKey, filterPredicate, filterValue,
		func(a eventtypes.Actor, key string) string {
			return eventtypes.GetActorFieldValue(a, key)
		})

	// Format response to match Ray Dashboard API format
	formattedActors := make(map[string]interface{})
	for _, actor := range actors {
		formattedActors[actor.ActorID] = formatActorForResponse(actor)
	}

	response := map[string]interface{}{
		"result": true,
		"msg":    "All actors fetched.",
		"data": map[string]interface{}{
			"actors": formattedActors,
		},
	}

	respData, err := json.Marshal(response)
	if err != nil {
		logrus.Errorf("Failed to marshal actors response: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	resp.Write(respData)
}

// formatActorForResponse converts an eventtypes.Actor to the format expected by Ray Dashboard
func formatActorForResponse(actor eventtypes.Actor) map[string]interface{} {
	result := map[string]interface{}{
		"actor_id":           actor.ActorID,
		"job_id":             actor.JobID,
		"placement_group_id": actor.PlacementGroupID,
		"state":              string(actor.State),
		"pid":                actor.PID,
		"address": map[string]interface{}{
			"node_id":    actor.Address.NodeID,
			"ip_address": actor.Address.IPAddress,
			"port":       actor.Address.Port,
			"worker_id":  actor.Address.WorkerID,
		},
		"name":               actor.Name,
		"num_restarts":       actor.NumRestarts,
		"actor_class":        actor.ActorClass,
		"required_resources": actor.RequiredResources,
		"exit_details":       actor.ExitDetails,
		"repr_name":          actor.ReprName,
		"call_site":          actor.CallSite,
		"is_detached":        actor.IsDetached,
		"ray_namespace":      actor.RayNamespace,
	}

	// Only include start_time if it's set (non-zero)
	if !actor.StartTime.IsZero() {
		result["start_time"] = actor.StartTime.UnixMilli()
	}

	// Only include end_time if it's set (non-zero)
	if !actor.EndTime.IsZero() {
		result["end_time"] = actor.EndTime.UnixMilli()
	}

	return result
}
func (s *ServerHandler) getLogicalActor(req *restful.Request, resp *restful.Response) {
	clusterName := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	clusterNamespace := req.Attribute(COOKIE_CLUSTER_NAMESPACE_KEY).(string)
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)
	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}

	actorID := req.PathParameter("single_actor")

	// Get actor from EventHandler's in-memory map
	clusterSessionKey := utils.BuildClusterSessionKey(clusterName, clusterNamespace, sessionName)
	actor, found := s.eventHandler.GetActorByID(clusterSessionKey, actorID)

	replyActorInfo := ReplyActorInfo{
		Data: ActorInfoData{},
	}

	if found {
		replyActorInfo.Result = true
		replyActorInfo.Msg = "Actor fetched."
		replyActorInfo.Data.Detail = formatActorForResponse(actor)
	} else {
		replyActorInfo.Result = false
		replyActorInfo.Msg = "Actor not found."
	}

	actData, err := json.MarshalIndent(&replyActorInfo, "", "  ")
	if err != nil {
		logrus.Errorf("Failed to marshal actor response: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	resp.Write(actData)
}

func (s *ServerHandler) getNodeLogFile(req *restful.Request, resp *restful.Response) {
	clusterNameID := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	clusterNamespace := req.Attribute(COOKIE_CLUSTER_NAMESPACE_KEY).(string)
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)

	// Parse query parameters
	nodeID := req.QueryParameter("node_id")
	filename := req.QueryParameter("filename")
	lines := req.QueryParameter("lines")

	// Validate required parameters
	if nodeID == "" {
		resp.WriteErrorString(http.StatusBadRequest, "Missing required parameter: node_id")
		return
	}
	if filename == "" {
		resp.WriteErrorString(http.StatusBadRequest, "Missing required parameter: filename")
		return
	}

	// Prevent path traversal attacks (e.g., ../../etc/passwd)
	if !fs.ValidPath(nodeID) || !fs.ValidPath(filename) {
		resp.WriteErrorString(http.StatusBadRequest, fmt.Sprintf("invalid path: path traversal not allowed (node_id=%s, filename=%s)", nodeID, filename))
		return
	}

	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}

	// Convert lines parameter to int
	maxLines := 0
	if lines != "" {
		parsedLines, err := strconv.Atoi(lines)
		if err != nil {
			resp.WriteErrorString(http.StatusBadRequest, fmt.Sprintf("invalid lines parameter: %s", lines))
			return
		}
		maxLines = parsedLines
	}

	content, err := s._getNodeLogFile(clusterNameID+"_"+clusterNamespace, sessionName, nodeID, filename, maxLines)
	if err != nil {
		var httpErr *utils.HTTPError
		if errors.As(err, &httpErr) {
			logrus.Errorf("Error getting node log file: %v", httpErr.Unwrap())
			resp.WriteError(httpErr.StatusCode(), httpErr)
		} else {
			logrus.Errorf("Error getting node log file: %v", err)
			resp.WriteError(http.StatusInternalServerError, err)
		}
		return
	}
	resp.Write(content)
}

func (s *ServerHandler) getTaskSummarize(req *restful.Request, resp *restful.Response) {
	clusterName := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	clusterNamespace := req.Attribute(COOKIE_CLUSTER_NAMESPACE_KEY).(string)
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)

	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}

	// Parse filter parameters
	filterKey := req.QueryParameter("filter_keys")
	filterValue := req.QueryParameter("filter_values")
	filterPredicate := req.QueryParameter("filter_predicates")
	summaryBy := req.QueryParameter("summary_by")

	// Get all tasks
	clusterSessionKey := utils.BuildClusterSessionKey(clusterName, clusterNamespace, sessionName)
	tasks := s.eventHandler.GetTasks(clusterSessionKey)

	// Apply generic filtering using utils.ApplyFilter
	tasks = utils.ApplyFilter(tasks, filterKey, filterPredicate, filterValue,
		func(t eventtypes.Task, key string) string {
			return eventtypes.GetTaskFieldValue(t, key)
		})

	// Summarize tasks based on summary_by parameter
	var summary map[string]interface{}
	if summaryBy == "lineage" {
		summary = summarizeTasksByLineage(tasks)
	} else {
		// Default to func_name
		summary = summarizeTasksByFuncName(tasks)
	}

	response := map[string]interface{}{
		"result": true,
		"msg":    "Tasks summarized.",
		"data": map[string]interface{}{
			"result": summary,
		},
	}

	respData, err := json.Marshal(response)
	if err != nil {
		logrus.Errorf("Failed to marshal task summarize response: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	resp.Write(respData)
}

// summarizeTasksByFuncName groups tasks by function name and counts by state
func summarizeTasksByFuncName(tasks []eventtypes.Task) map[string]interface{} {
	summary := make(map[string]map[string]int)

	for _, task := range tasks {
		funcName := task.FuncOrClassName
		if funcName == "" {
			funcName = "unknown"
		}
		if _, ok := summary[funcName]; !ok {
			summary[funcName] = make(map[string]int)
		}
		state := string(task.State)
		if state == "" {
			state = "UNKNOWN"
		}
		summary[funcName][state]++
	}

	return map[string]interface{}{
		"summary": summary,
		"total":   len(tasks),
	}
}

// TODO(Han-Ju Chen): This function has a bug - using JobID instead of actual lineage.
// Real lineage requires:
// 1. Add ParentTaskID field to Task struct (types/task.go)
// 2. Parse parent_task_id from Ray events (eventserver.go)
// 3. Build task tree structure based on ParentTaskID
// 4. Update rayjob example to generate nested tasks for testing
func summarizeTasksByLineage(tasks []eventtypes.Task) map[string]interface{} {
	summary := make(map[string]map[string]int)

	for _, task := range tasks {
		// Use JobID as a simple lineage grouping for now
		lineageKey := task.JobID
		if lineageKey == "" {
			lineageKey = "unknown"
		}
		if _, ok := summary[lineageKey]; !ok {
			summary[lineageKey] = make(map[string]int)
		}
		state := string(task.State)
		if state == "" {
			state = "UNKNOWN"
		}
		summary[lineageKey][state]++
	}

	return map[string]interface{}{
		"summary": summary,
		"total":   len(tasks),
	}
}

func (s *ServerHandler) getTaskDetail(req *restful.Request, resp *restful.Response) {
	clusterName := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	clusterNamespace := req.Attribute(COOKIE_CLUSTER_NAMESPACE_KEY).(string)
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)

	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}

	filterKey := req.QueryParameter("filter_keys")
	filterValue := req.QueryParameter("filter_values")
	filterPredicate := req.QueryParameter("filter_predicates")

	clusterSessionKey := utils.BuildClusterSessionKey(clusterName, clusterNamespace, sessionName)
	tasks := s.eventHandler.GetTasks(clusterSessionKey)
	tasks = utils.ApplyFilter(tasks, filterKey, filterPredicate, filterValue,
		func(t eventtypes.Task, key string) string {
			return eventtypes.GetTaskFieldValue(t, key)
		})

	taskResults := make([]interface{}, 0, len(tasks))
	for _, task := range tasks {
		taskResults = append(taskResults, formatTaskForResponse(task))
	}

	response := ReplyTaskInfo{
		Result: true,
		Msg:    "Tasks fetched.",
		Data: TaskInfoData{
			Result: TaskInfoDataResult{
				Result:             taskResults,
				Total:              len(taskResults),
				NumFiltered:        len(taskResults),
				NumAfterTruncation: len(taskResults),
			},
		},
	}

	respData, err := json.Marshal(response)
	if err != nil {
		logrus.Errorf("Failed to marshal task response: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	resp.Write(respData)
}

// formatTaskForResponse converts an eventtypes.Task to the format expected by Ray Dashboard
func formatTaskForResponse(task eventtypes.Task) map[string]interface{} {
	result := map[string]interface{}{
		"task_id":            task.TaskID,
		"name":               task.Name,
		"attempt_number":     task.AttemptNumber,
		"state":              string(task.State),
		"job_id":             task.JobID,
		"node_id":            task.NodeID,
		"actor_id":           task.ActorID,
		"placement_group_id": task.PlacementGroupID,
		"type":               string(task.Type),
		"func_or_class_name": task.FuncOrClassName,
		"language":           task.Language,
		"required_resources": task.RequiredResources,
		"worker_id":          task.WorkerID,
		"error_type":         task.ErrorType,
		"error_message":      task.ErrorMessage,
		"call_site":          task.CallSite,
	}

	if !task.StartTime.IsZero() {
		result["start_time"] = task.StartTime.UnixMilli()
	}

	if !task.EndTime.IsZero() {
		result["end_time"] = task.EndTime.UnixMilli()
	}

	return result
}

// CookieHandle is a preprocessing filter function
func (s *ServerHandler) CookieHandle(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {
	// Get cookie from request
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
	clusterNamespace, err := req.Request.Cookie(COOKIE_CLUSTER_NAMESPACE_KEY)
	if err != nil {
		resp.WriteHeaderAndEntity(http.StatusBadRequest, "Cluster Namespace Cookie not found")
		return
	}

	// Validate cookie values to prevent path traversal attacks
	if !fs.ValidPath(clusterName.Value) || !fs.ValidPath(clusterNamespace.Value) || !fs.ValidPath(sessionName.Value) {
		resp.WriteHeaderAndEntity(http.StatusBadRequest, fmt.Sprintf("invalid cookie values: path traversal not allowed (cluster_name=%s, cluster_namespace=%s, session_name=%s)", clusterName.Value, clusterNamespace.Value, sessionName.Value))
		return
	}

	http.SetCookie(resp, &http.Cookie{MaxAge: 600, Path: "/", Name: COOKIE_CLUSTER_NAME_KEY, Value: clusterName.Value})
	http.SetCookie(resp, &http.Cookie{MaxAge: 600, Path: "/", Name: COOKIE_CLUSTER_NAMESPACE_KEY, Value: clusterNamespace.Value})
	http.SetCookie(resp, &http.Cookie{MaxAge: 600, Path: "/", Name: COOKIE_SESSION_NAME_KEY, Value: sessionName.Value})

	if sessionName.Value == "live" {
		// Always query K8s to get the service name to prevent SSRF attacks.
		// Do not trust user-provided cookies for service name.
		// TODO: here might be a bottleneck if there are many requests in the future.
		svcInfo, err := getClusterSvcInfo(s.clientManager.clients, clusterName.Value, clusterNamespace.Value)
		if err != nil {
			resp.WriteHeaderAndEntity(http.StatusBadRequest, err.Error())
			return
		}
		req.SetAttribute(ATTRIBUTE_SERVICE_NAME, svcInfo)
	}
	req.SetAttribute(COOKIE_CLUSTER_NAME_KEY, clusterName.Value)
	req.SetAttribute(COOKIE_SESSION_NAME_KEY, sessionName.Value)
	req.SetAttribute(COOKIE_CLUSTER_NAMESPACE_KEY, clusterNamespace.Value)
	logrus.Infof("Request URL %s", req.Request.URL.String())
	chain.ProcessFilter(req, resp)
}

func getClusterSvcInfo(clis []client.Client, name, namespace string) (ServiceInfo, error) {
	if len(clis) == 0 {
		return ServiceInfo{}, errors.New("No available kubernetes config found")
	}
	cli := clis[0]
	rc := rayv1.RayCluster{}
	err := cli.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: name}, &rc)
	if err != nil {
		return ServiceInfo{}, errors.New("RayCluster not found")
	}
	svcName := rc.Status.Head.ServiceName
	if svcName == "" {
		return ServiceInfo{}, errors.New("RayCluster head service not ready")
	}
	return ServiceInfo{ServiceName: svcName, Namespace: namespace, Port: 8265}, nil
}
