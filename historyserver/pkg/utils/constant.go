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

	RAY_AUTH_TOKEN_SECRET_KEY = "auth_token"

	// RayContainerIndex is the index of the Ray container in the head pod template.
	RayContainerIndex = 0
	// DashboardPortName is the name the ray-operator gives the dashboard port.
	DashboardPortName = "dashboard"
	// DefaultDashboardPort is used when the head container does not declare a dashboard port.
	DefaultDashboardPort = 8265
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
