package logcollector

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/ray-project/kuberay/historyserver/utils"
	"github.com/sirupsen/logrus"
)

func (r *RayLogHandler) PersistMetaLoop(stop chan struct{}) {
	clistDir := path.Clean(r.RootDir + "/" + "cluster_list")
	if err := r.Writter.CreateDirectory(clistDir); err != nil {
		logrus.Errorf("CreateDirectory %s error %v", clistDir, err)
		return
	}

	//
	//	r.RootDir/cluster_list
	//  | - RayClusterName_RayClusterId_SessionId_timeStamp
	//  | - RayClusterName_RayClusterId_SessionId_timeStamp
	//  r.RootDir/RayClusterName_RayClusterID_meta
	//
	now := time.Now()
	timestamp := now.Unix()
	createTime := now.UTC().Format(("2006-01-02T15:04:05Z"))
	rayClusterInfo := map[string]interface{}{
		"rayClusterName":  r.RayClusterName,
		"rayClusterID":    r.RayClusterID,
		"sessionId":       path.Base(r.SessionDir),
		"createTimestamp": timestamp,
	}
	data, err := json.Marshal(rayClusterInfo)
	if err != nil {
		logrus.Errorf("Build cluster meta information error %v", err)
		return
	}
	fileName := path.Clean(clistDir + "/" + r.RayClusterName + "_" + r.RayClusterID + "_" + path.Base(r.SessionDir) + "_" + fmt.Sprintf("%v", timestamp))
	if err := r.Writter.WriteFile(fileName, bytes.NewReader(data)); err != nil {
		logrus.Errorf("CreateObjectIfNotExist %s error %v", fileName, err)
		return
	}

	if err := r.Writter.CreateDirectory(r.MetaDir); err != nil {
		logrus.Errorf("CreateDirectory %s error %v", r.MetaDir, err)
		return
	}

	// persist basic info
	clusterInfoData, err := json.MarshalIndent(&utils.ClusterInfo{
		CreateTime:      createTime,
		Name:            r.RayClusterName,
		SessionName:     path.Base(r.SessionDir),
		CreateTimeStamp: timestamp,
	}, "", "  ")
	if err != nil {
		logrus.Errorf("MarshalIndent error %v", err)
		return
	}
	basicInfoFileName := path.Join(r.MetaDir, utils.OssMetaFile_BasicInfo)
	logrus.Debugf("Begin to create oss object %s, value: %v ...", basicInfoFileName, string(clusterInfoData))
	if err := r.Writter.WriteFile(basicInfoFileName, bytes.NewReader(clusterInfoData)); err != nil {
		logrus.Errorf("Failed to create object '%s': %v", basicInfoFileName, err)
		return
	}
	logrus.Debugf("Create oss object %s success", basicInfoFileName)

	// 设置每隔秒执行一次
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop() // 确保在终止时停止ticker

	for {
		select {
		case <-ticker.C:
			if err := r.PersistMeta(); err != nil {
				logrus.Errorf("Failed to persist meta: %v", err)
			}
		case <-stop:
			logrus.Warnf("Receive stop signal, so return PushLogsLoop")
			return
		}
	}
}
func (r *RayLogHandler) PersistMeta() error {

	logrus.Debugf("Begin to PersisMeta ...")
	defer logrus.Debugf("End to PersisMeta ...")

	for _, metaurl := range metaCommonUrlInfo {
		if _, err := r.PersisUrlInfo(metaurl); err != nil {
			logrus.Errorf("PersisUrlInfo %s error %v", metaurl.Url, err)
			// no need break or return
		}
	}
	r.PersisMetaNodeSummary()
	r.PersisMetaJobsSummary()

	return nil
}

func (r *RayLogHandler) PersisUrlInfo(urlinfo *UrlInfo) ([]byte, error) {

	logrus.Debugf("Begin to request url %s to key file %s", urlinfo.Url, urlinfo.Key)

	resp, err := r.HttpClient.Get(urlinfo.Url)
	if err != nil {
		logrus.Errorf("request %s error %v", urlinfo.Url, err)
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("request %s read body error %v", urlinfo.Url, err)
		return nil, err
	}

	md5Hash := md5.Sum(body)
	md5Hex := hex.EncodeToString(md5Hash[:])
	if md5Hex == urlinfo.Hash {
		logrus.Debugf("meta url %s respons data no change , no need persistent ", urlinfo.Url)
		return body, nil
	} else {
		logrus.Debugf("meta url %s respons data has change , old hash is %s new is %s", urlinfo.Url, urlinfo.Hash, md5Hex)
		urlinfo.Hash = md5Hex
	}

	objectName := path.Join(r.MetaDir, urlinfo.Key)
	logrus.Debugf("Begin to create oss object %s ...", objectName)
	err = r.Writter.WriteFile(objectName, bytes.NewReader(body))
	if err != nil {
		logrus.Errorf("Failed to create object '%s': %v", objectName, err)
		return body, err
	}
	logrus.Debugf("Create oss object %s success", objectName)
	return body, nil
}

func (r *RayLogHandler) PersisMetaNodeSummary() {
	body, err := r.PersisUrlInfo(NodeSummaryUrlInfo)
	if err != nil {
		logrus.Errorf("Failed to persist meta url %s: %v", NodeSummaryUrlInfo.Url, err)
		return
	}

	var reponData = map[string]interface{}{}
	if err := json.Unmarshal(body, &reponData); err != nil {
		logrus.Errorf("Ummarshal resp body error %v. key %s repons body %s", err, NodeSummaryUrlInfo.Key, reponData)
		return
	}

	data := reponData["data"].(map[string]interface{})
	summary := data["summary"].([]interface{})
	currentNodeIDS := make(map[string]struct{}, 0)

	for _, node := range summary {
		nodeSummary := node.(map[string]interface{})
		rayletInfo := nodeSummary["raylet"].(map[string]interface{})
		nodeId := rayletInfo["nodeId"].(string)
		currentNodeIDS[nodeId] = struct{}{}
	}

	for oldnodeID, _ := range NodeUrlInfo {
		if _, ok := currentNodeIDS[oldnodeID]; !ok {
			delete(NodeUrlInfo, oldnodeID)
		}
	}

	for newnodeID, _ := range currentNodeIDS {
		if _, ok := NodeUrlInfo[newnodeID]; !ok {
			NodeUrlInfo[newnodeID] = make([]*UrlInfo, 0, 2)

			NodeUrlInfo[newnodeID] = append(NodeUrlInfo[newnodeID],
				&UrlInfo{
					Key: fmt.Sprintf("%s%s", utils.OssMetaFile_Node_Prefix, newnodeID),
					Url: fmt.Sprintf("http://localhost:8265/nodes/%s", newnodeID),
				})
			NodeUrlInfo[newnodeID] = append(NodeUrlInfo[newnodeID],
				&UrlInfo{
					Key: fmt.Sprintf("%s%s", utils.OssMetaFile_NodeLogs_Prefix, newnodeID),
					Url: fmt.Sprintf("http://localhost:8265/api/v0/logs?node_id=%s", newnodeID),
				})
		}
	}

	for _, nodeUrlInfoList := range NodeUrlInfo {
		for _, nodeUrlInfo := range nodeUrlInfoList {
			if _, err := r.PersisUrlInfo(nodeUrlInfo); err != nil {
				logrus.Errorf("Persisnode UrlInfo %s error %v", nodeUrlInfo.Url, err)
			}
		}
	}
	return
}

func (r *RayLogHandler) PersisMetaJobsSummary() {

	body, err := r.PersisUrlInfo(JobsUrlInfo)
	if err != nil {
		logrus.Errorf("Failed to persist meta url %s: %v", JobsUrlInfo.Url, err)
		return
	}
	var reponData = []interface{}{}
	if err := json.Unmarshal(body, &reponData); err != nil {
		logrus.Errorf("Ummarshal resp body error %v. key %s repons body %s", err, JobsUrlInfo.Key, reponData)
		return
	}
	currentJobIDS := make(map[string]string, 0)
	for _, jobinfo := range reponData {
		job := jobinfo.(map[string]interface{})
		jobid, ok := job["job_id"].(string)
		if !ok {
			continue
		}
		status, statusOk := job["status"].(string)
		if !statusOk {
			continue
		}
		currentJobIDS[jobid] = status
	}

	for jobID, status := range currentJobIDS {
		if _, ok := r.JobResourcesUrlInfo[jobID]; !ok {
			r.JobResourcesUrlInfo[jobID] = &JobUrlInfo{
				UrlInfos: []*UrlInfo{
					&UrlInfo{
						Key: fmt.Sprintf("%s%s", utils.OssMetaFile_JOBTASK_DETAIL_Prefix, jobID),
						Url: fmt.Sprintf("http://localhost:8265/api/v0/tasks?detail=1&limit=10000&filter_keys=job_id&filter_predicates=%%3D&filter_values=%s", jobID),
					},
					&UrlInfo{
						Key: fmt.Sprintf("%s%s", utils.OssMetaFile_JOBTASK_SUMMARIZE_BY_FUNC_NAME_Prefix, jobID),
						Url: fmt.Sprintf("http://localhost:8265/api/v0/tasks/summarize?filter_keys=job_id&filter_predicates=%%3D&filter_values=%s", jobID),
					},
					&UrlInfo{
						Key: fmt.Sprintf("%s%s", utils.OssMetaFile_JOBTASK_SUMMARIZE_BY_LINEAGE_Prefix, jobID),
						Url: fmt.Sprintf("http://localhost:8265/api/v0/tasks/summarize?filter_keys=job_id&filter_predicates=%%3D&filter_values=%s&summary_by=lineage", jobID),
					},
					&UrlInfo{
						Key: fmt.Sprintf("%s%s", utils.OssMetaFile_JOBDATASETS_Prefix, jobID),
						Url: fmt.Sprintf("http://localhost:8265/api/data/datasets/%s", jobID),
					},
				},
				Status: status,
			}
		}
	}

	for jobID, taskurlsInfo := range r.JobResourcesUrlInfo {
		if taskurlsInfo.StopPersist {
			logrus.Debugf("JobID %s stop perisist, status is ", jobID, taskurlsInfo.Status)
			continue
		}
		var hasError bool
		for _, taskurl := range taskurlsInfo.UrlInfos {
			if _, err := r.PersisUrlInfo(taskurl); err != nil {
				logrus.Errorf("Persis task UrlInfo %s error %v", taskurl.Url, err)
				// no need break
				hasError = true
			}
		}
		if taskurlsInfo.Status == JOBSTATUS_FAILED ||
			taskurlsInfo.Status == JOBSTATUS_STOPPED ||
			taskurlsInfo.Status == JOBSTATUS_SUCCEEDED {
			if !hasError {
				taskurlsInfo.StopPersist = true
			}
		}
	}

	return
}
