package logcollector

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	"io"
	"path"
	"time"

	"github.com/sirupsen/logrus"
)

// TODO(alex): This file is just a work around because some ray resource events are not implemented yet.
// We should delete this file after history server can get the resources by ray events

var metaCommonUrlInfo = []*types.UrlInfo{
	{
		Key:  utils.OssMetaFile_Applications,
		Url:  "http://localhost:8265/api/serve/applications/",
		Type: "URL",
	},
	{
		Key:  utils.OssMetaFile_PlacementGroups,
		Url:  "http://localhost:8265/api/v0/placement_groups",
		Type: "URL",
	},
}

var JobsUrlInfo = &types.UrlInfo{
	Key:  utils.OssMetaFile_Jobs,
	Url:  "http://localhost:8265/api/jobs/",
	Type: "URL",
}

var JobResourcesUrlInfo = map[string]*types.JobUrlInfo{}

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
	for _, metaurl := range metaCommonUrlInfo {
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
	} else {
		logrus.Debugf("Meta URL %s response data has changed, old hash is %s, new hash is %s", urlinfo.Url, urlinfo.Hash, md5Hex)
		urlinfo.Hash = md5Hex
	}

	objectName := path.Join(r.MetaDir, urlinfo.Key)
	logrus.Debugf("Creating object %s...", objectName)
	err = r.Writer.WriteFile(objectName, bytes.NewReader(body))
	if err != nil {
		logrus.Errorf("Failed to create object '%s': %v", objectName, err)
		return body, err
	}
	logrus.Debugf("Successfully created object %s", objectName)
	return body, nil
}

func (r *RayLogHandler) PersistDatasetsMeta() {

	body, err := r.PersistUrlInfo(JobsUrlInfo)
	if err != nil {
		logrus.Errorf("Failed to persist meta url %s: %v", JobsUrlInfo.Url, err)
		return
	}
	var jobsData = []interface{}{}
	if err := json.Unmarshal(body, &jobsData); err != nil {
		logrus.Errorf("Ummarshal resp body error %v. key: %s response body: %v", err, JobsUrlInfo.Key, jobsData)
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
		if _, ok := JobResourcesUrlInfo[jobID]; !ok {
			JobResourcesUrlInfo[jobID] = &types.JobUrlInfo{
				Url: &types.UrlInfo{
					Key: fmt.Sprintf("%s%s", utils.OssMetaFile_JOBDATASETS_Prefix, jobID),
					Url: fmt.Sprintf("http://localhost:8265/api/data/datasets/%s", jobID),
				},
				Status: status,
			}
		}
	}

	for _, urlInfo := range JobResourcesUrlInfo {
		if urlInfo.StopPersist {
			continue
		}

		if _, err := r.PersistUrlInfo(urlInfo.Url); err != nil {
			logrus.Errorf("Persis task UrlInfo %s failed, error %v", urlInfo.Url.Url, err)
			// no need break
		}

		if urlInfo.Status == types.JOBSTATUS_FAILED ||
			urlInfo.Status == types.JOBSTATUS_STOPPED ||
			urlInfo.Status == types.JOBSTATUS_SUCCEEDED {
			urlInfo.StopPersist = true
		}
	}
}
