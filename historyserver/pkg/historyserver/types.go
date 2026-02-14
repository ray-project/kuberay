package historyserver

// TODO(jwj): Can be extracted to a task-specific interface, e.g., TaskSummaryProvider.
type TaskDataResult struct {
	Total                 int                      `json:"total"`
	NumAfterTruncation    int                      `json:"num_after_truncation"`
	NumFiltered           int                      `json:"num_filtered"`
	Result                []map[string]interface{} `json:"result"`
	PartialFailureWarning string                   `json:"partial_failure_warning"`
	Warnings              []string                 `json:"warnings"`
}

type TaskData struct {
	Result TaskDataResult `json:"result"`
}

type RespTasksInfo struct {
	Result bool     `json:"result"`
	Msg    string   `json:"msg"`
	Data   TaskData `json:"data"`
}

type ReplyActorInfo struct {
	Result bool          `json:"result"`
	Msg    string        `json:"msg"`
	Data   ActorInfoData `json:"data"`
}

type ActorInfoData struct {
	Detail map[string]interface{} `json:"detail"`
}

type ClusterStatusResponse struct {
	Result bool              `json:"result"`
	Msg    string            `json:"msg"`
	Data   ClusterStatusData `json:"data"`
}

type ClusterStatusData struct {
	AutoscalingStatus *string `json:"autoscalingStatus"`
	AutoscalingError  *string `json:"autoscalingError"`
	ClusterStatus     any     `json:"clusterStatus"` // TODO: update it once ray dashboard api /api/cluster_status supports autoscaler info
}

type FormattedClusterStatusResponse struct {
	Result bool                       `json:"result"`
	Msg    string                     `json:"msg"`
	Data   FormattedClusterStatusData `json:"data"`
}

type FormattedClusterStatusData struct {
	ClusterStatus string `json:"clusterStatus"`
}
