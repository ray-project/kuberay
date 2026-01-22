package gcs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	gstorage "cloud.google.com/go/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	"github.com/sirupsen/logrus"
	giterator "google.golang.org/api/iterator"
)

type GCSBucketHandler struct {
	GCSBucket      string
	RayClusterName string
	RayClusterID   string
	RayNodeName    string

	StorageClient *gstorage.Client
	FlushInterval time.Duration
}

// CreateDirectory creates a new "directory" under GCS. Since GCS is a "flat" namespace,
// we simulate a directory by using forward slash and immediately closing the writer.
func (h *GCSBucketHandler) CreateDirectory(directoryPath string) error {
	ctx := context.Background()

	// make sure the path ends with a / so a directory is "created"
	objectPath := fmt.Sprintf("%s/", path.Clean(directoryPath))
	writer := h.StorageClient.Bucket(h.GCSBucket).Object(objectPath).NewWriter(ctx)
	if err := writer.Close(); err != nil {
		logrus.Errorf("Failed to create directory: %s, error: %v", objectPath, err)
		return err
	}

	logrus.Infof("Successfully created GCS directory: %s", objectPath)
	return nil
}

func (h *GCSBucketHandler) WriteFile(file string, reader io.ReadSeeker) error {
	ctx := context.Background()

	writer := h.StorageClient.Bucket(h.GCSBucket).Object(file).NewWriter(ctx)
	defer writer.Close()
	_, err := io.Copy(writer, reader)
	if err != nil {
		return err
	}

	return nil
}

func (h *GCSBucketHandler) ListFiles(clusterId string, directory string) []string {
	// TODO(chiayi): Look into potential timeout issues
	ctx := context.Background()

	pathPrefix := path.Join(h.GCSBucket, clusterId, directory)

	query := &gstorage.Query{
		Prefix: pathPrefix,
		// Delimiter tells GCS to stop searching at the next '/'
		Delimiter: "/",
	}
	fileIterator := h.StorageClient.Bucket(h.GCSBucket).Objects(ctx, query)

	var fileList []string
	for {
		attrs, err := fileIterator.Next()
		if err == giterator.Done {
			break
		}
		if err != nil {
			logrus.Error("Failed to read bucket")
			if err == gstorage.ErrObjectNotExist {
				logrus.Errorf("object does not exist. Bucket(%q).Objects() with query %+v: %w", h.GCSBucket, query, err)
				return nil
			} else {
				logrus.Errorf("Bucket(%q).Objects() with query %+v: %w", h.GCSBucket, query, err)
				return nil
			}
		}

		// When Delimiter is used:
		// Objects *within* the prefix have attrs.Name set and attrs.Prefix is empty.
		// "Subdirectories" have attrs.Prefix set (e.g., "path/to/directory/node_events/subdir/") and attrs.Name is empty.

		// We only want files, so check if attrs.Name is non-empty.
		// Exclude the placeholder object if it exists for the directory itself.
		// attrs.Name contains the whole object path.
		if !strings.HasSuffix(attrs.Name, "/") {
			fileList = append(fileList, attrs.Name)
		}
	}

	return fileList
}

// List will return a list of ClusterInfo
func (h *GCSBucketHandler) List() []utils.ClusterInfo {
	ctx := context.Background()

	clusterList := make(utils.ClusterInfoList, 0, 20)
	bucket := h.StorageClient.Bucket(h.GCSBucket)
	objectIterator := bucket.Objects(ctx, nil)
	for {
		objectAttr, err := objectIterator.Next()
		if err == giterator.Done {
			break
		}
		if err != nil {
			logrus.Fatalf("Failed to get attribute of ray clusters: %v", err)
		}

		// TODO(chaiyi): Split the name into the RayCluster info
		// TODO(chiayi): Remove potential "sub" files
		cluster := &utils.ClusterInfo{}
		cluster.Name = objectAttr.Name

		clusterList = append(clusterList, *cluster)
	}

	return clusterList
}

func (h *GCSBucketHandler) GetContent(clusterId string, fileName string) io.Reader {
	ctx := context.Background()

	// TODO(chiayi): Find filePath. Use MatchGlob to find file with fileName
	reader, err := h.StorageClient.Bucket(h.GCSBucket).Object(fileName).NewReader(ctx)
	if err != nil {
		logrus.Fatalf("Failed to initialize file reader: %v", err)
	}
	defer reader.Close()

	// TODO(chiayi): ReadAll can potentially cause OOM error depending on the size of the file.
	// Change into bufio.Scanner if needed or limit the size of the read
	data, err := io.ReadAll(reader)
	if err != nil {
		logrus.Fatalf("Failed to get all content of the file: %s, %v", fileName, err)
	}
	return bytes.NewReader(data)
}

func NewReader(bucketName string) (storage.StorageReader, error) {
	return NewGCSBucketHandler(bucketName, "")
}

func NewWriter(bucketName string, rayClusterName string) (storage.StorageWriter, error) {
	return NewGCSBucketHandler(bucketName, rayClusterName)
}

func NewGCSBucketHandler(bucketName string, rayCluster string) (*GCSBucketHandler, error) {
	ctx := context.Background()
	storageClient, err := gstorage.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	return &GCSBucketHandler{
		GCSBucket:      bucketName,
		RayClusterName: rayCluster,
		StorageClient:  storageClient,
	}, nil
}
