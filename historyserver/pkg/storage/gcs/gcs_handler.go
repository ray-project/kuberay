package gcs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	gstorage "cloud.google.com/go/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	"github.com/sirupsen/logrus"
	gIterator "google.golang.org/api/iterator"
	"google.golang.org/api/option"
	gTransport "google.golang.org/api/transport/http"
)

type RayLogsHandler struct {
	GCSBucket      string
	LogFiles       chan string
	RootDir        string
	SessionDir     string
	RayClusterName string
	RayClusterID   string
	RayNodeName    string
	LogBatching    int

	StorageClient *gstorage.Client
	PushInterval  time.Duration
}

// CreateDirectory creates a new "directory" under GCS. Since GCS is a "flat" namespace,
// we simulate a directory by using forward slash and immediately closing the writer.
// Closing the writer will "finalize" the directory creation
func (h *RayLogsHandler) CreateDirectory(directoryPath string) error {
	ctx := context.Background()

	// make sure the path ends with a / so a directory is "created"
	objectPath := fmt.Sprintf("%s/", path.Clean(directoryPath))
	// Check if directory exists
	_, err := h.StorageClient.Bucket(h.GCSBucket).Object(objectPath).Attrs(ctx)
	if errors.Is(err, gstorage.ErrObjectNotExist) {
		writer := h.StorageClient.Bucket(h.GCSBucket).Object(objectPath).NewWriter(ctx)
		if createErr := writer.Close(); createErr != nil {
			logrus.Errorf("Failed to create directory: %s, error: %v", objectPath, createErr)
			return createErr
		}

		logrus.Infof("Successfully created GCS directory: %s", objectPath)
		return nil
	} else if err != nil {
		logrus.Errorf("Failed to check if GCS directory already exist: %s, error: %v", objectPath, err)
		return err
	}

	logrus.Infof("Directory already exists: %s", objectPath)
	return nil
}

func (h *RayLogsHandler) WriteFile(file string, reader io.ReadSeeker) error {
	ctx := context.Background()

	writer := h.StorageClient.Bucket(h.GCSBucket).Object(file).NewWriter(ctx)
	_, err := io.Copy(writer, reader)
	if err != nil {
		logrus.Error("GCS Client failed to Copy from source.")
		writer.Close()
		return err
	}

	// We don't defer close here since the close function acts as finalizing the write.
	if err := writer.Close(); err != nil {
		logrus.Error("GCS Client failed to finalize write.")
		return err
	}
	return nil
}

func (h *RayLogsHandler) ListFiles(clusterId string, directory string) []string {
	ctx := context.Background()

	pathPrefix := strings.TrimPrefix(path.Join(h.RootDir, clusterId, directory), "/") + "/"

	query := &gstorage.Query{
		Prefix: pathPrefix,
		// Delimiter tells GCS to stop searching at the next '/'
		Delimiter: "/",
	}
	fileIterator := h.StorageClient.Bucket(h.GCSBucket).Objects(ctx, query)

	var fileList []string
	for {
		attrs, err := fileIterator.Next()
		if err == gIterator.Done {
			break
		}
		if err != nil {
			logrus.Error("Failed to read bucket")
			if err == gstorage.ErrObjectNotExist {
				logrus.Errorf("object does not exist. Bucket(%q).Objects() with query %+v: %v", h.GCSBucket, query, err)
				return nil
			} else {
				logrus.Errorf("Bucket(%q).Objects() with query %+v: %v", h.GCSBucket, query, err)
				return nil
			}
		}

		// When Delimiter is used:
		// Objects *within* the prefix have attrs.Name set and attrs.Prefix is empty.
		// "Subdirectories" have attrs.Prefix set (e.g., "path/to/directory/node_events/subdir/") and attrs.Name is empty.

		// We only want files, so check if attrs.Name is non-empty.
		// Exclude the placeholder object if it exists for the directory itself.
		// attrs.Name contains the whole object path.
		if attrs.Name != "" && !strings.HasSuffix(attrs.Name, "/") {
			fileNameOnly := path.Base(attrs.Name)
			fileList = append(fileList, fileNameOnly)
		}
	}

	return fileList
}

// List will return a list of ClusterInfo
func (h *RayLogsHandler) List() []utils.ClusterInfo {
	ctx := context.Background()

	clusterList := make(utils.ClusterInfoList, 0, 20)
	bucket := h.StorageClient.Bucket(h.GCSBucket)
	pathPrefix := path.Join(h.RootDir, "metadir") + "/"
	query := &gstorage.Query{
		// Match with only non-directory objects
		MatchGlob: pathPrefix + "**/*[!/]",
	}
	objectIterator := bucket.Objects(ctx, query)
	for {
		cluster := &utils.ClusterInfo{}
		objectAttr, err := objectIterator.Next()
		if err == gIterator.Done {
			logrus.Infof("Finished iterating through gcs objects")
			break
		}
		if err != nil {
			logrus.Errorf("Failed to get attribute of ray clusters: %v", err)
			return nil
		}

		fullObjectPath := objectAttr.Name
		metaInfo := strings.Split(strings.TrimPrefix(fullObjectPath, pathPrefix), "/")
		if len(metaInfo) != 2 {
			logrus.Errorf("Unable to properly parse cluster metadir path with fullpath: %s", fullObjectPath)
			return nil
		}
		clusterMeta := strings.Split(metaInfo[0], "_")
		if len(clusterMeta) != 2 {
			logrus.Errorf("Unable to get cluster name and namespace from directory: %s", metaInfo[0])
			continue
		}
		cluster.Name = clusterMeta[0]
		cluster.Namespace = clusterMeta[1]

		cluster.SessionName = metaInfo[1]
		time, err := utils.GetDateTimeFromSessionID(metaInfo[1])
		if err != nil {
			logrus.Errorf("Failed to get date time from the given sessionID: %s, error: %v", metaInfo[1], err)
			continue
		}
		cluster.CreateTimeStamp = time.Unix()
		cluster.CreateTime = time.UTC().Format(("2006-01-02T15:04:05Z"))

		logrus.Infof("Parsed cluster %s for session %s to list", cluster.Name, cluster.SessionName)
		clusterList = append(clusterList, *cluster)
	}

	return clusterList
}

func (h *RayLogsHandler) GetContent(clusterId string, fileName string) io.Reader {
	ctx := context.Background()

	bucket := h.StorageClient.Bucket(h.GCSBucket)
	query := &gstorage.Query{
		MatchGlob: "**/" + clusterId + "*/**/" + fileName,
	}
	objectIterator := bucket.Objects(ctx, query)
	fileAttrs, err := objectIterator.Next()
	if err == gIterator.Done {
		logrus.Errorf("File %s was not found in bucket for cluster %s", fileName, clusterId)
		return nil
	}
	if err != nil {
		logrus.Errorf("Failed when searching for file %v", err)
		return nil
	}

	reader, err := h.StorageClient.Bucket(h.GCSBucket).Object(fileAttrs.Name).NewReader(ctx)
	if err != nil {
		logrus.Errorf("Failed to create reader for file: %s in cluster: %s", fileName, clusterId)
		return nil
	}
	defer reader.Close()
	// TODO(chiayi): ReadAll can potentially cause OOM error depending on the size of the file.
	// Change into bufio.Scanner if needed or limit the size of the read
	data, err := io.ReadAll(reader)
	if err != nil {
		logrus.Errorf("Failed to get all content of the file: %s, %v", fileName, err)
		return nil
	}
	return bytes.NewReader(data)
}

func createGCSBucket(gcsClient *gstorage.Client, projectID, bucketName string) error {
	ctx := context.Background()

	if projectID == "" {
		logrus.Errorf("Project ID cannot be empty. Failed to create GCS bucket: %s", bucketName)
		return errors.New("Project ID cannot be empty when creating a GCS Bucket")
	}

	bucket := gcsClient.Bucket(bucketName)
	if err := bucket.Create(ctx, projectID, nil); err != nil {
		logrus.Errorf("Failed to create GCS bucket: %s", bucketName)
		return err
	}

	logrus.Infof("Created GCS Bucket: %s", bucketName)
	return nil
}

func NewReader(c *types.RayHistoryServerConfig, jd map[string]interface{}) (storage.StorageReader, error) {
	config := &config{}
	config.completeHistoryServerConfig(c, jd)
	return New(config)
}

func NewWriter(c *types.RayCollectorConfig, jd map[string]interface{}) (storage.StorageWriter, error) {
	config := &config{}
	config.completeCollectorConfig(c, jd)
	return New(config)
}

// New will create a RayLogsHandler for reading and writing to GCS
// Currently, workload identity is required for using this client.
// If workload identity is not set, default behavior will also search GOOGLE_APPLICATION_CREDENTIALS env var
func New(c *config) (*RayLogsHandler, error) {
	logrus.Infof("Starting GCS client ...")

	ctx := context.Background()

	baseTransport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	}

	// The gTransport is the base transport that is wrapped with the Google authenticator
	// using option.WithHTTPClient() seems to completely override the client which causes
	// the final storageClient to not use Google auth, so this adds it
	authTransport, err := gTransport.NewTransport(ctx,
		baseTransport,
		option.WithScopes(gstorage.ScopeFullControl),
	)
	if err != nil {
		logrus.Errorf("Failed to create authentication transport object")
		return nil, err
	}

	// Create a custom client with the authenticated transport
	customHttpTransportClient := &http.Client{
		Transport: authTransport,
		Timeout:   90 * time.Second,
	}

	// Finally create the storage client with the custom http client
	storageClient, err := gstorage.NewClient(ctx,
		option.WithHTTPClient(customHttpTransportClient),
	)
	if err != nil {
		logrus.Errorf("Failed to create google cloud storage client")
		return nil, err
	}

	// Check if bucket exists
	_, err = storageClient.Bucket(c.Bucket).Attrs(ctx)
	if errors.Is(err, gstorage.ErrBucketNotExist) {
		logrus.Warnf("Bucket %s does not exist, will attempt to create bucket", c.Bucket)
		err = createGCSBucket(storageClient, c.GCPProjectID, c.Bucket)
		if err != nil {
			storageClient.Close()
			return nil, err
		}
	} else if err != nil {
		logrus.Error("Failed to check if bucket exist or not")
		return nil, err
	}

	return &RayLogsHandler{
		StorageClient:  storageClient,
		GCSBucket:      c.Bucket,
		RayClusterName: c.RayClusterName,
		RayClusterID:   c.RayClusterID,
		RootDir:        c.RootDir,
		LogFiles:       make(chan string, 100),
		LogBatching:    c.LogBatching,
		RayNodeName:    c.RayNodeName,
		SessionDir:     c.SessionDir,
		PushInterval:   c.PushInterval,
	}, nil
}
