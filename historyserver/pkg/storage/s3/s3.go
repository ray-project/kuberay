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
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

type RayLogsHandler struct {
	S3Client       *s3.Client
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
	ctx := context.Background()
	objectDir := fmt.Sprintf("%s/", path.Clean(d))

	_, err := r.S3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(r.S3Bucket),
		Key:    aws.String(objectDir),
	})
	if err != nil {
		// Directory doesn't exist, create it
		logrus.Infof("Begin to create s3 dir %s ...", objectDir)
		_, err = r.S3Client.PutObject(ctx, &s3.PutObjectInput{
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
	ctx := context.Background()
	// Reset reader to the beginning to ensure we read from the start
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to start of reader: %w", err)
	}
	_, err := r.S3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(r.S3Bucket),
		Key:    aws.String(file),
		Body:   reader,
	})
	return err
}

func (r *RayLogsHandler) _listFiles(prefix string, delimiter string, onlyBase bool) []string {
	ctx := context.Background()
	files := []string{}

	paginator := s3.NewListObjectsV2Paginator(r.S3Client, &s3.ListObjectsV2Input{
		Bucket:    aws.String(r.S3Bucket),
		Prefix:    aws.String(prefix + "/"),
		MaxKeys:   aws.Int32(100),
		Delimiter: aws.String(delimiter),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			logrus.Errorf("Failed to list objects from %s: %v", prefix+"/", err)
			return []string{}
		}
		logrus.Infof("[ListFiles]Returned objects in %v. length of page.Contents: %v, length of page.CommonPrefixes: %v",
			prefix+"/", len(page.Contents), len(page.CommonPrefixes))

		for _, object := range page.Contents {
			objName := aws.ToString(object.Key)
			if onlyBase {
				objName = path.Base(objName)
			}
			files = append(files, objName)
		}

		for _, object := range page.CommonPrefixes {
			objName := aws.ToString(object.Prefix)
			if onlyBase {
				objName = path.Base(objName)
			}
			files = append(files, objName+"/")
		}
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
	ctx := context.Background()
	defer func() {
		if recover := recover(); recover != nil {
			fmt.Println("Recovered from panic:", recover)
		}
	}()

	clusters := make(utils.ClusterInfoList, 0, 10)
	logrus.Debugf("Prepare to get list clusters info ...")

	getClusters := func() {
		listPrefix := path.Join(r.S3RootDir, "metadir") + "/"
		paginator := s3.NewListObjectsV2Paginator(r.S3Client, &s3.ListObjectsV2Input{
			Bucket:    aws.String(r.S3Bucket),
			Prefix:    aws.String(listPrefix),
			MaxKeys:   aws.Int32(100),
			Delimiter: aws.String(""),
		})

		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				logrus.Errorf("Failed to list objects from %s: %v", listPrefix, err)
				return
			}
			logrus.Infof("[List]Returned objects in %v. length of page.Contents: %v, length of page.CommonPrefixes: %v",
				listPrefix, len(page.Contents), len(page.CommonPrefixes))

			for _, object := range page.Contents {
				c := &utils.ClusterInfo{}
				metaInfo := strings.Trim(strings.TrimPrefix(aws.ToString(object.Key), listPrefix), "/")
				metas := strings.Split(metaInfo, "/")
				if len(metas) < 2 {
					continue
				}

				logrus.Infof("Process %++v", metas)
				namespaceName := strings.Split(metas[0], "_")
				if len(namespaceName) < 2 {
					logrus.Warnf("Skip invalid namespace name %q in %s", metas[0], metaInfo)
					continue
				}
				c.Name = namespaceName[0]
				c.Namespace = namespaceName[1]
				c.SessionName = metas[1]

				sessionInfo := strings.Split(metas[1], "_")
				if len(sessionInfo) < 3 {
					logrus.Warnf("Skip invalid session info %q in %s", metas[1], metaInfo)
					continue
				}
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
		}
	}

	getClusters()
	sort.Sort(clusters)
	return clusters
}

func (r *RayLogsHandler) GetContent(clusterId string, fileName string) io.Reader {
	ctx := context.Background()
	fullPath := path.Join(r.S3RootDir, clusterId, fileName)
	logrus.Infof("Prepare to get object %s info ...", fullPath)

	result, err := r.S3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.S3Bucket),
		Key:    aws.String(fullPath),
	})
	if err != nil {
		// Close the first result's Body if it exists to prevent connection leak
		if result != nil && result.Body != nil {
			result.Body.Close()
		}
		logrus.Errorf("Failed to get object %s: %v", fullPath, err)
		dirPath := path.Dir(fullPath)
		allFiles := r._listFiles(dirPath, "", false)
		found := false
		for _, f := range allFiles {
			if path.Base(f) == path.Base(fullPath) {
				logrus.Infof("Get object %s info success", f)
				result, err = r.S3Client.GetObject(ctx, &s3.GetObjectInput{
					Bucket: aws.String(r.S3Bucket),
					Key:    aws.String(f),
				})
				if err != nil {
					if result != nil && result.Body != nil {
						result.Body.Close()
					}
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

func NewWriter(c *types.RayCollectorConfig, jd map[string]interface{}) (storage.StorageWriter, error) {
	config := &config{}
	config.complete(c, jd)

	return New(config)
}

// TODO: refactor this
func createBucketIfNotExists(ctx context.Context, s3Client *s3.Client, bucketName, region string) error {
	_, err := s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err == nil {
		logrus.Infof("Bucket %s already exists", bucketName)
		return nil
	}

	if isBucketNotFound(err) {
		logrus.Infof("Bucket %s does not exist, creating...", bucketName)
		if err := createBucket(ctx, s3Client, bucketName, region); err != nil {
			return err
		}
		logrus.Infof("Successfully created bucket %s", bucketName)
		return nil
	}

	logrus.Warnf("HeadBucket error for %s: %v, attempting to create bucket", bucketName, err)
	if err := createBucket(ctx, s3Client, bucketName, region); err != nil {
		return err
	}
	logrus.Infof("Successfully created bucket %s", bucketName)
	return nil
}

func isBucketNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchBucket", "NotFound", "404":
			return true
		}
	}
	return false
}

func createBucket(ctx context.Context, s3Client *s3.Client, bucketName, region string) error {
	input := &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	}
	useLocation := region != "" && region != "us-east-1"
	if useLocation {
		input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(region),
		}
	}
	_, err := s3Client.CreateBucket(ctx, input)
	if err == nil {
		return nil
	}
	if isBucketAlreadyExists(err) {
		logrus.Infof("Bucket %s already exists", bucketName)
		return nil
	}
	if useLocation && isInvalidLocationConstraint(err) {
		logrus.Warnf("CreateBucket with location constraint %s failed, retrying without location constraint: %v", region, err)
		_, retryErr := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
			Bucket: aws.String(bucketName),
		})
		if retryErr == nil || isBucketAlreadyExists(retryErr) {
			return nil
		}
		return fmt.Errorf("failed to create bucket %s without location constraint: %w", bucketName, retryErr)
	}
	logrus.Errorf("Failed to create bucket %s: %v", bucketName, err)
	return fmt.Errorf("failed to create bucket %s: %w", bucketName, err)
}

func isBucketAlreadyExists(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "BucketAlreadyExists" ||
			apiErr.ErrorCode() == "BucketAlreadyOwnedByYou"
	}
	return false
}

func isInvalidLocationConstraint(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "InvalidLocationConstraint", "IllegalLocationConstraintException":
			return true
		}
	}
	return false
}

func New(c *config) (*RayLogsHandler, error) {
	logrus.Infof("Begin to create s3 client ...")
	ctx := context.Background()

	httpTimeout := 30 * time.Second
	httpClient := &http.Client{
		Timeout: httpTimeout,
	}

	credsProvider := credentials.NewStaticCredentialsProvider(c.S3ID, c.S3Secret, c.S3Token)
	endpoint := normalizeEndpoint(strings.TrimSpace(c.S3Endpoint), c.DisableSSL)

	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(c.S3Region),
		awsconfig.WithCredentialsProvider(credsProvider),
		awsconfig.WithHTTPClient(httpClient),
	}

	// Use full endpoint URL (including scheme) for WithBaseEndpoint so that
	// custom endpoints (e.g. MinIO at http://minio-service:9000) use the correct scheme.
	if endpoint != "" {
		if _, err := url.Parse(endpoint); err != nil {
			return nil, fmt.Errorf("failed to parse endpoint URL %s: %w", endpoint, err)
		}
		loadOptions = append(loadOptions, awsconfig.WithBaseEndpoint(endpoint))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = c.S3ForcePathStyle
		o.EndpointOptions.DisableHTTPS = c.DisableSSL
	})

	// Ensure bucket exists, create if not
	logrus.Infof("Checking if bucket %s exists...", c.S3Bucket)
	if err := createBucketIfNotExists(ctx, s3Client, c.S3Bucket, c.S3Region); err != nil {
		return nil, fmt.Errorf("failed to ensure bucket exists: %w", err)
	}

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
			Timeout: httpTimeout,
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

func normalizeEndpoint(endpoint string, disableSSL bool) string {
	if endpoint == "" {
		return ""
	}

	if strings.HasPrefix(endpoint, "http://") {
		return endpoint
	}
	if strings.HasPrefix(endpoint, "https://") {
		if disableSSL {
			return "http://" + strings.TrimPrefix(endpoint, "https://")
		}
		return endpoint
	}

	scheme := "https"
	if disableSSL {
		scheme = "http"
	}
	return scheme + "://" + endpoint
}
