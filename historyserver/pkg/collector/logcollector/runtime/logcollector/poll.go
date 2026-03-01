package logcollector

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// PollAdditionalEndpointsPeriodically periodically fetches user-configured additional
// endpoints from the Ray Dashboard and stores their responses in storage. Unlike
// FetchAndStoreClusterMetadata (which runs once), this runs continuously on a timer
// until shutdown.
//
// Each endpoint response is stored at: {ClusterDir}/{sessionName}/fetched_endpoints/{storageKey}
// where storageKey is derived from the endpoint path (e.g., "/api/v0/nodes/summary"
// becomes "restful__api__v0__nodes__summary"). Each poll cycle overwrites the
// previous response.
func (r *RayLogHandler) PollAdditionalEndpointsPeriodically() {
	if len(r.AdditionalEndpoints) == 0 {
		logrus.Info("No additional endpoints configured, skipping polling")
		return
	}

	// Note: session name staleness is not a concern here because the log collector runs as a
	// sidecar in the Ray head pod. If the Ray head process restarts (creating a new session),
	// the entire pod — including this sidecar container — restarts, so resolveSessionName()
	// always runs in a fresh container lifecycle with the current session.
	sessionName, err := r.resolveSessionName()
	if err != nil {
		logrus.Errorf("Failed to resolve session name for additional endpoints polling: %v", err)
		return
	}
	logrus.Infof("Starting additional endpoints polling (interval=%v, endpoints=%v)", r.EndpointPollInterval, r.AdditionalEndpoints)

	// Perform an initial poll immediately on startup.
	r.pollAllEndpoints(sessionName)

	ticker := time.NewTicker(r.EndpointPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ShutdownChan:
			logrus.Info("Shutdown signaled, stopping additional endpoints polling")
			return
		case <-ticker.C:
			r.pollAllEndpoints(sessionName)
		}
	}
}

// processAdditionalEndpoints performs a final poll of all additional endpoints
// before shutdown. This mirrors processSessionLatestLogs as a shutdown cleanup step.
//
// Unlike PollAdditionalEndpointsPeriodically, this does NOT retry session name
// resolution because it runs during shutdown — if session_latest is gone (e.g.,
// Ray head already exited), retrying would hang forever since ShutdownChan has
// not been closed yet.
func (r *RayLogHandler) processAdditionalEndpoints() {
	if len(r.AdditionalEndpoints) == 0 {
		return
	}
	logrus.Info("Processing additional endpoints before shutdown")

	// Resolve session name directly without retry — this is a shutdown path.
	sessionLatestDir := utils.RaySessionLatestPath
	sessionRealDir, err := filepath.EvalSymlinks(sessionLatestDir)
	if err != nil {
		logrus.Errorf("Failed to resolve session name for final additional endpoints poll: %v", err)
		return
	}
	sessionName := filepath.Base(sessionRealDir)

	r.pollAllEndpoints(sessionName)
	logrus.Info("Finished processing additional endpoints")
}

// pollAllEndpoints fetches all configured additional endpoints and stores their responses.
func (r *RayLogHandler) pollAllEndpoints(sessionName string) {
	for _, endpoint := range r.AdditionalEndpoints {
		r.pollSingleEndpoint(endpoint, sessionName)
	}
}

// pollSingleEndpoint fetches a single endpoint from the Ray Dashboard and writes
// the response to storage.
func (r *RayLogHandler) pollSingleEndpoint(endpoint, sessionName string) {
	url := r.DashboardAddress + endpoint

	ctx, cancel := context.WithTimeout(context.Background(), metadataRequestTimeout)
	go func() {
		select {
		case <-r.ShutdownChan:
			cancel()
		case <-ctx.Done():
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		logrus.Errorf("Failed to create request for additional endpoint %s: %v", endpoint, err)
		return
	}

	resp, err := r.HttpClient.Do(req)
	if err != nil {
		cancel()
		logrus.Warnf("Failed to fetch additional endpoint %s: %v", endpoint, err)
		return
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	cancel()
	if err != nil {
		logrus.Warnf("Failed to read response body for additional endpoint %s: %v", endpoint, err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		logrus.Warnf("Additional endpoint %s returned status %d", endpoint, resp.StatusCode)
		return
	}

	storageKey := utils.EndpointPathToStorageKey(endpoint)
	objectKey := path.Join(r.ClusterDir, sessionName, utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME, storageKey)
	if err := r.Writer.WriteFile(objectKey, bytes.NewReader(body)); err != nil {
		logrus.Errorf("Failed to store additional endpoint %s at %s: %v", endpoint, objectKey, err)
		return
	}

	logrus.Infof("Successfully stored additional endpoint %s at %s (%d bytes)", endpoint, objectKey, len(body))
}
