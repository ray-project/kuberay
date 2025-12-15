// Package s3 is
/*
Copyright 2024 by the kuberay authors.

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
package s3

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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

type RayLogsHandler struct {
	S3Client       *s3.S3
	LogFiles       chan string
	HttpClient     *http.Client
	S3Bucket       string
	SessionDir     string
	S3RootDir      string
	LogDir         string
	RayClusterName string
	RayClusterID   string
	RayNodeName    string
	LogBatching    int
	PushInterval   time.Duration
}

func (r *RayLogsHandler) CreateDirectory(d string) error {
	objectDir := fmt.Sprintf("%s/", path.Clean(d))

	_, err := r.S3Client.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(r.S3Bucket),
		Key:    aws.String(objectDir),
	})
	if err != nil {
		// Directory doesn't exist, create it
		logrus.Infof("Begin to create s3 dir %s ...", objectDir)
		_, err = r.S3Client.PutObject(&s3.PutObjectInput{
			Bucket: aws.String(r.S3Bucket),
			Key:    aws.String(objectDir),
			Body:   bytes.NewReader([]byte("")),
		})
		if err != nil {
			logrus.Errorf("Failed to create directory '%s': %v", objectDir, err)
			return err
		}
		logrus.Infof("Create s3 dir %s success", objectDir)
	}
	return nil
}

func (r *RayLogsHandler) WriteFile(file string, reader io.ReadSeeker) error {
	_, err := r.S3Client.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(r.S3Bucket),
		Key:    aws.String(file),
		Body:   reader,
	})
	return err
}

func (r *RayLogsHandler) _listFiles(prefix string, delimiter string, onlyBase bool) []string {
	files := []string{}

	listInput := &s3.ListObjectsV2Input{
		Bucket:    aws.String(r.S3Bucket),
		Prefix:    aws.String(prefix + "/"),
		MaxKeys:   aws.Int64(100),
		Delimiter: aws.String(delimiter),
	}

	err := r.S3Client.ListObjectsV2Pages(listInput,
		func(page *s3.ListObjectsV2Output, lastPage bool) bool {
			logrus.Infof("[ListFiles]Returned objects in %v. length of page.Contents: %v, length of page.CommonPrefixes: %v",
				prefix+"/", len(page.Contents), len(page.CommonPrefixes))

			for _, object := range page.Contents {
				objName := *object.Key
				if onlyBase {
					objName = path.Base(*object.Key)
				}
				files = append(files, objName)
			}

			for _, object := range page.CommonPrefixes {
				objName := *object.Prefix
				if onlyBase {
					objName = path.Base(*object.Prefix)
				}
				files = append(files, objName+"/")
			}
			return true
		})
	if err != nil {
		logrus.Errorf("Failed to list objects from %s: %v", prefix+"/", err)
		return []string{}
	}

	return files
}

func (r *RayLogsHandler) ListFiles(clusterId string, dir string) []string {
	prefix := path.Join(r.S3RootDir, clusterId, dir)

	defer func() {
		if recover := recover(); recover != nil {
			fmt.Println("Recovered from panic:", recover)
		}
	}()

	logrus.Debugf("Prepare to get list clusters info ...")
	nodes := r._listFiles(prefix, "/", true)
	// Note: clusters is not defined in this scope, removed sorting
	return nodes
}

func (r *RayLogsHandler) List() (res []utils.ClusterInfo) {
	defer func() {
		if recover := recover(); recover != nil {
			fmt.Println("Recovered from panic:", recover)
		}
	}()

	clusters := make(utils.ClusterInfoList, 0, 10)
	logrus.Debugf("Prepare to get list clusters info ...")

	getClusters := func() {
		listInput := &s3.ListObjectsV2Input{
			Bucket:    aws.String(r.S3Bucket),
			Prefix:    aws.String(path.Join(r.S3RootDir, "metadir") + "/"),
			MaxKeys:   aws.Int64(100),
			Delimiter: aws.String(""),
		}

		err := r.S3Client.ListObjectsV2Pages(listInput,
			func(page *s3.ListObjectsV2Output, lastPage bool) bool {
				logrus.Infof("[List]Returned objects in %v. length of page.Contents: %v, length of page.CommonPrefixes: %v",
					path.Join(r.S3RootDir, "metadir")+"/", len(page.Contents), len(page.CommonPrefixes))

				for _, object := range page.Contents {
					c := &utils.ClusterInfo{}
					metaInfo := strings.Trim(strings.TrimPrefix(*object.Key, path.Join(r.S3RootDir, "metadir/")), "/")
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
					createTime, err := time.Parse("2006-01-02_15-04-05", date+"_"+dataTime)
					if err != nil {
						logrus.Errorf("Failed to parse time %s: %v", date+"_"+dataTime, err)
						continue
					}
					c.CreateTimeStamp = createTime.Unix()
					c.CreateTime = createTime.UTC().Format(("2006-01-02T15:04:05Z"))
					clusters = append(clusters, *c)
				}
				return true
			})
		if err != nil {
			logrus.Errorf("Failed to list objects from %s: %v", path.Join(r.S3RootDir, "metadir")+"/", err)
			return
		}
	}

	getClusters()
	sort.Sort(clusters)
	return clusters
}

func (r *RayLogsHandler) GetContent(clusterId string, fileName string) io.Reader {
	logrus.Infof("Prepare to get object %s info ...", fileName)

	result, err := r.S3Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(r.S3Bucket),
		Key:    aws.String(fileName),
	})
	if err != nil {
		logrus.Errorf("Failed to get object %s: %v", fileName, err)
		allFiles := r._listFiles(clusterId+"/"+path.Dir(fileName), "", false)
		found := false
		for _, f := range allFiles {
			if path.Base(f) == fileName {
				logrus.Infof("Get object %s info success", f)
				result, err = r.S3Client.GetObject(&s3.GetObjectInput{
					Bucket: aws.String(r.S3Bucket),
					Key:    aws.String(f),
				})
				if err != nil {
					logrus.Errorf("Failed to get object %s: %v", f, err)
					return nil
				}
				found = true
				break
			}
		}
		if !found {
			logrus.Errorf("Failed to get object by list all files %s", fileName)
			return nil
		}
	}

	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
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

func NewWritter(c *types.RayCollectorConfig, jd map[string]interface{}) (storage.StorageWriter, error) {
	config := &config{}
	config.complete(c, jd)

	return New(config)
}

func New(c *config) (*RayLogsHandler, error) {
	logrus.Infof("Begin to create s3 client ...")

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Create AWS session
	sess, err := session.NewSession(&aws.Config{
		Credentials:      credentials.NewStaticCredentials(c.S3ID, c.S3Secret, c.S3Token),
		Endpoint:         aws.String(c.S3Endpoint),
		Region:           aws.String(c.S3Region),
		HTTPClient:       httpClient,
		DisableSSL:       c.DisableSSL,
		S3ForcePathStyle: c.S3ForcePathStyle, // IMPORTANT: Required for MinIO
	})
	if err != nil {
		logrus.Fatalf("Create aws session error %v", err)
	}

	s3Client := s3.New(sess)

	sessionDir := strings.TrimSpace(c.SessionDir)
	sessionDir = filepath.Clean(sessionDir)

	logdir := strings.TrimSpace(path.Join(sessionDir, utils.RAY_SESSIONDIR_LOGDIR_NAME))
	logdir = filepath.Clean(logdir)
	logrus.Infof("Clean logdir is %s", logdir)

	return &RayLogsHandler{
		S3Client:       s3Client,
		S3Bucket:       c.S3Bucket,
		SessionDir:     sessionDir,
		S3RootDir:      c.RootDir,
		LogDir:         logdir,
		LogFiles:       make(chan string, 100),
		RayClusterName: c.RayClusterName,
		RayClusterID:   c.RayClusterID,
		RayNodeName:    c.RayNodeName,
		HttpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		LogBatching:  c.LogBatching,
		PushInterval: c.PushInterval,
	}, nil
}
