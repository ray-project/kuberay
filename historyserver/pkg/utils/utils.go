package utils

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/sirupsen/logrus"
)

const (
	RAY_SESSIONDIR_LOGDIR_NAME  = "logs"
	RAY_SESSIONDIR_METADIR_NAME = "meta"
)

const (
	OssMetaFile_BasicInfo = "ack__basicinfo"

	OssMetaFile_NodeSummaryKey                        = "restful__nodes_view_summary"
	OssMetaFile_Node_Prefix                           = "restful__nodes_"
	OssMetaFile_JOBTASK_DETAIL_Prefix                 = "restful__api__v0__tasks_detail_job_id_"
	OssMetaFile_JOBTASK_SUMMARIZE_BY_FUNC_NAME_Prefix = "restful__api__v0__tasks_summarize_by_func_name_job_id_"
	OssMetaFile_JOBTASK_SUMMARIZE_BY_LINEAGE_Prefix   = "restful__api__v0__tasks_summarize_by_lineage_job_id_"
	OssMetaFile_JOBDATASETS_Prefix                    = "restful__api__data__datasets_job_id_"
	OssMetaFile_NodeLogs_Prefix                       = "restful__api__v0__logs_node_id_"
	OssMetaFile_ClusterStatus                         = "restful__api__cluster_status"
	OssMetaFile_LOGICAL_ACTORS                        = "restful__logical__actors"
	OssMetaFile_ALLTASKS_DETAIL                       = "restful__api__v0__tasks_detail"
	OssMetaFile_Events                                = "restful__events"
	OssMetaFile_PlacementGroups                       = "restful__api__v0__placement_groups_detail"

	OssMetaFile_ClusterSessionName = "static__api__cluster_session_name"

	OssMetaFile_Jobs         = "restful__api__jobs"
	OssMetaFile_Applications = "restful__api__serve__applications"
)

const RAY_HISTORY_SERVER_LOGNAME = "historyserver-ray.log"

func RecreateObjectDir(bucket *oss.Bucket, dir string, options ...oss.Option) error {
	objectDir := fmt.Sprintf("%s/", path.Clean(dir))

	isExist, err := bucket.IsObjectExist(objectDir)
	if err != nil {
		logrus.Errorf("Failed to check object dir %s exists: %v", objectDir, err)
		return err
	}

	if isExist {
		logrus.Infof("ObjectDir %s has exist, begin to delete ...", objectDir)
		err = bucket.DeleteObject(objectDir)
		if err != nil {
			logrus.Errorf("Failed to delete objectdir %s: %v", objectDir, err)
			return err
		}
		logrus.Infof("ObjectDir %s has delete success...", objectDir)

		// List all files with the specified prefix and delete them
		marker := oss.Marker("")
		// To delete only the src directory and all files within it, set prefix to "src/"
		prefix := oss.Prefix(objectDir)
		var totalDeleted int

		for {
			lor, err := bucket.ListObjects(marker, prefix)
			if err != nil {
				logrus.Errorf("Failed to list objectsdir %s error %v", objectDir, err)
				return err
			}

			objects := make([]string, len(lor.Objects))
			for i, object := range lor.Objects {
				objects[i] = object.Key
			}

			// Delete objects
			delRes, err := bucket.DeleteObjects(objects, oss.DeleteObjectsQuiet(true))
			if err != nil {
				logrus.Errorf("Failed to delete allobjects in dir %s : %v", objectDir, err)
				return err
			}

			if len(delRes.DeletedObjects) > 0 {
				logrus.Errorf("Some dir %s objects failed to delete: %v", objectDir, delRes.DeletedObjects)
				return fmt.Errorf("Some dir %s objects failed to delete: %v", objectDir, delRes.DeletedObjects)
			}

			totalDeleted += len(objects)

			// Update marker
			marker = oss.Marker(lor.NextMarker)
			if !lor.IsTruncated {
				break
			}
		}

	}

	logrus.Infof("Begin to create oss object dir %s ...", objectDir)
	err = bucket.PutObject(objectDir, bytes.NewReader([]byte("")), options...)
	if err != nil {
		logrus.Errorf("Failed to create oss object dir %s: %v", objectDir, err)
		return err
	}
	return nil
}

func CreateObjectIfNotExist(bucket *oss.Bucket, obj string, options ...oss.Option) error {
	isExist, err := bucket.IsObjectExist(obj)
	if err != nil {
		logrus.Errorf("Failed to check if object %s exists: %v", obj, err)
		return err
	}
	if !isExist {
		logrus.Infof("Begin to create oss object %s ...", obj)
		err = bucket.PutObject(obj, bytes.NewReader([]byte("")), options...)
		if err != nil {
			logrus.Errorf("Failed to create directory '%s': %v", obj, err)
			return err
		}
		logrus.Infof("Create oss object %s success", obj)
	}
	return nil
}

func CreateObjectDirIfNotExist(bucket *oss.Bucket, dir string, options ...oss.Option) error {
	objectDir := fmt.Sprintf("%s/", path.Clean(dir))

	isExist, err := bucket.IsObjectExist(objectDir)
	if err != nil {
		logrus.Errorf("Failed to check if dirObject %s exists: %v", objectDir, err)
		return err
	}
	if !isExist {
		logrus.Infof("Begin to create oss dir %s ...", objectDir)
		err = bucket.PutObject(objectDir, bytes.NewReader([]byte("")), options...)
		if err != nil {
			logrus.Errorf("Failed to create directory '%s': %v", objectDir, err)
			return err
		}
		logrus.Infof("Create oss dir %s success", objectDir)
	}
	return nil
}

func DeleteObject(bucket *oss.Bucket, objectName string) error {
	isExist, err := bucket.IsObjectExist(objectName)
	if err != nil {
		logrus.Errorf("Failed to check if object %s exists: %v", objectName, err)
		return err
	}

	if isExist {
		// Delete a single file
		err = bucket.DeleteObject(objectName)
		if err != nil {
			logrus.Warnf("Failed to delete object '%s': %v", objectName, err)
			return err
		}
	}
	return nil
}

func GetMetaDirByNameID(ossHistorySeverDir, rayClusterNameID string) string {
	return fmt.Sprintf("%s/", path.Clean(path.Join(ossHistorySeverDir, rayClusterNameID, RAY_SESSIONDIR_METADIR_NAME)))
}

func GetLogDirByNameID(ossHistorySeverDir, rayClusterNameID, rayNodeID, sessionId string) string {
	return fmt.Sprintf("%s/", path.Clean(path.Join(ossHistorySeverDir, rayClusterNameID, sessionId, RAY_SESSIONDIR_LOGDIR_NAME, rayNodeID)))
}

func GetLogDir(ossHistorySeverDir, rayClusterName, rayClusterID, sessionId, rayNodeID string) string {
	return fmt.Sprintf("%s/", path.Clean(path.Join(ossHistorySeverDir, AppendRayClusterNameID(rayClusterName, rayClusterID), sessionId, RAY_SESSIONDIR_LOGDIR_NAME, rayNodeID)))
}

const (
	// connector is the separator for creating flat storage keys.
	//
	// Design Philosophy:
	// - Format: "{clusterName}_{namespace}" for router/historyserver
	//           "{clusterName}_{clusterID}" for collector
	//
	// Why "_" instead of "/"?
	// Using "/" would create a hierarchical path like "namespace/cluster/session/..."
	// which requires multiple ListObjects API calls to traverse:
	//   1. First list all clusters under a namespace
	//   2. Then list contents of the target cluster
	//
	// Using "_" creates a flat path like "namespace_cluster/session/..."
	// which allows direct access with a single ListObjects call.
	//
	// Why this is SAFE for parsing:
	// - Kubernetes namespace follows DNS-1123 label spec
	// - DNS-1123 only allows: lowercase letters, digits, and hyphens (-)
	// - Namespace CANNOT contain "_", so we can unambiguously split from the LAST "_"
	//
	// DO NOT CHANGE: Would break existing stored data paths
	connector = "_"
)

func AppendRayClusterNameID(rayClusterName, rayClusterID string) string {
	return fmt.Sprintf("%s%s%s", rayClusterName, connector, rayClusterID)
}

func GetRarClusterNameAndID(rayClusterNameID string) (string, string) {
	nameID := strings.Split(rayClusterNameID, connector)
	if len(nameID) < 2 {
		logrus.Fatalf("rayClusterNameID %s must match name%sid pattern", rayClusterNameID, connector)
	}
	return strings.Join(nameID[:len(nameID)-1], "_"), nameID[len(nameID)-1]
}

func GetSessionDir() (string, error) {
	session_latest_path := "/tmp/ray/session_latest"
	for i := 0; i < 12; i++ {
		rp, err := os.Readlink(session_latest_path)
		if err != nil {
			logrus.Errorf("read session_latest file error %v", err)
			time.Sleep(time.Second * 5)
			continue
		}
		return rp, nil
	}
	return "", fmt.Errorf("timeout log_monitor --session-dir not found")
}

func GetRayNodeID() (string, error) {
	for i := 0; i < 12; i++ {
		nodeidBytes, err := os.ReadFile("/tmp/ray/raylet_node_id")
		if err != nil {
			logrus.Errorf("read nodeid file error %v", err)
			time.Sleep(time.Second * 5)
			continue
		}
		return strings.Trim(string(nodeidBytes), "\n"), nil
	}
	return "", fmt.Errorf("timeout --node_id= not found")
}
