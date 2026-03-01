package utils

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/sirupsen/logrus"
)

const (
	RAY_SESSIONDIR_LOGDIR_NAME            = "logs"
	RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME = "fetched_endpoints"
	DATETIME_LAYOUT                       = "2006-01-02_15-04-05.000000"
	// The following regex shouldn't be changed unless the ray session ID changes.
	SESSION_ID_REGEX = `session_(\d{4}-\d{2}-\d{2})_(\d{2}-\d{2}-\d{2})_(\d{6})`
)

// EndpointPathToStorageKey converts a Ray Dashboard API endpoint path to a
// storage key that follows the existing naming convention used by OssMetaFile_*
// constants (e.g., "restful__api__v0__cluster_metadata").
//
// Examples:
//
//	"/api/v0/cluster_metadata"  -> "restful__api__v0__cluster_metadata"
//	"/api/v0/nodes/summary"     -> "restful__api__v0__nodes__summary"
//	"/api/serve/applications/"  -> "restful__api__serve__applications"
func EndpointPathToStorageKey(endpointPath string) string {
	trimmed := strings.Trim(endpointPath, "/")
	return "restful__" + strings.ReplaceAll(trimmed, "/", "__")
}

var regex = regexp.MustCompile(SESSION_ID_REGEX)

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

func GetLogDirByNameID(ossHistorySeverDir, rayClusterNameID, rayNodeID, sessionId string) string {
	return fmt.Sprintf("%s/", path.Clean(path.Join(ossHistorySeverDir, rayClusterNameID, sessionId, RAY_SESSIONDIR_LOGDIR_NAME, rayNodeID)))
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

func GetSessionDir() (string, error) {
	for i := 0; i < 12; i++ {
		rp, err := os.Readlink(RaySessionLatestPath)
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
		nodeidBytes, err := os.ReadFile(RayNodeIDPath)
		if err != nil {
			logrus.Errorf("read nodeid file error %v", err)
			time.Sleep(time.Second * 5)
			continue
		}
		return strings.Trim(string(nodeidBytes), "\n"), nil
	}
	return "", fmt.Errorf("timeout --node_id= not found")
}

// ConvertBase64ToHex converts an ID to hex format.
// Handles both cases:
// 1. Already hex format - returns as-is
// 2. Base64-encoded - decodes to hex
// It tries RawURLEncoding first (Ray's default), falling back to StdEncoding if that fails.
func ConvertBase64ToHex(id string) (string, error) {
	// Check if already hex (only [0-9a-f])
	if matched, _ := regexp.MatchString("^[0-9a-fA-F]+$", id); matched {
		return id, nil
	}

	// Try base64 decode
	idBytes, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		// Try standard Base64 if URL-safe fails
		idBytes, err = base64.StdEncoding.DecodeString(id)
		if err != nil {
			return id, fmt.Errorf("failed to decode Base64 ID: %w", err)
		}
	}
	return fmt.Sprintf("%x", idBytes), nil
}

// IsHexNil returns true if hexStr decodes to a non-empty byte slice where every byte is 0xff.
func IsHexNil(hexStr string) (bool, error) {
	s := strings.TrimSpace(hexStr)

	if len(s) == 0 {
		return false, nil
	}

	// Hex string must have even length.
	if len(s)%2 != 0 {
		return false, hex.ErrLength
	}

	bytes, err := hex.DecodeString(s)
	if err != nil {
		return false, err
	}

	for _, v := range bytes {
		if v != 0xff {
			return false, nil
		}
	}
	return true, nil
}

// BuildClusterSessionKey constructs the key used to identify a specific cluster session.
// Format: "{clusterName}_{namespace}_{sessionName}"
// Example: "raycluster-historyserver_default_session_2026-01-11_19-38-40"
func BuildClusterSessionKey(clusterName, namespace, sessionName string) string {
	return clusterName + connector + namespace + connector + sessionName
}

// GetDateTimeFromSessionID will convert sessionID string i.e. `session_2026-01-27_10-52-59_373533_1` to time.Time
func GetDateTimeFromSessionID(sessionID string) (time.Time, error) {
	matches := regex.FindStringSubmatch(sessionID)

	if len(matches) < 4 {
		return time.Time{}, fmt.Errorf("Invalid session string format, expected `session_YYYY-MM-DD_HH-MM-SS_MICROSECOND` got: %s", sessionID)
	}

	timeStr := fmt.Sprintf("%s_%s.%s", matches[1], matches[2], matches[3])

	t, err := time.Parse(DATETIME_LAYOUT, timeStr)
	if err != nil {
		return time.Time{}, err
	}

	return t, nil
}
