package logcollector

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const (
	clusterMetadataEndpoint  = "/api/v0/cluster_metadata"
	metadataRetryInterval    = 5 * time.Second  // initial interval
	metadataMaxRetryInterval = 60 * time.Second // max cap for backoff
	metadataRequestTimeout   = 30 * time.Second // per-request timeout
)

// FetchAndStoreClusterMetadata fetches /api/v0/cluster_metadata from the Ray Dashboard
// once on startup and stores the result in storage per session. It retries with exponential
// backoff until the fetch succeeds or the collector is shut down.
//
// The metadata is stored per session (not per cluster) because different sessions can use
// different Ray images, resulting in different rayVersion / pythonVersion values.
func (r *RayLogHandler) FetchAndStoreClusterMetadata() {
	url := r.DashboardAddress + clusterMetadataEndpoint
	retryInterval := metadataRetryInterval

	// Resolve the session name first so we can store metadata under the correct session path.
	sessionName, err := r.resolveSessionName()
	if err != nil {
		logrus.Errorf("Failed to resolve session name for cluster metadata: %v", err)
		return
	}
	logrus.Infof("Resolved session name for cluster metadata: %s", sessionName)

	for {
		logrus.Infof("Fetching cluster metadata from %s", url)

		ctx, cancel := context.WithTimeout(context.Background(), metadataRequestTimeout)
		// Listen for shutdown to cancel in-flight request.
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
			logrus.Errorf("Failed to create request for fetching cluster metadata: %v", err)
			return
		}

		resp, err := r.HttpClient.Do(req)
		cancel()
		if err != nil {
			logrus.Warnf("Failed to fetch cluster metadata from %s: %v, retrying in %v", url, err, retryInterval)
			if !r.sleepOrShutdown(retryInterval) {
				return
			}
			retryInterval = nextBackoff(retryInterval)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			logrus.Warnf("Failed to read cluster metadata response body: %v, retrying in %v", err, retryInterval)
			if !r.sleepOrShutdown(retryInterval) {
				return
			}
			retryInterval = nextBackoff(retryInterval)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			logrus.Warnf("Cluster metadata returned status %d, retrying in %v", resp.StatusCode, retryInterval)
			if !r.sleepOrShutdown(retryInterval) {
				return
			}
			retryInterval = nextBackoff(retryInterval)
			continue
		}

		// Successfully fetched — store it under the session path
		storageKey := utils.EndpointPathToStorageKey(clusterMetadataEndpoint)
		objectKey := path.Join(r.ClusterDir, sessionName, utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME, storageKey)
		if err := r.Writer.WriteFile(objectKey, bytes.NewReader(body)); err != nil {
			logrus.Errorf("Failed to store cluster metadata at %s: %v", objectKey, err)
			// Retry storage write as well
			if !r.sleepOrShutdown(retryInterval) {
				return
			}
			retryInterval = nextBackoff(retryInterval)
			continue
		}

		logrus.Infof("Successfully stored cluster metadata at %s (%d bytes)", objectKey, len(body))
		return
	}
}

// resolveSessionName waits for the session_latest symlink to appear and resolves
// the session name from it. It retries with exponential backoff.
func (r *RayLogHandler) resolveSessionName() (string, error) {
	sessionLatestDir := filepath.Join("/tmp", "ray", "session_latest")
	retryInterval := metadataRetryInterval

	for {
		sessionRealDir, err := filepath.EvalSymlinks(sessionLatestDir)
		if err == nil {
			return filepath.Base(sessionRealDir), nil
		}

		logrus.Warnf("session_latest symlink not ready: %v, retrying in %v", err, retryInterval)
		if !r.sleepOrShutdown(retryInterval) {
			return "", fmt.Errorf("shutdown signaled while waiting for session_latest")
		}
		retryInterval = nextBackoff(retryInterval)
	}
}

// sleepOrShutdown sleeps for the given duration or returns false if shutdown is signaled.
func (r *RayLogHandler) sleepOrShutdown(d time.Duration) bool {
	select {
	case <-r.ShutdownChan:
		logrus.Info("Shutdown signaled, aborting cluster metadata fetch")
		return false
	case <-time.After(d):
		return true
	}
}

// nextBackoff doubles the interval up till metadataMaxRetryInterval.
func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > metadataMaxRetryInterval {
		next = metadataMaxRetryInterval
	}
	return next
}
