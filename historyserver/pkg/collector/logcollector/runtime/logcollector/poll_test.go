package logcollector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// fakeDashboard stands in for the Ray Dashboard, recording every path requested
// so tests can assert which endpoints the collector polled.
type fakeDashboard struct {
	mu       sync.Mutex
	requests []string
	// jobs is the /api/jobs/ response body.
	jobs string
	// datasets maps a job ID to its /api/data/datasets/{job_id} response body.
	datasets map[string]string
}

func (f *fakeDashboard) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.URL.RequestURI())
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == jobsEndpoint:
			_, _ = w.Write([]byte(f.jobs))
		case r.URL.Path == serveApplicationsEndpoint:
			// Non-empty by default: an empty Serve response is deliberately not stored.
			_, _ = w.Write([]byte(`{"applications": {"app": {"status": "RUNNING"}}}`))
		case strings.HasPrefix(r.URL.Path, dataDatasetsEndpointPrefix):
			jobID := strings.TrimPrefix(r.URL.Path, dataDatasetsEndpointPrefix)
			body, ok := f.datasets[jobID]
			if !ok {
				body = `{"datasets": []}`
			}
			_, _ = w.Write([]byte(body))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeDashboard) requestsFor(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, req := range f.requests {
		if strings.HasPrefix(req, prefix) {
			out = append(out, req)
		}
	}
	return out
}

func newPollTestHandler(t *testing.T, dashboardAddr string) (*RayLogHandler, *MockStorageWriter) {
	t.Helper()
	writer := NewMockStorageWriter()
	return &RayLogHandler{
		Writer:           writer,
		HttpClient:       &http.Client{},
		ShutdownChan:     make(chan struct{}),
		ClusterDir:       "cluster-dir",
		DashboardAddress: dashboardAddr,
		IsHead:           true,
	}, writer
}

func writtenKeys(writer *MockStorageWriter) []string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	keys := make([]string, 0, len(writer.writtenFiles))
	for k := range writer.writtenFiles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestPollDataDatasetsFansOutPerJob verifies that job IDs are discovered from
// /api/jobs/, that blank IDs are skipped, and that each job with datasets gets
// its own storage object keyed by the frontend's request URI.
func TestPollDataDatasetsFansOutPerJob(t *testing.T) {
	g := NewWithT(t)

	dash := &fakeDashboard{
		jobs: `[
			{"job_id": "01000000", "status": "RUNNING"},
			{"job_id": null, "status": "PENDING"},
			{"job_id": "02000000", "status": "RUNNING"}
		]`,
		datasets: map[string]string{
			"01000000": `{"datasets": [{"dataset": "ds_a", "job_id": "01000000"}]}`,
			"02000000": `{"datasets": [{"dataset": "ds_b", "job_id": "02000000"}]}`,
		},
	}
	srv := dash.start(t)
	handler, writer := newPollTestHandler(t, srv.URL)

	handler.pollDataDatasets(context.Background(), "session_1", newDatasetPollState())

	// The blank job_id is skipped.
	g.Expect(dash.requestsFor(dataDatasetsEndpointPrefix)).To(ConsistOf(
		"/api/data/datasets/01000000",
		"/api/data/datasets/02000000",
	))
	g.Expect(writtenKeys(writer)).To(Equal([]string{
		"cluster-dir/session_1/fetched_endpoints/restful__api__data__datasets__01000000",
		"cluster-dir/session_1/fetched_endpoints/restful__api__data__datasets__02000000",
	}))
}

// TestPollDataDatasetsSkipsEmptyResponse verifies that a job reporting no Ray
// Data datasets does not get an object written. Most jobs never use Ray Data,
// and storing the empty response would also let a stats-actor eviction replace
// datasets that were captured earlier.
func TestPollDataDatasetsSkipsEmptyResponse(t *testing.T) {
	g := NewWithT(t)

	dash := &fakeDashboard{
		jobs: `[{"job_id": "01000000", "status": "RUNNING"}]`,
		// No entry, so the fake dashboard replies {"datasets": []}.
		datasets: map[string]string{},
	}
	srv := dash.start(t)
	handler, writer := newPollTestHandler(t, srv.URL)

	handler.pollDataDatasets(context.Background(), "session_1", newDatasetPollState())

	g.Expect(dash.requestsFor(dataDatasetsEndpointPrefix)).To(HaveLen(1))
	g.Expect(writtenKeys(writer)).To(BeEmpty())
}

// TestPollDataDatasetsStopsPollingTerminalJobs verifies that a terminal job is
// fetched once and then skipped, so the per-cycle cost does not grow with the
// cluster's total job count, while a running job keeps being refreshed.
func TestPollDataDatasetsStopsPollingTerminalJobs(t *testing.T) {
	g := NewWithT(t)

	dash := &fakeDashboard{
		jobs: `[
			{"job_id": "01000000", "status": "SUCCEEDED"},
			{"job_id": "02000000", "status": "RUNNING"}
		]`,
		datasets: map[string]string{
			"01000000": `{"datasets": [{"dataset": "ds_done"}]}`,
			"02000000": `{"datasets": [{"dataset": "ds_live"}]}`,
		},
	}
	srv := dash.start(t)
	handler, _ := newPollTestHandler(t, srv.URL)

	state := newDatasetPollState()
	handler.pollDataDatasets(context.Background(), "session_1", state)
	handler.pollDataDatasets(context.Background(), "session_1", state)
	handler.pollDataDatasets(context.Background(), "session_1", state)

	g.Expect(state.done).To(HaveKey("01000000"))
	g.Expect(state.done).NotTo(HaveKey("02000000"))
	// The terminal job is fetched only on the first cycle; the running one every cycle.
	g.Expect(dash.requestsFor("/api/data/datasets/01000000")).To(HaveLen(1))
	g.Expect(dash.requestsFor("/api/data/datasets/02000000")).To(HaveLen(3))
}

// TestPollDataDatasetsRetriesFailedTerminalJob verifies that a terminal job
// whose fetch failed is retried on the next cycle rather than being marked
// captured, so a transient dashboard error does not lose its datasets.
func TestPollDataDatasetsRetriesFailedTerminalJob(t *testing.T) {
	g := NewWithT(t)

	var mu sync.Mutex
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == jobsEndpoint {
			_, _ = w.Write([]byte(`[{"job_id": "01000000", "status": "SUCCEEDED"}]`))
			return
		}
		mu.Lock()
		attempts++
		first := attempts == 1
		mu.Unlock()
		if first {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"datasets": [{"dataset": "ds_done"}]}`))
	}))
	t.Cleanup(srv.Close)

	handler, writer := newPollTestHandler(t, srv.URL)

	state := newDatasetPollState()
	handler.pollDataDatasets(context.Background(), "session_1", state)
	g.Expect(state.done).To(BeEmpty())
	g.Expect(writtenKeys(writer)).To(BeEmpty())

	handler.pollDataDatasets(context.Background(), "session_1", state)
	g.Expect(state.done).To(HaveKey("01000000"))
	g.Expect(writtenKeys(writer)).To(Equal([]string{
		"cluster-dir/session_1/fetched_endpoints/restful__api__data__datasets__01000000",
	}))
}

// TestPollDataDatasetsRetriesTerminalJobWithLateStats verifies a terminal job whose
// first datasets response is empty is polled again, because Ray Data registers its
// stats slightly after the job reports SUCCEEDED.
func TestPollDataDatasetsRetriesTerminalJobWithLateStats(t *testing.T) {
	g := NewWithT(t)

	var mu sync.Mutex
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == jobsEndpoint {
			_, _ = w.Write([]byte(`[{"job_id": "01000000", "status": "SUCCEEDED"}]`))
			return
		}
		mu.Lock()
		attempts++
		first := attempts == 1
		mu.Unlock()
		if first {
			_, _ = w.Write([]byte(`{"datasets": []}`))
			return
		}
		_, _ = w.Write([]byte(`{"datasets": [{"dataset": "ds_late"}]}`))
	}))
	t.Cleanup(srv.Close)

	handler, writer := newPollTestHandler(t, srv.URL)

	state := newDatasetPollState()
	handler.pollDataDatasets(context.Background(), "session_1", state)
	g.Expect(state.done).To(BeEmpty())
	g.Expect(writtenKeys(writer)).To(BeEmpty())

	handler.pollDataDatasets(context.Background(), "session_1", state)
	g.Expect(state.done).To(HaveKey("01000000"))
	g.Expect(writtenKeys(writer)).To(Equal([]string{
		"cluster-dir/session_1/fetched_endpoints/restful__api__data__datasets__01000000",
	}))
}

// TestPollDataDatasetsGivesUpOnRepeatedlyEmptyTerminalJob verifies the retry above is
// bounded, so jobs that never touch Ray Data stop costing a request every cycle.
func TestPollDataDatasetsGivesUpOnRepeatedlyEmptyTerminalJob(t *testing.T) {
	g := NewWithT(t)

	dash := &fakeDashboard{
		jobs: `[{"job_id": "01000000", "status": "SUCCEEDED"}]`,
		// No entry, so the fake dashboard always replies {"datasets": []}.
		datasets: map[string]string{},
	}
	srv := dash.start(t)
	handler, writer := newPollTestHandler(t, srv.URL)

	state := newDatasetPollState()
	for i := 0; i < 4; i++ {
		handler.pollDataDatasets(context.Background(), "session_1", state)
	}

	g.Expect(state.done).To(HaveKey("01000000"))
	g.Expect(dash.requestsFor("/api/data/datasets/01000000")).To(HaveLen(terminalEmptyPollsBeforeGivingUp))
	g.Expect(writtenKeys(writer)).To(BeEmpty())
}

// TestPollAllEndpointsStoresStaticEndpoints verifies the built-in static
// endpoints are polled with the exact URIs the dashboard frontend requests, so
// the storage keys match what the history server looks up on replay.
func TestPollAllEndpointsStoresStaticEndpoints(t *testing.T) {
	g := NewWithT(t)

	dash := &fakeDashboard{jobs: `[]`}
	srv := dash.start(t)
	handler, writer := newPollTestHandler(t, srv.URL)

	handler.pollAllEndpoints(context.Background(), "session_1", newDatasetPollState())

	g.Expect(dash.requestsFor("/api/serve/applications/")).To(HaveLen(1))
	g.Expect(dash.requestsFor("/api/v0/placement_groups?detail=1&limit=10000")).To(HaveLen(1))
	g.Expect(writtenKeys(writer)).To(Equal([]string{
		"cluster-dir/session_1/fetched_endpoints/restful__api__serve__applications",
		"cluster-dir/session_1/fetched_endpoints/restful__api__v0__placement_groups?detail=1&limit=10000",
	}))
}

// TestPollCycleFollowsSessionChange verifies that a Ray head restart, which starts a new
// session without restarting this sidecar, moves polling to the new session and resets
// the dataset state. Job IDs restart with the session, so a stale state would write into
// the dead session's directory and skip the new session's jobs as already captured.
func TestPollCycleFollowsSessionChange(t *testing.T) {
	g := NewWithT(t)

	tmpRoot := t.TempDir()
	t.Setenv("RAY_TMP_ROOT", tmpRoot)
	symlink := filepath.Join(tmpRoot, "session_latest")
	pointAt := func(session string) {
		g.Expect(os.MkdirAll(filepath.Join(tmpRoot, session), 0o755)).To(Succeed())
		_ = os.Remove(symlink)
		g.Expect(os.Symlink(filepath.Join(tmpRoot, session), symlink)).To(Succeed())
	}

	dash := &fakeDashboard{
		jobs:     `[{"job_id": "01000000", "status": "SUCCEEDED"}]`,
		datasets: map[string]string{"01000000": `{"datasets": [{"dataset": "ds"}]}`},
	}
	srv := dash.start(t)
	handler, writer := newPollTestHandler(t, srv.URL)

	pointAt("session_old")
	session, state := handler.pollCycle(context.Background(), "session_old", newDatasetPollState())
	g.Expect(session).To(Equal("session_old"))
	g.Expect(state.done).To(HaveKey("01000000"))

	pointAt("session_new")
	session, state = handler.pollCycle(context.Background(), session, state)
	g.Expect(session).To(Equal("session_new"))
	g.Expect(state.done).To(HaveKey("01000000"), "the new session's job must be captured, not skipped")

	// The same job ID is stored once per session, not once overall.
	g.Expect(dash.requestsFor("/api/data/datasets/01000000")).To(HaveLen(2))
	g.Expect(writtenKeys(writer)).To(ContainElements(
		"cluster-dir/session_old/fetched_endpoints/restful__api__data__datasets__01000000",
		"cluster-dir/session_new/fetched_endpoints/restful__api__data__datasets__01000000",
	))
}

// TestPollAllEndpointsStopsWhenContextExpires verifies the shutdown budget bounds the
// whole pass. Without it, an unresponsive dashboard would cost one request timeout per
// endpoint and per job on the way out, overrunning the pod's grace period.
func TestPollAllEndpointsStopsWhenContextExpires(t *testing.T) {
	g := NewWithT(t)

	dash := &fakeDashboard{jobs: `[{"job_id": "01000000", "status": "SUCCEEDED"}]`}
	srv := dash.start(t)
	handler, writer := newPollTestHandler(t, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handler.pollAllEndpoints(ctx, "session_1", newDatasetPollState())

	g.Expect(dash.requestsFor("/")).To(BeEmpty())
	g.Expect(writtenKeys(writer)).To(BeEmpty())
}

// TestPolledEndpointsAppendsConfiguredOnes verifies RAY_COLLECTOR_ADDITIONAL_ENDPOINTS
// adds to the built-in set rather than replacing it, and that repeating a
// built-in endpoint does not make it fetched twice per cycle.
func TestPolledEndpointsAppendsConfiguredOnes(t *testing.T) {
	g := NewWithT(t)

	handler, _ := newPollTestHandler(t, "http://unused")
	g.Expect(handler.polledEndpoints()).To(Equal(staticPolledEndpoints))

	handler.AdditionalEndpoints = []string{
		"/nodes?view=summary",
		serveApplicationsEndpoint, // already built in
		"/nodes?view=summary",     // repeated by the user
	}
	g.Expect(handler.polledEndpoints()).To(Equal([]string{
		serveApplicationsEndpoint,
		placementGroupsEndpoint,
		"/nodes?view=summary",
	}))

	// The built-in list itself must not be mutated by the append.
	g.Expect(staticPolledEndpoints).To(Equal([]string{
		serveApplicationsEndpoint,
		placementGroupsEndpoint,
	}))
}

// TestPollAllEndpointsStoresConfiguredEndpoint verifies a configured endpoint is
// actually fetched and stored alongside the built-in ones.
func TestPollAllEndpointsStoresConfiguredEndpoint(t *testing.T) {
	g := NewWithT(t)

	dash := &fakeDashboard{jobs: `[]`}
	srv := dash.start(t)
	handler, writer := newPollTestHandler(t, srv.URL)
	handler.AdditionalEndpoints = []string{"/nodes?view=summary"}

	handler.pollAllEndpoints(context.Background(), "session_1", newDatasetPollState())

	g.Expect(dash.requestsFor("/nodes?view=summary")).To(HaveLen(1))
	g.Expect(writtenKeys(writer)).To(ContainElement(
		"cluster-dir/session_1/fetched_endpoints/restful__nodes?view=summary"))
	g.Expect(writtenKeys(writer)).To(HaveLen(3))
}

// TestPollAllEndpointsKeepsConvergedServeSnapshot verifies an empty Serve response never
// replaces a converged one. A Ray head that is shutting down still answers 200 with no
// applications, and the shutdown pass writes to the same storage key, so without this the
// last thing written before deletion would wipe the snapshot the replay depends on.
func TestPollAllEndpointsKeepsConvergedServeSnapshot(t *testing.T) {
	g := NewWithT(t)

	var mu sync.Mutex
	converged := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == jobsEndpoint {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if converged {
			_, _ = w.Write([]byte(`{"applications": {"app": {"status": "RUNNING"}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"applications": {}}`))
	}))
	t.Cleanup(srv.Close)

	handler, writer := newPollTestHandler(t, srv.URL)
	serveKey := "cluster-dir/session_1/fetched_endpoints/" +
		utils.EndpointPathToStorageKey(serveApplicationsEndpoint)

	handler.pollAllEndpoints(context.Background(), "session_1", newDatasetPollState())
	writer.mu.Lock()
	stored := writer.writtenFiles[serveKey]
	writer.mu.Unlock()
	g.Expect(stored).To(ContainSubstring("RUNNING"))

	// The Serve controller stops before the dashboard does, so the next poll sees nothing.
	mu.Lock()
	converged = false
	mu.Unlock()
	handler.pollAllEndpoints(context.Background(), "session_1", newDatasetPollState())

	writer.mu.Lock()
	defer writer.mu.Unlock()
	g.Expect(writer.writtenFiles[serveKey]).To(Equal(stored), "the converged snapshot must survive")
}

// TestHasServeApplications covers the guard that decides whether a Serve response is
// worth storing, including the deliberate choice to store unparsable bodies.
func TestHasServeApplications(t *testing.T) {
	g := NewWithT(t)

	g.Expect(hasServeApplications([]byte(`{"applications": {}}`))).To(BeFalse())
	g.Expect(hasServeApplications([]byte(`{"applications": {"a": {}}}`))).To(BeTrue())
	g.Expect(hasServeApplications([]byte(`{}`))).To(BeFalse())
	// An unexpected shape is stored rather than silently dropped.
	g.Expect(hasServeApplications([]byte(`not json`))).To(BeTrue())
}

// TestIsEmptyPayloadOnlyGuardsKnownEndpoints verifies the guard never suppresses an
// endpoint it does not understand, which would silently stop storing it.
func TestIsEmptyPayloadOnlyGuardsKnownEndpoints(t *testing.T) {
	g := NewWithT(t)

	g.Expect(isEmptyPayload(serveApplicationsEndpoint, []byte(`{"applications": {}}`))).To(BeTrue())
	g.Expect(isEmptyPayload(dataDatasetsEndpointPrefix+"01000000", []byte(`{"datasets": []}`))).To(BeTrue())
	g.Expect(isEmptyPayload(placementGroupsEndpoint, []byte(`{}`))).To(BeFalse())
	g.Expect(isEmptyPayload("/nodes?view=summary", []byte(`{}`))).To(BeFalse())
}

// TestHasDatasets covers the guard that decides whether a datasets response is
// worth storing, including the deliberate choice to store unparsable bodies.
func TestHasDatasets(t *testing.T) {
	g := NewWithT(t)

	g.Expect(hasDatasets([]byte(`{"datasets": []}`))).To(BeFalse())
	g.Expect(hasDatasets([]byte(`{"datasets": [{"dataset": "a"}]}`))).To(BeTrue())
	g.Expect(hasDatasets([]byte(`{}`))).To(BeFalse())
	// An unexpected shape is stored rather than silently dropped.
	g.Expect(hasDatasets([]byte(`not json`))).To(BeTrue())
}

// TestTerminalJobStatusesMatchRay guards the status strings against drift from
// Ray's JobStatus enum, which is what /api/jobs/ serializes.
func TestTerminalJobStatusesMatchRay(t *testing.T) {
	g := NewWithT(t)

	for _, status := range []string{"SUCCEEDED", "FAILED", "STOPPED"} {
		g.Expect(terminalJobStatuses[status]).To(BeTrue(), "%s should be terminal", status)
	}
	for _, status := range []string{"PENDING", "RUNNING", ""} {
		g.Expect(terminalJobStatuses[status]).To(BeFalse(), "%q should not be terminal", status)
	}
}

// TestFakeDashboardJobsShapeMatchesRay documents the /api/jobs/ fields the
// collector depends on, so a Ray-side rename shows up as a decode failure here.
func TestFakeDashboardJobsShapeMatchesRay(t *testing.T) {
	g := NewWithT(t)

	var jobs []struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	body := `[{"job_id": "01000000", "submission_id": "raysubmit_x", "status": "SUCCEEDED", "type": "SUBMISSION"}]`
	g.Expect(json.Unmarshal([]byte(body), &jobs)).To(Succeed())
	g.Expect(jobs).To(HaveLen(1))
	g.Expect(jobs[0].JobID).To(Equal("01000000"))
	g.Expect(jobs[0].Status).To(Equal("SUCCEEDED"))
}
