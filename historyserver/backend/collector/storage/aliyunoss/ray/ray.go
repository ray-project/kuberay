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
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/backend/collector/storage/aliyunoss/rrsa"
	"github.com/ray-project/kuberay/historyserver/backend/types"
	"github.com/ray-project/kuberay/historyserver/utils"
)

type RayLogsHandler struct {
	OssClient      *oss.Client
	OssBucket      *oss.Bucket
	SessionDir     string
	OSSRootLogDir  string
	OSSRootMetaDir string
	OssRootDir     string
	LogDir         string
	LogFiles       chan string
	EnableMeta     bool
	RayClusterName string
	RayClusterID   string
	HttpClient     *http.Client

	LogBatching  int
	PushInterval time.Duration
}

func (r *RayLogsHandler) CreateDirectory(path string) error {
	return nil
}

func (r *RayLogsHandler) Append(file string, reader io.Reader, appendPosition int64) (nextPod int64, err error) {
	return 0, nil
}

func NewWritter(c *types.RayCollectorConfig, jd map[string]interface{}) (*RayLogsHandler, error) {
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
		OssClient:      client,
		OssBucket:      bucket,
		SessionDir:     sessionDir,
		OSSRootLogDir:  utils.GetOssLogDir(c.OSSHistoryServerDir, c.RayClusterName, c.RayClusterID, c.RayNodeName),
		OSSRootMetaDir: utils.GetOssMetaDir(c.OSSHistoryServerDir, c.RayClusterName, c.RayClusterID),
		OssRootDir:     c.OSSHistoryServerDir,
		LogDir:         logdir,
		LogFiles:       make(chan string, 100),
		EnableMeta:     c.Role == "Head",
		RayClusterName: c.RayClusterName,
		RayClusterID:   c.RayClusterID,
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
