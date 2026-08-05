package logcollector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// These are polled periodically, unlike the one-shot startup endpoints in
// startup_endpoints.go. Each string must match the frontend's request URI, query
// string included: the history server derives the storage key from that URI, so a
// mismatch silently makes the stored object unreachable.
const (
	// Paths mirror the Ray Dashboard frontend: serve.ts, placementGroup.ts, data.ts.
	serveApplicationsEndpoint = "/api/serve/applications/"

	// detail=1 adds the bundle and stats fields PlacementGroupTable needs;
	// limit=10000 matches the frontend default.
	placementGroupsEndpoint = "/api/v0/placement_groups?detail=1&limit=10000"

	// Only used to discover job IDs. Its response is not stored: the history
	// server rebuilds the job list from Ray events.
	jobsEndpoint = "/api/jobs/"

	// Requested per job, where job_id is the Ray core job ID in hex (e.g.
	// "01000000"), not the submission ID.
	dataDatasetsEndpointPrefix = "/api/data/datasets/"
)

// dataDatasetsEndpointPrefix is absent here: it needs a job ID, so pollDataDatasets
// handles it.
var staticPolledEndpoints = []string{
	serveApplicationsEndpoint,
	placementGroupsEndpoint,
}

// polledEndpoints deduplicates so that listing a built-in endpoint in
// RAY_COLLECTOR_ADDITIONAL_ENDPOINTS does not fetch and store it twice per cycle.
func (r *RayLogHandler) polledEndpoints() []string {
	endpoints := make([]string, 0, len(staticPolledEndpoints)+len(r.AdditionalEndpoints))
	seen := make(map[string]struct{}, cap(endpoints))
	for _, list := range [][]string{staticPolledEndpoints, r.AdditionalEndpoints} {
		for _, endpoint := range list {
			if _, ok := seen[endpoint]; ok {
				continue
			}
			seen[endpoint] = struct{}{}
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints
}

// terminalJobStatuses are the /api/jobs/ statuses a job never leaves, so its Ray
// Data datasets only need to be stored once.
// Ref: https://github.com/ray-project/ray/blob/ray-2.54.1/python/ray/dashboard/modules/job/common.py#L38-L50
var terminalJobStatuses = map[string]bool{
	"SUCCEEDED": true,
	"FAILED":    true,
	"STOPPED":   true,
}

// pollOutcome distinguishes "nothing worth storing" from "could not store", which
// decides whether a terminal job is worth polling again.
type pollOutcome int

const (
	pollFailed pollOutcome = iota
	pollStored
	pollSkippedEmpty
)

// A job's stats can appear slightly after it reports terminal, so giving up on the
// first empty response would lose them permanently.
const terminalEmptyPollsBeforeGivingUp = 2

// The final poll shares the pod's termination grace period (30s by default) with the
// log upload, so it gives up rather than risking a SIGKILL partway through.
const shutdownPollBudget = 10 * time.Second

// datasetPollState remembers across cycles which jobs no longer need their datasets
// fetched. The polling loop owns it exclusively, so it needs no lock.
type datasetPollState struct {
	done      map[string]struct{}
	emptyRuns map[string]int
}

func newDatasetPollState() *datasetPollState {
	return &datasetPollState{
		done:      make(map[string]struct{}),
		emptyRuns: make(map[string]int),
	}
}

// PollAdditionalEndpointsPeriodically fetches the built-in endpoints, plus anything
// from RAY_COLLECTOR_ADDITIONAL_ENDPOINTS, on a timer until shutdown. Each response
// is stored at {ClusterDir}/{sessionName}/fetched_endpoints/{storageKey}, and each
// cycle overwrites the previous one.
func (r *RayLogHandler) PollAdditionalEndpointsPeriodically() {
	// Blocking resolve is fine here but not in the loop below: on startup there is
	// nothing to poll until session_latest exists.
	sessionName, err := r.resolveSessionName()
	if err != nil {
		logrus.Errorf("Failed to resolve session name for endpoint polling: %v", err)
		return
	}
	logrus.Infof("Starting endpoint polling (interval=%v, endpoints=%v)", r.EndpointPollInterval, r.polledEndpoints())

	state := newDatasetPollState()

	// Perform an initial poll immediately on startup.
	r.pollAllEndpoints(context.Background(), sessionName, state)

	ticker := time.NewTicker(r.EndpointPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ShutdownChan:
			logrus.Info("Shutdown signaled, stopping endpoint polling")
			return
		case <-ticker.C:
			sessionName, state = r.pollCycle(context.Background(), sessionName, state)
		}
	}
}

// pollCycle runs one polling pass, re-resolving the session first.
//
// The Ray head container can restart on its own (an OOMKill restarts that container, not
// the pod), which starts a new session while this sidecar keeps running. Job IDs restart
// with it, so a stale state would both write into the dead session's directory and skip
// the new session's jobs as already captured.
func (r *RayLogHandler) pollCycle(ctx context.Context, sessionName string, state *datasetPollState) (string, *datasetPollState) {
	switch current, err := currentSessionName(); {
	case err != nil:
		logrus.Warnf("Failed to re-resolve session name, polling into %s: %v", sessionName, err)
	case current != sessionName:
		logrus.Infof("Session changed from %s to %s, resetting dataset polling state", sessionName, current)
		sessionName, state = current, newDatasetPollState()
	}

	r.pollAllEndpoints(ctx, sessionName, state)
	return sessionName, state
}

// currentSessionName resolves session_latest without retrying, for callers that must not
// block: the polling loop and the shutdown path.
func currentSessionName() (string, error) {
	sessionRealDir, err := filepath.EvalSymlinks(utils.GetRaySessionLatestPath())
	if err != nil {
		return "", err
	}
	return filepath.Base(sessionRealDir), nil
}

// processAdditionalEndpoints performs a final poll of all polled endpoints
// before shutdown. This mirrors processSessionLatestLogs as a shutdown cleanup step.
//
// Unlike PollAdditionalEndpointsPeriodically, this does NOT retry session name
// resolution because it runs during shutdown — if session_latest is gone (e.g.,
// Ray head already exited), retrying would hang forever since ShutdownChan has
// not been closed yet.
func (r *RayLogHandler) processAdditionalEndpoints() {
	logrus.Info("Processing polled endpoints before shutdown")

	sessionName, err := currentSessionName()
	if err != nil {
		logrus.Errorf("Failed to resolve session name for final endpoint poll: %v", err)
		return
	}

	// One budget for the whole pass, not per request: the endpoints are fetched
	// serially, so per-request timeouts would add up past the grace period.
	ctx, cancel := context.WithTimeout(context.Background(), shutdownPollBudget)
	defer cancel()

	// Fresh state, so this final pass re-captures every job rather than trusting
	// what the polling loop already stored.
	r.pollAllEndpoints(ctx, sessionName, newDatasetPollState())
	logrus.Info("Finished processing polled endpoints")
}

func (r *RayLogHandler) pollAllEndpoints(ctx context.Context, sessionName string, state *datasetPollState) {
	for _, endpoint := range r.polledEndpoints() {
		if ctx.Err() != nil {
			logrus.Warnf("Stopped polling before %s: %v", endpoint, ctx.Err())
			return
		}
		r.pollSingleEndpoint(ctx, endpoint, sessionName)
	}
	r.pollDataDatasets(ctx, sessionName, state)
}

// pollDataDatasets stores one datasets object per job discovered via jobsEndpoint.
// Terminal jobs are dropped from future cycles once settled: without that the
// per-cycle cost would grow with the cluster's total job count forever, and each
// datasets request also makes the dashboard query Prometheus.
func (r *RayLogHandler) pollDataDatasets(ctx context.Context, sessionName string, state *datasetPollState) {
	body, err := r.fetchEndpoint(ctx, jobsEndpoint)
	if err != nil {
		logrus.Warnf("Failed to fetch %s for dataset polling: %v", jobsEndpoint, err)
		return
	}

	var jobs []struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &jobs); err != nil {
		logrus.Warnf("Failed to parse %s response: %v", jobsEndpoint, err)
		return
	}

	for i, job := range jobs {
		// Bail out rather than letting every remaining job log its own failure.
		if ctx.Err() != nil {
			logrus.Warnf("Stopped dataset polling after %d/%d jobs: %v", i, len(jobs), ctx.Err())
			return
		}
		// job_id is empty for submission jobs whose driver has not started yet.
		if job.JobID == "" {
			continue
		}
		if _, ok := state.done[job.JobID]; ok {
			continue
		}

		outcome := r.pollSingleEndpoint(ctx, dataDatasetsEndpointPrefix+job.JobID, sessionName)
		if !terminalJobStatuses[job.Status] {
			continue
		}
		switch outcome {
		case pollStored:
			state.done[job.JobID] = struct{}{}
		case pollSkippedEmpty:
			state.emptyRuns[job.JobID]++
			if state.emptyRuns[job.JobID] >= terminalEmptyPollsBeforeGivingUp {
				state.done[job.JobID] = struct{}{}
			}
		case pollFailed:
			// Retry next cycle so a transient dashboard error does not lose datasets.
		}
	}
}

func (r *RayLogHandler) pollSingleEndpoint(ctx context.Context, endpoint, sessionName string) pollOutcome {
	body, err := r.fetchEndpoint(ctx, endpoint)
	if err != nil {
		logrus.Warnf("Failed to poll endpoint %s: %v", endpoint, err)
		return pollFailed
	}

	if isEmptyPayload(endpoint, body) {
		logrus.Debugf("Skipping %s: nothing to store", endpoint)
		return pollSkippedEmpty
	}

	storageKey := utils.EndpointPathToStorageKey(endpoint)
	objectKey := path.Join(r.ClusterDir, sessionName, utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME, storageKey)
	if err := r.Writer.WriteFile(objectKey, bytes.NewReader(body)); err != nil {
		logrus.Errorf("Failed to store endpoint %s at %s: %v", endpoint, objectKey, err)
		return pollFailed
	}

	logrus.Infof("Successfully stored endpoint %s at %s (%d bytes)", endpoint, objectKey, len(body))
	return pollStored
}

// isEmptyPayload reports whether a response carries nothing worth storing.
//
// Overwriting a converged snapshot with an empty one is worse than not writing at all: a
// Ray head that is shutting down answers 200 with an empty body, and on replay that is
// indistinguishable from a cluster that never used the feature. The history server
// already synthesizes empty responses for these paths, so skipping the write costs
// nothing.
func isEmptyPayload(endpoint string, body []byte) bool {
	switch {
	case strings.HasPrefix(endpoint, dataDatasetsEndpointPrefix):
		return !hasDatasets(body)
	case endpoint == serveApplicationsEndpoint:
		return !hasServeApplications(body)
	default:
		return false
	}
}

// hasServeApplications reports whether a Serve response lists at least one application.
// Unparsable bodies count as non-empty so unexpected shapes are stored, not dropped.
func hasServeApplications(body []byte) bool {
	var resp struct {
		Applications map[string]json.RawMessage `json:"applications"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return true
	}
	return len(resp.Applications) > 0
}

// hasDatasets reports whether a datasets response carries at least one dataset.
// Unparsable bodies count as non-empty so unexpected shapes are stored, not dropped.
func hasDatasets(body []byte) bool {
	var resp struct {
		Datasets []json.RawMessage `json:"datasets"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return true
	}
	return len(resp.Datasets) > 0
}

// fetchEndpoint performs a single GET against the Ray Dashboard and returns the
// response body. In-flight requests are canceled on shutdown.
func (r *RayLogHandler) fetchEndpoint(parent context.Context, endpoint string) ([]byte, error) {
	url := r.DashboardAddress + endpoint

	ctx, cancel := context.WithTimeout(parent, defaultRequestTimeout)
	defer cancel()
	go func() {
		select {
		case <-r.ShutdownChan:
			cancel()
		case <-ctx.Done():
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := r.HttpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return body, nil
}
