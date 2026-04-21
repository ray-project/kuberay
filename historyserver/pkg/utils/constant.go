package utils

import (
	"os"
	"path/filepath"
)

const defaultTmpRayRoot = "/tmp/ray"

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

func GetRayNodeIDPath() string {
	return filepath.Join(GetTmpRayRoot(), "raylet_node_id")
}
