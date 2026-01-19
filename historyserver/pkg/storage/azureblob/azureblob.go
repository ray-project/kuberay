/*
Copyright 2024 The KubeRay Authors.

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

package azureblob

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

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

type RayLogsHandler struct {
	ContainerClient *container.Client
	LogFiles        chan string
	HttpClient      *http.Client
	ContainerName   string
	SessionDir      string
	RootDir         string
	LogDir          string
	RayClusterName  string
	RayClusterID    string
	RayNodeName     string
	LogBatching     int
	PushInterval    time.Duration
}

func (r *RayLogsHandler) CreateDirectory(d string) error {
	// Azure Blob Storage doesn't require explicit directory markers.
	// Virtual directories are automatically inferred from blob paths.
	// Creating empty marker blobs causes "<no name>" display issues
	// in Azure Storage Explorer.
	return nil
}

func (r *RayLogsHandler) WriteFile(file string, reader io.ReadSeeker) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	blobClient := r.ContainerClient.NewBlockBlobClient(file)

	// Read all content from reader
	data, err := io.ReadAll(reader)
	if err != nil {
		logrus.Errorf("Failed to read data for file %s: %v", file, err)
		return err
	}

	_, err = blobClient.UploadBuffer(ctx, data, nil)
	if err != nil {
		logrus.Errorf("Failed to upload file %s: %v", file, err)
		return err
	}

	return nil
}

func (r *RayLogsHandler) listBlobs(prefix string, delimiter string, onlyBase bool) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	files := []string{}
	prefixWithSlash := prefix + "/"

	if delimiter != "" {
		// Hierarchical listing (directory-like)
		pager := r.ContainerClient.NewListBlobsHierarchyPager(delimiter, &container.ListBlobsHierarchyOptions{
			Prefix:     &prefixWithSlash,
			MaxResults: to32(100),
		})

		for pager.More() {
			resp, err := pager.NextPage(ctx)
			if err != nil {
				logrus.Errorf("Failed to list blobs from %s: %v", prefixWithSlash, err)
				return []string{}
			}

			logrus.Infof("[ListFiles]Returned blobs in %v. length of Segment.BlobItems: %v, length of Segment.BlobPrefixes: %v",
				prefixWithSlash, len(resp.Segment.BlobItems), len(resp.Segment.BlobPrefixes))

			for _, blob := range resp.Segment.BlobItems {
				objName := *blob.Name
				if onlyBase {
					objName = path.Base(*blob.Name)
				}
				files = append(files, objName)
			}

			for _, prefix := range resp.Segment.BlobPrefixes {
				objName := *prefix.Name
				if onlyBase {
					objName = path.Base(*prefix.Name)
				}
				files = append(files, objName+"/")
			}
		}
	} else {
		// Flat listing
		pager := r.ContainerClient.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
			Prefix:     &prefixWithSlash,
			MaxResults: to32(100),
		})

		for pager.More() {
			resp, err := pager.NextPage(ctx)
			if err != nil {
				logrus.Errorf("Failed to list blobs from %s: %v", prefixWithSlash, err)
				return []string{}
			}

			logrus.Infof("[ListFiles]Returned blobs in %v. length of Segment.BlobItems: %v",
				prefixWithSlash, len(resp.Segment.BlobItems))

			for _, blob := range resp.Segment.BlobItems {
				objName := *blob.Name
				if onlyBase {
					objName = path.Base(*blob.Name)
				}
				files = append(files, objName)
			}
		}
	}

	return files
}

func (r *RayLogsHandler) ListFiles(clusterId string, dir string) []string {
	prefix := path.Join(r.RootDir, clusterId, dir)
	logrus.Debugf("Prepare to list files ...")
	return r.listBlobs(prefix, "/", true)
}

func (r *RayLogsHandler) List() (res []utils.ClusterInfo) {
	clusters := make(utils.ClusterInfoList, 0, 10)
	logrus.Debugf("Prepare to get list clusters info ...")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	metadirPrefix := path.Join(r.RootDir, "metadir") + "/"

	pager := r.ContainerClient.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
		Prefix:     &metadirPrefix,
		MaxResults: to32(100),
	})

	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			logrus.Errorf("Failed to list blobs from %s: %v", metadirPrefix, err)
			break
		}

		logrus.Infof("[List]Returned blobs in %v. length of Segment.BlobItems: %v",
			metadirPrefix, len(resp.Segment.BlobItems))

		for _, blob := range resp.Segment.BlobItems {
			c := &utils.ClusterInfo{}
			metaInfo := strings.Trim(strings.TrimPrefix(*blob.Name, path.Join(r.RootDir, "metadir/")), "/")
			metas := strings.Split(metaInfo, "/")
			if len(metas) < 2 {
				continue
			}
			logrus.Infof("Process %++v", metas)
			namespaceName := strings.Split(metas[0], "_")
			if len(namespaceName) < 2 {
				continue
			}
			c.Name = namespaceName[0]
			c.Namespace = namespaceName[1]
			c.SessionName = metas[1]
			sessionInfo := strings.Split(metas[1], "_")
			if len(sessionInfo) < 3 {
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
			c.CreateTime = createTime.UTC().Format("2006-01-02T15:04:05Z")
			clusters = append(clusters, *c)
		}
	}

	sort.Sort(clusters)
	return clusters
}

func (r *RayLogsHandler) GetContent(clusterId string, fileName string) io.Reader {
	fullPath := path.Join(r.RootDir, clusterId, fileName)
	logrus.Infof("Prepare to get blob %s info ...", fullPath)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	blobClient := r.ContainerClient.NewBlobClient(fullPath)

	resp, err := blobClient.DownloadStream(ctx, nil)
	if err != nil {
		// Close the response body if it exists to prevent connection leak
		if resp.Body != nil {
			resp.Body.Close()
		}
		logrus.Errorf("Failed to get blob %s: %v", fullPath, err)

		// Try to find the file by listing
		dirPath := path.Dir(fullPath)
		allFiles := r.listBlobs(dirPath, "", false)
		found := false
		for _, f := range allFiles {
			if path.Base(f) == path.Base(fullPath) {
				logrus.Infof("Get blob %s info success", f)
				blobClient = r.ContainerClient.NewBlobClient(f)
				resp, err = blobClient.DownloadStream(ctx, nil)
				if err != nil {
					if resp.Body != nil {
						resp.Body.Close()
					}
					logrus.Errorf("Failed to get blob %s: %v", f, err)
					return nil
				}
				found = true
				break
			}
		}
		if !found {
			logrus.Errorf("Failed to get blob by listing all files %s", fileName)
			return nil
		}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("Failed to read all data from blob %s: %v", fileName, err)
		return nil
	}
	return bytes.NewReader(data)
}

func NewReader(c *types.RayHistoryServerConfig, jd map[string]interface{}) (storage.StorageReader, error) {
	cfg := &config{}
	cfg.completeHSConfig(c, jd)
	return New(cfg)
}

func NewWriter(c *types.RayCollectorConfig, jd map[string]interface{}) (storage.StorageWriter, error) {
	cfg := &config{}
	cfg.complete(c, jd)
	return New(cfg)
}

func createAzureBlobClient(c *config) (*azblob.Client, error) {
	// Auto-detect auth mode if not specified
	authMode := c.AuthMode
	if authMode == "" {
		if c.ConnectionString != "" {
			authMode = AuthModeConnectionString
		} else if c.AccountURL != "" {
			authMode = AuthModeDefault
		} else {
			return nil, fmt.Errorf("either AZURE_STORAGE_CONNECTION_STRING or AZURE_STORAGE_ACCOUNT_URL must be set")
		}
	}

	if authMode == AuthModeConnectionString {
		logrus.Info("Using connection string authentication")
		return azblob.NewClientFromConnectionString(c.ConnectionString, nil)
	}

	// Token-based authentication
	var cred *azidentity.DefaultAzureCredential
	var err error
	if authMode == AuthModeWorkloadIdentity {
		logrus.Info("Using workload identity authentication")
		wiCred, err := azidentity.NewWorkloadIdentityCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create workload identity credential: %w", err)
		}
		return azblob.NewClient(c.AccountURL, wiCred, nil)
	}

	logrus.Info("Using default Azure credential chain")
	cred, err = azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create default credential: %w", err)
	}
	return azblob.NewClient(c.AccountURL, cred, nil)
}

func ensureContainerExists(ctx context.Context, client *azblob.Client, containerName string) error {
	containerClient := client.ServiceClient().NewContainerClient(containerName)

	_, err := containerClient.GetProperties(ctx, nil)
	if err != nil {
		// Container doesn't exist, try to create it
		logrus.Infof("Container %s does not exist, creating...", containerName)
		_, err = containerClient.Create(ctx, nil)
		if err != nil {
			// Check if container already exists (race condition)
			if strings.Contains(err.Error(), "ContainerAlreadyExists") {
				logrus.Infof("Container %s already exists", containerName)
				return nil
			}
			logrus.Errorf("Failed to create container %s: %v", containerName, err)
			return fmt.Errorf("failed to create container %s: %w", containerName, err)
		}
		logrus.Infof("Successfully created container %s", containerName)
		return nil
	}

	logrus.Infof("Container %s already exists", containerName)
	return nil
}

func New(c *config) (*RayLogsHandler, error) {
	logrus.Infof("Begin to create azure blob client ...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := createAzureBlobClient(c)
	if err != nil {
		logrus.Errorf("Failed to create azure blob client: %v", err)
		return nil, err
	}

	// Ensure container exists
	if err := ensureContainerExists(ctx, client, c.ContainerName); err != nil {
		return nil, fmt.Errorf("failed to ensure container exists: %w", err)
	}

	containerClient := client.ServiceClient().NewContainerClient(c.ContainerName)

	sessionDir := strings.TrimSpace(c.SessionDir)
	sessionDir = filepath.Clean(sessionDir)

	logdir := strings.TrimSpace(path.Join(sessionDir, utils.RAY_SESSIONDIR_LOGDIR_NAME))
	logdir = filepath.Clean(logdir)
	logrus.Infof("Clean logdir is %s", logdir)

	return &RayLogsHandler{
		ContainerClient: containerClient,
		LogFiles:        make(chan string, 100),
		HttpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		ContainerName:  c.ContainerName,
		SessionDir:     sessionDir,
		RootDir:        c.RootDir,
		LogDir:         logdir,
		RayClusterName: c.RayClusterName,
		RayClusterID:   c.RayClusterID,
		RayNodeName:    c.RayNodeName,
		LogBatching:    c.LogBatching,
		PushInterval:   c.PushInterval,
	}, nil
}

// Helper function to convert int to *int32
func to32(n int) *int32 {
	v := int32(n)
	return &v
}
