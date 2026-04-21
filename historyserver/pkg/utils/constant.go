package utils

import "os"

var (
	TmpRayRoot             = getTmpRayRoot()
	RayPrevLogsPath        = TmpRayRoot + "/prev-logs"
	RayPersistCompletePath = TmpRayRoot + "/persist-complete-logs"
	RaySessionLatestPath   = TmpRayRoot + "/session_latest"
	RayNodeIDPath          = TmpRayRoot + "/raylet_node_id"
)

func getTmpRayRoot() string {
	if tmpRoot := os.Getenv("RAY_TMP_ROOT"); tmpRoot != "" {
		return tmpRoot
	}
	return "/tmp/ray"
}
