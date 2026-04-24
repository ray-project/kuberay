// Package server contains the HTTP layer of the History Server v2 beta.
//
// handlers.go wires HTTP requests to snapshot reads and reuses the formatters
// from formatters.go to produce Ray Dashboard-compatible JSON responses.
//
// Handler dispatch pattern for every endpoint:
//  1. extractCookies — fail fast with 400 if any of the three cookies is missing.
//  2. If session_name == "live", defer to redirectRequest (live reverse proxy,
//     implemented in server.go).
//  3. Otherwise, loader.Load(clusterNameID, sessionName):
//     - ErrSnapshotNotFound → 503 + Retry-After: 600 (dead session whose
//     snapshot has not yet been written by the processor; retry in 10 min).
//     - other error         → 500 (unexpected).
//  4. Pull the relevant sub-map from the snapshot, apply query-param filters
//     that v1 supports, run formatters, and write the response envelope.
//
// Design note: Server.loader is typed as the small snapshotLoader interface
// rather than *SnapshotLoader so that tests can inject a fake. The concrete
// *SnapshotLoader from cache.go satisfies the interface.
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

// timelineNow is the current-time source used by getTasksTimeline's download
// filename. Pulled out as a variable so tests can freeze it — the production
// value is time.Now and does not need to be set.
var timelineNow = time.Now

// Cookie keys and the live-session sentinel. Values mirror the v1
// historyserver/pkg/historyserver/router.go constants (COOKIE_*). Keeping the
// same names on the wire lets the Ray Dashboard frontend switch between v1
// and v2 without cookie changes.
const (
	cookieClusterNameKey      = "cluster_name"
	cookieClusterNamespaceKey = "cluster_namespace"
	cookieSessionNameKey      = "session_name"
	// liveSessionSentinel is the cookie value the v1 listClusters writes for
	// live (still-running) clusters. Handlers proxy to Ray Dashboard for this
	// value instead of reading a snapshot.
	liveSessionSentinel = "live"

	// retryAfterSecondsOnMissingSnapshot is the Retry-After value (seconds)
	// returned to clients for dead-but-unsnapped sessions. 600s matches the
	// processor tick interval — by the next tick the snapshot is expected.
	retryAfterSecondsOnMissingSnapshot = "600"
)

// snapshotLoader is the narrow interface the handlers depend on. *SnapshotLoader
// (defined in cache.go) satisfies it. Tests inject fakes that track calls and
// return ErrSnapshotNotFound without touching storage.
type snapshotLoader interface {
	Load(clusterNameID, sessionName string) (*snapshot.SessionSnapshot, error)
}

// Server is the HTTP handler container for the v2 History Server.
//
// Fields:
//   - loader       : narrow snapshotLoader interface — tests can inject fakes.
//   - reader       : storage backend for endpoints that still read raw files
//     (e.g. getTimezone reads fetched_endpoints/ directly, mirroring v1).
//   - clientManager: v1 ClientManager — used by getClusters to enumerate live
//     RayClusters via its exported ListRayClusters method.
//   - proxyResolver: wired in main (Wave 4) to resolve head-service info and
//     the kube-apiserver host used by redirectRequest. Separated from
//     clientManager because v1 ClientManager's controller-runtime client is
//     private and this beta cannot modify v1 to expose it.
//   - dashboardDir : filesystem path to static Dashboard assets (reserved for
//     future homepage/index.html routes; unused by the current handler set).
//   - dashboardAddr: optional override. Empty means "derive from ClientManager".
//   - useKubeProxy : if true, proxy through the Kubernetes API server; else
//     connect directly through in-cluster service discovery.
//   - httpClient   : used by redirectRequest to forward proxied requests. Its
//     RoundTripper is kube-aware when useKubeProxy=true; main supplies the
//     configured client via SetHTTPClient before Run. Simple clients also
//     work (SetHTTPClient omitted) for direct DNS mode.
//   - httpServer   : populated by Run() so Shutdown() can close the listener.
type Server struct {
	loader        snapshotLoader
	supervisor    *Supervisor
	reader        storage.StorageReader
	clientManager *historyserver.ClientManager
	proxyResolver ProxyResolver
	dashboardDir  string
	dashboardAddr string
	useKubeProxy  bool
	httpClient    *http.Client
	httpServer    *http.Server
}

// NewServer creates a Server wired to a *SnapshotLoader and the dependencies
// required for live-session proxying and the dead-session file endpoints.
//
// The ProxyResolver and custom httpClient are injected separately (via
// SetProxyResolver / SetHTTPClient) because v1 ClientManager hides its
// controller-runtime fields — main.go builds the proxy wiring from a fresh
// rest.Config and calls the setters. See ProxyResolver's doc for rationale.
//
// When httpClient is nil after construction a plain default client is used,
// which is sufficient for in-cluster DNS mode.
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
		// supervisor is optional: tests that never exercise /enter_cluster
		// (e.g. legacy handler tests) leave it nil, in which case the
		// enterCluster handler falls back to the beta cookie-only behavior
		// — set the cookies and return 200 without blocking. WHY: beta-v2
		// keeps legacy tests green and also tolerates a degraded-mode
		// deployment where the processor path is disabled for debugging.
		supervisor:    supervisor,
		reader:        reader,
		clientManager: cm,
		dashboardDir:  dashboardDir,
		useKubeProxy:  useKubeProxy,
		// httpClient stays nil until SetHTTPClient is called; redirectRequest
		// short-circuits to 501 in that case — matching W6 test expectations.
	}
}

// SetHTTPClient wires the *http.Client redirectRequest uses to proxy live
// traffic. Intended to be called from main after constructing a kube-aware
// RoundTripper (see v1 NewServerHandler for reference).
func (s *Server) SetHTTPClient(c *http.Client) {
	s.httpClient = c
}

// newServerWithLoader is an internal constructor used by tests to inject a
// fake snapshotLoader. Production code uses NewServer.
func newServerWithLoader(loader snapshotLoader) *Server {
	return &Server{loader: loader}
}

// --- cookie & error helpers ---------------------------------------------------

// extractCookies reads the three cookies written by /enter_cluster (v1 sets
// them in router.go: cluster_name, cluster_namespace, session_name).
//
// Returns:
//   - clusterNameID = "{name}_{namespace}" (the on-disk folder prefix used by
//     storage.StorageReader.GetContent; matches v1 clusterNameID composition).
//   - sessionName   = the cookie value, or "live" for running clusters.
//   - ok == false if any cookie is missing (caller should 400).
func extractCookies(req *restful.Request) (clusterNameID, sessionName string, ok bool) {
	clusterNameCookie, err := req.Request.Cookie(cookieClusterNameKey)
	if err != nil {
		return "", "", false
	}
	clusterNamespaceCookie, err := req.Request.Cookie(cookieClusterNamespaceKey)
	if err != nil {
		return "", "", false
	}
	sessionNameCookie, err := req.Request.Cookie(cookieSessionNameKey)
	if err != nil {
		return "", "", false
	}
	if clusterNameCookie.Value == "" || clusterNamespaceCookie.Value == "" || sessionNameCookie.Value == "" {
		return "", "", false
	}
	return clusterNameCookie.Value + "_" + clusterNamespaceCookie.Value, sessionNameCookie.Value, true
}

// extractSessionKey returns (clusterNameID, sessionName, clusterSessionKey).
// clusterSessionKey is the "{name}_{ns}_{session}" form used by internal maps
// (snapshot.SessionKey and v1 ClusterTaskMap/ClusterActorMap keys).
func extractSessionKey(req *restful.Request) (clusterNameID, sessionName, clusterSessionKey string, ok bool) {
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

// writeMissingCookies centralizes the 400 response so every handler says the
// same thing. Body phrasing deliberately matches v1 CookieHandle for parity.
func writeMissingCookies(resp *restful.Response) {
	resp.WriteErrorString(http.StatusBadRequest,
		"required cookies missing: cluster_name, cluster_namespace, session_name")
}

// handleMissingSnapshot maps loader errors to HTTP responses.
//
//	ErrSnapshotNotFound → 503 Service Unavailable + Retry-After: 600
//	    (The cluster is dead but the processor has not yet written the
//	    snapshot; the frontend should retry after the next tick.)
//	other error         → 500 Internal Server Error.
//
// This is the single source of truth for that classification; every handler
// routes through it so behavior is consistent across endpoints.
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

// redirectRequest is implemented in server.go (W8). It reverse-proxies
// live-session requests to the Ray Dashboard on the head pod. Declared here
// only via the *Server method-set; the body lives in server.go so the HTTP
// lifecycle and proxy concerns are co-located.

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
	clusterNameID, sessionName, _, ok := extractSessionKey(req)
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

	// Envelope matches v1 RespTasksInfo exactly.
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
// []Task slice (one element per attempt), which is what ApplyTaskFilters and
// the formatters expect. Matches v1's GetTasks shape.
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

// getTaskSummarize implements summary_by=func_name (the default Dashboard path).
// summary_by=lineage requires utils.ToSummaryByLineage plus Actors; both are
// wired. Matches v1 response envelope.
func (s *Server) getTaskSummarize(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := extractSessionKey(req)
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
	// For summary, maximize entries to minimize data loss — mirrors v1.
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
// Timeline view. Logic ported from v1 EventHandler.GetTasksTimeline (see
// timeline.go:generateTimelineFromSnapshot for details). Response is a bare
// JSON array — the frontend parses it directly; no envelope.
//
// Query params:
//   - job_id  : optional filter; empty means all jobs in the session.
//   - download: "1" sets Content-Disposition so the browser saves the array
//     as a .json file. Filename includes job_id and the current timestamp,
//     matching v1.
func (s *Server) getTasksTimeline(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := extractSessionKey(req)
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

	// Download header mirrors v1 router.go:2080. The time layout is the same
	// 2006-01-02_15-04-05 pattern — changing it would break any tooling that
	// matches on filename.
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
	clusterNameID, sessionName, _, ok := extractSessionKey(req)
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

	// Keyed by hex actor ID to match v1 and Ray Dashboard frontend.
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
	clusterNameID, sessionName, _, ok := extractSessionKey(req)
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

	// Lookup logic mirrors v1 GetActorByID: direct base64 key, then fall back
	// to a linear scan converting each actor's base64 ID to hex for comparison.
	// The O(n) fallback is a known limitation; fix tracked in v1 PR #4563.
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
	clusterNameID, sessionName, _, ok := extractSessionKey(req)
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

	// v1 returns a bare JSON array (no envelope) for the jobs listing.
	formatted := make([]interface{}, 0, len(snap.Jobs))
	for _, job := range snap.Jobs {
		formatted = append(formatted, formatJobForResponse(job))
	}
	writeJSON(resp, formatted)
}

// --- getJob: GET /api/jobs/{job_id} ------------------------------------------

func (s *Server) getJob(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := extractSessionKey(req)
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
		// v1 writes plain text with 200 in this case; match it verbatim so the
		// frontend behaves the same way.
		if _, werr := resp.Write([]byte(fmt.Sprintf("Job %s does not exist", jobID))); werr != nil {
			logrus.Errorf("getJob: write not-found body failed: %v", werr)
		}
		return
	}
	writeJSON(resp, formatJobForResponse(job))
}

// --- getNodes: GET /nodes ----------------------------------------------------

// getNodes supports view=summary (default) and view=hostNameList, matching v1.
func (s *Server) getNodes(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := extractSessionKey(req)
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

// writeNodesSummary replicates v1 getNodesSummary against a snapshot node map.
// The response uses the latest state transition as the "current" snapshot for
// each node — for dead sessions that is the final state, which is what users
// of historical data care about.
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

// writeNodesHostNameList returns hostnames for ALIVE nodes. Fallback order
// matches v1: Hostname -> NodeName -> NodeID (Ray does not yet populate the
// first two; see ray-project/ray#60129).
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

// getNode returns a single node with its actors filled in (the Dashboard
// frontend renders actors on the NodeDetail page, so we filter snap.Actors
// by NodeID here — same pattern as v1).
func (s *Server) getNode(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := extractSessionKey(req)
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

	// Fill actors scheduled on this node (keyed by hex actor ID, matching v1).
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

// getEvents returns log events — either all events grouped by job_id (no
// job_id param) or a single job's events (job_id param present, possibly
// empty). The empty-job_id branch matches v1 which distinguishes "param not
// given" from "param given but empty".
func (s *Server) getEvents(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, _, ok := extractSessionKey(req)
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
// categorization. Snapshot-independent — reads directly from storage via
// ListFiles. Mirrors v1 pkg/historyserver/router.go:1066 + _getNodeLogs.
//
// Query params:
//   - node_id : hex node ID (subdirectory under logs/). Validated against
//     path-traversal (fs.ValidPath) — same guard as v1.
//   - glob    : double-star-capable glob pattern. When the pattern contains a
//     directory prefix (e.g. "logs/raylet*") it is split so the base directory
//     narrows the ListFiles call and the leaf pattern is matched per file.
//
// Errors:
//   - Missing cookies     → 400
//   - Live session        → proxy
//   - Path traversal      → 400
//   - Glob match failure  → 400
//   - Missing s.reader    → 500 (wiring bug; should be present in production)
//
// The response envelope matches v1 exactly: a "data.result" object keyed by
// component category (worker_out, worker_err, core_worker, driver, raylet,
// gcs_server, internal, autoscaler, agent, dashboard) — frontend keys on it.
func (s *Server) getNodeLogs(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, ok := extractCookies(req)
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

	// Split the glob pattern just like v1 — "logs/raylet*" becomes
	// base="logs", pattern="raylet*" so ListFiles operates on the narrower
	// directory and Match only considers leaf file names.
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

// listNodeLogFiles is the helper that does the storage reads and assembles the
// JSON body for getNodeLogs. Pulled out so it can be unit-tested without the
// HTTP layer. Port of v1 _getNodeLogs in pkg/historyserver/reader.go:73.
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

// listFilesRecursive walks a directory tree from s.reader, returning all leaf
// files with paths relative to the starting dir. Directories are identified
// by a trailing "/" in ListFiles output (storage backend convention).
// Ported from v1 ServerHandler.listFilesRecursive.
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

// categorizeLogFiles sorts log file names into Ray's component buckets. The
// order of the switch matters — "log_monitor" must be checked before "monitor"
// because the former contains the latter as a substring. Ported verbatim from
// v1 pkg/historyserver/reader.go:142.
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

// getClusterMetadata streams the body of fetched_endpoints/<key> verbatim to
// the caller. Snapshot-independent — the collector writes the Ray Dashboard's
// /api/v0/cluster_metadata response into this file when the cluster is alive;
// on replay we return it as-is. Mirrors v1 router.go:920.
func (s *Server) getClusterMetadata(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, ok := extractCookies(req)
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
// renders on the cluster-overview page. Mirrors v1 router.go:820.
//
// Two modes:
//   - format=1  → {"clusterStatus": "<Ray-formatted multi-line text>"} by
//     loading snap + per-node debug_state.txt and invoking the ported
//     builder in cluster_status.go. This is the mode the Dashboard uses.
//   - anything else → an empty ClusterStatusData envelope, matching v1.
//
// Snapshot + raw-file hybrid: the node counts come from debug_state.txt (for
// parity with live Ray output), while failed-nodes and pending-demands come
// from the snapshot (the only source for historical state transitions and
// task states in dead clusters).
func (s *Server) getClusterStatus(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, ok := extractCookies(req)
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
		// v1 parity: the empty envelope is what the frontend currently
		// ignores when format != "1". Do not pull the snapshot here.
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

// getNodeLog is the unified singular-log endpoint (GET /api/v0/logs/{media_type}),
// dispatching to a file-read or stream handler based on the path parameter.
// Matches the contract Ray Dashboard uses when the user clicks an individual
// log file. Plan §8 row: "/api/v0/logs/{media}".
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

// getNodeLogFile returns the content of a specific log file from object storage.
//
// Scope for PoC: requires `node_id` + `filename` query params (the simple case
// Ray Dashboard uses most). task_id / actor_id / pid-based resolution (which
// v1 supports) is left as a TODO — those need snapshot lookups plus the
// TaskLogInfo.{StdoutFile,StderrFile} fields that are only present for some
// task types. Callers passing only task_id/actor_id get a 400 with a clear
// message so the frontend can fall back to its own resolution.
func (s *Server) getNodeLogFile(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, ok := extractCookies(req)
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

	// v1 supports resolving node_id+filename from task_id / actor_id by looking
	// at TaskLogInfo on the stored task. PoC punts: tell the caller to resolve
	// client-side (Ray Dashboard already has this code path).
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

	// Content-Disposition to trigger browser download when caller passes
	// download_filename. Mirrors v1 getNodeLogFile behavior so Ray Dashboard's
	// "download log" button continues to work.
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

// getNodeLogStream: Ray Dashboard uses this for live tail via SSE. For dead
// clusters we have no tail (snapshot is immutable) — return 501 with a clear
// message that streaming is only meaningful on live clusters. For live
// clusters the cookie dispatch above already forwards to Ray Dashboard.
func (s *Server) getNodeLogStream(req *restful.Request, resp *restful.Response) {
	_, sessionName, ok := extractCookies(req)
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
