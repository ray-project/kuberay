package utils

type SessionStatus string

const (
	SessionStatusInProgress SessionStatus = "in_progress"
	SessionStatusCompleted  SessionStatus = "completed"
	SessionStatusTerminated SessionStatus = "terminated"
	SessionStatusUnknown    SessionStatus = "unknown"
)

type MetaJson struct {
	SessionName      string        `json:"session_name"`
	ClusterID        string        `json:"cluster_id"`
	ClusterNamespace string        `json:"cluster_namespace"`
	StartTime        int64         `json:"start_time"`
	EndTime          int64         `json:"end_time"`
	Status           SessionStatus `json:"status"`
	// RayStatus captures the latest Ray resource status/condition at termination time.
	// Populated on a best-effort basis from the Ray dashboard during collector shutdown.
	RayStatus string `json:"ray_status,omitempty"`
}

type ClusterInfo struct {
	Name            string        `json:"name"`
	Namespace       string        `json:"namespace"`
	SessionName     string        `json:"sessionName"`
	CreateTime      string        `json:"createTime"`
	CreateTimeStamp int64         `json:"createTimeStamp"`
	Status          SessionStatus `json:"status,omitempty"`
	EndTime         int64         `json:"endTime,omitempty"`
}

type ClusterInfoList []ClusterInfo

func (a ClusterInfoList) Len() int           { return len(a) }
func (a ClusterInfoList) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ClusterInfoList) Less(i, j int) bool { return a[i].CreateTimeStamp > a[j].CreateTimeStamp } // Sort descending
