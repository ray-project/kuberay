// Package server — HTTP route registration for the v2 beta History Server.
//
// RegisterRouter wires each Ray Dashboard-facing URL to a method on *Server.
// The shape mirrors v1 pkg/historyserver/router.go's RegisterRouter: same
// paths, same content types, same cookie-required set. Two deliberate
// differences from v1:
//
//  1. Scoped *restful.Container. v1 calls the global restful.Add(ws). We
//     take a container argument so Run() can give us a fresh container and
//     tests can introspect container.RegisteredWebServices() without global
//     state bleed between parallel test runs.
//
//  2. No v1 CookieHandle filter. The beta handlers extract cookies directly
//     (see handlers.go extractCookies) — that is simpler, removes the
//     per-request k8s lookup v1 does on every request, and keeps the route
//     definitions terse. The v1 side-effect of refreshing cookie MaxAge=600
//     on each response is lost, but the frontend re-enters /enter_cluster
//     whenever the user switches clusters, so practical cookie lifetime is
//     the same.
//
// The route table below mirrors implementation_plan §8 endpoint 對應表.
package server

import (
	"net/http"
	"time"

	restful "github.com/emicklei/go-restful/v3"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/metrics"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// RegisterRouter adds the v2 beta route set to container. Call exactly once
// per container. Mirrors v1's RegisterRouter in pkg/historyserver/router.go:318.
func (s *Server) RegisterRouter(container *restful.Container) {
	s.registerEnterCluster(container) // /enter_cluster/{ns}/{name}/{session}
	s.registerClusters(container)     // /clusters
	s.registerTimezone(container)     // /timezone
	s.registerNodes(container)        // /nodes, /nodes/{node_id}
	s.registerEvents(container)       // /events
	s.registerAPI(container)          // /api/...
	s.registerLogical(container)      // /logical/actors[/{single_actor}]
	s.registerHealthz(container)      // /readz, /livez
	s.registerMetrics(container)      // /metrics (Prometheus exposition)
}

// --- /enter_cluster ----------------------------------------------------------

// registerEnterCluster is the cookie-setter endpoint. Visiting
// /enter_cluster/{namespace}/{name}/{session} writes the three cookies the
// rest of the API relies on, then returns a simple JSON ack. Mirrors v1
// routerRayClusterSet.
//
// MaxAge=600 matches v1; callers are expected to return to this endpoint
// periodically (or every time the active cluster changes).
func (s *Server) registerEnterCluster(container *restful.Container) {
	ws := new(restful.WebService)
	ws.Path("/enter_cluster").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON)

	ws.Route(ws.GET("/{namespace}/{name}/{session}").To(s.enterCluster).
		Doc("set cookies for a (namespace, name, session) tuple").
		Param(ws.PathParameter("namespace", "cluster namespace")).
		Param(ws.PathParameter("name", "cluster name")).
		Param(ws.PathParameter("session", "session name, or 'live' for a running cluster")).
		Writes(""))

	container.Add(ws)
}

// enterCluster writes the three cookies and — for dead sessions in lazy
// mode — blocks until a snapshot is available before acknowledging.
//
// Flow (lazy mode, beta-v2):
//  1. Write cookies unconditionally so the frontend can always proceed.
//  2. If session == "live" OR supervisor is nil, return 200 immediately
//     (same shape as beta). WHY the nil check: handler tests build a
//     Server without a Supervisor, and a "degraded mode" deployment that
//     disables the processor path for debugging is still useful.
//  3. Otherwise, call Supervisor.Ensure with the request ctx. This blocks
//     until the snapshot is cached / fetched / built. On success return
//     200; on error return 500 with the error body.
//
// The body format intentionally matches v1 so the Dashboard frontend can
// parse it identically.
func (s *Server) enterCluster(req *restful.Request, resp *restful.Response) {
	name := req.PathParameter("name")
	namespace := req.PathParameter("namespace")
	session := req.PathParameter("session")

	writeEnterClusterCookies(resp, name, namespace, session)

	if session == liveSessionSentinel || s.supervisor == nil {
		// Degraded / live path: no snapshot work to do.
		writeEnterClusterAck(resp, name, namespace, session)
		return
	}

	// Blocking path. WHY the histogram observation wraps Ensure: operators
	// need to see how often we hit the slow Pipeline path vs. the fast LRU
	// path; duration is the cleanest single signal.
	info := utils.ClusterInfo{Name: name, Namespace: namespace, SessionName: session}
	start := time.Now()
	err := s.supervisor.Ensure(req.Request.Context(), info)
	metrics.EnterClusterDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.EnterClusterTotal.WithLabelValues("error").Inc()
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	metrics.EnterClusterTotal.WithLabelValues("ok").Inc()
	writeEnterClusterAck(resp, name, namespace, session)
}

// writeEnterClusterCookies sets the three session cookies the rest of the
// API relies on. Extracted from the handler body so enterCluster stays
// readable and the cookie-writing is trivially reusable across the
// blocking / non-blocking branches.
//
// MaxAge=600 matches v1; the frontend re-enters /enter_cluster whenever the
// active cluster changes so effective lifetime is driven by UX, not TTL.
func writeEnterClusterCookies(resp *restful.Response, name, namespace, session string) {
	const cookieMaxAgeSeconds = 600
	http.SetCookie(resp, &http.Cookie{
		MaxAge: cookieMaxAgeSeconds, Path: "/",
		Name: cookieClusterNameKey, Value: name,
	})
	http.SetCookie(resp, &http.Cookie{
		MaxAge: cookieMaxAgeSeconds, Path: "/",
		Name: cookieClusterNamespaceKey, Value: namespace,
	})
	http.SetCookie(resp, &http.Cookie{
		MaxAge: cookieMaxAgeSeconds, Path: "/",
		Name: cookieSessionNameKey, Value: session,
	})
}

// writeEnterClusterAck writes the v1-compatible JSON ack body.
func writeEnterClusterAck(resp *restful.Response, name, namespace, session string) {
	if err := resp.WriteAsJson(map[string]interface{}{
		"result":    "success",
		"name":      name,
		"namespace": namespace,
		"session":   session,
	}); err != nil {
		logrus.Errorf("enterCluster: WriteAsJson failed: %v", err)
	}
}

// --- /clusters ---------------------------------------------------------------

func (s *Server) registerClusters(container *restful.Container) {
	ws := new(restful.WebService)
	ws.Path("/clusters").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON)
	ws.Route(ws.GET("/").To(s.getClusters).
		Doc("list all known clusters (live + dead)").
		Writes([]string{}))
	container.Add(ws)
}

// --- /timezone ---------------------------------------------------------------

func (s *Server) registerTimezone(container *restful.Container) {
	ws := new(restful.WebService)
	ws.Path("/timezone").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON)
	ws.Route(ws.GET("/").To(s.getTimezone).
		Doc("return the session timezone metadata").
		Writes(""))
	container.Add(ws)
}

// --- /nodes ------------------------------------------------------------------

// registerNodes sets up /nodes and /nodes/{node_id}. view=summary (default)
// returns node summaries; view=hostNameList returns alive hostnames — both
// paths are dispatched from getNodes itself.
func (s *Server) registerNodes(container *restful.Container) {
	ws := new(restful.WebService)
	ws.Path("/nodes").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON)

	ws.Route(ws.GET("").To(s.getNodes).
		Doc("list node information for the active session").
		Param(ws.QueryParameter("view",
			"'summary' (default) for summaries+resources, 'hostNameList' for alive hostnames")).
		Writes(""))

	ws.Route(ws.GET("/{node_id}").To(s.getNode).
		Doc("fetch one node by its ID, with scheduled actors inlined").
		Param(ws.PathParameter("node_id", "the unique node identifier")).
		Writes(""))

	container.Add(ws)
}

// --- /events -----------------------------------------------------------------

func (s *Server) registerEvents(container *restful.Container) {
	ws := new(restful.WebService)
	ws.Path("/events").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON)
	ws.Route(ws.GET("/").To(s.getEvents).
		Doc("all events grouped by job, or the events of a single job_id").
		Param(ws.QueryParameter("job_id", "optional job_id filter")).
		Writes(""))
	container.Add(ws)
}

// --- /api --------------------------------------------------------------------

// registerAPI wires the Ray Dashboard /api/* endpoints:
//   - /api/cluster_status, /api/grafana_health, /api/prometheus_health: stubs.
//   - /api/jobs/ + /api/jobs/{job_id}: snapshot-backed.
//   - /api/v0/cluster_metadata, /api/v0/logs: stubs (file-based; W7+).
//   - /api/v0/tasks, /api/v0/tasks/summarize: snapshot-backed.
//   - /api/v0/tasks/timeline: stub (Wave 3).
//
// Order matters: go-restful matches more specific routes first — registering
// /v0/tasks before /v0/tasks/summarize would otherwise shadow the summarize
// route.
func (s *Server) registerAPI(container *restful.Container) {
	ws := new(restful.WebService)
	ws.Path("/api").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON)

	// Cluster-level stubs — handler returns 501.
	ws.Route(ws.GET("/cluster_status").To(s.getClusterStatus).
		Doc("(stub) cluster status — not yet implemented for snapshot mode").
		Writes(""))

	// Jobs.
	ws.Route(ws.GET("/jobs/").To(s.getJobs).
		Doc("list jobs for the active session").
		Writes(""))
	ws.Route(ws.GET("/jobs/{job_id}").To(s.getJob).
		Doc("fetch a single job by job_id").
		Param(ws.PathParameter("job_id", "the job_id returned by the Ray jobs API")).
		Writes(""))

	// v0 endpoints.
	ws.Route(ws.GET("/v0/cluster_metadata").To(s.getClusterMetadata).
		Doc("(stub) cluster metadata — not yet implemented for snapshot mode").
		Writes(""))

	ws.Route(ws.GET("/v0/logs").To(s.getNodeLogs).
		Doc("list log files for a node").
		Param(ws.QueryParameter("node_id", "node_id")).
		Param(ws.QueryParameter("glob", "glob pattern")).
		Writes(""))

	// Singular /v0/logs/{media_type} dispatches to file content or streaming.
	// Matches Ray Dashboard's call when user clicks an individual log file.
	ws.Route(ws.GET("/v0/logs/{media_type}").To(s.getNodeLog).
		Doc("fetch a specific log file (media_type=file) or stream (media_type=stream, live only)").
		Param(ws.PathParameter("media_type", "media type: 'file' for log content, 'stream' for SSE (live only)")).
		Param(ws.QueryParameter("node_id", "node_id")).
		Param(ws.QueryParameter("filename", "log file name under logs/{node_id}/")).
		Param(ws.QueryParameter("download_filename", "triggers download with given filename (Content-Disposition)")).
		Writes(""))

	ws.Route(ws.GET("/v0/tasks").To(s.getTasks).
		Doc("list tasks with State API-compatible filtering").
		Param(ws.QueryParameter("limit", "max rows to return")).
		Param(ws.QueryParameter("timeout", "request timeout (seconds)")).
		Param(ws.QueryParameter("detail", "set to true for full task detail")).
		Param(ws.QueryParameter("exclude_driver", "exclude driver tasks (default true)")).
		Param(ws.QueryParameter("filter_keys", "filter keys")).
		Param(ws.QueryParameter("filter_predicates", "filter predicates")).
		Param(ws.QueryParameter("filter_values", "filter values")).
		Writes(""))

	ws.Route(ws.GET("/v0/tasks/summarize").To(s.getTaskSummarize).
		Doc("summarize tasks by func_name (default) or lineage").
		Param(ws.QueryParameter("filter_keys", "filter keys")).
		Param(ws.QueryParameter("filter_predicates", "filter predicates")).
		Param(ws.QueryParameter("filter_values", "filter values")).
		Param(ws.QueryParameter("summary_by", "'func_name' (default) or 'lineage'")).
		Writes(""))

	ws.Route(ws.GET("/v0/tasks/timeline").To(s.getTasksTimeline).
		Doc("(stub) Chrome-trace tasks timeline — pending Wave 3").
		Param(ws.QueryParameter("job_id", "filter by job_id")).
		Param(ws.QueryParameter("download", "set to 1 to attach response as a file")).
		Produces(restful.MIME_JSON).
		Writes(""))

	container.Add(ws)
}

// --- /logical ----------------------------------------------------------------

// registerLogical registers the actor endpoints. Dashboard frontend calls
// GET /logical/actors with no filters and filters client-side, so no filter
// query params are exposed here. Mirrors v1 routerLogical.
func (s *Server) registerLogical(container *restful.Container) {
	ws := new(restful.WebService)
	ws.Path("/logical").Consumes(restful.MIME_JSON).Produces(restful.MIME_JSON)

	ws.Route(ws.GET("/actors").To(s.getLogicalActors).
		Doc("list all actors for the active session, keyed by hex actor ID").
		Writes(""))

	// single_actor is a greedy wildcard because Base64 IDs may contain "/" —
	// same rationale as v1 (see the extended comment in v1 routerLogical).
	ws.Route(ws.GET("/actors/{single_actor:*}").To(s.getLogicalActor).
		Doc("fetch a single actor — actorID may be base64 or hex").
		Param(ws.PathParameter("single_actor", "hex or base64 actor ID")).
		Writes(""))

	container.Add(ws)
}

// --- /readz + /livez ---------------------------------------------------------

// registerHealthz provides the liveness/readiness endpoints. These are plain
// HTTP (not JSON) to match common Kubernetes probe conventions and the v1
// implementation.
func (s *Server) registerHealthz(container *restful.Container) {
	ws := new(restful.WebService)
	// No Consumes/Produces JSON here — we return text/plain.
	ws.Route(ws.GET("/readz").To(writeHealthOK).Doc("readiness probe"))
	ws.Route(ws.GET("/livez").To(writeHealthOK).Doc("liveness probe"))
	container.Add(ws)
}

func writeHealthOK(_ *restful.Request, resp *restful.Response) {
	resp.Header().Set("Content-Type", "text/plain")
	if _, err := resp.Write([]byte("ok")); err != nil {
		logrus.Errorf("writeHealthOK: write failed: %v", err)
	}
}

// --- /metrics ----------------------------------------------------------------

// registerMetrics exposes Prometheus exposition at /metrics on the same HTTP
// listener. We bridge promhttp into go-restful by calling ServeHTTP directly —
// no content-type advertisement (Produces) because promhttp sets its own
// text/plain; version=... header per the exposition spec.
//
// Keeping /metrics on the same port as the API avoids a second Service port
// in the HS Deployment; the eventprocessor binary uses a dedicated sidecar
// listener on :9090 because it has no main HTTP server.
func (s *Server) registerMetrics(container *restful.Container) {
	ws := new(restful.WebService)
	ws.Path("/metrics")
	ws.Route(ws.GET("").To(func(req *restful.Request, resp *restful.Response) {
		metrics.Handler().ServeHTTP(resp.ResponseWriter, req.Request)
	}).Doc("Prometheus scrape endpoint"))
	container.Add(ws)
}
