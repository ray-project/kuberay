package logcollector

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const (
	timezoneEndpoint = "/timezone"
)

type timezoneInfo struct {
	Offset string `json:"offset"`
	Value  string `json:"value"`
}

func (r *RayLogHandler) writeTimezoneMeta(sessionID string) {
	tzInfo, ok := r.fetchDashboardTimezone()
	if !ok {
		logrus.Warnf("Skipping timezone metadata write for session %s: dashboard not available", sessionID)
		return
	}

	storageKey := utils.EndpointPathToStorageKey(timezoneEndpoint)
	objectKey := path.Join(
		r.ClusterDir,
		path.Base(sessionID),
		utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME,
		storageKey,
	)

	data, err := json.Marshal(tzInfo)
	if err != nil {
		logrus.Errorf("Failed to marshal timezone info: %v", err)
		return
	}

	if err := r.Writer.WriteFile(objectKey, bytes.NewReader(data)); err != nil {
		logrus.Errorf("Failed to write timezone meta file %s: %v", objectKey, err)
	}
}

func (r *RayLogHandler) fetchDashboardTimezone() (timezoneInfo, bool) {
	url := r.DashboardAddress + timezoneEndpoint

	client := r.HttpClient
	if client == nil {
		client = http.DefaultClient
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var tzInfo timezoneInfo

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logrus.Warnf("Failed to create dashboard timezone request: %v", err)
		return tzInfo, false
	}

	resp, err := client.Do(req)
	if err != nil {
		logrus.Warnf("Failed to fetch dashboard timezone from %s: %v", url, err)
		return tzInfo, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logrus.Warnf("Unexpected dashboard timezone status %d from %s", resp.StatusCode, url)
		return tzInfo, false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Warnf("Failed to read dashboard timezone response from %s: %v", url, err)
		return tzInfo, false
	}

	if err := json.Unmarshal(body, &tzInfo); err != nil {
		logrus.Warnf("Failed to parse dashboard timezone response from %s: %v", url, err)
		return tzInfo, false
	}

	if tzInfo.Offset == "" && tzInfo.Value == "" {
		logrus.Warnf("Dashboard timezone response from %s is empty", url)
		return tzInfo, false
	}

	return tzInfo, true
}
