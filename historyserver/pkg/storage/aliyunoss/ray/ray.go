package ray

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/storage/aliyunoss/rrsa"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

type RayLogsHandler struct {
	OssClient           *oss.Client
	OssBucket           string
	LogFiles            chan string
	HttpClient          *http.Client
	SessionDir          string
	OssRootDir          string
	LogDir              string
	RayClusterName      string
	RayClusterNamespace string
	RayNodeName         string
	LogBatching         int
	PushInterval        time.Duration
}

func (r *RayLogsHandler) CreateDirectory(d string) error {
	ctx := context.TODO()
	objectDir := fmt.Sprintf("%s/", path.Clean(d))

	isExist, err := r.OssClient.IsObjectExist(ctx, r.OssBucket, objectDir)
	if err != nil {
		logrus.Errorf("Failed to check if dirObject %s exists: %v", objectDir, err)
		return err
	}
	if !isExist {
		logrus.Infof("Begin to create oss dir %s ...", objectDir)
		_, err = r.OssClient.PutObject(ctx, &oss.PutObjectRequest{
			Bucket: oss.Ptr(r.OssBucket),
			Key:    oss.Ptr(objectDir),
			Body:   bytes.NewReader([]byte("")),
		})
		if err != nil {
			logrus.Errorf("Failed to create directory '%s': %v", objectDir, err)
			return err
		}
		logrus.Infof("Create oss dir %s success", objectDir)
	}
	return nil
}

func (r *RayLogsHandler) Append(file string, reader io.Reader, appendPosition int64) (nextPod int64, err error) {
	result, err := r.OssClient.AppendObject(context.TODO(), &oss.AppendObjectRequest{
		Bucket:   oss.Ptr(r.OssBucket),
		Key:      oss.Ptr(file),
		Position: &appendPosition,
		Body:     reader,
	})
	if err != nil {
		return 0, err
	}

	return result.NextPosition, nil
}

func (r *RayLogsHandler) WriteFile(file string, reader io.ReadSeeker) error {
	_, err := r.OssClient.PutObject(context.TODO(), &oss.PutObjectRequest{
		Bucket: oss.Ptr(r.OssBucket),
		Key:    oss.Ptr(file),
		Body:   reader,
	})
	return err
}

func (r *RayLogsHandler) _listFiles(prefix string, delimiter string, onlyBase bool) []string {
	ctx := context.Background()
	files := []string{}

	p := r.OssClient.NewListObjectsV2Paginator(&oss.ListObjectsV2Request{
		Bucket:    oss.Ptr(r.OssBucket),
		Prefix:    oss.Ptr(prefix + "/"),
		Delimiter: oss.Ptr(delimiter),
		MaxKeys:   100,
	})

	for p.HasNext() {
		page, err := p.NextPage(ctx)
		if err != nil {
			logrus.Errorf("Failed to list objects from %s: %v", prefix+"/", err)
			return []string{}
		}
		logrus.Infof("[ListFiles]Returned objects in %v. length of Contents: %v, length of CommonPrefixes: %v", prefix+"/", len(page.Contents),
			len(page.CommonPrefixes))
		for _, objects := range page.Contents {
			objName := *objects.Key
			if onlyBase {
				objName = path.Base(*objects.Key)
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
	}
	return files
}

func (r *RayLogsHandler) ListFiles(clusterStoragePrefix string, dir string) []string {
	prefix := path.Join(r.OssRootDir, clusterStoragePrefix, dir)

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
	ctx := context.Background()
	clusters := make(utils.ClusterInfoList, 0, 10)
	logrus.Debugf("Prepare to get list clusters info ...")

	getClusters := func() {
		p := r.OssClient.NewListObjectsV2Paginator(&oss.ListObjectsV2Request{
			Bucket:    oss.Ptr(r.OssBucket),
			Prefix:    oss.Ptr(path.Join(r.OssRootDir, "metadir") + "/"),
			Delimiter: oss.Ptr(""),
			MaxKeys:   100,
		})

		for p.HasNext() {
			page, err := p.NextPage(ctx)
			if err != nil {
				logrus.Errorf("Failed to list objects from %s: %v", path.Join(r.OssRootDir, "metadir")+"/", err)
				return
			}
			logrus.Infof("[List]Returned objects in %v. length of Contents: %v, length of CommonPrefixes: %v", path.Join(r.OssRootDir, "metadir")+"/", len(page.Contents),
				len(page.CommonPrefixes))
			for _, objects := range page.Contents {
				metaInfo := strings.Trim(strings.TrimPrefix(*objects.Key, path.Join(r.OssRootDir, "metadir/")), "/")
				session, err := utils.ParseMetaDirRelPath(metaInfo)
				if err != nil {
					continue
				}
				logrus.Infof("Process %++v", session)
				sessionInfo := strings.Split(session.SessionName, "_")
				if len(sessionInfo) < 3 {
					continue
				}
				date := sessionInfo[1]
				dataTime := sessionInfo[2]
				// TODO(jwj): Use utils.GetDateTimeFromSessionID instead of time.Parse.
				createTime, err := time.Parse("2006-01-02_15-04-05", date+"_"+dataTime)
				if err != nil {
					logrus.Errorf("Failed to parse time %s: %v", date+"_"+dataTime, err)
					continue
				}
				clusters = append(clusters, utils.ClusterInfo{
					Name:            session.Name,
					Namespace:       session.Namespace,
					SessionName:     session.SessionName,
					CreateTimeStamp: createTime.Unix(),
					CreateTime:      createTime.UTC().Format("2006-01-02T15:04:05Z"),
				})
			}
		}
	}
	getClusters()
	sort.Sort(clusters)
	return clusters
}

func (r *RayLogsHandler) GetContent(clusterStoragePrefix string, fileName string) io.Reader {
	ctx := context.TODO()
	logrus.Infof("Prepare to get object %s info ...", fileName)
	result, err := r.OssClient.GetObject(ctx, &oss.GetObjectRequest{
		Bucket: oss.Ptr(r.OssBucket),
		Key:    oss.Ptr(fileName),
	})
	if err != nil {
		logrus.Errorf("Failed to get object %s: %v", fileName, err)
		allFiles := r._listFiles(clusterStoragePrefix+"/"+path.Dir(fileName), "", false)
		found := false
		for _, f := range allFiles {
			if path.Base(f) == path.Base(fileName) {
				logrus.Infof("Get object %s info success", f)
				result, err = r.OssClient.GetObject(ctx, &oss.GetObjectRequest{
					Bucket: oss.Ptr(r.OssBucket),
					Key:    oss.Ptr(f),
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

func NewWriter(c *types.RayCollectorConfig, jd map[string]interface{}) (storage.StorageWriter, error) {
	config := &config{}
	config.complete(c, jd)

	return New(config)
}

func New(c *config) (*RayLogsHandler, error) {
	// Create OSS client using Alibaba Cloud OSS SDK v2.
	// Ref: https://github.com/aliyun/alibabacloud-oss-go-sdk-v2.

	logrus.Infof("Begin to create oss client ...")
	httpClient := &http.Client{
		Timeout: 5 * time.Second, // Set timeout
	}
	provider, err := rrsa.NewCredentialsProvider()
	if err != nil {
		logrus.Fatalf("Failed to create credentials provider: %v", err)
	}

	cfg := oss.LoadDefaultConfig().
		WithCredentialsProvider(provider).
		WithRegion(c.OSSRegion).
		WithEndpoint(c.OSSEndpoint).
		WithHttpClient(httpClient)
	client := oss.NewClient(cfg)

	logrus.Infof("Begin to use oss bucket %s ...", c.OSSBucket)
	sessionDir := strings.TrimSpace(c.SessionDir)
	sessionDir = filepath.Clean(sessionDir)

	logdir := strings.TrimSpace(path.Join(sessionDir, utils.RAY_SESSIONDIR_LOGDIR_NAME))
	logdir = filepath.Clean(logdir)
	logrus.Infof("Clean logdir is %s", logdir)

	return &RayLogsHandler{
		OssClient:           client,
		OssBucket:           c.OSSBucket,
		SessionDir:          sessionDir,
		OssRootDir:          c.RootDir,
		LogDir:              logdir,
		LogFiles:            make(chan string, 100),
		RayClusterName:      c.RayClusterName,
		RayClusterNamespace: c.RayClusterNamespace,
		RayNodeName:         c.RayNodeName,
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
