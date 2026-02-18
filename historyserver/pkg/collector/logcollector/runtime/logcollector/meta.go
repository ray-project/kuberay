package logcollector

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sync"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	"github.com/sirupsen/logrus"
)

// TODO(alex): This file is just a work around because some ray resource events are not implemented yet.
// We should delete this file after history server can get the resources by ray events

// JobResourcesUrlInfoMap is a thread-safe map for storing job resource URL information
// TODO(alex): consider to use lru cache if needed in order to prevent memory leak
type JobResourcesUrlInfoMap struct {
	data map[string]*types.JobUrlInfo
	mu   sync.RWMutex
}

func (j *JobResourcesUrlInfoMap) RLock() {
	j.mu.RLock()
}

func (j *JobResourcesUrlInfoMap) RUnlock() {
	j.mu.RUnlock()
}

func (j *JobResourcesUrlInfoMap) Lock() {
	j.mu.Lock()
}

func (j *JobResourcesUrlInfoMap) Unlock() {
	j.mu.Unlock()
}

func (j *JobResourcesUrlInfoMap) Get(key string) (*types.JobUrlInfo, bool) {
	j.RLock()
	defer j.RUnlock()
	val, ok := j.data[key]
	return val, ok
}

func (j *JobResourcesUrlInfoMap) Set(key string, val *types.JobUrlInfo) {
	j.Lock()
	defer j.Unlock()
	j.data[key] = val
}

func (j *JobResourcesUrlInfoMap) Delete(key string) {
	j.Lock()
	defer j.Unlock()
	delete(j.data, key)
}

func (j *JobResourcesUrlInfoMap) Keys() []string {
	j.RLock()
	defer j.RUnlock()
	keys := make([]string, 0, len(j.data))
	for k := range j.data {
		keys = append(keys, k)
	}
	return keys
}

var jobResourcesUrlInfo = &JobResourcesUrlInfoMap{
	data: make(map[string]*types.JobUrlInfo),
}

// InitMetaUrlInfo initializes the meta URL info with dashboard address
func (r *RayLogHandler) InitMetaUrlInfo() {
	r.MetaCommonUrlInfo = []*types.UrlInfo{
		{
			Key:  utils.OssMetaFile_Applications,
			Url:  fmt.Sprintf("%s/api/serve/applications/", r.DashboardAddress),
			Type: "URL",
		},
		{
			Key:  utils.OssMetaFile_PlacementGroups,
			Url:  fmt.Sprintf("%s/api/v0/placement_groups", r.DashboardAddress),
			Type: "URL",
		},
		{
			Key:  utils.OssMetaFile_ClusterMetadata,
			Url:  fmt.Sprintf("%s/api/v0/cluster_metadata", r.DashboardAddress),
			Type: "URL",
		},
		{
			Key:  utils.OssMetaFile_ClusterStatus,
			Url:  fmt.Sprintf("%s/api/cluster_status", r.DashboardAddress),
			Type: "URL",
		},
	}
	r.JobsUrlInfo = &types.UrlInfo{
		Key:  utils.OssMetaFile_Jobs,
		Url:  fmt.Sprintf("%s/api/jobs/", r.DashboardAddress),
		Type: "URL",
	}
}

func (r *RayLogHandler) PersistMetaLoop(stop <-chan struct{}) {
	// create meta directory
	if err := r.Writer.CreateDirectory(r.MetaDir); err != nil {
		logrus.Errorf("CreateDirectory %s error %v", r.MetaDir, err)
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.PersistMeta(); err != nil {
				logrus.Errorf("Failed to persist meta: %v", err)
			}
		case <-stop:
			logrus.Warnf("Received stop signal, returning from PersistMetaLoop")
			return
		}
	}
}

func (r *RayLogHandler) PersistMeta() error {
	for _, metaurl := range r.MetaCommonUrlInfo {
		if _, err := r.PersistUrlInfo(metaurl); err != nil {
			logrus.Errorf("Failed to persist URL info for %s: %v", metaurl.Url, err)
			// no need break or return
		}
	}
	// Datasets API is called by job ID, so we should handle it in a separate function
	r.PersistDatasetsMeta()

	return nil
}

func (r *RayLogHandler) PersistUrlInfo(urlinfo *types.UrlInfo) ([]byte, error) {

	logrus.Infof("Requesting URL %s for key file %s", urlinfo.Url, urlinfo.Key)

	resp, err := r.HttpClient.Get(urlinfo.Url)
	if err != nil {
		logrus.Errorf("Failed to request %s: %v", urlinfo.Url, err)
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("Failed to read response body from %s: %v", urlinfo.Url, err)
		return nil, err
	}

	// check in memory cache, if the response body is the same with that in cache, skip writting into object store
	md5Hash := md5.Sum(body)
	md5Hex := hex.EncodeToString(md5Hash[:])
	if md5Hex == urlinfo.Hash {
		logrus.Debugf("Meta URL %s response data has not changed, no need to persist", urlinfo.Url)
		return body, nil
	}

	objectName := path.Join(r.MetaDir, urlinfo.Key)
	logrus.Debugf("Creating object %s...", objectName)
	err = r.Writer.WriteFile(objectName, bytes.NewReader(body))
	if err != nil {
		logrus.Errorf("Failed to create object '%s': %v", objectName, err)
		return body, err
	}
	// Write hash after object store persisted to prevent data inconsistency
	urlinfo.Hash = md5Hex

	logrus.Debugf("Successfully created object %s", objectName)
	return body, nil
}

func (r *RayLogHandler) PersistDatasetsMeta() {
	body, err := r.PersistUrlInfo(r.JobsUrlInfo)
	if err != nil {
		logrus.Errorf("Failed to persist meta url %s: %v", r.JobsUrlInfo.Url, err)
		return
	}
	var jobsData = []interface{}{}
	if err := json.Unmarshal(body, &jobsData); err != nil {
		logrus.Errorf("Ummarshal resp body error %v. key: %s response body: %v", err, r.JobsUrlInfo.Key, jobsData)
		return
	}
	currentJobIDs := make(map[string]string, 0)
	for _, jobinfo := range jobsData {
		job := jobinfo.(map[string]interface{})
		jobid, ok := job["job_id"].(string)
		if !ok {
			continue
		}
		status, ok := job["status"].(string)
		if !ok {
			continue
		}
		currentJobIDs[jobid] = status
	}

	for jobID, status := range currentJobIDs {
		if existingJob, ok := jobResourcesUrlInfo.Get(jobID); !ok {
			// Add new job
			jobResourcesUrlInfo.Set(jobID, &types.JobUrlInfo{
				Url: &types.UrlInfo{
					Key: fmt.Sprintf("%s%s", utils.OssMetaFile_JOBDATASETS_Prefix, jobID),
					Url: fmt.Sprintf("%s/api/data/datasets/%s", r.DashboardAddress, jobID),
				},
				Status: status,
			})
		} else if !existingJob.StopPersist {
			// Update status for existing jobs only if not already stopped persisting
			existingJob.Status = status
		}
	}

	// Process each job individually to avoid holding lock for too long
	allJobIDs := jobResourcesUrlInfo.Keys()
	for _, jobID := range allJobIDs {
		urlInfo, ok := jobResourcesUrlInfo.Get(jobID)
		if !ok {
			continue
		}

		if urlInfo.StopPersist {
			continue
		}

		var isPersistentSuccess = true
		if _, err := r.PersistUrlInfo(urlInfo.Url); err != nil {
			logrus.Errorf("Persis task UrlInfo %s failed, error %v", urlInfo.Url.Url, err)
			isPersistentSuccess = false
		}

		if urlInfo.Status == types.JOBSTATUS_FAILED ||
			urlInfo.Status == types.JOBSTATUS_STOPPED ||
			urlInfo.Status == types.JOBSTATUS_SUCCEEDED {
			// Only mark StopPersist when persistent success in order to prevent data inconsistency
			if isPersistentSuccess {
				urlInfo.StopPersist = true
			}
		}
	}
}
