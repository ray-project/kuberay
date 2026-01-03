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
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/storage/aliyunoss/rrsa"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

type RayLogsHandler struct {
	OssBucket      *oss.Bucket
	LogFiles       chan string
	HttpClient     *http.Client
	SessionDir     string
	OssRootDir     string
	LogDir         string
	RayClusterName string
	RayClusterID   string
	RayNodeName    string
	LogBatching    int
	PushInterval   time.Duration
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

func (r *RayLogsHandler) WriteFile(file string, reader io.ReadSeeker) error {
	return r.OssBucket.PutObject(file, reader)
}

func (r *RayLogsHandler) _listFiles(prefix string, delimiter string, onlyBase bool) []string {
	continueToken := ""
	files := []string{}
	for {
		options := []oss.Option{
			oss.Prefix(prefix + "/"),
			oss.ContinuationToken(continueToken),
			oss.MaxKeys(100),
			oss.Delimiter(delimiter),
		}

		// List all files
		lsRes, err := r.OssBucket.ListObjectsV2(options...)
		if err != nil {
			logrus.Errorf("Failed to list objects from %s: %v", prefix+"/", err)
			return []string{}
		}
		logrus.Infof("[ListFiles]Returned objects in %v. length of lsRes.Objects: %v, length of lsRes.CommonPrefixes: %v", prefix+"/", len(lsRes.Objects),
			len(lsRes.CommonPrefixes))
		for _, objects := range lsRes.Objects {
			objName := objects.Key
			if onlyBase {
				objName = path.Base(objects.Key)
			}
			files = append(files, objName)
		}
		for _, object := range lsRes.CommonPrefixes {
			objName := object
			if onlyBase {
				objName = path.Base(object)
			}
			files = append(files, objName+"/")
		}
		if lsRes.IsTruncated {
			continueToken = lsRes.NextContinuationToken
		} else {
			break
		}
	}
	return files
}

func (r *RayLogsHandler) ListFiles(clusterId string, dir string) []string {
	prefix := path.Join(r.OssRootDir, clusterId, dir)

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered from panic:", r)
		}
	}()
	// Initial continuation token
	clusters := make(utils.ClusterInfoList, 0, 10)
	logrus.Debugf("Prepare to get list clusters info ...")
	nodes := r._listFiles(prefix, "/", true)
	sort.Sort(clusters)
	return nodes
}

func (r *RayLogsHandler) List() (res []utils.ClusterInfo) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered from panic:", r)
		}
	}()
	// Initial continuation token
	continueToken := ""
	clusters := make(utils.ClusterInfoList, 0, 10)
	logrus.Debugf("Prepare to get list clusters info ...")

	getClusters := func() {
		for {
			options := []oss.Option{
				oss.Prefix(path.Join(r.OssRootDir, "metadir") + "/"),
				oss.ContinuationToken(continueToken),
				oss.MaxKeys(100),
				oss.Delimiter(""),
			}

			// List all files
			lsRes, err := r.OssBucket.ListObjectsV2(options...)
			if err != nil {
				logrus.Errorf("Failed to list objects from %s: %v", path.Join(r.OssRootDir, "metadir")+"/", err)
				return
			}
			logrus.Infof("[List]Returned objects in %v. length of lsRes.Objects: %v, length of lsRes.CommonPrefixes: %v", path.Join(r.OssRootDir, "metadir")+"/", len(lsRes.Objects),
				len(lsRes.CommonPrefixes))
			for _, objects := range lsRes.Objects {
				c := &utils.ClusterInfo{}
				metaInfo := strings.Trim(strings.TrimPrefix(objects.Key, path.Join(r.OssRootDir, "metadir/")), "/")
				metas := strings.Split(metaInfo, "/")
				if len(metas) < 2 {
					continue
				}
				logrus.Infof("Process %++v", metas)
				namespaceName := strings.Split(metas[0], "_")
				c.Name = namespaceName[0]
				c.Namespace = namespaceName[1]
				c.SessionName = metas[1]
				sessionInfo := strings.Split(metas[1], "_")
				date := sessionInfo[1]
				dataTime := sessionInfo[2]
				creationTime, err := time.Parse("2006-01-02_15-04-05", date+"_"+dataTime)
				if err != nil {
					logrus.Errorf("Failed to parse time %s: %v", date+"_"+dataTime, err)
					continue
				}
				c.CreationTimestamp = creationTime.Unix()
				c.CreationTime = creationTime.UTC().Format(("2006-01-02T15:04:05Z"))
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
		allFiles := r._listFiles(clusterId+"/"+path.Dir(fileName), "", false)
		found := false
		for _, f := range allFiles {
			if path.Base(f) == fileName {
				logrus.Infof("Get object %s info success", f)
				body, err = r.OssBucket.GetObject(f, options...)
				if err != nil {
					logrus.Errorf("Failed to get object %s: %v", f, err)
					return nil
				}
			}
		}
		if !found {
			logrus.Errorf("Failed to get object by list all files %s", fileName)
			return nil
		}
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

func NewWriter(c *types.RayCollectorConfig, jd map[string]interface{}) (storage.StorageWriter, error) {
	config := &config{}
	config.complete(c, jd)

	return New(config)
}

func New(c *config) (*RayLogsHandler, error) {
	logrus.Infof("Begin to create oss client ...")
	httpClient := &http.Client{
		Timeout: 5 * time.Second, // Set timeout
	}
	provider, err := rrsa.NewOssProvider()
	if err != nil {
		logrus.Fatalf("Create rrsa provider error %v", err)
	}
	var client *oss.Client
	client, err = oss.New(c.OSSEndpoint, "", "", oss.HTTPClient(httpClient), oss.SetCredentialsProvider(provider))
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
				MaxIdleConns:        100,              // Max idle connections
				MaxIdleConnsPerHost: 20,               // Max idle connections per host
				IdleConnTimeout:     90 * time.Second, // Idle connection timeout
			},
		},
		LogBatching:  c.LogBatching,
		PushInterval: c.PushInterval,
	}, nil
}
