package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/emicklei/go-restful/v3"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/metrics"
	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/snapshot"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/historyserver"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// timelineNow is the time source for getTasksTimeline's download filename.
var timelineNow = time.Now

const (
	cookieClusterNameKey      = "cluster_name"
	cookieClusterNamespaceKey = "cluster_namespace"
	cookieSessionNameKey      = "session_name"

	// liveSessionSentinel is the cookie value used for live (still-running)
	// clusters. Handlers proxy to Ray Dashboard for this value instead of
	// reading a snapshot.
	liveSessionSentinel = "live"

	// retryAfterSecondsOnMissingSnapshot is the Retry-After value (seconds)
	// returned to clients for dead-but-unsnapped sessions.
	retryAfterSecondsOnMissingSnapshot = "600"
)

// snapshotLoader is the narrow interface the handlers depend on.
// *SnapshotLoader (cache.go) satisfies it.
type snapshotLoader interface {
	Load(clusterNameID, sessionName string) (*snapshot.SessionSnapshot, error)
}

// Server is the HTTP handler container for the v2 History Server.
type Server struct {
	loader        snapshotLoader
	supervisor    *Supervisor
	reader        storage.StorageReader
	clientManager *historyserver.ClientManager
	proxyResolver ProxyResolver
	dashboardDir  string
	dashboardAddr string       // optional override; empty means "derive from clientManager".
	useKubeProxy  bool         // proxy through kube-apiserver vs in-cluster DNS.
	httpClient    *http.Client // nil → redirectRequest returns 501.
	httpServer    *http.Server
}

// NewServer creates a Server wired to a *SnapshotLoader and the dependencies
// required for live-session proxying and the dead-session file endpoints.
// SetProxyResolver and SetHTTPClient inject the proxy wiring after construction.
func NewServer(
	loader *SnapshotLoader,
	supervisor *Supervisor,
	reader storage.StorageReader,
	cm *historyserver.ClientManager,
	dashboardDir string,
	useKubeProxy bool,
) *Server {
	return &Server{
		loader: loader,
		// supervisor is optional; nil falls back to cookie-only behavior in enterCluster.
		supervisor:    supervisor,
		reader:        reader,
		clientManager: cm,
		dashboardDir:  dashboardDir,
		useKubeProxy:  useKubeProxy,
	}
}

// SetHTTPClient wires the *http.Client redirectRequest uses to proxy live traffic.
func (s *Server) SetHTTPClient(c *http.Client) {
	s.httpClient = c
}

// newServerWithLoader is the test-only constructor for injecting a fake snapshotLoader.
func newServerWithLoader(loader snapshotLoader) *Server {
	return &Server{loader: loader}
}

// --- cookie & error helpers ---------------------------------------------------

// readSessionCookies returns (clusterNameID, sessionName, clusterSessionKey).
// clusterSessionKey is the "{name}_{ns}_{session}" form used as snapshot.SessionKey.
func readSessionCookies(req *restful.Request) (clusterNameID, sessionName, clusterSessionKey string, ok bool) {
	clusterNameCookie, err := req.Request.Cookie(cookieClusterNameKey)
	if err != nil {
		return "", "", "", false
	}
	clusterNamespaceCookie, err := req.Request.Cookie(cookieClusterNamespaceKey)
	if err != nil {
		return "", "", "", false
	}
	sessionNameCookie, err := req.Request.Cookie(cookieSessionNameKey)
	if err != nil {
		return "", "", "", false
	}
	name, ns, sess := clusterNameCookie.Value, clusterNamespaceCookie.Value, sessionNameCookie.Value
	if name == "" || ns == "" || sess == "" {
		return "", "", "", false
	}
	return name + "_" + ns, sess, utils.BuildClusterSessionKey(name, ns, sess), true
}

// writeMissingCookies writes the canonical 400 response for missing cookies.
func writeMissingCookies(resp *restful.Response) {
	resp.WriteErrorString(http.StatusBadRequest,
		"required cookies missing: cluster_name, cluster_namespace, session_name")
}

// handleMissingSnapshot maps loader errors to HTTP responses.
//
//   - ErrSnapshotNotFound: 503 + Retry-After: 600 (dead session whose
//     snapshot has not yet been written).
//   - other error: 500 Internal Server Error.
func (s *Server) handleMissingSnapshot(resp *restful.Response, err error) {
	if errors.Is(err, ErrSnapshotNotFound) {
		metrics.MissingSnapshot503.Inc()
		resp.AddHeader("Retry-After", retryAfterSecondsOnMissingSnapshot)
		resp.WriteErrorString(http.StatusServiceUnavailable,
			"snapshot not yet generated, retry in 10 min")
		return
	}
	logrus.Errorf("handleMissingSnapshot: unexpected loader error: %v", err)
	resp.WriteErrorString(http.StatusInternalServerError, err.Error())
}

// writeJSON serializes v as JSON and writes it, setting Content-Type.
func writeJSON(resp *restful.Response, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		logrus.Errorf("writeJSON: marshal failed: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	resp.Header().Set("Content-Type", "application/json")
	if _, err := resp.Write(data); err != nil {
		logrus.Errorf("writeJSON: write failed: %v", err)
	}
}

// --- getTasks: GET /api/v0/tasks ---------------------------------------------

// getTasks implements the Ray State API's tasks endpoint, mirroring v1.
// The snapshot stores Tasks as taskID -> []attempt (one entry per TaskAttempt);
// flatten, apply ParseOptionsFromReq + ApplyTaskFilters, then format.
func (s *Server) getTasks(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}

	opts, err := utils.ParseOptionsFromReq(req)
	if err != nil {
		resp.WriteErrorString(http.StatusBadRequest, err.Error())
		return
	}

	snap, err := s.loader.Load(clusterNameID, sessionName)
	if err != nil {
		s.handleMissingSnapshot(resp, err)
		return
	}

	tasks := flattenTasks(snap.Tasks)
	numAfterTruncation := len(tasks)
	numTotal := numAfterTruncation

	tasks, numFiltered := utils.ApplyTaskFilters(tasks, opts)

	formatted := make([]map[string]interface{}, 0, len(tasks))
	for _, t := range tasks {
		formatted = append(formatted, formatTaskForResponse(t, opts.Detail))
	}

	response := map[string]interface{}{
		"result": true,
		"msg":    "",
		"data": map[string]interface{}{
			"result": map[string]interface{}{
				"total":                   numTotal,
				"num_after_truncation":    numAfterTruncation,
				"num_filtered":            numFiltered,
				"result":                  formatted,
				"partial_failure_warning": "",
				"warnings":                nil,
			},
		},
	}
	writeJSON(resp, response)
}

// flattenTasks converts the snapshot's taskID -> []attempt map into a flat
// []Task slice (one element per attempt) for filters and formatters.
func flattenTasks(byID map[string][]eventtypes.Task) []eventtypes.Task {
	total := 0
	for _, attempts := range byID {
		total += len(attempts)
	}
	out := make([]eventtypes.Task, 0, total)
	for _, attempts := range byID {
		out = append(out, attempts...)
	}
	return out
}

// --- getTaskSummarize: GET /api/v0/tasks/summarize ---------------------------

// getTaskSummarize implements summary_by=func_name (default) and summary_by=lineage.
func (s *Server) getTaskSummarize(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}

	opts, err := utils.ParseOptionsFromReq(req)
	if err != nil {
		resp.WriteErrorString(http.StatusBadRequest, err.Error())
		return
	}
	summaryBy := req.QueryParameter("summary_by")
	// Maximize entries for the summary to minimize data loss.
	opts.Limit = utils.RayMaxLimitFromAPIServer

	snap, err := s.loader.Load(clusterNameID, sessionName)
	if err != nil {
		s.handleMissingSnapshot(resp, err)
		return
	}

	tasks := flattenTasks(snap.Tasks)
	numAfterTruncation := len(tasks)
	numTotal := numAfterTruncation

	tasks, numFiltered := utils.ApplyTaskFilters(tasks, opts)

	var inner map[string]interface{}
	if summaryBy == "lineage" {
		actors := make([]eventtypes.Actor, 0, len(snap.Actors))
		for _, a := range snap.Actors {
			actors = append(actors, a)
		}
		lineageSummary := utils.ToSummaryByLineage(tasks, actors)
		inner = map[string]interface{}{
			"node_id_to_summary": map[string]*utils.TaskSummaries{
				"cluster": lineageSummary,
			},
		}
	} else {
		funcNameSummary := summarizeTasksByFuncName(tasks)
		inner = map[string]interface{}{
			"node_id_to_summary": map[string]*utils.TaskSummariesByFuncName{
				"cluster": funcNameSummary,
			},
		}
	}

	response := map[string]interface{}{
		"result": true,
		"msg":    "",
		"data": map[string]interface{}{
			"result": map[string]interface{}{
				"total":                   numTotal,
				"num_after_truncation":    numAfterTruncation,
				"num_filtered":            numFiltered,
				"result":                  inner,
				"partial_failure_warning": "",
				"warnings":                nil,
			},
		},
	}
	writeJSON(resp, response)
}

// --- getTasksTimeline: GET /api/v0/tasks/timeline ----------------------------

// getTasksTimeline returns Chrome-Tracing-Format events for the Dashboard's
// Timeline view (see timeline.go:generateTimelineFromSnapshot). Response is
// a bare JSON array — the frontend parses it directly; no envelope.
//
// Query params:
//   - job_id: optional filter; empty means all jobs in the session.
//   - download: "1" sets Content-Disposition so the browser saves the array
//     as a .json file. Filename includes job_id and the current timestamp.
func (s *Server) getTasksTimeline(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}

	snap, err := s.loader.Load(clusterNameID, sessionName)
	if err != nil {
		s.handleMissingSnapshot(resp, err)
		return
	}

	jobID := req.QueryParameter("job_id")
	events := generateTimelineFromSnapshot(snap, jobID)

	respData, err := json.Marshal(events)
	if err != nil {
		logrus.Errorf("getTasksTimeline: marshal failed: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	// Download header layout: changing the time pattern would break any tooling
	// that matches on filename.
	if req.QueryParameter("download") == "1" {
		nowStr := timelineNow().Format("2006-01-02_15-04-05")
		filename := fmt.Sprintf("timeline-%s.json", nowStr)
		if jobID != "" {
			filename = fmt.Sprintf("timeline-%s-%s.json", jobID, nowStr)
		}
		resp.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	}
	resp.Header().Set("Content-Type", "application/json")
	if _, err := resp.Write(respData); err != nil {
		logrus.Errorf("getTasksTimeline: write failed: %v", err)
	}
}

// --- getLogicalActors: GET /logical/actors -----------------------------------

func (s *Server) getLogicalActors(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}

	snap, err := s.loader.Load(clusterNameID, sessionName)
	if err != nil {
		s.handleMissingSnapshot(resp, err)
		return
	}

	// Keyed by hex actor ID to match the Ray Dashboard frontend.
	formatted := make(map[string]interface{}, len(snap.Actors))
	for _, actor := range snap.Actors {
		actorIDHex, _ := utils.ConvertBase64ToHex(actor.ActorID)
		formatted[actorIDHex] = formatActorForResponse(actor)
	}

	response := map[string]interface{}{
		"result": true,
		"msg":    "All actors fetched.",
		"data": map[string]interface{}{
			"actors": formatted,
		},
	}
	writeJSON(resp, response)
}

// --- getLogicalActor: GET /logical/actors/{single_actor} ---------------------

func (s *Server) getLogicalActor(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}

	actorID := req.PathParameter("single_actor")

	snap, err := s.loader.Load(clusterNameID, sessionName)
	if err != nil {
		s.handleMissingSnapshot(resp, err)
		return
	}

	// Direct base64 key first; fall back to a linear scan converting each
	// actor's base64 ID to hex (O(n), known limitation; fix tracked in v1 PR #4563).
	actor, found := snap.Actors[actorID]
	if !found {
		for _, a := range snap.Actors {
			if hexID, err := utils.ConvertBase64ToHex(a.ActorID); err == nil && hexID == actorID {
				actor, found = a, true
				break
			}
		}
	}

	reply := map[string]interface{}{
		"result": found,
		"msg": func() string {
			if found {
				return "Actor fetched."
			}
			return "Actor not found."
		}(),
		"data": map[string]interface{}{
			"detail": func() interface{} {
				if found {
					return formatActorForResponse(actor)
				}
				return map[string]interface{}{}
			}(),
		},
	}
	writeJSON(resp, reply)
}

// --- getJobs: GET /api/jobs/ --------------------------------------------------

func (s *Server) getJobs(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}

	snap, err := s.loader.Load(clusterNameID, sessionName)
	if err != nil {
		s.handleMissingSnapshot(resp, err)
		return
	}

	// Bare JSON array (no envelope), matching the Dashboard's expectation.
	formatted := make([]interface{}, 0, len(snap.Jobs))
	for _, job := range snap.Jobs {
		formatted = append(formatted, formatJobForResponse(job))
	}
	writeJSON(resp, formatted)
}

// --- getJob: GET /api/jobs/{job_id} ------------------------------------------

func (s *Server) getJob(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}

	jobID := req.PathParameter("job_id")

	snap, err := s.loader.Load(clusterNameID, sessionName)
	if err != nil {
		s.handleMissingSnapshot(resp, err)
		return
	}

	job, found := snap.Jobs[jobID]
	if !found {
		// Plain text with 200 (not 404 JSON) — frontend depends on this shape.
		if _, werr := resp.Write([]byte(fmt.Sprintf("Job %s does not exist", jobID))); werr != nil {
			logrus.Errorf("getJob: write not-found body failed: %v", werr)
		}
		return
	}
	writeJSON(resp, formatJobForResponse(job))
}

// --- getNodes: GET /nodes ----------------------------------------------------

// getNodes supports view=summary (default) and view=hostNameList.
func (s *Server) getNodes(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}

	viewParam := req.QueryParameter("view")

	snap, err := s.loader.Load(clusterNameID, sessionName)
	if err != nil {
		s.handleMissingSnapshot(resp, err)
		return
	}

	switch viewParam {
	case "hostNameList":
		writeNodesHostNameList(snap.Nodes, resp)
	case "summary", "":
		writeNodesSummary(snap.Nodes, sessionName, resp)
	default:
		resp.WriteErrorString(http.StatusBadRequest,
			fmt.Sprintf("unsupported view parameter: %s", viewParam))
	}
}

// writeNodesSummary uses the latest state transition as each node's "current"
// snapshot — for dead sessions that is the final state.
func writeNodesSummary(nodeMap map[string]eventtypes.Node, sessionName string, resp *restful.Response) {
	summary := make([]map[string]interface{}, 0, len(nodeMap))
	nodeLogicalResources := make(map[string]string)

	for _, node := range nodeMap {
		nodeSummaryReplay := formatNodeSummaryReplayForResp(node, sessionName)
		if len(nodeSummaryReplay) > 0 {
			summary = append(summary, nodeSummaryReplay[len(nodeSummaryReplay)-1])
		}
		// Find the last non-empty resource string (DEAD transitions clear it).
		nodeResourceReplay := formatNodeResourceReplayForResp(node)
		for i := len(nodeResourceReplay) - 1; i >= 0; i-- {
			if rs, ok := nodeResourceReplay[i]["resourceString"].(string); ok && rs != "" {
				nodeLogicalResources[node.NodeID] = rs
				break
			}
		}
	}

	response := map[string]interface{}{
		"result": true,
		"msg":    "Node summary fetched.",
		"data": map[string]interface{}{
			"summary":              summary,
			"nodeLogicalResources": nodeLogicalResources,
		},
	}
	writeJSON(resp, response)
}

// writeNodesHostNameList returns hostnames for ALIVE nodes. Fallback:
// Hostname → NodeName → NodeID.
// Ref: https://github.com/ray-project/ray/issues/60129
func writeNodesHostNameList(nodeMap map[string]eventtypes.Node, resp *restful.Response) {
	hostNameList := make([]string, 0)
	for _, node := range nodeMap {
		if len(node.StateTransitions) == 0 {
			continue
		}
		lastState := node.StateTransitions[len(node.StateTransitions)-1].State
		if lastState != eventtypes.NODE_ALIVE {
			continue
		}
		hostname := node.Hostname
		if hostname == "" {
			hostname = node.NodeName
		}
		if hostname == "" {
			hostname = node.NodeID
		}
		hostNameList = append(hostNameList, hostname)
	}
	response := map[string]interface{}{
		"result": true,
		"msg":    "Node hostname list fetched.",
		"data": map[string]interface{}{
			"hostNameList": hostNameList,
		},
	}
	writeJSON(resp, response)
}

// --- getNode: GET /nodes/{node_id} -------------------------------------------

// getNode returns a single node with its scheduled actors filled in. The
// Dashboard's NodeDetail page expects actors filtered by NodeID.
func (s *Server) getNode(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}

	targetNodeID := req.PathParameter("node_id")
	if targetNodeID == "" {
		resp.WriteErrorString(http.StatusBadRequest, "node_id is required")
		return
	}

	snap, err := s.loader.Load(clusterNameID, sessionName)
	if err != nil {
		s.handleMissingSnapshot(resp, err)
		return
	}

	node, found := snap.Nodes[targetNodeID]
	if !found {
		resp.WriteErrorString(http.StatusNotFound, fmt.Sprintf("node %s not found", targetNodeID))
		return
	}

	nodeSummaryReplay := formatNodeSummaryReplayForResp(node, sessionName)
	if len(nodeSummaryReplay) == 0 {
		resp.WriteErrorString(http.StatusNotFound,
			fmt.Sprintf("node %s has no state transitions yet", targetNodeID))
		return
	}
	detail := nodeSummaryReplay[len(nodeSummaryReplay)-1]

	// Fill actors scheduled on this node (keyed by hex actor ID).
	nodeActors := make(map[string]interface{})
	for _, actor := range snap.Actors {
		nodeIDHex, _ := utils.ConvertBase64ToHex(actor.Address.NodeID)
		if nodeIDHex == targetNodeID {
			actorIDHex, _ := utils.ConvertBase64ToHex(actor.ActorID)
			nodeActors[actorIDHex] = formatActorForResponse(actor)
		}
	}
	detail["actors"] = nodeActors

	response := map[string]interface{}{
		"result": true,
		"msg":    "Node details fetched.",
		"data": map[string]interface{}{
			"detail": detail,
		},
	}
	writeJSON(resp, response)
}

// --- getEvents: GET /events --------------------------------------------------

// getEvents returns log events: all events grouped by job_id (when no job_id
// param) or one job's events (when job_id is set, including empty string —
// "param not given" and "param given but empty" are distinct).
func (s *Server) getEvents(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}

	snap, err := s.loader.Load(clusterNameID, sessionName)
	if err != nil {
		s.handleMissingSnapshot(resp, err)
		return
	}

	_, jobIDExists := req.Request.URL.Query()["job_id"]
	jobID := req.QueryParameter("job_id")

	var response map[string]interface{}
	if jobIDExists {
		events := make([]map[string]interface{}, 0)
		for _, e := range snap.LogEvents.ByJobID[jobID] {
			ev := e // copy to avoid taking the address of the loop variable
			resp := ev.ToAPIResponse()
			events = append(events, resp)
		}
		response = map[string]interface{}{
			"result": true,
			"msg":    "Job events fetched.",
			"data": map[string]interface{}{
				"jobId":  jobID,
				"events": events,
			},
		}
	} else {
		all := make(map[string][]map[string]interface{}, len(snap.LogEvents.ByJobID))
		for jID, evs := range snap.LogEvents.ByJobID {
			list := make([]map[string]interface{}, 0, len(evs))
			for _, e := range evs {
				ev := e
				list = append(list, ev.ToAPIResponse())
			}
			all[jID] = list
		}
		response = map[string]interface{}{
			"result": true,
			"msg":    "All events fetched.",
			"data": map[string]interface{}{
				"events": all,
			},
		}
	}
	writeJSON(resp, response)
}

// --- getNodeLogs: GET /api/v0/logs -------------------------------------------

// getNodeLogs lists log files for a node, grouped by Ray's component-type
// categorization. Snapshot-independent — reads directly from storage.
//
// Query params:
//   - node_id: hex node ID (subdirectory under logs/). Path-traversal guarded.
//   - glob: double-star-capable glob pattern. Directory prefixes (e.g.
//     "logs/raylet*") narrow the ListFiles call; the leaf pattern matches
//     per file.
//
// Errors:
//   - Missing cookies, path traversal, glob match failure: 400.
//   - Live session: proxy.
//   - Missing s.reader: 500.
//
// The response is a "data.result" object keyed by component category
// (worker_out, worker_err, core_worker, driver, raylet, gcs_server, internal,
// autoscaler, agent, dashboard).
func (s *Server) getNodeLogs(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}
	if s.reader == nil {
		resp.WriteErrorString(http.StatusInternalServerError,
			"storage reader not configured")
		return
	}

	nodeID := req.QueryParameter("node_id")
	if nodeID != "" && !fs.ValidPath(nodeID) {
		resp.WriteErrorString(http.StatusBadRequest,
			fmt.Sprintf("invalid path: path traversal not allowed (node_id=%s)", nodeID))
		return
	}

	// Split the glob: "logs/raylet*" → base="logs", pattern="raylet*" so
	// ListFiles narrows the directory and Match works on leaf file names.
	var folder, glob string
	if rawGlob := req.QueryParameter("glob"); rawGlob != "" {
		base, pattern := doublestar.SplitPattern(rawGlob)
		glob = pattern
		if base != "." {
			if !fs.ValidPath(base) {
				resp.WriteErrorString(http.StatusBadRequest,
					fmt.Sprintf("invalid path: path traversal not allowed (glob=%s)", rawGlob))
				return
			}
			folder = base
		}
	}

	data, err := s.listNodeLogFiles(clusterNameID, sessionName, nodeID, folder, glob)
	if err != nil {
		logrus.Errorf("getNodeLogs: %v", err)
		resp.WriteError(http.StatusBadRequest, err)
		return
	}
	resp.Header().Set("Content-Type", "application/json")
	if _, err := resp.Write(data); err != nil {
		logrus.Errorf("getNodeLogs: write failed: %v", err)
	}
}

// listNodeLogFiles assembles the JSON body for getNodeLogs.
func (s *Server) listNodeLogFiles(clusterNameID, sessionName, nodeID, folder, glob string) ([]byte, error) {
	logPath := path.Join(sessionName, utils.RAY_SESSIONDIR_LOGDIR_NAME, nodeID)
	if folder != "" {
		logPath = path.Join(logPath, folder)
	}

	var matchedFiles []string
	if glob == "" {
		matchedFiles = s.reader.ListFiles(clusterNameID, logPath)
	} else {
		var files []string
		if strings.Contains(glob, "**") {
			// ** requires recursive enumeration; walk sub-directories and
			// concatenate relative paths so Match sees the full leaf.
			files = s.listFilesRecursive(clusterNameID, logPath)
		} else {
			files = s.reader.ListFiles(clusterNameID, logPath)
		}
		for _, file := range files {
			matched, err := doublestar.Match(glob, file)
			if err != nil {
				return nil, fmt.Errorf("invalid glob pattern %q matching against %q: %w", glob, file, err)
			}
			if matched {
				matchedFiles = append(matchedFiles, file)
			}
		}
	}

	categorized := categorizeLogFiles(matchedFiles)
	envelope := map[string]interface{}{
		"result": true,
		"msg":    "",
		"data": map[string]interface{}{
			"result": categorized,
		},
	}
	return json.Marshal(envelope)
}

// listFilesRecursive walks a directory tree, returning all leaf files with
// paths relative to the starting dir. Directories are marked by a trailing
// "/" in ListFiles output.
func (s *Server) listFilesRecursive(clusterID, dir string) []string {
	entries := s.reader.ListFiles(clusterID, dir)
	var result []string
	for _, entry := range entries {
		if strings.HasSuffix(entry, "/") {
			subDir := path.Join(dir, entry)
			subFiles := s.listFilesRecursive(clusterID, subDir)
			for _, f := range subFiles {
				result = append(result, path.Join(strings.TrimSuffix(entry, "/"), f))
			}
		} else {
			result = append(result, entry)
		}
	}
	return result
}

// categorizeLogFiles sorts log file names into Ray's component buckets.
// Switch order matters: "log_monitor" must be checked before "monitor".
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

// --- getClusterMetadata: GET /api/v0/cluster_metadata ------------------------

// getClusterMetadata streams fetched_endpoints/<key> verbatim. Snapshot-
// independent — the collector wrote it during live operation; we return as-is.
func (s *Server) getClusterMetadata(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}
	if s.reader == nil {
		resp.WriteErrorString(http.StatusInternalServerError,
			"storage reader not configured")
		return
	}

	storageKey := utils.EndpointPathToStorageKey("/api/v0/cluster_metadata")
	endpointPath := path.Join(sessionName, utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME, storageKey)

	r := s.reader.GetContent(clusterNameID, endpointPath)
	if r == nil {
		resp.WriteErrorString(http.StatusNotFound, "Cluster metadata not found")
		return
	}
	body, err := io.ReadAll(r)
	if err != nil {
		logrus.Errorf("getClusterMetadata: read failed: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, "Failed to read cluster metadata")
		return
	}
	resp.Header().Set("Content-Type", "application/json")
	if _, err := resp.Write(body); err != nil {
		logrus.Errorf("getClusterMetadata: write failed: %v", err)
	}
}

// --- getClusterStatus: GET /api/cluster_status -------------------------------

// getClusterStatus returns the autoscaler status block the Ray Dashboard
// renders on the cluster-overview page.
//
// Two modes:
//   - format=1: returns {"clusterStatus": "<Ray-formatted multi-line text>"}
//     by loading snap + per-node debug_state.txt and invoking
//     buildFormattedClusterStatus. This is the mode the Dashboard uses.
//   - anything else: returns an empty ClusterStatusData envelope.
//
// The format=1 path is hybrid: node counts come from debug_state.txt (for
// parity with live Ray output), failed nodes and pending demands come from
// the snapshot.
func (s *Server) getClusterStatus(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}

	format := req.QueryParameter("format")
	if format != "1" {
		// Do not pull the snapshot in this branch — frontend ignores the response
		// when format != "1".
		writeJSON(resp, map[string]interface{}{
			"result": true,
			"msg":    "Got cluster status.",
			"data": map[string]interface{}{
				"autoscalingStatus": nil,
				"autoscalingError":  nil,
				"clusterStatus":     nil,
			},
		})
		return
	}

	if s.reader == nil {
		resp.WriteErrorString(http.StatusInternalServerError,
			"storage reader not configured")
		return
	}

	snap, err := s.loader.Load(clusterNameID, sessionName)
	if err != nil {
		s.handleMissingSnapshot(resp, err)
		return
	}

	statusString := buildFormattedClusterStatus(s.reader, snap, clusterNameID, sessionName)
	writeJSON(resp, map[string]interface{}{
		"result": true,
		"msg":    "Got formatted cluster status.",
		"data": map[string]interface{}{
			"clusterStatus": statusString,
		},
	})
}

// getNodeLog is the unified singular-log endpoint that dispatches to a
// file-read or stream handler based on media_type.
func (s *Server) getNodeLog(req *restful.Request, resp *restful.Response) {
	mediaType := req.PathParameter("media_type")
	switch mediaType {
	case "file":
		s.getNodeLogFile(req, resp)
	case "stream":
		s.getNodeLogStream(req, resp)
	default:
		resp.WriteErrorString(http.StatusBadRequest,
			fmt.Sprintf("unsupported media_type: %s (must be 'file' or 'stream')", mediaType))
	}
}

// getNodeLogFile returns the content of a specific log file from object
// storage. Requires node_id + filename query params. task_id / actor_id /
// pid-based resolution returns 400 — callers must resolve client-side.
func (s *Server) getNodeLogFile(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}
	if s.reader == nil {
		resp.WriteErrorString(http.StatusInternalServerError, "storage reader not configured")
		return
	}

	nodeID := req.QueryParameter("node_id")
	filename := req.QueryParameter("filename")
	taskID := req.QueryParameter("task_id")
	actorID := req.QueryParameter("actor_id")

	// task_id / actor_id resolution is not implemented; caller must pass
	// node_id + filename directly.
	if (taskID != "" || actorID != "") && (nodeID == "" || filename == "") {
		resp.WriteErrorString(http.StatusBadRequest,
			"task_id / actor_id resolution not yet implemented in beta; "+
				"pass node_id and filename directly (TODO: port v1 _getNodeLogFile task-resolution path)")
		return
	}

	if nodeID == "" || filename == "" {
		resp.WriteErrorString(http.StatusBadRequest,
			"node_id and filename query params are required")
		return
	}

	// Path-traversal guard for both components before concatenation.
	if !fs.ValidPath(nodeID) {
		resp.WriteErrorString(http.StatusBadRequest,
			fmt.Sprintf("invalid path: path traversal not allowed (node_id=%s)", nodeID))
		return
	}
	if !fs.ValidPath(filename) {
		resp.WriteErrorString(http.StatusBadRequest,
			fmt.Sprintf("invalid path: path traversal not allowed (filename=%s)", filename))
		return
	}

	logPath := path.Join(sessionName, utils.RAY_SESSIONDIR_LOGDIR_NAME, nodeID, filename)
	reader := s.reader.GetContent(clusterNameID, logPath)
	if reader == nil {
		resp.WriteErrorString(http.StatusNotFound, fmt.Sprintf("log file not found: %s", logPath))
		return
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		resp.WriteError(http.StatusInternalServerError, err)
		return
	}

	// Content-Disposition triggers browser download when download_filename is
	// set; backs Ray Dashboard's "download log" button.
	if df := req.QueryParameter("download_filename"); df != "" {
		disposition := mime.FormatMediaType("attachment", map[string]string{"filename": df})
		if disposition == "" {
			disposition = fmt.Sprintf(`attachment; filename=%q`, df)
		}
		resp.AddHeader("Content-Disposition", disposition)
	}
	resp.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := resp.Write(body); err != nil {
		logrus.Errorf("getNodeLogFile: write failed: %v", err)
	}
}

// getNodeLogStream returns 501 for dead clusters (snapshots are immutable;
// no tail). Live clusters are dispatched to Ray Dashboard upstream.
func (s *Server) getNodeLogStream(req *restful.Request, resp *restful.Response) {
	_, sessionName, _, ok := readSessionCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}
	resp.WriteErrorString(http.StatusNotImplemented,
		"log streaming only available for live clusters")
}
