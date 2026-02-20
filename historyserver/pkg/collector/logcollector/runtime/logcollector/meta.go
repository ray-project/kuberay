package logcollector

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const (
	defaultDashboardPort     = 8265
	metadataRetryInterval    = 5 * time.Second  // initial interval
	metadataMaxRetryInterval = 60 * time.Second // max cap for backoff
	metadataRequestTimeout   = 30 * time.Second // per-request timeout
)

// dashboardAddress returns the Ray Dashboard base URL.
// Uses set DashboardAddress or defaults to http://localhost:8265.
func (r *RayLogHandler) dashboardAddress() string {
	if r.DashboardAddress != "" {
		return r.DashboardAddress
	}
	return fmt.Sprintf("http://localhost:%d", defaultDashboardPort)
}

// FetchAndStoreClusterMetadata fetches /api/v0/cluster_metadata from the Ray Dashboard
// once on startup and stores the result in storage. It retries with exponential backoff
// until the fetch succeeds or the collector is shut down.
func (r *RayLogHandler) FetchAndStoreClusterMetadata() {
	url := r.dashboardAddress() + "/api/v0/cluster_metadata"
	retryInterval := metadataRetryInterval

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

		if resp.StatusCode != 200 {
			logrus.Warnf("Cluster metadata returned status %d, retrying in %v", resp.StatusCode, retryInterval)
			if !r.sleepOrShutdown(retryInterval) {
				return
			}
			retryInterval = nextBackoff(retryInterval)
			continue
		}

		// Successfully fetched — store it
		objectKey := path.Join(r.MetaDir, utils.OssMetaFile_ClusterMetadata)
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
