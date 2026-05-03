// Tests for handlers.go. Each test builds a minimal *restful.Request (with
// cookies already populated — handlers extract cookies themselves, bypassing
// v1's CookieHandle filter) and asserts the HTTP response from a fake
// snapshotLoader. No real HTTP listener or storage is spun up.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emicklei/go-restful/v3"

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/snapshot"
	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
)

// --- fake snapshotLoader ------------------------------------------------------

// fakeLoader lets tests control what Load returns without touching storage.
// It records every call so tests can assert the loader was (or was not) invoked.
type fakeLoader struct {
	snap *snapshot.SessionSnapshot
	err  error
	// calls records each (clusterNameID, sessionName) pair that Load saw, in order.
	calls [][2]string
}

func (f *fakeLoader) Load(clusterNameID, sessionName string) (*snapshot.SessionSnapshot, error) {
	f.calls = append(f.calls, [2]string{clusterNameID, sessionName})
	return f.snap, f.err
}

// --- request builder ----------------------------------------------------------

// newReqWithCookies constructs a *restful.Request with the three cookies
// pre-populated. urlStr may include a query string; pathParams populate
// PathParameter (e.g., {"job_id": "j-1"}).
func newReqWithCookies(t *testing.T, clusterName, namespace, sessionName, urlStr string, pathParams map[string]string) *restful.Request {
	t.Helper()
	httpReq := httptest.NewRequest(http.MethodGet, urlStr, nil)
	if clusterName != "" {
		httpReq.AddCookie(&http.Cookie{Name: cookieClusterNameKey, Value: clusterName})
	}
	if namespace != "" {
		httpReq.AddCookie(&http.Cookie{Name: cookieClusterNamespaceKey, Value: namespace})
	}
	if sessionName != "" {
		httpReq.AddCookie(&http.Cookie{Name: cookieSessionNameKey, Value: sessionName})
	}
	req := restful.NewRequest(httpReq)
	for k, v := range pathParams {
		req.PathParameters()[k] = v
	}
	return req
}

// newResp wraps an httptest.ResponseRecorder in a *restful.Response.
func newResp() (*restful.Response, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	return restful.NewResponse(rec), rec
}

// --- fixtures -----------------------------------------------------------------

// fullSnapshot builds a small but meaningfully populated SessionSnapshot.
// Uses raw (base64-style) IDs like v1 does internally.
func fullSnapshot() *snapshot.SessionSnapshot {
	// Two task attempts for the same task — getTasks should flatten to 2.
	task1a := eventtypes.Task{
		TaskID:      "task-1",
		TaskAttempt: 0,
		State:       eventtypes.RUNNING,
		JobID:       "job-1",
		TaskType:    eventtypes.NORMAL_TASK,
	}
	task1b := eventtypes.Task{
		TaskID:      "task-1",
		TaskAttempt: 1,
		State:       eventtypes.FINISHED,
		JobID:       "job-1",
		TaskType:    eventtypes.NORMAL_TASK,
	}
	task2 := eventtypes.Task{
		TaskID:      "task-2",
		TaskAttempt: 0,
		State:       eventtypes.RUNNING,
		JobID:       "job-2",
		TaskType:    eventtypes.NORMAL_TASK,
	}

	actor1 := eventtypes.Actor{
		ActorID: "actor-1",
		JobID:   "job-1",
		State:   eventtypes.ALIVE,
		Name:    "Worker",
	}
	actor2 := eventtypes.Actor{
		ActorID: "actor-2",
		JobID:   "job-2",
		State:   eventtypes.DEAD,
	}

	job1 := eventtypes.Job{
		JobID:      "job-1",
		EntryPoint: "python main.py",
	}

	node1 := eventtypes.Node{
		NodeID:        "node-1",
		NodeIPAddress: "10.0.0.1",
		StateTransitions: []eventtypes.NodeStateTransition{
			{State: eventtypes.NODE_ALIVE, Timestamp: time.Unix(1000, 0)},
		},
	}

	return &snapshot.SessionSnapshot{
		SessionKey:  "c1_ns1_session_1",
		GeneratedAt: time.Unix(2000, 0),
		Tasks: map[string][]eventtypes.Task{
			"task-1": {task1a, task1b},
			"task-2": {task2},
		},
		Actors: map[string]eventtypes.Actor{
			"actor-1": actor1,
			"actor-2": actor2,
		},
		Jobs: map[string]eventtypes.Job{
			"job-1": job1,
		},
		Nodes: map[string]eventtypes.Node{
			"node-1": node1,
		},
		LogEvents: snapshot.LogEventPayload{
			ByJobID: map[string][]eventtypes.LogEvent{
				"job-1": {
					{EventID: "ev-1", SourceType: "CORE_WORKER", Message: "hello"},
				},
			},
		},
	}
}

// --- tests --------------------------------------------------------------------

// Test 1: cookie session_name=="live" must short-circuit to the redirect stub
// (501 in W6). Critically, the loader must not be invoked.
func TestGetTasks_LiveSessionProxiesWithoutLoad(t *testing.T) {
	loader := &fakeLoader{}
	s := newServerWithLoader(loader)

	req := newReqWithCookies(t, "c1", "ns1", liveSessionSentinel, "/api/v0/tasks", nil)
	resp, rec := newResp()

	s.getTasks(req, resp)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 from live-proxy stub, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if len(loader.calls) != 0 {
		t.Fatalf("expected loader not to be invoked on live path, got %d calls", len(loader.calls))
	}
}

// Test 2: with a populated snapshot, getTasks returns 200 + both attempts of
// task-1 plus task-2 (3 entries total after the default exclude_driver filter).
func TestGetTasks_ReturnsTasksFromSnapshot(t *testing.T) {
	loader := &fakeLoader{snap: fullSnapshot()}
	s := newServerWithLoader(loader)

	req := newReqWithCookies(t, "c1", "ns1", "session_1", "/api/v0/tasks", nil)
	resp, rec := newResp()

	s.getTasks(req, resp)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	// Confirm loader saw the cookie-derived identifiers.
	if got := len(loader.calls); got != 1 {
		t.Fatalf("expected exactly 1 loader call, got %d", got)
	}
	if loader.calls[0] != [2]string{"c1_ns1", "session_1"} {
		t.Fatalf("unexpected loader args: %v", loader.calls[0])
	}

	// Sanity-check envelope shape and task count.
	var payload struct {
		Result bool `json:"result"`
		Data   struct {
			Result struct {
				Result []map[string]interface{} `json:"result"`
				Total  int                      `json:"total"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.Result {
		t.Fatalf("expected result=true")
	}
	if payload.Data.Result.Total != 3 {
		t.Fatalf("expected total=3 (2 attempts for task-1 + 1 for task-2), got %d", payload.Data.Result.Total)
	}
	// Each task's task_id should be one of the IDs we put in.
	sawTask1 := 0
	sawTask2 := 0
	for _, entry := range payload.Data.Result.Result {
		switch entry["task_id"] {
		case "task-1":
			sawTask1++
		case "task-2":
			sawTask2++
		}
	}
	if sawTask1 != 2 || sawTask2 != 1 {
		t.Fatalf("expected 2 attempts for task-1 and 1 for task-2, got %d and %d", sawTask1, sawTask2)
	}
}

// Test 3: missing snapshot -> 503 + Retry-After: 600. This is the core "dead
// but not-yet-snapshotted" contract from implementation_plan §Phase 4.3.
func TestGetTasks_MissingSnapshotReturns503WithRetryAfter(t *testing.T) {
	loader := &fakeLoader{err: ErrSnapshotNotFound}
	s := newServerWithLoader(loader)

	req := newReqWithCookies(t, "c1", "ns1", "session_1", "/api/v0/tasks", nil)
	resp, rec := newResp()

	s.getTasks(req, resp)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "600" {
		t.Fatalf("expected Retry-After=600, got %q", got)
	}
}

// Test 4: no cookies at all -> 400. All handlers must reject this uniformly.
func TestGetTasks_MissingCookiesReturns400(t *testing.T) {
	loader := &fakeLoader{}
	s := newServerWithLoader(loader)

	// No cookies attached — newReqWithCookies with empty strings skips them.
	req := newReqWithCookies(t, "", "", "", "/api/v0/tasks", nil)
	resp, rec := newResp()

	s.getTasks(req, resp)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if len(loader.calls) != 0 {
		t.Fatalf("expected loader not to be invoked when cookies are missing, got %d calls", len(loader.calls))
	}
}

// Test 5: single-actor lookup hits both the found and not-found branches.
func TestGetLogicalActor_FoundAndNotFound(t *testing.T) {
	loader := &fakeLoader{snap: fullSnapshot()}
	s := newServerWithLoader(loader)

	// --- existing actor-1 ---
	req := newReqWithCookies(t, "c1", "ns1", "session_1",
		"/logical/actors/actor-1",
		map[string]string{"single_actor": "actor-1"})
	resp, rec := newResp()
	s.getLogicalActor(req, resp)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	var okPayload struct {
		Result bool   `json:"result"`
		Msg    string `json:"msg"`
		Data   struct {
			Detail map[string]interface{} `json:"detail"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &okPayload); err != nil {
		t.Fatalf("unmarshal found response: %v", err)
	}
	if !okPayload.Result {
		t.Fatalf("expected result=true for existing actor, body=%q", rec.Body.String())
	}
	if okPayload.Msg != "Actor fetched." {
		t.Fatalf("unexpected msg: %q", okPayload.Msg)
	}
	if _, has := okPayload.Data.Detail["actorId"]; !has {
		t.Fatalf("expected detail.actorId in response, got %v", okPayload.Data.Detail)
	}

	// --- missing actor-999 ---
	req2 := newReqWithCookies(t, "c1", "ns1", "session_1",
		"/logical/actors/actor-999",
		map[string]string{"single_actor": "actor-999"})
	resp2, rec2 := newResp()
	s.getLogicalActor(req2, resp2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 even for missing actor (matches v1), got %d", rec2.Code)
	}
	var missPayload struct {
		Result bool   `json:"result"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &missPayload); err != nil {
		t.Fatalf("unmarshal miss response: %v", err)
	}
	if missPayload.Result {
		t.Fatalf("expected result=false for missing actor")
	}
	if missPayload.Msg != "Actor not found." {
		t.Fatalf("unexpected miss msg: %q", missPayload.Msg)
	}
}

// --- bonus sanity test: non-NotFound loader error maps to 500 -----------------

func TestHandleMissingSnapshot_OtherErrorReturns500(t *testing.T) {
	loader := &fakeLoader{err: errors.New("disk on fire")}
	s := newServerWithLoader(loader)

	req := newReqWithCookies(t, "c1", "ns1", "session_1", "/api/v0/tasks", nil)
	resp, rec := newResp()

	s.getTasks(req, resp)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for non-NotFound error, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "disk on fire") {
		t.Fatalf("expected body to echo underlying error, got %q", rec.Body.String())
	}
}

// --- W11: getTasksTimeline ---------------------------------------------------

// TestGetTasksTimeline_ReturnsChromeTraceEvents verifies that with a single
// task carrying one profile event:
//  1. The response is a JSON array (no envelope).
//  2. The array contains a process_name metadata event, a thread_name
//     metadata event, and one "X"-phase trace event.
func TestGetTasksTimeline_ReturnsChromeTraceEvents(t *testing.T) {
	task := eventtypes.Task{
		TaskID:      "task-x",
		TaskAttempt: 0,
		JobID:       "job-x",
		ProfileData: &eventtypes.ProfileData{
			ComponentID:   "worker-1",
			ComponentType: "worker",
			NodeIPAddress: "10.0.0.7",
			Events: []eventtypes.ProfileEventRaw{
				{
					// "task::" (double colon) prefix triggers the extraData
					// "name" override — that is the v1 code path we want to
					// exercise. "task:execute" (single colon) is a different
					// Ray event family whose display name is not overridden.
					EventName: "task::my_task",
					StartTime: 1_000_000, // 1000us
					EndTime:   2_000_000, // 2000us
					ExtraData: `{"task_id":"task-x-hex","name":"my_task"}`,
				},
			},
		},
	}
	snap := &snapshot.SessionSnapshot{
		Tasks: map[string][]eventtypes.Task{
			"task-x": {task},
		},
	}
	loader := &fakeLoader{snap: snap}
	s := newServerWithLoader(loader)

	req := newReqWithCookies(t, "c1", "ns1", "session_1",
		"/api/v0/tasks/timeline", nil)
	resp, rec := newResp()

	s.getTasksTimeline(req, resp)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}

	// Response must be a bare JSON array (no envelope).
	var events []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("unmarshal response as array: %v (body=%q)", err, rec.Body.String())
	}

	// Expect exactly 3 events: process_name, thread_name, trace event.
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}

	var sawProcessName, sawThreadName, sawTrace bool
	for _, ev := range events {
		switch ev["ph"] {
		case "M":
			if ev["name"] == "process_name" {
				sawProcessName = true
			} else if ev["name"] == "thread_name" {
				sawThreadName = true
			}
		case "X":
			sawTrace = true
			// The trace event should use the override display name from
			// extraData, not the raw event name.
			if ev["name"] != "my_task" {
				t.Errorf("expected trace name to be 'my_task' (from extraData), got %v", ev["name"])
			}
			// Category should still be the raw event name.
			if ev["cat"] != "task::my_task" {
				t.Errorf("expected cat='task::my_task', got %v", ev["cat"])
			}
		}
	}
	if !sawProcessName {
		t.Errorf("expected a process_name metadata event; events=%+v", events)
	}
	if !sawThreadName {
		t.Errorf("expected a thread_name metadata event; events=%+v", events)
	}
	if !sawTrace {
		t.Errorf("expected one X-phase trace event; events=%+v", events)
	}
}

// --- W11: getNodeLogs --------------------------------------------------------

// nodeLogsFakeReader extends fakeStorageReader with a controllable
// ListFiles response. The embedded reader still handles GetContent for the
// cluster_status / cluster_metadata tests.
type nodeLogsFakeReader struct {
	*fakeStorageReader
	// files maps "clusterID|dir" to the list of entries ListFiles returns.
	files map[string][]string
}

func newNodeLogsFakeReader() *nodeLogsFakeReader {
	return &nodeLogsFakeReader{
		fakeStorageReader: newFakeStorageReader(),
		files:             map[string][]string{},
	}
}

func (f *nodeLogsFakeReader) ListFiles(clusterID, dir string) []string {
	return f.files[clusterID+"|"+dir]
}

func (f *nodeLogsFakeReader) putListFiles(clusterID, dir string, entries []string) {
	f.files[clusterID+"|"+dir] = entries
}

// TestGetNodeLogs_ReturnsCategorizedFiles verifies the happy path: the handler
// lists files under the node's log dir, categorizes them per Ray's convention
// (worker_out / raylet / etc.), and wraps the result in v1's envelope.
func TestGetNodeLogs_ReturnsCategorizedFiles(t *testing.T) {
	loader := &fakeLoader{}
	s := newServerWithLoader(loader)
	r := newNodeLogsFakeReader()
	s.reader = r

	// Path must match v1's convention: {session}/logs/{node_id}
	const clusterID = "c1_ns1"
	logDir := "session_1/logs/node-abc"
	r.putListFiles(clusterID, logDir, []string{
		"raylet.out",
		"worker-aaa.out",
		"gcs_server.err", // "gcs_server." prefix categorizes as gcs_server
	})

	req := newReqWithCookies(t, "c1", "ns1", "session_1",
		"/api/v0/logs?node_id=node-abc", nil)
	req.Request.URL.RawQuery = "node_id=node-abc"
	resp, rec := newResp()

	s.getNodeLogs(req, resp)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}

	var payload struct {
		Result bool `json:"result"`
		Data   struct {
			Result map[string][]string `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v (body=%q)", err, rec.Body.String())
	}
	if !payload.Result {
		t.Fatalf("expected result=true, got %+v", payload)
	}

	// Each category must contain the expected entry.
	assertCategoryContains(t, payload.Data.Result, "raylet", "raylet.out")
	assertCategoryContains(t, payload.Data.Result, "worker_out", "worker-aaa.out")
	assertCategoryContains(t, payload.Data.Result, "gcs_server", "gcs_server.err")
}

// assertCategoryContains verifies that the given category exists and includes
// the expected filename. Emits a descriptive failure when either is missing.
func assertCategoryContains(t *testing.T, categorized map[string][]string, category, filename string) {
	t.Helper()
	entries, ok := categorized[category]
	if !ok {
		t.Fatalf("expected category %q in response, got keys %v", category, keysOfStringsMap(categorized))
	}
	for _, e := range entries {
		if e == filename {
			return
		}
	}
	t.Fatalf("expected category %q to contain %q, got %v", category, filename, entries)
}

func keysOfStringsMap(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// --- W11: getClusterMetadata -------------------------------------------------

// TestGetClusterMetadata_StreamsStoredBody verifies the handler reads
// fetched_endpoints/restful__api__v0__cluster_metadata verbatim and writes the
// bytes back to the client without transformation.
func TestGetClusterMetadata_StreamsStoredBody(t *testing.T) {
	const stored = `{"rayVersion":"2.50.0","pythonVersion":"3.11"}`
	storeReader := newFakeStorageReader()
	// "restful__api__v0__cluster_metadata" is what
	// utils.EndpointPathToStorageKey("/api/v0/cluster_metadata") returns.
	storeReader.put("c1_ns1",
		"session_1/fetched_endpoints/restful__api__v0__cluster_metadata",
		[]byte(stored))

	s := newServerWithLoader(&fakeLoader{})
	s.reader = storeReader

	req := newReqWithCookies(t, "c1", "ns1", "session_1",
		"/api/v0/cluster_metadata", nil)
	resp, rec := newResp()

	s.getClusterMetadata(req, resp)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != stored {
		t.Fatalf("expected body %q, got %q", stored, body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %q", ct)
	}
}

// TestGetClusterMetadata_ReturnsNotFoundWhenMissing verifies the 404 branch:
// when the file doesn't exist on storage the handler responds 404 rather than
// a generic 500.
func TestGetClusterMetadata_ReturnsNotFoundWhenMissing(t *testing.T) {
	s := newServerWithLoader(&fakeLoader{})
	s.reader = newFakeStorageReader() // empty store

	req := newReqWithCookies(t, "c1", "ns1", "session_1",
		"/api/v0/cluster_metadata", nil)
	resp, rec := newResp()

	s.getClusterMetadata(req, resp)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on missing metadata, got %d (body=%q)", rec.Code, rec.Body.String())
	}
}

// --- W11: getClusterStatus ---------------------------------------------------

// TestGetClusterStatus_Format1BuildsFromDebugStateAndSnapshot verifies the
// format=1 branch: the handler lists node IDs from storage, parses each
// debug_state.txt, merges failed-node info from snap.Nodes, and renders the
// Ray-compatible autoscaler status block.
func TestGetClusterStatus_Format1BuildsFromDebugStateAndSnapshot(t *testing.T) {
	// Snapshot with one DEAD node to exercise FailedNodes rendering.
	snap := &snapshot.SessionSnapshot{
		Nodes: map[string]eventtypes.Node{
			"n1": {
				NodeID:        "n1",
				NodeIPAddress: "10.0.0.1",
				Labels:        map[string]string{"ray.io/node-group": "workers"},
				StateTransitions: []eventtypes.NodeStateTransition{
					{
						State:     eventtypes.NODE_DEAD,
						Timestamp: time.Unix(1700000000, 0),
						DeathInfo: &eventtypes.NodeDeathInfo{Reason: eventtypes.UNEXPECTED_TERMINATION},
					},
				},
			},
		},
	}
	loader := &fakeLoader{snap: snap}
	s := newServerWithLoader(loader)

	r := newNodeLogsFakeReader()
	s.reader = r

	// One node directory named "node-xyz". debug_state.txt parses is_idle: 1 and
	// a CPU/memory total block so the resource section has non-empty data.
	const clusterID = "c1_ns1"
	r.putListFiles(clusterID, "session_1/logs", []string{"node-xyz"})

	debugState := `Node ID: node-xyz
Node name: host-xyz
cluster_resource_scheduler state:
Local id: 1 Local resources: {"total":{CPU: [10000], memory: [1073741824]}, "available":{CPU: [5000], memory: [536870912]}} is_idle: 1 labels: {"ray.io/node-group":"headgroup"}
`
	r.put(clusterID, "session_1/logs/node-xyz/debug_state.txt", []byte(debugState))

	req := newReqWithCookies(t, "c1", "ns1", "session_1",
		"/api/cluster_status?format=1", nil)
	req.Request.URL.RawQuery = "format=1"
	resp, rec := newResp()

	s.getClusterStatus(req, resp)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}

	var payload struct {
		Result bool `json:"result"`
		Data   struct {
			ClusterStatus string `json:"clusterStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v (body=%q)", err, rec.Body.String())
	}
	if !payload.Result {
		t.Fatalf("expected result=true, got %+v", payload)
	}

	body := payload.Data.ClusterStatus
	// Spot-check the Ray-formatted text includes the block header, the idle
	// node group from debug_state.txt, and the failed-node entry from snap.
	if !strings.Contains(body, "Autoscaler status") {
		t.Errorf("expected 'Autoscaler status' header in body, got:\n%s", body)
	}
	if !strings.Contains(body, "1 headgroup") {
		t.Errorf("expected '1 headgroup' in Idle section, got:\n%s", body)
	}
	if !strings.Contains(body, "workers: NodeTerminated") {
		t.Errorf("expected failed-node entry 'workers: NodeTerminated', got:\n%s", body)
	}
	if !strings.Contains(body, "CPU") {
		t.Errorf("expected CPU resource entry, got:\n%s", body)
	}
}

// TestGetClusterStatus_EmptyFormatReturnsEmptyEnvelope verifies the non-format=1
// branch returns an empty ClusterStatusData (matching v1 — frontend only uses
// format=1).
func TestGetClusterStatus_EmptyFormatReturnsEmptyEnvelope(t *testing.T) {
	loader := &fakeLoader{}
	s := newServerWithLoader(loader)

	req := newReqWithCookies(t, "c1", "ns1", "session_1",
		"/api/cluster_status", nil)
	resp, rec := newResp()

	s.getClusterStatus(req, resp)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%q)", rec.Code, rec.Body.String())
	}
	// Loader must NOT be invoked on the empty-format path — v1 does not read
	// the snapshot either; reproducing that is cheap and reduces S3 load.
	if len(loader.calls) != 0 {
		t.Fatalf("expected loader not invoked for empty-format, got %d calls", len(loader.calls))
	}
}
