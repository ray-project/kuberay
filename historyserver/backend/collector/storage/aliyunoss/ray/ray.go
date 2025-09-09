// Package ray is
/*
Copyright 2024 by the zhangjie bingyu.zj@alibaba-inc.com Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package ray

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/backend/collector/storage"
	"github.com/ray-project/kuberay/historyserver/backend/collector/storage/aliyunoss/rrsa"
	"github.com/ray-project/kuberay/historyserver/backend/types"
	"github.com/ray-project/kuberay/historyserver/utils"
)

type RayLogsHandler struct {
	OssBucket      *oss.Bucket
	SessionDir     string
	OssRootDir     string
	LogDir         string
	LogFiles       chan string
	RayClusterName string
	RayClusterID   string
	RayNodeName    string
	HttpClient     *http.Client

	LogBatching  int
	PushInterval time.Duration
}

func (r *RayLogsHandler) CreateDirectory(d string) error {
	objectDir := fmt.Sprintf("%s/", path.Clean(d))

	isExist, err := r.OssBucket.IsObjectExist(objectDir)
	if err != nil {
		logrus.Errorf("Failed to check if dirObject %s exists: %v", objectDir, err)
		return err
	}
	if !isExist {
		logrus.Infof("Begin to create oss dir %s ...", objectDir)
		err = r.OssBucket.PutObject(objectDir, bytes.NewReader([]byte("")))
		if err != nil {
			logrus.Errorf("Failed to create directory '%s': %v", objectDir, err)
			return err
		}
		logrus.Infof("Create oss dir %s success", objectDir)
	}
	return nil
}

func (r *RayLogsHandler) Append(file string, reader io.Reader, appendPosition int64) (nextPod int64, err error) {
	return r.OssBucket.AppendObject(file, reader, appendPosition)
}

func (r *RayLogsHandler) WriteFile(file string, reader io.Reader) error {
	return r.OssBucket.PutObject(file, reader)
}

func (r *RayLogsHandler) List() []utils.ClusterInfo {
	// 初始的继续标记
	continueToken := ""
	clusters := make(utils.ClusterInfoList, 0, 10)
	logrus.Debugf("Prepare to get list clusters info ...")

	getClusters := func() {
		for {
			options := []oss.Option{
				oss.Prefix(path.Join(r.OssRootDir, "cluster_list") + "/"),
				oss.ContinuationToken(continueToken),
				oss.MaxKeys(100),
				oss.Delimiter("/"),
			}

			// 列举所有文件
			lsRes, err := r.OssBucket.ListObjectsV2(options...)
			if err != nil {
				logrus.Errorf("Failed to list objects from %s: %v", path.Join(r.OssRootDir, "cluster_list")+"/", err)
				return
			}
			logrus.Infof("Returned objects in %v. length of lsRes.Objects: %v, length of lsRes.CommonPrefixes: %v", path.Join(r.OssRootDir, "cluster_list")+"/", len(lsRes.Objects),
				len(lsRes.CommonPrefixes))
			for _, objects := range lsRes.Objects {
				logrus.Infof("Process %++v", objects)
				c := &utils.ClusterInfo{}
				metas := strings.Split(objects.Key, "#")
				if len(metas) < 4 {
					continue
				}
				c.Name = path.Base(metas[0]) + "_" + metas[1]
				c.SessionName = metas[2]
				cs, _ := strconv.ParseInt(metas[3], 10, 64)
				c.CreateTimeStamp = cs
				t := time.Unix(cs, 0)
				c.CreateTime = t.UTC().Format(("2006-01-02T15:04:05Z"))
				clusters = append(clusters, *c)
			}
			if lsRes.IsTruncated {
				continueToken = lsRes.NextContinuationToken
			} else {
				break
			}
		}
	}
	getClusters()
	sort.Sort(clusters)
	return clusters
}

func (r *RayLogsHandler) GetContent(clusterId string, fileName string) io.Reader {
	logrus.Infof("Prepare to get object %s info ...", fileName)
	options := []oss.Option{}
	body, err := r.OssBucket.GetObject(fileName, options...)
	if err != nil {
		logrus.Errorf("Failed to get object %s: %v", fileName, err)
		return nil
	}
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		logrus.Errorf("Failed to read all data from object %s : %v", fileName, err)
		return nil
	}
	return bytes.NewReader(data)
}

func NewReader(c *types.RayHistoryServerConfig, jd map[string]interface{}) (storage.StorageReader, error) {
	config := &config{}
	config.completeHSConfig(c, jd)

	return New(config)
}

func NewWritter(c *types.RayCollectorConfig, jd map[string]interface{}) (storage.StorageWritter, error) {
	config := &config{}
	config.complete(c, jd)

	return New(config)
}

func New(c *config) (*RayLogsHandler, error) {
	logrus.Infof("Begin to create oss client ...")
	httpClient := &http.Client{
		Timeout: 5 * time.Second, // 设置超时时间
	}
	provider, err := rrsa.NewOssProvider()
	if err != nil {
		logrus.Fatalf("Create rrsa provider error %v", err)
	}
	client, err := oss.New(c.OSSEndpoint, "", "", oss.HTTPClient(httpClient), oss.SetCredentialsProvider(provider))
	if err != nil {
		logrus.Fatalf("Create oss client error %v", err)
	}
	logrus.Infof("Begin to create oss bucket %s ...", c.OSSBucket)
	bucket, err := client.Bucket(c.OSSBucket)
	if err != nil {
		logrus.Fatalf("Create oss bucket instance error %v", err)
	}
	sessionDir := strings.TrimSpace(c.SessionDir)
	sessionDir = filepath.Clean(sessionDir)

	logdir := strings.TrimSpace(path.Join(sessionDir, utils.RAY_SESSIONDIR_LOGDIR_NAME))
	logdir = filepath.Clean(logdir)
	logrus.Infof("Clean logdir is %s", logdir)

	return &RayLogsHandler{
		OssBucket:      bucket,
		SessionDir:     sessionDir,
		OssRootDir:     c.RootDir,
		LogDir:         logdir,
		LogFiles:       make(chan string, 100),
		RayClusterName: c.RayClusterName,
		RayClusterID:   c.RayClusterID,
		RayNodeName:    c.RayNodeName,
		HttpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,              // 最大空闲连接数
				MaxIdleConnsPerHost: 20,               // 每个主机的最大空闲连接数
				IdleConnTimeout:     90 * time.Second, // 空闲连接的超时时间
			},
		},
		LogBatching:  c.LogBatching,
		PushInterval: c.PushInterval,
	}, nil
}
