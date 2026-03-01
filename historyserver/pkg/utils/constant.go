package utils

import "path"

const TmpRayRoot = "/tmp/ray"

var (
	RayPrevLogsPath        = path.Join(TmpRayRoot, "prev-logs")
	RayPersistCompletePath = path.Join(TmpRayRoot, "persist-complete-logs")
	RaySessionLatestPath   = path.Join(TmpRayRoot, "session_latest")
	RayNodeIDPath          = path.Join(TmpRayRoot, "raylet_node_id")
)
