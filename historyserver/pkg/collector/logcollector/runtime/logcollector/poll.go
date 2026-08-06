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

// Each string must match the frontend's request URI, query string included: the
// storage key is derived from it, so a mismatch makes the stored object unreachable.
const (
	serveApplicationsEndpoint = "/api/serve/applications/"
	placementGroupsEndpoint   = "/api/v0/placement_groups?detail=1&limit=10000"
	// Only used to discover job IDs; its response is not stored.
	jobsEndpoint = "/api/jobs/"
	// Per job, where job_id is the hex core job ID (e.g. "01000000"), not the submission ID.
	dataDatasetsEndpointPrefix = "/api/data/datasets/"
)

// dataDatasetsEndpointPrefix needs a job ID, so pollDataDatasets handles it.
var staticPolledEndpoints = []string{
	serveApplicationsEndpoint,
	placementGroupsEndpoint,
}

// polledEndpoints merges the built-in and configured endpoints, deduplicated.
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

// Statuses a job never leaves, so its datasets only need to be stored once.
// Ref: https://github.com/ray-project/ray/blob/ray-2.54.1/python/ray/dashboard/modules/job/common.py#L38-L50
var terminalJobStatuses = map[string]bool{
	"SUCCEEDED": true,
	"FAILED":    true,
	"STOPPED":   true,
}

// pollOutcome distinguishes "nothing worth storing" from "could not store".
type pollOutcome int

const (
	pollFailed pollOutcome = iota
	pollStored
	pollSkippedEmpty
)

// Stats can appear slightly after a job reports terminal, so one empty response is not final.
const terminalEmptyPollsBeforeGivingUp = 2

// The final poll shares the pod's termination grace period, so it gives up rather than overrun it.
const shutdownPollBudget = 10 * time.Second

// Caps the shutdown join on the periodic poller: a storage write in flight is not
// cancelable, and waiting it out could eat the grace period the final poll needs.
const periodicPollJoinTimeout = 5 * time.Second

// datasetPollState tracks jobs whose datasets no longer need fetching. Owned by one goroutine; no lock.
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

// PollAdditionalEndpointsPeriodically fetches the built-in endpoints, plus anything from
// RAY_COLLECTOR_ADDITIONAL_ENDPOINTS, on a timer; each cycle overwrites the previous one.
// It stops on the shutdown signal, not ShutdownChan: ShutdownChan closes only after the
// final shutdown poll, and a tick in between could overwrite that final snapshot.
// Run joins this goroutine before that final poll, so the ctx cancels at stop to keep a
// blocked resolve or in-flight cycle from stalling shutdown.
func (r *RayLogHandler) PollAdditionalEndpointsPeriodically(stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()

	sessionName, err := waitForSessionName(ctx)
	if err != nil {
		logrus.Errorf("Failed to resolve session name for endpoint polling: %v", err)
		return
	}
	logrus.Infof("Starting endpoint polling (interval=%v, endpoints=%v)", r.EndpointPollInterval, r.polledEndpoints())

	state := newDatasetPollState()
	r.pollAllEndpoints(ctx, sessionName, state, false)

	ticker := time.NewTicker(r.EndpointPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			logrus.Info("Shutdown signaled, stopping endpoint polling")
			return
		case <-ticker.C:
			sessionName, state = r.pollCycle(ctx, sessionName, state)
		}
	}
}

// waitForSessionName resolves session_latest, retrying until ctx is canceled:
// at startup the symlink appears only once Ray has bootstrapped.
func waitForSessionName(ctx context.Context) (string, error) {
	for {
		name, err := currentSessionName()
		if err == nil {
			return name, nil
		}
		logrus.Warnf("session_latest symlink not ready: %v, retrying in %v", err, defaultRetryInterval)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(defaultRetryInterval):
		}
	}
}

// pollCycle runs one polling pass, re-resolving the session first: the Ray head container
// can restart alone, starting a new session (and new job IDs) while this sidecar lives on.
func (r *RayLogHandler) pollCycle(ctx context.Context, sessionName string, state *datasetPollState) (string, *datasetPollState) {
	switch current, err := currentSessionName(); {
	case err != nil:
		logrus.Warnf("Failed to re-resolve session name, polling into %s: %v", sessionName, err)
	case current != sessionName:
		logrus.Infof("Session changed from %s to %s, resetting dataset polling state", sessionName, current)
		sessionName, state = current, newDatasetPollState()
	}

	r.pollAllEndpoints(ctx, sessionName, state, false)
	return sessionName, state
}

// currentSessionName resolves session_latest without retrying, for callers that must not block.
func currentSessionName() (string, error) {
	sessionRealDir, err := filepath.EvalSymlinks(utils.GetRaySessionLatestPath())
	if err != nil {
		return "", err
	}
	return filepath.Base(sessionRealDir), nil
}

// processAdditionalEndpoints performs one final poll before shutdown.
func (r *RayLogHandler) processAdditionalEndpoints() {
	logrus.Info("Processing polled endpoints before shutdown")

	sessionName, err := currentSessionName()
	if err != nil {
		logrus.Errorf("Failed to resolve session name for final endpoint poll: %v", err)
		return
	}

	// One budget for the whole pass: serial per-request timeouts would add up past the grace period.
	ctx, cancel := context.WithTimeout(context.Background(), shutdownPollBudget)
	defer cancel()

	// Fresh state, so this final pass re-captures every job.
	r.pollAllEndpoints(ctx, sessionName, newDatasetPollState(), true)
	logrus.Info("Finished processing polled endpoints")
}

// finalPoll marks the shutdown pass, where an empty Serve response is distrusted.
func (r *RayLogHandler) pollAllEndpoints(ctx context.Context, sessionName string, state *datasetPollState, finalPoll bool) {
	for _, endpoint := range r.polledEndpoints() {
		if ctx.Err() != nil {
			logrus.Warnf("Stopped polling before %s: %v", endpoint, ctx.Err())
			return
		}
		r.pollSingleEndpoint(ctx, endpoint, sessionName, finalPoll)
	}
	r.pollDataDatasets(ctx, sessionName, state, finalPoll)
}

// pollDataDatasets stores one datasets object per job discovered via jobsEndpoint.
// Settled terminal jobs are skipped so the per-cycle cost does not grow with job count.
func (r *RayLogHandler) pollDataDatasets(ctx context.Context, sessionName string, state *datasetPollState, finalPoll bool) {
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

		outcome := r.pollSingleEndpoint(ctx, dataDatasetsEndpointPrefix+job.JobID, sessionName, finalPoll)
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
			// Retried next cycle.
		}
	}
}

func (r *RayLogHandler) pollSingleEndpoint(ctx context.Context, endpoint, sessionName string, finalPoll bool) pollOutcome {
	body, err := r.fetchEndpoint(ctx, endpoint)
	if err != nil {
		logrus.Warnf("Failed to poll endpoint %s: %v", endpoint, err)
		return pollFailed
	}

	if isEmptyPayload(endpoint, body, finalPoll) {
		logrus.Debugf("Skipping %s: nothing to store", endpoint)
		return pollSkippedEmpty
	}

	// A canceled ctx means shutdown started: the final poll owns the store from here.
	if ctx.Err() != nil {
		return pollFailed
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
// Datasets: empty can mean stats-actor eviction, so it never overwrites a snapshot.
// Serve: empty from a healthy cluster is the live truth and is stored, but on the final
// shutdown poll it usually means the Serve controller died before the dashboard.
func isEmptyPayload(endpoint string, body []byte, finalPoll bool) bool {
	switch {
	case strings.HasPrefix(endpoint, dataDatasetsEndpointPrefix):
		return !hasDatasets(body)
	case finalPoll && endpoint == serveApplicationsEndpoint:
		return !hasServeApplications(body)
	default:
		return false
	}
}

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

// fetchEndpoint GETs one dashboard endpoint; in-flight requests are canceled on shutdown.
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
