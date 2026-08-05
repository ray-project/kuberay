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
	"time"

	. "github.com/onsi/gomega"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// fakeDashboard stands in for the Ray Dashboard, recording every requested path.
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

// TestPollDataDatasetsFansOutPerJob verifies per-job fan-out from /api/jobs/, skipping blank IDs.
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

	handler.pollDataDatasets(context.Background(), "session_1", newDatasetPollState(), false)

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

// TestPollDataDatasetsSkipsEmptyResponse verifies an empty datasets response is never written,
// so a stats-actor eviction cannot replace datasets captured earlier.
func TestPollDataDatasetsSkipsEmptyResponse(t *testing.T) {
	g := NewWithT(t)

	dash := &fakeDashboard{
		jobs: `[{"job_id": "01000000", "status": "RUNNING"}]`,
		// No entry, so the fake dashboard replies {"datasets": []}.
		datasets: map[string]string{},
	}
	srv := dash.start(t)
	handler, writer := newPollTestHandler(t, srv.URL)

	handler.pollDataDatasets(context.Background(), "session_1", newDatasetPollState(), false)

	g.Expect(dash.requestsFor(dataDatasetsEndpointPrefix)).To(HaveLen(1))
	g.Expect(writtenKeys(writer)).To(BeEmpty())
}

// TestPollDataDatasetsStopsPollingTerminalJobs verifies a terminal job is fetched once
// while a running job keeps being refreshed.
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
	handler.pollDataDatasets(context.Background(), "session_1", state, false)
	handler.pollDataDatasets(context.Background(), "session_1", state, false)
	handler.pollDataDatasets(context.Background(), "session_1", state, false)

	g.Expect(state.done).To(HaveKey("01000000"))
	g.Expect(state.done).NotTo(HaveKey("02000000"))
	// The terminal job is fetched only on the first cycle; the running one every cycle.
	g.Expect(dash.requestsFor("/api/data/datasets/01000000")).To(HaveLen(1))
	g.Expect(dash.requestsFor("/api/data/datasets/02000000")).To(HaveLen(3))
}

// TestPollDataDatasetsRetriesFailedTerminalJob verifies a failed fetch is retried, so a
// transient dashboard error does not lose a terminal job's datasets.
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
	handler.pollDataDatasets(context.Background(), "session_1", state, false)
	g.Expect(state.done).To(BeEmpty())
	g.Expect(writtenKeys(writer)).To(BeEmpty())

	handler.pollDataDatasets(context.Background(), "session_1", state, false)
	g.Expect(state.done).To(HaveKey("01000000"))
	g.Expect(writtenKeys(writer)).To(Equal([]string{
		"cluster-dir/session_1/fetched_endpoints/restful__api__data__datasets__01000000",
	}))
}

// TestPollDataDatasetsRetriesTerminalJobWithLateStats verifies an empty first response is
// polled again, because Ray Data registers stats slightly after the job reports SUCCEEDED.
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
	handler.pollDataDatasets(context.Background(), "session_1", state, false)
	g.Expect(state.done).To(BeEmpty())
	g.Expect(writtenKeys(writer)).To(BeEmpty())

	handler.pollDataDatasets(context.Background(), "session_1", state, false)
	g.Expect(state.done).To(HaveKey("01000000"))
	g.Expect(writtenKeys(writer)).To(Equal([]string{
		"cluster-dir/session_1/fetched_endpoints/restful__api__data__datasets__01000000",
	}))
}

// TestPollDataDatasetsGivesUpOnRepeatedlyEmptyTerminalJob verifies the retry is bounded.
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
		handler.pollDataDatasets(context.Background(), "session_1", state, false)
	}

	g.Expect(state.done).To(HaveKey("01000000"))
	g.Expect(dash.requestsFor("/api/data/datasets/01000000")).To(HaveLen(terminalEmptyPollsBeforeGivingUp))
	g.Expect(writtenKeys(writer)).To(BeEmpty())
}

// TestPollAllEndpointsStoresStaticEndpoints verifies the built-in endpoints are stored under
// the exact URIs the frontend requests.
func TestPollAllEndpointsStoresStaticEndpoints(t *testing.T) {
	g := NewWithT(t)

	dash := &fakeDashboard{jobs: `[]`}
	srv := dash.start(t)
	handler, writer := newPollTestHandler(t, srv.URL)

	handler.pollAllEndpoints(context.Background(), "session_1", newDatasetPollState(), false)

	g.Expect(dash.requestsFor("/api/serve/applications/")).To(HaveLen(1))
	g.Expect(dash.requestsFor("/api/v0/placement_groups?detail=1&limit=10000")).To(HaveLen(1))
	g.Expect(writtenKeys(writer)).To(Equal([]string{
		"cluster-dir/session_1/fetched_endpoints/restful__api__serve__applications",
		"cluster-dir/session_1/fetched_endpoints/restful__api__v0__placement_groups?detail=1&limit=10000",
	}))
}

// TestPollCycleFollowsSessionChange verifies a Ray head restart moves polling to the new
// session and resets state, since job IDs restart with the session.
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

// TestPeriodicPollingStopsOnShutdownSignal verifies the loop exits on the shutdown signal,
// not ShutdownChan, so no tick can overwrite the final shutdown snapshot.
func TestPeriodicPollingStopsOnShutdownSignal(t *testing.T) {
	g := NewWithT(t)

	tmpRoot := t.TempDir()
	t.Setenv("RAY_TMP_ROOT", tmpRoot)
	g.Expect(os.MkdirAll(filepath.Join(tmpRoot, "session_1"), 0o755)).To(Succeed())
	g.Expect(os.Symlink(filepath.Join(tmpRoot, "session_1"), filepath.Join(tmpRoot, "session_latest"))).To(Succeed())

	dash := &fakeDashboard{jobs: `[]`}
	srv := dash.start(t)
	handler, _ := newPollTestHandler(t, srv.URL)
	handler.EndpointPollInterval = time.Hour

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		handler.PollAdditionalEndpointsPeriodically(stop)
		close(done)
	}()

	// ShutdownChan stays open; the stop signal alone must end the loop.
	close(stop)
	g.Eventually(done).Should(BeClosed())
}

// TestPollAllEndpointsStopsWhenContextExpires verifies the shutdown budget bounds the whole pass.
func TestPollAllEndpointsStopsWhenContextExpires(t *testing.T) {
	g := NewWithT(t)

	dash := &fakeDashboard{jobs: `[{"job_id": "01000000", "status": "SUCCEEDED"}]`}
	srv := dash.start(t)
	handler, writer := newPollTestHandler(t, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handler.pollAllEndpoints(ctx, "session_1", newDatasetPollState(), false)

	g.Expect(dash.requestsFor("/")).To(BeEmpty())
	g.Expect(writtenKeys(writer)).To(BeEmpty())
}

// TestPolledEndpointsAppendsConfiguredOnes verifies RAY_COLLECTOR_ADDITIONAL_ENDPOINTS adds
// to the built-in set, deduplicated.
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

// TestPollAllEndpointsStoresConfiguredEndpoint verifies a configured endpoint is stored too.
func TestPollAllEndpointsStoresConfiguredEndpoint(t *testing.T) {
	g := NewWithT(t)

	dash := &fakeDashboard{jobs: `[]`}
	srv := dash.start(t)
	handler, writer := newPollTestHandler(t, srv.URL)
	handler.AdditionalEndpoints = []string{"/nodes?view=summary"}

	handler.pollAllEndpoints(context.Background(), "session_1", newDatasetPollState(), false)

	g.Expect(dash.requestsFor("/nodes?view=summary")).To(HaveLen(1))
	g.Expect(writtenKeys(writer)).To(ContainElement(
		"cluster-dir/session_1/fetched_endpoints/restful__nodes?view=summary"))
	g.Expect(writtenKeys(writer)).To(HaveLen(3))
}

// TestServeSnapshotFollowsLiveButSurvivesShutdown verifies a periodic poll mirrors the live
// cluster (empty included) while the final shutdown poll cannot erase a converged snapshot.
func TestServeSnapshotFollowsLiveButSurvivesShutdown(t *testing.T) {
	g := NewWithT(t)

	var mu sync.Mutex
	serveBody := `{"applications": {"app": {"status": "RUNNING"}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == jobsEndpoint {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write([]byte(serveBody))
	}))
	t.Cleanup(srv.Close)

	handler, writer := newPollTestHandler(t, srv.URL)
	serveKey := "cluster-dir/session_1/fetched_endpoints/" +
		utils.EndpointPathToStorageKey(serveApplicationsEndpoint)
	storedServe := func() string {
		writer.mu.Lock()
		defer writer.mu.Unlock()
		return writer.writtenFiles[serveKey]
	}
	setServeBody := func(body string) {
		mu.Lock()
		defer mu.Unlock()
		serveBody = body
	}

	handler.pollAllEndpoints(context.Background(), "session_1", newDatasetPollState(), false)
	g.Expect(storedServe()).To(ContainSubstring("RUNNING"))

	// A final poll must not erase the converged snapshot with a dying dashboard's answer.
	setServeBody(`{"applications": {}}`)
	handler.pollAllEndpoints(context.Background(), "session_1", newDatasetPollState(), true)
	g.Expect(storedServe()).To(ContainSubstring("RUNNING"))

	// A periodic poll mirrors the live cluster, empty included.
	handler.pollAllEndpoints(context.Background(), "session_1", newDatasetPollState(), false)
	g.Expect(storedServe()).To(Equal(`{"applications": {}}`))
}

func TestHasServeApplications(t *testing.T) {
	g := NewWithT(t)

	g.Expect(hasServeApplications([]byte(`{"applications": {}}`))).To(BeFalse())
	g.Expect(hasServeApplications([]byte(`{"applications": {"a": {}}}`))).To(BeTrue())
	g.Expect(hasServeApplications([]byte(`{}`))).To(BeFalse())
	// An unexpected shape is stored rather than silently dropped.
	g.Expect(hasServeApplications([]byte(`not json`))).To(BeTrue())
}

// TestIsEmptyPayloadOnlyGuardsKnownEndpoints verifies the guard never suppresses endpoints it
// does not understand.
func TestIsEmptyPayloadOnlyGuardsKnownEndpoints(t *testing.T) {
	g := NewWithT(t)

	// Serve is guarded only on the final shutdown poll; datasets always.
	g.Expect(isEmptyPayload(serveApplicationsEndpoint, []byte(`{"applications": {}}`), true)).To(BeTrue())
	g.Expect(isEmptyPayload(serveApplicationsEndpoint, []byte(`{"applications": {}}`), false)).To(BeFalse())
	g.Expect(isEmptyPayload(dataDatasetsEndpointPrefix+"01000000", []byte(`{"datasets": []}`), false)).To(BeTrue())
	g.Expect(isEmptyPayload(placementGroupsEndpoint, []byte(`{}`), true)).To(BeFalse())
	g.Expect(isEmptyPayload("/nodes?view=summary", []byte(`{}`), true)).To(BeFalse())
}

func TestHasDatasets(t *testing.T) {
	g := NewWithT(t)

	g.Expect(hasDatasets([]byte(`{"datasets": []}`))).To(BeFalse())
	g.Expect(hasDatasets([]byte(`{"datasets": [{"dataset": "a"}]}`))).To(BeTrue())
	g.Expect(hasDatasets([]byte(`{}`))).To(BeFalse())
	// An unexpected shape is stored rather than silently dropped.
	g.Expect(hasDatasets([]byte(`not json`))).To(BeTrue())
}

// TestTerminalJobStatusesMatchRay guards against drift from Ray's JobStatus enum.
func TestTerminalJobStatusesMatchRay(t *testing.T) {
	g := NewWithT(t)

	for _, status := range []string{"SUCCEEDED", "FAILED", "STOPPED"} {
		g.Expect(terminalJobStatuses[status]).To(BeTrue(), "%s should be terminal", status)
	}
	for _, status := range []string{"PENDING", "RUNNING", ""} {
		g.Expect(terminalJobStatuses[status]).To(BeFalse(), "%q should not be terminal", status)
	}
}

// TestFakeDashboardJobsShapeMatchesRay documents the /api/jobs/ fields the collector depends on.
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
