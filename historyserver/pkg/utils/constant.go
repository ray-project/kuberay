package utils

import (
	"os"
	"path/filepath"
)

const (
	defaultTmpRayRoot = "/tmp/ray"

	// Lowercase, normalized CRD kinds. Used both as the comparison key
	// for ToLower(OwnerKind) and as the cluster-metadata path subdir segment.
	RayJobKind     = "rayjob"
	RayServiceKind = "rayservice"
	RayClusterKind = "raycluster"
)

func GetTmpRayRoot() string {
	if tmpRoot := os.Getenv("RAY_TMP_ROOT"); tmpRoot != "" {
		return tmpRoot
	}
	return defaultTmpRayRoot
}

func GetRayPrevLogsPath() string {
	return filepath.Join(GetTmpRayRoot(), "prev-logs")
}

func GetRayPersistCompletePath() string {
	return filepath.Join(GetTmpRayRoot(), "persist-complete-logs")
}

func GetRaySessionLatestPath() string {
	return filepath.Join(GetTmpRayRoot(), "session_latest")
}

// GetRayRotatedStagingPath returns the directory where the collector stages rotated
// log segments it has pinned with hard links. It sits beside prev-logs rather than
// inside it so that the prev-logs walker, which uploads and then deletes whole node
// directories, never touches captures that are still draining.
func GetRayRotatedStagingPath() string {
	return filepath.Join(GetTmpRayRoot(), "rotated-staging")
}
